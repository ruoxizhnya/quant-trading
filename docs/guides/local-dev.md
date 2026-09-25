---
status: evergreen
type: how-to
last-verified: 2026-09-25
verified-by: 实际命令逐条复核（`find -name '*_test.go'` 实测 273、`.gitignore` 已无 `*_test.go` 规则、
  `ls cmd/` 只有 analysis/data/strategy、`docker compose` 实测报 `'compose' is not a docker command`
  而 `docker-compose` v2.32.1 可用、`C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe` 实测存在且能离线编译全仓）＋
  AUD-51 自灌自证改造后同步「已知陷阱」＋ ADR-026 / AUD-57 落地后同步鉴权与限流两节
  （`*_test.go` 实测 **275**、前端 `vitest` 实测 **208 条 / 18 文件**、探针连打 130 次实测 `200×130 / 429×0`）
---

# 本地开发指南（How-to）

> 只写**当前确实可用**的步骤。遇到与本文不符的情况，以代码为准并回来更新本文。

---

## 环境要求

| 项 | 版本 | 来源 |
|---|---|---|
| Go | **1.25** | `go.mod` |
| PostgreSQL | **17.5**（宿主机原生） | `~/.workbuddy/binaries/postgres/` |
| Redis | **7.4.11**（宿主机原生） | `~/.workbuddy/binaries/redis-7.4.11/` |
| Node | 22（仅前端） | — |
| Python | 3.12（仅文档校验脚本） | `tools/check_doc_links.py` |

> **数据库与缓存是宿主机原生安装，不是容器**（2026-09-25 定案，见
> `docker-compose.yml` 文件头）。原因：Docker Desktop 在本机受沙箱限制
> （启动时拉起的 `wsl.exe` 在程序黑名单里，且起来后还会自发退出），不该把
> 最该稳的一层挂在最不稳的一层上。**服务仍然全部跑在容器里。**

> ⚠️ **Go 不在 PATH**：本机装在 `C:\Users\ruoxi\sdk\go1.25.0`。跑任何 Go 命令前先：
> ```bash
> export PATH="/c/Users/ruoxi/sdk/go1.25.0/bin:$PATH"
> ```

---

## 起基础设施

```bash
cp .env.example .env          # 一次性；默认已是本地 open-access，见下方
tools/local-stack.sh start    # ★ 一键：原生 PG/Redis → 容器服务 → 等健康 → 端口体检
```

`tools/local-stack.sh {start|stop|status}` 是**整栈**的统一入口，负责顺序、
就绪等待与体检。它做的事，以及为什么不能只看 `docker-compose up` 的退出码：

1. 探 Docker 引擎**是否真的活着**（Docker Desktop 在本机**会自发退出**，
   此后所有 docker 命令都报 `open //./pipe/dockerDesktopLinuxEngine: ...`，
   读起来像路径问题而不是「引擎没了」）；
2. `tools/local-infra.sh start`（原生 PostgreSQL 17.5 + Redis 7.4.11）；
3. `docker-compose up -d`；
4. **等健康** —— 判据是 **HTTP 探针全通**，不是 `up` 返回 0。`up` 只代表容器被
   创建了：应用连不上 PG/Redis 是 `logger.Fatal()` 无重试（容器立刻退出），
   健康检查还有 `start_period`；
5. **宿主机端口体检** —— 读真实 `netstat`，断言 8080/8081/8082/8085 只绑回环
   （AUD-13 运行时那一半，见 ADR-025）。

`tools/local-infra.sh {start|stop|status}` 只管**原生**那半（PG + Redis），
`status` 会断言 AUD-13（数据库/缓存只监听回环）—— 这条断言原先在
`tools/check_deploy_consistency.py` 里守 compose 的端口映射，服务移出 compose 后
挪到了这里，改为断言真实的 netstat 监听 socket。

> ⚠️ **`local-infra.sh start` 在 Agent 沙箱里跑会被回收**：沙箱按进程组清理整棵
> 进程树，`nohup ... &` 起的进程随那次工具调用一起消失（实测 `pg_ctl start` /
> `nohup &` / `Start-Process` 三者在这个环境下都无效）。那种情况下要改用
> 「后台任务」姿势直接 exec 两个二进制。**在你自己开的终端里正常。**

> ⚠️ **必须先有 `.env`**。2026-09-25 起 `JWT_SECRET` **不再必填**（本地默认
> open-access），但 `AUTH_INSECURE` / `AUTH_INSECURE_EXPOSURE` / `DATABASE_PASSWORD`
> 仍从它读。`cp .env.example .env` 得到的默认值就是可直接跑的本机配置。

> ⚠️ **必须用带连字符的 `docker-compose`**。本机 Docker CLI 没装 `compose`
> 插件，空格写法会报 `'compose' is not a docker command`；`docker-compose` v2.32.1 可用。

连接串：`postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable`
（见 `pkg/storage/postgres_test.go` 的 `testStore()`）。库表会在首次连接时自动建
（`CREATE TABLE IF NOT EXISTS`，在 `pkg/storage/postgres.go`）。

**新起的库是空的**——依赖预置行情数据的测试会自动 skip（见下文"已知陷阱"）。

---

## 本地复现 CI

`.github/workflows/ci.yml` 里的四步，本地可以这样跑：

```bash
export PATH="/c/Users/ruoxi/sdk/go1.25.0/bin:$PATH"
go build ./...
go vet ./...
go test ./... -count=1
python tools/check_doc_links.py
```

`go test` 需要 Postgres 在跑，否则存储层测试会静默 skip（**跳过不是通过**，
PIT 回归测试就跑不到了）。

`docker-compose.yml` 中的服务：`data-service` / `strategy-service` / `analysis-service` / `web`。
**数据库与缓存不在 compose 里** —— 它们是宿主机原生安装（见上）。

---

## 跑服务（本地）

推荐直接用 `tools/local-stack.sh start`（原生库 + 容器服务 + 健康校验一条龙）。

要用 `go run` 直接跑（改代码时热迭代更快）则是**原生跑法**，走豁免 (a)：

```bash
# analysis：open-access 本地模式 —— AUTH_INSECURE=true + server.host=127.0.0.1
# （config/analysis-service.yaml 的 server.host 是 0.0.0.0，所以必须覆盖它）
AUTH_INSECURE=true SERVER_HOST=127.0.0.1 go run ./cmd/analysis   # :8085 主服务（含 risk/execution，已合并）

go run ./cmd/data        # :8081
go run ./cmd/strategy    # :8082
```

> 若一定要带鉴权跑：`JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/analysis`
> —— **前端能用了**（浏览器里创建首个管理员即可），但整套 playwright 用不了，
> 见下文「两种启动姿势」① 的说明。

> `cmd/ai`（:8086）已于 2026-09-18 **删除**（TASKS P2-5）：零调用方，且建在废弃交互层的
> 定位上。AI 能力走 `cmd/analysis` 的 MCP 工具层，不再是独立服务。

### analysis 服务的两种启动姿势

**① 带鉴权（任何非本机部署都必须）**

```bash
export JWT_SECRET=$(openssl rand -hex 32)
```

生效后 `/api/*` 需要 `Authorization: Bearer <access_token>`，
token 从 `POST /api/auth/login` 拿。不设就直接 `auth: JWT secret missing` 退出。

**首个管理员怎么来**（ADR-026）：不用命令行、不用环境变量，在浏览器里建 ——

1. SPA 启动时问 `GET /api/auth/status`（公开），拿到
   `{auth_enabled: true, bootstrap_required: ?}`；
2. `bootstrap_required=true` ⟺ `users` 表为空 ⇒ 登录页显示「创建首个管理员」；
3. 提交 `POST /api/auth/bootstrap`（公开，**只在空表时可用**）⇒ 直接签发 token
   并进入控制台。**窗口此后永久关闭**，同一个端点一律 403；
4. 之后新建账号只走 `POST /api/auth/admin/users`（admin 权限）。

> ⚠️ 这条路径的授权条件是**状态**而不是凭据：谁先到达实例谁就能拿到 admin。
> 本地 compose 默认只发布到回环（ADR-025），所以先引导、再把实例暴露出去。
> 每次 bootstrap 尝试都会在 `audit_logs` 留一行（`endpoint=/api/auth/bootstrap`）。

> ⚠️ 这个姿势要求 `users` 表**可读**：库连不上时 `/api/auth/status` 返回 500，
> 前端按 fail closed 处理（当成需要登录）。open-access 形态没有这个依赖 ——
> 没配密钥时它连库都不碰。

> ⚠️ **开了鉴权就别指望整套 playwright**：`dashboard.spec.ts` 之类都假定
> 「打开就是控制台」，全会被守卫拦到登录页。这时只跑
> `e2e/tests/auth-login-flow.spec.ts`（它是唯一两种形态都成立的）。

**② 无鉴权的本地模式（仅开发，也是本地默认）**

必须 `AUTH_INSECURE=true`，**外加**下面二者之一：

```bash
# (a) 原生跑法：进程直接绑 loopback
AUTH_INSECURE=true CONFIG_PATH=config/analysis-service.yaml go run ./cmd/analysis
#     且配置里 server.host 必须是 127.0.0.1 / localhost

# (b) 容器跑法：容器**必须**绑 0.0.0.0（否则发布端口转发不进来），
#     于是判据换成「发布层」—— 需要逐字声明：
AUTH_INSECURE=true AUTH_INSECURE_EXPOSURE=loopback-published
```

两条都不满足时豁免**无效**（照样拒绝启动）—— 否则等于把回测和下单接口开给
整个局域网。豁免生效时日志会打 WARN 横幅（容器形态下还会带上 `exposure` 与
`guaranteed_by`，指明是谁在保证暴露面）。

> **`AUTH_INSECURE_EXPOSURE` 的取值必须逐字等于 `loopback-published`**：
> 拼错、大小写不符、写 `loopback-only` / `true`，一律按「没声明」处理 →
> 拒绝启动（fail closed）。这一条掉了等于这个开关可以靠一个笔误打开。
>
> 声明本身**不提供保证** —— 保证是两处机器校验给的：
> `tools/check_deploy_consistency.py` 检查 3c（静态，双向：开了 open-access
> 就不许有非回环映射；有非回环映射就必须配 `JWT_SECRET`）+ `tools/local-stack.sh`
> 的 `status`（运行时读真实 netstat，断言 8080/8081/8082/8085 只绑回环）。
> 决策见 [ADR-025](../adr/adr-025-auth-exposure-publish-layer.md)。

> e2e 走的就是这种姿势：`e2e/playwright.config.ts` 的 `BACKEND_URL` 默认
> `http://localhost:8085`，而 Playwright 与 `e2e/tests/integration_test.go`
> **都不带 token**（`e2e/tests/rbac-open-access.spec.ts` 开篇即写明「e2e
> environment runs with auth DISABLED」）。所以 e2e 必须打一个 open-access 的栈。
> `e2e/tests` 的 Go 套件还会在门里判这一条：拿到 401 时**明确 skip 并打印
> 姿势指引**，而不是报一条读不出信息的红（AUD-52）。

### CORS

两个服务的 `server.cors.allowed_origins`（env：`SERVER_CORS_ALLOWED_ORIGINS`，
逗号分隔）控制跨源白名单。**留空 = 不回显任何 `Access-Control-Allow-Origin`**，
浏览器侧一律拦截。dev 下 Vite 已代理 `/api`，同源，不需要配。

---

## 测试

```bash
go test ./...                                  # 全量
go test ./pkg/... -coverprofile=coverage.out   # 带覆盖率
```

现有 **275** 个 `*_test.go`（`find . -name '*_test.go' -not -path './web/*' -not -path './e2e/*' | wc -l`）。

> 跑全仓测试前把 Go 的 bin 目录放进 `PATH`，并设 `GOPROXY=off` —— 见 `docs/TEST.md` §2.0
> 「本机运行前置」。缺前者会让 `pkg/ai/pipeline` 的子进程找不到 `go`，缺后者会让它联网
> 解析 import 而挂到超时（AUD-56）。

---

## 已知陷阱（会浪费你时间的坑）

| 坑 | 现象 | 处置 |
|---|---|---|
| **测试"通过"但其实没跑** | 连不上库时 `testStore()` 会 `t.Skip`。CI 日志里看到 SKIP 要警惕 | 起 Postgres 再跑；`go test -v` 看 SKIP 数 |
| **新库是空的** | 少数依赖预置数据的测试在空库下会走 `t.Skip`（例如 `TestGetAllStocks`） | 起 Postgres 并跑一次数据同步；`go test -v` 看 SKIP 数。**依赖行情/日历的存储层测试已改为自灌自证**（自己写数据、自己清理），不再需要预置数据 —— 见 TASKS AUD-51 |
| **Windows 特有的测试失败** | 写死 POSIX 路径（`/tmp`、`/nonexistent/path`）在 Windows 下语义不同 | 已修；新增测试请用 `t.TempDir()` |
| **`make build` 会失败** | Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录早在 ODR-021 合并进 analysis 后**已不存在** | 用 `go build ./...` 代替；或修 Makefile（TASKS P1-10） |
| **两份 compose 文件互相漂移** | `docker-compose.yml` 与 `docker-compose.services.yml` 重复定义同批服务且版本标签不一致；后者还引用不存在的 Dockerfile | 只用 `docker-compose.yml`（TASKS P1-8） |
| **AI pipeline 编译后不加载** | `go build` 真跑，但产物从未 `plugin.Open`，回测必然 `strategy not found` | 见 TASKS P0-5 |
| **容器里连不上数据库/缓存** | 应用日志 `connection refused` 指向 `localhost:5432` —— 说明容器用了容器自己的 localhost | 容器内必须用 **`host.docker.internal`**。`docker-compose.yml` 已为三个服务显式注入；漏注入时护栏会报（`check_deploy_consistency.py` 检查 3） |
| **服务起不来：`Failed to connect to PostgreSQL`** | 应用连不上库是 `logger.Fatal()` 直接退出、**没有重试**（`cmd/data/setup.go:137` / `:146`） | 先 `tools/local-infra.sh status` 确认原生库在跑。容器启动时由 `deploy/wait-for-deps.sh` 有界等待（默认 60s） |
| **服务起不来：`auth: JWT secret missing`** | P0-4 之后没有密钥就拒绝启动（此前是静默 open-access） | 见上方「analysis 服务的两种启动姿势」的 **① 带鉴权**（或 ② 本地无鉴权） |

---

## 前端

```bash
cd web && npm install && npm run dev    # :5173
```

Dev 下 Vite 只代理 `/api`（见 `web/vite.config.ts`），
因此 `/alerts`、`/compliance` 等缺少 `/api` 前缀的调用会 404（TASKS P1-9）。

**鉴权相关（ADR-026）**：`web/src/api/client.ts` 是全前端唯一的 HTTP 出口
（两处 `fetch`：`request()` / `download()`），`Authorization: Bearer` 在那里
注入一次。**没有 token 时不带头** —— 这是 open-access 形态与加鉴权之前
"逐字节相同"的依据，别改成"总是带上、值可能为空串"。

401 的处理是「换令牌 → 重放」，结局分三态（`refreshed` / `rejected` /
`unavailable`）：只有服务端明确拒绝才算会话失效；断网不登出。见
`web/src/api/client.auth.test.ts`。

**姿势探测也是四态**（`web/src/stores/auth.ts` 的 `probe`）：`unknown` /
`open-access` / `anonymous` / `unavailable`。后两者**不能合并** ——
`anonymous` 是「后端**说了**要凭据」，`unavailable` 是「后端**没说话**」
（429 / 5xx / 断网）。合并的后果是一次限流把已登录用户弹到登录页（AUD-57）。
`unavailable` 仍然 **fail closed**（受保护页面照样拦），但界面说的是
「连不上 + 重试」，不是一个按下去也不会成功的凭据表单；而且它是**可再探**
状态 —— 后端一恢复，下一次跳转就把会话接回来，不需要手动刷新页面。

> `/api/auth/status` 在 `cmd/analysis` 里**被限流豁免**（与 `/health` 同理）：
> 它必须答得出来，否则前端分不清「被限流」和「未认证」。别把 `/api/auth`
> 前缀整个放开 —— `/api/auth/login` 与 `/api/auth/refresh` **必须继续限流**
> （口令爆破的第一道闸）。名单在 `cmd/analysis/middleware.go` 的
> `rateLimitExemptPaths`（逐字相等匹配），护栏
> `cmd/analysis/rate_limit_exempt_test.go`。

```bash
cd web && npm run typecheck && npm test      # 208 条（含鉴权 43 条）
# ⚠️ Windows 下别直接跑 node_modules/.bin/vitest（POSIX 脚本，报 WinError 193）：
node node_modules/vitest/vitest.mjs run src/api/client.auth.test.ts
```
