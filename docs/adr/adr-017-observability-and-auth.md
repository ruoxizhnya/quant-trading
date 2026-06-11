# ADR-017: Observability Stack + API Authentication (前置 Phase 4 验收)

**Date:** 2026-06-11
**Status:** Proposed
**Supersedes:** Future ADR-017 placeholder (`docs/ADR.md` §Future ADRs Phase 5)
**Advances to Phase 4**: 是（不再延后到 Phase 5）

## Context

[ODR-013 2026-06-11 综合审查](../odr/odr-013-comprehensive-audit-2026-06-11.md) 发现 2 项 CRITICAL 架构问题：

1. **AR-001 — 零分布式追踪/指标系统**：跨服务调用（analysis → risk/execution/strategy/ai）无 trace span 串联、无 latency histogram、无错误率指标。生产环境故障定位时间从分钟级恶化到小时级。
2. **AR-004 — API 零认证授权**：5 个微服务（含 AI service）的所有端点无 auth 中间件。任何能访问网络的人都能触发回测、创建订单。AI service 无 rate limit 导致 LLM 成本无上限。

原 ADR 索引 (`docs/ADR.md` §Future ADRs) 将 "API authentication and access control" 推到 Phase 5，但生产部署必须前置：

- 实盘交易端点绝不能公开
- AI service 调 LLM 需 token 预算控制
- 监管/合规要求审计日志

## Decision

### §1. 可观测性栈 — OpenTelemetry + Prometheus

**Adopt OpenTelemetry (OTel) + Prometheus + 集中日志 (Loki/Grafana stack)**：

| 维度 | 技术 | 实施 |
|---|---|---|
| **Tracing** | OpenTelemetry SDK + OTLP | `cmd/analysis/main.go` 加 `otelgin` middleware；`pkg/ai/client.go` LLM 调用包 span；`pkg/backtest/engine.go` 关键节点 span |
| **Metrics** | Prometheus client_golang | 暴露 `/metrics` 端点；`backtest_duration_seconds{strategy,universe}`、`http_client_requests_total{service,status}`、`llm_tokens_total{provider,model}`、`cache_hit_ratio` 4 类核心指标 |
| **Logging** | zerolog + otel correlation | 现有 zerolog 升级；trace_id 通过 `X-Request-ID` 跨服务透传 |
| **Visualization** | Grafana (可选 Phase 5) | 暂不强制实施 |

**request_id 透传机制**：
```
Browser → /api/backtest
  → cmd/analysis/middleware: 注入 X-Request-ID (uuid) + ctx
  → pkg/backtest/engine.go: 跨函数 context.Value("request_id")
  → pkg/ai/client.go: HTTP 调用携带 X-Request-ID
  → cmd/ai/main.go: 记录 server-side 日志
```

### §2. API 鉴权 — JWT + RBAC + 审计日志

**Adopt JWT-based auth + RBAC + audit log**：

| 组件 | 实施 |
|---|---|
| **Auth middleware** | `gin-contrib/jwt` (HS256) 验证 token；过期/无效返 401 |
| **RBAC 策略** | `viewer` (read-only) / `trader` (下单单只) / `admin` (全权) 三角色 |
| **Token 颁发** | `POST /api/auth/login` (用户名+密码 bcrypt 比对) → 返回 access_token (15min) + refresh_token (7d) |
| **用户存储** | 新表 `users(id, username, password_hash, role, created_at)` + `audit_logs(id, user_id, role, ip, endpoint, method, payload_hash, timestamp)` |
| **Mutating 端点** | `POST/PUT/DELETE` 必须 auth；`GET` 暂不强制（public dashboard） |
| **AI service** | 加 token bucket 限流：`golang.org/x/time/rate` 10 req/min/user |
| **凭据安全** | `pgcrypto` 加密 DB 密码；`SOPS` 加密 yaml 密钥；docker-compose 启动校验 `DB_PASSWORD != "postgres"` 否则 fail-fast |

### §3. 审计日志

`audit_logs` 表记录所有 mutating API 调用，retention 90 天（个人）/ 365 天（机构，可配置）：

```sql
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id INT NOT NULL,
    role VARCHAR(16) NOT NULL,
    ip INET NOT NULL,
    endpoint TEXT NOT NULL,
    method VARCHAR(8) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,  -- SHA-256 of body
    trace_id VARCHAR(64),
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_user_time ON audit_logs(user_id, timestamp DESC);
CREATE INDEX idx_audit_endpoint ON audit_logs(endpoint, timestamp DESC);
```

## Options Considered

**Option A — 不引入额外栈，使用现有 zerolog + 自建 metrics**
- ❌ 拒绝：标准化的 trace 跨语言/跨服务传播是硬需求，自建成本高于引入 OTel

**Option B — 仅在 production 启用，开发环境禁用**
- ❌ 拒绝：开发期就需要 trace 帮助调试；通过 env 控制 sampling rate 即可

**Option C — Phase 5 才实施（按原计划）**
- ❌ 拒绝：实盘前置条件；AI service 限流无法延后

## Consequences

### Positive
- 故障定位从小时级降到分钟级
- LLM 成本可观测、可限流
- 满足未来 A 股实盘合规审计要求
- 为机构用户 Phase 5 onboarding 铺路

### Negative
- 新增依赖：`go.opentelemetry.io/otel`、`prometheus/client_golang`、`golang-jwt/jwt/v5`
- users/audit_logs 表 + bcrypt + JWT 三套基础设施
- 性能开销：trace sampling 1% 时 <1% latency；100% 时 3-5% latency（可接受）
- 开发期 login 流程增加摩擦

### Migration
- 现有 GET 端点无感；POST 端点首次调用需携带 token
- 老客户端需走 `/api/auth/login` 获取 token
- 6 个月双轨期：保留 legacy /v0 路由 + 401 引导客户端升级

## Implementation Roadmap

| Sprint | Tasks (见 [TASKS.md §Sprint 6](../../TASKS.md)) | Effort |
|---|---|---|
| **Sprint 6 P0-3** (2 days) | OTel SDK + middleware + /metrics + request_id 透传 | Critical |
| **Sprint 6 P0-1** (1 day) | AI service token bucket 限流 (10 req/min/user) | Critical |
| **Sprint 6 P1-8** (1 week) | users/audit_logs 表 + JWT middleware + bcrypt + login endpoint | Major |
| **Sprint 7 P2-X** (optional) | Prometheus + Grafana 部署 + 告警规则 | Operational |

## Related

- [ODR-013 AR-001/AR-004/AR-016 findings](../odr/odr-013-comprehensive-audit-2026-06-11.md)
- [ADR-008 §Regime Path Decision](adr-008-inter-service-comm.md)（服务合并后 middleware 复用）
- Original Future ADR placeholder: `docs/ADR.md` §Future ADRs
