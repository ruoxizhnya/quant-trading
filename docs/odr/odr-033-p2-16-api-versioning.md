# ODR-033: P2-16 API 版本化 — URL 重写 + Deprecation 头 + Discovery

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation (API Infrastructure)
> **Related ADRs**: ADR-019 (服务合并 in-process 注入)
> **Related ODRs**: ODR-021 (P1-15 服务合并), ODR-027 (P2-1/P2-2)
> **Supersedes**: —
> **Relates to**: TASKS §P2-16, BR-015 (API 演进能力)

## Context

quant-trading 当前 API 路径都是 unversioned (`/api/backtest`,
`/api/compliance/check` 等), 13 个 handler 文件分布在 `cmd/analysis` 下。
当未来需要发布 v2 API (例如新的 backtest engine、新的 compliance 规则)
时, 旧客户端调用 `/api/backtest` 会立即被打断, 强制升级。

行业标准做法 (Stripe / GitHub / Cloudflare / Kubernetes API) 是:
- v1 / v2 路径同时可用, 旧路径加 deprecation 提示
- Sunset 日期到达后强制重定向到新路径
- 提供 `/api/version` discovery endpoint 让客户端自动发现版本

P2-16 解决这个 gap: 提供 `APIVersionMiddleware` (URL 重写) +
`DeprecationHeader` (RFC 8594 响应头) + `DiscoveryHandler` 三件套, 在
`pkg/api` 包内统一处理, **不需要修改任何现有 handler 的 RegisterRoutes
方法** (避免 13 个 handler 文件改动)。

## Decision

### 1. 包结构: 新增 `pkg/api/versioning.go` (单文件, 3 个核心对象)

```
pkg/api/                                [新增]
├── versioning.go                       (P2-16) — 中间件 + Discovery [本 ODR]
├── versioning_test.go                  (25 TestXxx)
└── (未来可加: rate_limit.go, cors.go, auth.go)
```

**为什么新建 `pkg/api` 包**:
- 当前 API 基础设施 (CORS, rate-limit, auth, metrics) 散落在
  `cmd/analysis/main.go`, 不可复用。`pkg/api` 是 leaf package,
  把所有跨 handler 的中间件集中, 未来 P2-17+ (rate-limit, OIDC) 都能
  挂在这一个包下。
- `pkg/api` 不依赖任何业务包 (只依赖 `gin`), 反向不引入循环依赖。

### 2. `APIVersionMiddleware` — URL 重写 + re-dispatch

```go
func APIVersionMiddleware(engine *gin.Engine, cfg VersioningConfig) gin.HandlerFunc
```

**算法**:
1. **快速路径**: 路径不以 `/api/` 开头 → 跳过。
2. **canonical `/api/v1/...`**: 重写为 `/api/...`, 调用
   `engine.HandleContext(c)` 重新分发到原 handler。
3. **unversioned `/api/...`**: 检查 `cfg.LegacyRedirect` 或
   `cfg.SunsetDate` 已过期, 触发 301/308 重定向到 canonical
   `/api/<CurrentVersion>/<rest>`。

**实现关键**: gin 的 radix-tree 路由在 middleware chain 之前就匹配
了 handler, 所以 URL 重写后必须调用 `engine.HandleContext(c)` 重新
分发, 让 gin 重新走一遍路由树。

**为什么 middleware 需要 `*gin.Engine` 参数**: re-dispatch 通过
`engine.HandleContext(c)` 实现, 中间件必须能引用 engine。当前
API: `engine.Use(APIVersionMiddleware(engine, cfg))`。

**re-dispatch 跨 reset 边界**: `engine.HandleContext(c)` 内部调用
`c.reset()`, 会清空 `c.Keys`。所以版本信息必须放在 `c.Request.Header`
(`X-Original-API-Version`), request header 不会被 reset。

### 3. `DeprecationHeader` middleware — RFC 8594 合规

```go
func DeprecationHeader(cfg VersioningConfig) gin.HandlerFunc
```

**Headers 设置** (只在 unversioned `/api/...` 响应上):
- `Deprecation`: RFC 8594 / RFC 9745 标准, 形如 `"<RFC3339>"` 或
  `"true"`。
- `Sunset`: 同上, 形如 `"<RFC3339>"`。
- `Link`: `</api/v1>; rel="successor-version"` (RFC 8288 关系链接)。
- `X-API-Deprecated`: `true` (兼容不支持 RFC 8594 的旧客户端)。

**跳过条件** (防止误标):
- 路径不以 `/api/` 开头 (e.g. `/health`)
- 请求原本是 canonical (`X-Original-API-Version != "legacy"`)

### 4. `DiscoveryHandler` — 客户端自动发现

```go
func DiscoveryHandler(serviceName string, cfg VersioningConfig, endpoints []string) gin.HandlerFunc
```

**响应 JSON**:
```json
{
  "service": "quant-trading-analysis",
  "current_version": "v1",
  "supported_versions": ["v1"],
  "deprecated_since": "2026-01-01T00:00:00Z",
  "sunset_at": "2027-01-01T00:00:00Z",
  "latest_stable": "v1",
  "endpoints": ["GET /api/backtest", "POST /api/compliance/check"]
}
```

**挂载路径**: `cfg.DiscoveryPath` (默认空, caller 显式挂载)。
推荐路径: `/api/version` 或 `/.well-known/api-version`。

**客户端使用**: 启动时 GET 一次, 解析 `current_version` /
`sunset_at`, 决定是否升级 SDK; 定期轮询 (e.g. 1 小时) 应对服务端
主动降级。

### 5. `VersioningConfig` — 配置化

```go
type VersioningConfig struct {
    CurrentVersion  string      // e.g. "v1"
    LegacyRedirect  bool        // 严格模式: true = 301/308 强制重定向
    DeprecationDate *time.Time  // 旧路径的 deprecation 公告日
    SunsetDate      *time.Time  // 旧路径的 sunset 截止日
    DiscoveryPath   string      // discovery endpoint 路径
}
```

**viper 读取** (生产配置):
```yaml
api:
  current_version: v1
  legacy_redirect: false  # 软 deprecation, 不强制
  deprecation_date: 2026-01-01
  sunset_date: 2027-01-01
  discovery_path: /api/version
```

**两阶段迁移**:
- 阶段 1 (软 deprecation): `LegacyRedirect = false`, 旧路径继续
  工作但加 deprecation 头, 客户端慢慢迁移。
- 阶段 2 (强制重定向): `LegacyRedirect = true` 或 `SunsetDate` 已
  过期 → 301/308 强制重定向, 旧路径直接打回。

### 6. `CurrentAPIVersion` 工具函数

```go
func CurrentAPIVersion(c *gin.Context) string
```

handler 内部可调用此函数获取当前请求的 API 版本 (供审计日志使用),
无需关心 version 信息的存储细节 (内部用 request header 跨过
re-dispatch 边界)。

### 7. 测试覆盖 (25 TestXxx)

- 单元: `isVersioned` / `stripVersionPrefix` / `extractVersion` /
  `DefaultVersioningConfig`
- 中间件: canonical path 重写 / legacy path 不重定向 / 软 deprecation
  header / canonical path 无 deprecation / 严格模式 301/308 / 非
  `/api/` 路径不受影响 / SunsetDate 触发重定向 / future SunsetDate
  不重定向 / DeprecationDate header 格式 / 多种 current version (v1/v2)
- 中间件: POST + body 完整重写 / deep canonical path (`/api/v1/strategies/123/run`)
  / 不存在的 canonical 路径返回 404
- Discovery: 基础响应 / 含 SunsetAt + DeprecatedSince
- `CurrentAPIVersion`: 4 种 path 返回正确版本

## Consequences

### Positive

- **零侵入**: 不需要修改 13 个 handler 的 `RegisterRoutes` 方法,
  旧路径继续工作, 新路径 (`/api/v1/...`) 自动可用, 客户端无感迁移。
- **RFC 8594 / RFC 9745 合规**: Deprecation / Sunset / Link 三个
  header 全部按 IETF 标准, 旧客户端 (支持 RFC 8594) 自动识别
  deprecation, 新客户端基于 Link 头自动跳转。
- **两阶段迁移**: 软 deprecation → 严格重定向, 运维可以按
  `DeprecationDate` / `SunsetDate` 精确控制迁移节奏。
- **客户端自动发现**: Discovery endpoint 让 SDK 可以"启动时探测
  版本", 不用硬编码 `v1`。
- **可注入**: 25 个测试全部走 gin httptest, 50 goroutine race-clean,
  不依赖真实 DB / network。
- **未来 P2-17+ 基础设施挂载点**: `pkg/api` 是新建的 leaf package,
  rate-limit / OIDC / CORS 等横切关注点都可以挂在这里, 不需要
  每次新建包。

### Negative

- **re-dispatch 有性能开销**: 每次 `/api/v1/...` 请求会触发两次
  middleware chain (原始 + re-dispatch), 大约 +30% latency on
  /api/ 路径。当前没有压测数据, 留待 P2-16.1 跑 benchmark。
- **`c.Request.Header` 多了一个内部 header**: `X-Original-API-Version`
  会出现在所有 `/api/v1/...` 请求的 request header 中, 占用
  ~50 字节 / 请求。`request_id` 等通用 header 也会类似处理, 这是
  gin 中间件的常规 trade-off。
- **`engine` 参数必须显式传入**: `engine.Use(APIVersionMiddleware(engine, cfg))`
  比 `engine.Use(APIVersionMiddleware(cfg))` 多一个参数, 略丑。
  好处是 re-dispatch 显式可控, 团队 code review 时一目了然。
- **`VersionInfo.DeprecatedSince` 是 RFC3339 但 `Deprecation` header
  是 http.TimeFormat**: 两个格式不一致, 客户端解析时需要识别。
  遵循 RFC 8594 规定 (header 用 http.TimeFormat), 不能改。
- **没有处理 "v1 内部子版本" (e.g. `v1.2`)**: 当前只支持 `v<NUMBER>`
  整数版本, 不支持语义化版本 (`v1.2.0`)。如果未来要按 minor
  版本分发, 需要 P2-16.2 扩 isVersioned regex。
- **Discovery endpoint 默认空**: caller 必须显式挂载, 否则
  客户端无法自动发现。如果忘记挂载, 监控告警很难发现 (因为
  服务正常运行, 只是没接 discovery 而已)。P2-16.3 加 startup 健康
  检查 (如未挂载, 启动 log 警告)。

## Artifacts

### 新增文件

```
pkg/api/versioning.go                (325 lines, 3 对象 + 2 中间件 + 配置)
pkg/api/versioning_test.go           (469 lines, 25 TestXxx)
docs/odr/odr-033-p2-16-api-versioning.md
docs/TASKS.md                        (P2-16 状态 ⬜ → ✅)
docs/ADR.md                          (ODR Index 加 ODR-033 行)
```

### 净行数

+ 794 lines (实现 325 + 测试 469)。其中 ~ 59% 是测试代码, race-clean。

## Metrics

| Metric | 目标 | 实际 |
|--------|------|------|
| 零侵入 (13 handler 不改) | ✓ | ✓ (middleware + re-dispatch 透明) |
| RFC 8594 / RFC 9745 合规 | ✓ | ✓ (Deprecation / Sunset / Link) |
| Discovery endpoint | ✓ | ✓ (`/api/version` JSON) |
| 两阶段迁移 (软 → 严格) | ✓ | ✓ (LegacyRedirect + SunsetDate) |
| 中间件可注入 | ✓ | ✓ (viper 读 cfg) |
| 单元测试 | ≥ 20 | 25 |
| `go test -race` 通过 | ✓ | ✓ |
| `go vet ./...` 通过 | ✓ | ✓ |
| `go build ./...` 通过 | ✓ | ✓ |
| re-dispatch 性能开销 | < 50% | 未测 (P2-16.1 benchmark) |
| `pkg/api` leaf package 复用 | ✓ | ✓ (未来 P2-17+ 挂载点) |

## Lessons Learned

1. **gin radix-tree 路由在 middleware 之前就匹配了 handler** — URL
   重写后必须 `engine.HandleContext(c)` 重新分发, 这不是优化,
   是 gin 架构的 hard constraint。任何 URL rewrite 中间件都会
   踩这个坑, 应该写进内部 wiki。
2. **`c.Keys` 在 `HandleContext` 内部被 `c.reset()` 清空** — 版本
   信息必须放在 `c.Request.Header` (request header 不被 reset),
   不能用 `c.Set`。这是 25 个测试中 `TestCurrentAPIVersion` 第
   一版踩到的 bug, fix 后用 header 跨过 reset 边界。
3. **re-entry guard 防止 middleware 死循环** — APIVersionMiddleware
   在 re-dispatch 后会再次被调用 (因为 engine 重启 chain),
   必须在 middleware 入口检查 `X-Original-API-Version` header,
   已设置就跳过。否则无限递归。
4. **三件套 (URL rewrite + Deprecation header + Discovery) 是行业
   标配** — Stripe / GitHub / Cloudflare / Kubernetes API 全部
   走同款, RFC 8594 / 9745 / 8288 三个标准加起来不到 30 页,
   应该让团队都读一遍。
5. **测试 helper 模式** (`newTestRouter`) 优于每个测试自己拼
   engine + middleware — 25 个测试共用 1 个 helper, 修改
   middleware API 时只需要改 1 处, 测试代码几乎零修改。

## Follow-up Tasks (留待后续 sprint)

- **P2-16.1 性能 benchmark** — 测量 re-dispatch 的 latency 开销,
  当前预期 +30%, 真实数据需要跑 `go test -bench` 验证。
- **P2-16.2 语义化版本支持** — 当前只支持 `v<NUMBER>` 整数, 未来
  需要按 minor 版本分发 (e.g. `v1.2/foo`), 扩 `isVersioned` regex。
- **P2-16.3 Discovery 挂载健康检查** — startup 时检查
  `cfg.DiscoveryPath` 是否挂载, 未挂载则 log warn (不 panic,
  因为有些部署可能不需要 discovery)。
- **P2-16.4 Pkg/api 持续扩展** — 新建 `pkg/api` 是 leaf package,
  未来 rate-limit / OIDC / CORS / metrics 中间件都挂这里, 避免
  散落到 `cmd/analysis/main.go`。
- **P2-16.5 Deprecation 头在 v1 → v2 迁移时的客户端协调** — 当前
  软 deprecation, 未来 v2 发布时, 同时维护 v1 软 deprecation
  + v2 canonical, 6 个月后 v1 sunset, 1 年后 v1 强制 301。
- **P2-16.6 OpenAPI Schema 同步** — `endpoints []string` 当前是
  字符串列表, 未来需要 OpenAPI Schema 自动生成 (从代码注解),
  client SDK 自动根据 schema 生成。
