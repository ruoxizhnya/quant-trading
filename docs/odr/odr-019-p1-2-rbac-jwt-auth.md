# ODR-019: P1-2/P1-8 RBAC + JWT auth + audit_logs

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (认证授权)
> **Related ADRs**: ADR-017 §2 (Auth), ADR-020 (Engine 拆解 — 旁路)
> **Supersedes**: —
> **Relates to**: ODR-013 (CQ-005/CQ-009 审计), TASKS §P1-2 / §P1-8, AR-004, BR-002

## Context

Sprint 6 完结后, 5 个微服务 (analysis/data/strategy/risk/execution/ai)
的所有端点都**没有任何认证**:
- 任何能访问 :8085 的人都能 `POST /api/backtest` 创建任务
- AI service 调 LLM 没有任何限流, 一次 click storm 可烧掉整个 token 预算
- 监管/合规审计无据可查 (零 audit log)

ODR-013 把这归为 P0 (CQ-005) + P1 (CQ-009) 两项, 估时 1w。
ADR-017 §2 已给出设计 (JWT + RBAC + bcrypt), 实施跟设计一致。

## Decision

按 "4 件套" 拆分:

| 组件 | 文件 | 职责 |
|------|------|------|
| **认证服务** | `pkg/auth/auth.go` (411 行) | `Service.CreateUser/Authenticate/IssueTokens/Refresh/RecordAudit/ListAudit/ListUsers` |
| **JWT Claims** | `pkg/auth/claims.go` (32 行) | `Claims` struct (Username/Role/Kind) + `UserIDInt64()` helper |
| **中间件** | `pkg/auth/middleware.go` (226 行) | `Middleware()` (Bearer 校验) + `RequireRole(roles...)` + `AuditMiddleware()` |
| **HTTP handlers** | `cmd/analysis/handlers_auth.go` (183 行) | `/api/auth/{login,refresh,me}` + `/api/auth/admin/{users,audit}` |
| **DB schema** | `migrations/019_add_auth_tables.sql` + `pkg/storage/postgres.go` | `users` + `audit_logs` 表 + 4 索引 |

### Role 矩阵 (RBAC)

| Role | 读 | 写回测 | 下单 | 管理用户 | 查 audit |
|------|----|-------|-----|---------|---------|
| `viewer` | ✅ | ❌ | ❌ | ❌ | ❌ |
| `trader` | ✅ | ✅ | ✅ | ❌ | ❌ |
| `admin` | ✅ | ✅ | ✅ | ✅ | ✅ |

### Disabled mode (向后兼容)

如果 `JWT_SECRET` 环境变量 + yaml `auth.jwt_secret` 都为空, `Service.Enabled()` 返 `false`,
`Middleware()` 自动变 no-op, 旧 dev / test 流程不破坏。生产环境必须显式配置
secret, 启动 banner 会打 `[auth] JWT enabled` 或 `[auth] running in open-access mode (dev only)`。

### 审计日志字段

```sql
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    role VARCHAR(16),
    ip INET,
    endpoint TEXT NOT NULL,
    method VARCHAR(8) NOT NULL,
    payload_hash VARCHAR(64),   -- SHA-256 of body, NOT the body itself
    trace_id VARCHAR(64),        -- X-Request-ID for OTel correlation
    status_code INT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**只存 SHA-256 hash, 不存原文** — PII / 凭证不会落 audit log。
retention 默认无限 (后续 P2 加 partition by month 滚动清理)。

## Consequences

### 正面

- **实盘前置**: 任何 mutating 端点 (POST/PUT/DELETE/PATCH) 现在都要求 Bearer token,
  无 token 直接 401
- **审计可追**: 每次 mutating 调用都留痕 (user/role/ip/endpoint/payload_hash/trace_id/status_code)
- **角色边界清晰**: 3 角色 × 5 能力矩阵覆盖 80% 场景, 后续要加 role 直接扩枚举
- **可降级**: dev 模式开箱即用, prod 一行 env (`JWT_SECRET=...`) 即可启用
- **零停机切换**: 已有的 GET 端点不被强制 auth, 不破坏现有前端

### 负面 / 取舍

- **未做 rate limit per user**: AI service 的 token bucket 限流留待 P1-14 (已完成
  `pkg/ai/ratelimit.go`, 但 analysis service 整体 per-user 限流留 P2)
- **L5 review 端点暂未加 reviewer 角色检查**: 当前任意登录用户可 review AI pipeline
  job, 需 P2 配合 P1-29 AlertManager 一起加 role
- **未加 refresh token revocation**: refresh token 在 7d 内一直有效, 即使用户被禁用
  也能用至到期。`Authenticate` 路径会查 `disabled` 标志, 但 bypass refresh
  的攻击窗口 (7d) 留待 P2 加 `token_revocations` 表
- **bootstrap admin 流程**: 没有内置 "首次启动创建 admin" 的脚手架, 需要
  手动 SQL INSERT 或 `POST /api/auth/admin/users` (但这本身又需要 admin token,
  形成 chicken-and-egg)。**解法**: 启动 banner 提示操作员, 第一次用 `psql`
  插 admin 行 (cost=12 bcrypt hash 在 `migrations/019` 注释里有示例)
- **未做 password reset 流程**: 用户忘记密码, 只能 admin 重置 (P2)
- **未做 CSRF**: 纯 API 服务, 浏览器 fetch 必须显式带 `Authorization` header
  才能调用 mutating 端点, 攻击者构造表单 POST 拿不到 token。但如果后续加 cookie
  session, 必须配套 CSRF token

## Artifacts

### 新增

- `pkg/auth/auth.go` (411 行)
- `pkg/auth/claims.go` (32 行)
- `pkg/auth/middleware.go` (226 行)
- `pkg/auth/auth_test.go` (10 TestXxx)
- `pkg/auth/middleware_test.go` (10 TestXxx)
- `cmd/analysis/handlers_auth.go` (183 行)
- `migrations/019_add_auth_tables.sql` (51 行)

### 修改

- `pkg/storage/postgres.go` (+ 27 行: users/audit_logs 创建 + 4 索引)
- `cmd/analysis/main.go` (+ 37 行: 集成 authSvc + 挂中间件 + registerRoutes 多 1 参数)
- `go.mod` / `go.sum` (+ golang-jwt/v5 v5.3.1, + golang.org/x/crypto bcrypt)

### 文档

- `docs/TASKS.md` — P1-2 / P1-8 状态 ⬜ → ✅
- `docs/ADR.md` — ODR Index 添加 ODR-019
- `docs/ARCHITECTURE.md` — DB schema 章节加 users/audit_logs (P3 todo)

## Metrics

- 新增 Go 代码: ~852 行 (auth 4 文件) + 183 行 (handlers_auth) = **1035 行**
- 新增测试用例: **20 TestXxx** (auth 10 + middleware 10, 全部 PASS)
- 测试时长: `pkg/auth` 0.6s
- `go vet ./pkg/auth/... ./cmd/analysis/...` exit 0
- `go build ./...` exit 0
- 端点数: 5 (login / refresh / me / admin/users / admin/audit)
- DB 表: 2 新 (users / audit_logs)
- 索引: 4 新 (audit × 3, users × 1 partial)
- 中间件链: 5 (recovery / cors / ratelimit / requestlog / auth / audit)

## Lessons Learned

1. **Disabled mode 设计救场**: 第一版我把 Middleware 写成"没 secret 就 panic",
   跑测试时炸穿——所有 dev 流程都过不了。改 `Service.Enabled()` 后, dev 模式
   无感, prod 一行 env 启用, **降低开发摩擦比"安全"更重要** (在 dev 阶段)
2. **审计只存 hash 不存 body**: 这是个"看起来麻烦"但**不**是 over-engineering
   的设计。如果存 body, GDPR / 监管来查 log 直接需要清表, 反而更难。SHA-256
   足够做"调用指纹比对" (e.g. 两个不同来源的请求是否同一 payload)
3. **公开路径白名单要 keep-in-sync**: 现阶段 5 个白名单路径, 写成 `[]string`
   在 `isPublicPath()`。后续增多需要改成 path prefix 匹配 + 数据库化 (P2)
4. **bootstrap admin chicken-and-egg**: 这个问题的解不是"写个 API 自动建
   第一个 admin", 而是"文档明示 SQL 模板 + 启动 banner 强提示"。任何"自动
   bootstrap"都会成为攻击面
5. **Middleware 顺序很关键**: `requestLogger` 必须**在** `auth` **之前**
   (否则 401 不会被 log), `audit` 必须**在** `auth` **之后** (否则 user_id
   永远是 nil)。`ratelimit` 位置无所谓。当前顺序:
   `recovery → cors → ratelimit → requestlog → auth → audit`
6. **bcrypt cost 12 = 250ms 一次**: 单次登录 ~250ms, 不算慢。但**高并发登录
   风暴**会拖慢 API。生产环境建议加 IP-based rate limit (5 req/min/IP),
   或者迁移到 argon2id (P2)。当前 disabled rate limit on /api/auth/login
   是已知短板
