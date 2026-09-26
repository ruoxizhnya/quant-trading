# ADR-026: 首个管理员自助引导 + SPA 鉴权（让「配了 JWT_SECRET」不再等于关掉 UI）

> **Status**: Accepted
> **Date**: 2026-09-25
> **Category**: Architecture / Security / Frontend
> **Related**: [ADR-017](adr-017-observability-and-auth.md)（API 鉴权 / RBAC / 审计）· [ADR-025](adr-025-auth-exposure-publish-layer.md)（open-access 的发布层判据）· [TASKS.md](../TASKS.md)（AUD-52 的收尾项）· [guides/local-dev.md](../guides/local-dev.md)
> **Upstream**: 用户（若曦）2026-09-25 决策（「一次性 HTTP 引导（推荐）」+「去做让前端 UI 可用的项目」）

---

## Context

ADR-025 关掉了 AUD-52 的一半矛盾（本地 compose 默认 open-access + 端口全回环），
但它的**反面**原样留着，而且在文档里被明确登记为"未解决"：

> 「局域网/手机访问」必须配 `JWT_SECRET`，而前端没有登录页 ⇒ 对外访问目前等于 UI 不可用。
> 要真支持需单独立项（首个管理员引导 + 前端登录）。

三件事共同造成这个结果：

1. **前端完全没有鉴权设施**。没有登录路由、没有 auth store、`web/src/api/client.ts`
   的 `fetch` 不带头（`Content-Type` 之外一个字段都没有）。`401: '未授权，请重新登录'`
   只存在于 `STATUS_MESSAGES` 里，**没有任何 401 拦截**。
2. **系统没有首个管理员引导**。`CreateUser` 挂在 `RequireRole(admin)` 之后
   （`cmd/analysis/handlers_auth.go:35`）—— 鸡生蛋：要建管理员得先是管理员。
3. **鉴权中间件挂在 router 上、先于路由匹配**（`pkg/auth/middleware.go:221`），
   公开白名单只有 `/api/auth/login`、`/api/auth/refresh`、`/health`、`/api/health`、
   `/metrics`。所以一旦开启鉴权，**每一个** `/api/*` 都是 401，包括不存在的路径。

于是"开鉴权"这个选项在当前形态下只有一个后果：**前端一行数据都拿不到**。
实测（2026-09-25，容器栈 + 经 nginx）：

```
GET :8080/api/strategies   → 401      ← 前端自己
GET :8080/api/stocks/count → 401
```

同时既有的 160 条 Playwright 用例全部假定 open-access（`rbac-open-access.spec.ts`
开篇即写 "The e2e environment runs with auth DISABLED"）。**"让前端 UI 可用"
不是给登录页化妆，而是要把这条链路真正打通。**

### 一个必须先承认的取舍

「首个管理员」这件事**没有可以事后补救的权限模型**：谁先到谁就能拿到 admin。
这不是实现缺陷，而是自助引导的固有性质 —— 任何不依赖外部预置凭据的方案
（否则又回到"要先用命令行/环境变量塞一个账号"）都必须是这个形状。所以本
ADR 的目标不是消除它，而是**让它可被约束、可被发现、可被审计**：

- 窗口只在 `users` 表为空时打开，第一次成功后**永久关闭**；
- 本地 compose 默认不发布到回环之外（ADR-025），对外暴露前应先完成引导；
- 这条唯一未鉴权的写路径**会留下 audit_logs 行**（实测见下）。

---

## Decision

### 1. 后端：一次性 HTTP 引导 `POST /api/auth/bootstrap`

- **公开**（加入 `publicPaths` 白名单）。
- 授权条件不是凭据而是**状态**：`auth.Enabled() && COUNT(users) == 0`。
  空表本身就是门。
- `pkg/auth.Service.CreateFirstAdmin` 在**一个事务**里做「计数 → 插入」，
  并用 `pg_advisory_xact_lock(7215001)` 把这一段变成临界区。
  没有这把锁时，READ COMMITTED 下两个并发请求会各自看到空表、各自插入 ——
  **两个首个管理员**。
- `ON CONFLICT (username) DO NOTHING RETURNING id` 兜住"锁之外还有别的写入者"
  的边界：拿不到返回行也一律判 `ErrBootstrapClosed`。
- 成功后**直接签发 token**（自动登录）。让调用方再走一次 `/login` 不增加任何
  安全性（它刚刚证明了自己能到达一个无人认领的实例），只多一个卡住的可能
  （比如用户名打错）。
- bcrypt 放在**取锁之前**：cost 12 约 250ms，临界区只该覆盖"查数 + 插行"。

### 2. 后端：公开的 `GET /api/auth/status`

返回两个布尔：`auth_enabled`、`bootstrap_required`。

**必须公开**，而且是**功能上的必须**，不是便利：SPA 要在**持有任何凭据之前**
决定渲染「登录表单」还是「创建首个管理员表单」，而这个决定本身需要问后端。
任何把它放在 token 之后的设计，在全新实例上都进不去。

它泄露的信息是外部本来就可观测的：open-access 的实例对任何人、任何 `/api/*`
都回 200；尚未引导的实例按定义就是没人用过的那个。

`auth_enabled=false` 时**不碰数据库**直接返回（`BootstrapRequired` 的短路），
这样"没配 JWT_SECRET 就不需要数据库也能跑起来"成立 —— 这是本地/CI 形态的
承重属性，不是优化。

### 3. 前端：一个出口、一个守卫、一个页面

- **一个出口**：`web/src/api/client.ts` 是全前端唯一的 HTTP 出口（两处 `fetch`：
  `request()` 与 `download()`）。token 注入在这里做一次，两处都做。
  头顺序是 `{ Content-Type, ...authHeader(), ...init.headers }` —— 显式的
  `init.headers` **永远压过**自动注入，因为 `api/execution.ts` 的
  emergency-flatten 带的是**交易用的 emergency token，不是 JWT**。
- **没有 token 时不带头**。open-access 下请求与加鉴权之前逐字节相同 ——
  这条不变量由 `client.auth.test.ts` 的第一条测试守着。
- **401 → 换令牌 → 重放**，且结局分**三态**：`refreshed` / `rejected` /
  `unavailable`。把"服务端拒绝了会话"（401/403）和"我们根本没问出口"
  （网络失败、刷新端点 5xx）分开。用一个布尔合并两者，一次网络抖动就会被读成
  "会话过期"→ 用户被登出 —— 这是实现过程中真实踩到并修掉的一个 bug。
- **并发去重**：`refreshInFlight` 保证并发的多个 401 只换一次令牌。
  refresh 是**轮换**语义（返回新的 refresh token），并发换两次会让后到的那次
  带着已作废的 token，用户被"随机"登出。
- **凭据存 localStorage**（`web/src/api/authToken.ts`，普通模块而非 store：
  client 不能依赖 pinia，否则 `client.test.ts` 这类不装 pinia 的单测会全挂，
  而且会形成 `stores/auth → api/auth → api/client → stores/auth` 的环）。
- **一个守卫**（`web/src/router/authGuard.ts`，单独成文件以便单测）：
  ① `isOpenAccess` → 放行（**必须先判**，它保证默认形态行为不变）；
  ② `meta.public` → 放行（否则守卫把自己重定向到自己）；③ 否则没凭据去 `/login`
  并带上 `redirect`。
- **一个页面**（`web/src/pages/Login.vue`，在 `AppLayout` **之外**）：同一个页面
  承担「登录」与「创建首个管理员」两态，因为对用户来说是同一个意图。
  `redirect` 只接受站内绝对路径（挡开放重定向）。
- **顶栏**在 `isAuthenticated` 时多出身份与登出；否则**一个节点都不多渲染**
  （默认形态的视觉回归与其余 UI 用例跑的就是那个形态）。
- **登出是纯本地的**：后端没有会话表也没有吊销端点，access/refresh 都是无状态
  JWT ⇒ 无法让已泄漏的 token 失效。这是已知边界。

### 4. 护栏：白名单与路由表必须机器对齐

`publicPaths` 是路由表的**手抄镜像**。漏抄一行 = 那个端点永远 401，且**只在
开启鉴权时**发作。所以加了两条：

- `cmd/analysis/auth_public_paths_test.go` **AST 扫** `registerAuthRoutes` 的
  公开组，逐条断言在名单里（反向也查）；扫描结果为空则直接失败（防"永真通过"）。
- 同一文件的行为腿：无 token 打 `POST /api/auth/bootstrap`（空 body ⇒ 400 而非
  401，不碰数据库）＋ 反证腿 `/api/auth/me` 必须 401。
- 契约腿：`in cmd/analysis` 走真 router + 真库，钉住 `/api/auth/status` 驱动
  SPA 分支的那两个布尔。

### 5. 探针必须**答得出来**：限流豁免 + 「没问到 ≠ 未登录」

这一条是**落地之后跑出来的**，不是设计时就想到的（2026-09-25 全量 playwright
回归读出 127 条红）。**两处都要改，只改一处不够**：

**① 后端：`/api/auth/status` 加入限流豁免名单。**
`rate_limit.per_minute: 100`（`config/analysis-service.yaml`）此前只豁免
`/health` 与 `/api/health`。实测连打 130 次 → **200×93 / 429×37**。
入选判据不是「这个端点重不重要」，而是一条更窄的：
**限流它，会不会让一个正常客户端分不清 429 与 401？**
一个便宜的、无副作用的、本来就公开的探针被限流，收益远小于代价 —— 1 个 429
毁掉的是**整站**（前端 fail closed 把它读成未登录），而不是毁掉一个组件。
`/api/auth/login` 与 `/api/auth/refresh` **必须继续限流**（口令爆破的第一道
闸），所以名单是**逐字相等**匹配、不做前缀匹配，加一条必须是有意识的动作。

**② 前端：`probe()` 必须区分「后端说了」与「没问到」。**
新增姿势 `unavailable`（见 `AuthPosture`），与 `anonymous` 分开：
`anonymous` = 后端**说了**要凭据、而我们没有；`unavailable` = 后端**没说话**。
把后者当前者，就是**凭空造出「你已登出」这个事实** —— 一次限流或一次网络抖动
会把已登录的用户弹到登录页。`unavailable` 属于**可再探**状态：守卫每次跳转都
`await probe()`，后端一恢复，下一次跳转就把会话接回来，用户不必手动刷新。

**仍然是 fail closed**：`unavailable` 下受保护页面照样被拦（`authEnabled` 为
true）。改的只是**界面说什么** —— `Login.vue` 在 `unavailable` 下不渲染凭据
表单，而是说明「连不上」并给一个重试。摆一个按下去也不会成功的表单，是把未知
伪装成已知。

护栏 `cmd/analysis/rate_limit_exempt_test.go` 三腿缺一不可：**行为腿**必须带
**对照腿**（否则限流器整个没接线时，豁免那几条照样全绿 —— 永真是假护栏最
常见的形状）、**窄度腿**（`/api/auth/login` 仍须被限流）、**名单腿**（任何增删
都必须是有意识的动作）。

---

## Consequences

### 正面

- 「配 `JWT_SECRET` 开鉴权」从一个**等于关掉 UI** 的选项，变成一条走得通的路径：
  `POST /api/auth/bootstrap` → 拿到 token → SPA 正常加载。
- 本地/CI 的 open-access 形态**行为零变化**：不加头、不跳转、不多渲染节点。
  前端单测 197 条、Playwright 回归、Go 全仓测试都在这个形态下跑。
- 未鉴权的唯一写路径**可被发现**：`auth.AuditMiddleware` 挂在 router 上，
  `POST /api/auth/bootstrap` 是 mutating 方法 ⇒ 每次调用留一行 audit_logs。
  实测（2026-09-25）：

  ```
  endpoint             | method | status_code | user_id
  /api/auth/bootstrap  | POST   |         201 |
  ```

  `user_id` 为空正是"这次调用还没有身份"的诚实记录。

### 负面 / 已知边界（不打算在本 ADR 里修）

| 边界 | 说明 |
|---|---|
| 先到先得 | 谁先到达实例谁就能拿到 admin。固有性质；靠回环发布 + 引导后即永久关闭约束 |
| 无法吊销 | 没有会话表 ⇒ 登出只是丢本地凭据，泄漏的 token 在 TTL 内仍然有效 |
| token 落 localStorage | XSS 可读。对本地/局域网工具可接受；上公网应先换 httpOnly cookie + CSRF |
| 鉴权形态下跑不了整套 playwright | 其余用例都假定"打开就是控制台"。`auth-login-flow.spec.ts` 是唯一两种形态都成立的，鉴权形态下只跑它 |
| `/api/auth/status` 在库不可达时 500 | 故意的：SPA 按 fail closed 处理。**但"处理"不等于"跳登录页"** —— 见 §5，`unavailable` 下界面说的是「连不上 + 重试」，不是摆一个凭据表单 |
| 限流豁免名单是手写的 | 与 `publicPaths` 同性质：逐字相等匹配 + `rate_limit_exempt_test.go` 三腿护栏。新加端点仍需有意识动作 |

### 被否决的方案

| 方案 | 为什么不 |
|---|---|
| 环境变量预置管理员（`ADMIN_USER`/`ADMIN_PASSWORD`） | 把"第一个账号"变成部署步骤而非产品能力；密码进 compose/.env，轮换困难；且仍然无法解释"没有 env 时的行为" |
| CLI 子命令 `./analysis create-admin` | 要求操作者能进容器/宿主 shell，而本项目的主要使用形态恰恰是"容器起好了，用浏览器" |
| 由 `JWT_SECRET` 派生一个初始 token | 把秘密派生值当凭据用；轮换密钥就轮换凭据；且无法撤销 |
| 首次登录时自动建 admin（谁先登谁当管理员） | 与"空表即门"等价但更隐蔽：没有任何显式动作可被审计，也没有"窗口已关"的明确信号 |
| `/api/auth/status` 放在 token 之后 | 全新实例上不可达 ⇒ 这个 ADR 要解决的问题原样存在 |
| 未配置 `JWT_SECRET` 时也返回真实状态 | `BootstrapRequired` 会去查库 ⇒ 破坏"没配密钥就不需要数据库" |

---

## 验证（2026-09-25）

**后端**（真库 `quant_trading`，`pkg/auth/bootstrap_test.go`）

- 首次 `CreateFirstAdmin` 成功且 role=admin、密码非明文；
- 第二次 / 第三次一律 `ErrBootstrapClosed`，`users` 行数仍为 1；
- **反证腿**：删空 `users` 后窗口重新打开（证明门看的是表，不是缓存）；
- **确定性锁探针**：另一会话先持有 `bootstrapLockKey` ⇒ `CreateFirstAdmin`
  500ms 内不返回、放开后成功（去掉 `pg_advisory_xact_lock` 该测试立刻变红）；
- `auth` 关闭时 `BootstrapRequired` / `CreateFirstAdmin` 在 **nil pool** 上工作
  不 panic —— 反证腿内建于夹具：碰库就 panic。

**前端**（`vitest` **208 条 / 18 个文件**，其中鉴权相关 **43** 条：
`api/client.auth.test.ts` 11 / `stores/auth.test.ts` 18 / `router/authGuard.test.ts` 9 /
`pages/Login.test.ts` 5）

**破坏验证**（改坏 → 确认变红 → 还原，Go 13 条 + 前端 10 条 + 本轮 4 + 5 条，全部通过）

§5 的破坏验证（2026-09-25，两套 harness 都带 pre-flight 对照与逐字节还原核对）：

| 改坏点 | 期望变红的腿 | 结果 |
|---|---|---|
| 豁免名单删掉 `/api/auth/status` | `auth/status` 行为腿 + 名单腿 | ✓ |
| 把 `/api/auth/login` 加进豁免名单 | login 窄度腿 + 名单腿 | ✓ |
| 中间件回退成修复前的内联判断 | **只有**行为腿红（名单腿不红）| ✓ 证明行为腿读的是真中间件，不只是那张表 |
| 逐字相等改成前缀匹配 | 精确匹配腿 | ✓ |
| `probe()` 把 `unavailable` 写回 `anonymous` | 429 / 500 / 可再探 / 守卫自愈 4 条 | ✓ |
| 去掉「`unavailable` 可再探」 | store 可再探 + 守卫自愈 2 条 | ✓ |
| `/me` 的非 401 失败也写 `anonymous` | `/me` 网络失败腿 | ✓ |
| `Login.vue` 的 `isUnavailable` 恒 false | 离线面板 3 条 | ✓ |
| `Login.vue` 表单不再受 `isUnavailable` 约束 | 离线面板腿 | ✓ |

**端到端**（真 nginx + 真后端 + 真 PG，`e2e/tests/auth-login-flow.spec.ts`）

| 形态 | 结果 |
|---|---|
| open-access | 用例 1 通过（`/` 直接进控制台、登录页 0 处），用例 2/3 按设计 skip |
| 开启鉴权 + 空库 | 未登录访问 `/screener` → `/login?redirect=/screener`；填表创建管理员 → 回到 `/screener`、顶栏出现身份、登出后再访问仍被拦回 |
| 开启鉴权 + 已有管理员 | 用例 2 通过；用例 3 走登录分支通过（`E2E_AUTH_USER/PASSWORD`） |

**§5 的端到端取证**（A/B 交替，同一个命令）：

- 刚打满 130 次探针（额度耗尽）立刻跑 `auth-login-flow.spec.ts` → **红**：
  `beforeAll` 探 `/api/auth/status` 拿到非 2xx（429）；
- `sleep 65` 让窗口滑过后跑**同一条命令** → **1 passed / 2 skipped**，
  且「open-access 形态：`/` 直接进控制台，全程不出现登录页」**通过**。

⇒ 坐实了「UI 本身是好的，是限流把它推去登录页」，而不是 SPA 出了回归。

清理：引导出来的 `e2e-bootstrap-admin` 与其 audit_logs 行已删除，库回到空表。
