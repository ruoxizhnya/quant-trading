---
status: evergreen
type: how-to
last-verified: 2026-09-25
verified-by: 实际命令逐条复核（`find -name '*_test.go'` 实测 273、`.gitignore` 已无 `*_test.go` 规则、
  `ls cmd/` 只有 analysis/data/strategy、`docker compose` 实测报 `'compose' is not a docker command`
  而 `docker-compose` v2.32.1 可用、`C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe` 实测存在且能离线编译全仓）＋
  AUD-51 自灌自证改造后同步「已知陷阱」
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
cp .env.example .env         # 一次性：至少把 JWT_SECRET 换成 openssl rand -hex 32
tools/local-infra.sh start   # 原生 PostgreSQL 17.5 + Redis 7.4.11（不再由 compose 托管）
docker-compose up -d         # 应用服务（data / strategy / analysis / web）
```

`tools/local-infra.sh {start|stop|status}` 只管**原生**那半（PG + Redis），
`status` 会断言 AUD-13（数据库/缓存只监听回环）—— 这条断言原先在
`tools/check_deploy_consistency.py` 里守 compose 的端口映射，服务移出 compose 后
挪到了这里，改为断言真实的 netstat 监听 socket（`listen_addresses` 由脚本用
`-c` 显式指定，不靠配置文件默认值）。

> ⚠️ **`up` 返回 0 只代表容器被创建了**。应用连不上 PG/Redis 是 `logger.Fatal()`
> **没有重试**（`cmd/data/setup.go:137` / `:146`），容器会立刻退出；健康检查还有
> `start_period`，`up` 刚返回时状态还是 `starting`。所以起完要看
> `docker ps` 里的服务是否已成 `healthy` —— 容器内的等待由
> `deploy/wait-for-deps.sh` 有界承担（默认 60s），它替换掉了被移除的
> `depends_on: {condition: service_healthy}`。

> ⚠️ **`local-infra.sh start` 在 Agent 沙箱里跑会被回收**：沙箱按进程组清理整棵
> 进程树，`nohup ... &` 起的进程随那次工具调用一起消失（实测 `pg_ctl start` /
> `nohup &` / `Start-Process` 三者在这个环境下都无效）。那种情况下要改用
> 「后台任务」姿势直接 exec 两个二进制。**在你自己开的终端里正常。**

> ⚠️ **必须先有 `.env`**。P0-4 之后 `docker-compose.yml` 对 `JWT_SECRET` 用了
> `${JWT_SECRET:?...}` 必填插值 —— 没配的话连 `docker-compose up -d` 都会在解析
> 阶段报错（这是刻意的：analysis 监听 0.0.0.0，无鉴权不能起）。

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

```bash
# analysis 必须带密钥，否则拒绝启动（P0-4）
JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/analysis   # :8085 主服务（含 risk/execution，已合并）

go run ./cmd/data        # :8081
go run ./cmd/strategy    # :8082
```

> `cmd/ai`（:8086）已于 2026-09-18 **删除**（TASKS P2-5）：零调用方，且建在废弃交互层的
> 定位上。AI 能力走 `cmd/analysis` 的 MCP 工具层，不再是独立服务。

### analysis 服务的两种启动姿势

**① 带鉴权（默认，任何非 loopback 部署都必须）**

```bash
export JWT_SECRET=$(openssl rand -hex 32)
```

生效后 `/api/*` 需要 `Authorization: Bearer <access_token>`，
token 从 `POST /api/auth/login` 拿。不设就直接 `auth: JWT secret missing` 退出。

**② 无鉴权的本地模式（仅开发）**

必须**同时**满足两个条件，缺一不可：

```bash
AUTH_INSECURE=true CONFIG_PATH=config/analysis-service.yaml go run ./cmd/analysis
# 且配置里 server.host 必须是 127.0.0.1 / localhost
```

监听 `0.0.0.0` 时豁免**无效**（照样拒绝启动）—— 否则等于把回测和下单接口
开给整个局域网。豁免生效时日志会打 WARN 横幅。

> e2e 走的是这种姿势：`e2e/playwright.config.ts` 的 `BACKEND_URL` 默认
> `http://localhost:8085`，后端需要 `AUTH_INSECURE=true` + loopback 才能起来。

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

现有 **273** 个 `*_test.go`（`find . -name '*_test.go' -not -path './web/*' -not -path './e2e/*' | wc -l`）。

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
| **服务起不来：`auth: JWT secret missing`** | P0-4 之后没有密钥就拒绝启动（此前是静默 open-access） | 见下方「启动后端」 |

---

## 前端

```bash
cd web && npm install && npm run dev    # :5173
```

Dev 下 Vite 只代理 `/api`（见 `web/vite.config.ts`），
因此 `/alerts`、`/compliance` 等缺少 `/api` 前缀的调用会 404（TASKS P1-9）。
