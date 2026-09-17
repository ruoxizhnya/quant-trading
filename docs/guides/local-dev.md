---
status: evergreen
type: how-to
last-verified: 2026-09-17
verified-by: 实际命令核对（Makefile / docker-compose.yml / go.mod / cmd/）+ P0-4 启动门禁运行时取证
---

# 本地开发指南（How-to）

> 只写**当前确实可用**的步骤。遇到与本文不符的情况，以代码为准并回来更新本文。

---

## 环境要求

| 项 | 版本 | 来源 |
|---|---|---|
| Go | **1.25** | `go.mod` |
| PostgreSQL | 16 | `docker-compose.yml` |
| Redis | 7 | `docker-compose.yml` |
| Node | 22（仅前端） | — |
| Python | 3.12（仅文档校验脚本） | `tools/check_doc_links.py` |

> ⚠️ **Go 不在 PATH**：本机装在 `C:\Users\ruoxi\sdk\go1.25.0`。跑任何 Go 命令前先：
> ```bash
> export PATH="/c/Users/ruoxi/sdk/go1.25.0/bin:$PATH"
> ```

---

## 起基础设施

```bash
cp .env.example .env     # 一次性：至少把 JWT_SECRET 换成 openssl rand -hex 32
docker-compose up -d postgres redis
```

> ⚠️ **必须先有 `.env`**。P0-4 之后 `docker-compose.yml` 对 `JWT_SECRET` 用了
> `${JWT_SECRET:?...}` 必填插值 —— 没配的话连 `docker-compose up -d postgres`
> 都会在解析阶段报错（这是刻意的：analysis 监听 0.0.0.0，无鉴权不能起）。

> ⚠️ **必须用带连字符的 `docker-compose`**。本机 Docker 27.4.1 不支持 `docker compose`
> （空格写法会报 `'compose' is not a docker command`）。

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

`docker-compose.yml` 中的服务：`postgres` / `redis` / `data-service` / `strategy-service` / `analysis-service`。

---

## 跑服务（本地）

```bash
# analysis 必须带密钥，否则拒绝启动（P0-4）
JWT_SECRET=$(openssl rand -hex 32) go run ./cmd/analysis   # :8085 主服务（含 risk/execution，已合并）

go run ./cmd/data        # :8081
go run ./cmd/strategy    # :8082
go run ./cmd/ai          # :8086  AI 研究服务
```

> ⚠️ **`cmd/ai` 没有 Dockerfile，也不在 `docker-compose.yml` 里**——只能本地 `go run`。（见 TASKS P1-11）

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

现有 186 个 `*_test.go`。

> ⚠️ **`.gitignore:29` 的 `*_test.go` 规则会忽略所有新建的测试文件**。
> 新建测试后若 `git status` 看不到它，需 `git add -f path/to/file_test.go`。（见 TASKS P0-3）

---

## 已知陷阱（会浪费你时间的坑）

| 坑 | 现象 | 处置 |
|---|---|---|
| **测试"通过"但其实没跑** | 连不上库时 `testStore()` 会 `t.Skip`。CI 日志里看到 SKIP 要警惕 | 起 Postgres 再跑；`go test -v` 看 SKIP 数 |
| **新库是空的** | `TestGetAllStocks` / `TestHasOHLCVData` / `TestGetTradingDays` / `TestIsTradingDay` / `TestGetTradingDates` 需要预置行情数据，空库下会误报失败 | 已加 `skipIfNoSeedData` 守卫，空库时跳过而非失败 |
| **Windows 特有的测试失败** | 写死 POSIX 路径（`/tmp`、`/nonexistent/path`）在 Windows 下语义不同 | 已修；新增测试请用 `t.TempDir()` |
| **`make build` 会失败** | Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录早在 ODR-021 合并进 analysis 后**已不存在** | 用 `go build ./...` 代替；或修 Makefile（TASKS P1-10） |
| **两份 compose 文件互相漂移** | `docker-compose.yml` 与 `docker-compose.services.yml` 重复定义同批服务且版本标签不一致；后者还引用不存在的 Dockerfile | 只用 `docker-compose.yml`（TASKS P1-8） |
| **AI pipeline 编译后不加载** | `go build` 真跑，但产物从未 `plugin.Open`，回测必然 `strategy not found` | 见 TASKS P0-5 |
| **服务起不来：`auth: JWT secret missing`** | P0-4 之后没有密钥就拒绝启动（此前是静默 open-access） | 见下方「启动后端」 |

---

## 前端

```bash
cd web && npm install && npm run dev    # :5173
```

Dev 下 Vite 只代理 `/api`（见 `web/vite.config.ts`），
因此 `/alerts`、`/compliance` 等缺少 `/api` 前缀的调用会 404（TASKS P1-9）。
