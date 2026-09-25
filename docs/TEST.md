---
status: evergreen
last-verified: 2026-09-25
verified-by: AUD-44 路径引用复核 + AUD-47 §5–§7 内容复核（2026-09-22）+ AUD-56 新增 §2.0「本机运行前置」（2026-09-24）—— ① 路径：`pkg/tracker` 已迁至 `pkg/backtest/tracker`、`docs/phase-gate-reviews.md` 已归档至 `docs/archive/research-2026-Q2/`、§4 的 CLI 示例标注为非真实接口；② 内容：§5 沙箱限制改为实测值（30s CPU + 1 GiB，ODR-020）、§6 覆盖率目标标注为「目标不是门禁」（CI 不设阈值）、§7.1 `format.test.ts` 8→28 例、§7.2 e2e 改为实测 17 spec / 160 例、`--project=chrome`→`chromium`；③ §2.0：记下本机跑全仓测试的两个环境前置（`PATH` 里要有 `go`、`GOPROXY=off`），两者都会造成「红了但不指向代码」的假信号 —— 取证见 TASKS.md 的 AUD-56
---

# Test Plan & Quality Assurance (TEST.md)

> **Version:** 1.1.0 (AGENTS Template v2.0 Migration)
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [VISION.md](VISION.md) (product), [ADR.md](ADR.md) (architecture), [SPEC.md](SPEC.md) (implementation)
>
> **Changelog v1.1 (Migration):**
> - 添加标准元数据头部（Status, Last Updated, Related）
> - 统一文档链接格式

---

## 1. Testing Philosophy

We apply the **FIRST** principles to all testing:
- **Fast** — Unit tests run in < 1s; backtest regression suite in < 30s
- **Independent** — No test depends on another; tests can run in any order
- **Repeatable** — Same results every time (deterministic, fixed seeds)
- **Self-validating** — Tests pass or fail automatically; no manual inspection required
- **Timely** — Tests written alongside code, not after

**Core invariant tests (never allowed to fail):**
```
cash ≥ 0                          // Never spend more than you have
position_quantity ≥ 0            // Never hold negative shares
nav = cash + Σ(position_value)   // NAV is always traceable
all_trades.have_fee > 0          // Every trade has fees recorded
all_trades.have_timestamp        // Every trade is time-stamped
```

---

## 2. Test Categories

### 2.0 本机运行前置（Windows / 离线环境）

宿主有两套 Go：`C:\Users\ruoxi\sdk\go1.25.0`（**原装，离线能编译全仓**，但**不在 PATH** ——
早先「宿主没有原装 Go」的结论是错的，2026-09-25 订正）与下方这个免安装版。
~~到 `proxy.golang.org` 的网络不通~~ —— **2026-09-25 订正：网络是通的**
（`curl https://proxy.golang.org/` 返 HTTP 200，`docker build` 里的 `go mod download`
正常完成）。下面仍然带 `GOPROXY=off`，理由变成「不让任何子进程有机会联网挂住」，
不再是「离线」。这几件事各会制造一种「红了，但失败信号不指向任何代码问题」。
跑全仓测试前先备好环境：

```bash
export GOROOT="C:/Users/ruoxi/.workbuddy/binaries/go/go"
export GOMODCACHE="C:/Users/ruoxi/.workbuddy/binaries/go/modcache"
export GOCACHE="C:/Users/ruoxi/.workbuddy/binaries/go/buildcache"
export GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-mod=mod
export PATH="/c/Users/ruoxi/.workbuddy/binaries/go/go/bin:$PATH"   # 子进程按 PATH 找 go
export GOPROXY=off
go test ./...
```

- **`PATH` 必须带上 `go` 所在的目录**：`pkg/ai/pipeline` 的 `validateCompilation` 会
  `exec.Command("go", "build", ...)`，子进程按 **PATH** 找 `go` —— 只设 `GOROOT` 不够。
- **`GOPROXY=off` 仍建议带上**（根因 **AUD-56** 已于 2026-09-25 修掉，见 `TASKS.md`）：
  那条测试原来用 `import "github.com/nonexistent/fakepkg"` 逼 `go build` 失败，而
  `go build` 解析这个 import 会**联网** —— 墙内不通时它一直等，整仓测试挂在超时上
  （实测 `go test ./...` **601s 被杀**、单跑 70s `panic: test timed out`）。
  现在测试改成了「**正面证据 + 反证**」并自带 `t.Setenv("GOPROXY","off")`，生产侧的
  `go build` 子进程也有了 120s 上界。带着它只是多一层保险，读数：**74 包 57s** 全绿。
- **真库（PostgreSQL）测试**：Docker Desktop 在本机受两条限制 —— 启动时拉起的 `wsl.exe`
  在程序黑名单里（沙箱明写「不可批准、不可绕过」），且**起来之后还会自发退出**。
  所以库测试不能指望它。替代路径见 §2.0.1。

#### 2.0.1 原生 PostgreSQL（不依赖 Docker / WSL）

代码对 TimescaleDB 是 **best-effort**（`pkg/storage/postgres.go:840` 对 `create_hypertable`
失败只 `Warn`，注释明写「不该为此引入硬依赖」），`migrations/` 也不自动执行、建表全在
`pkg/storage/postgres.go` 的内联 DDL 里 —— 所以**纯原生 PG 能跑通全链路**。

```
二进制   C:/Users/ruoxi/.workbuddy/binaries/postgres/pgsql/bin/
数据目录 C:/Users/ruoxi/.workbuddy/binaries/postgres/data
连接     postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable
         （与 .env 的 DATABASE_PASSWORD、pkg/storage/postgres_test.go 的硬编码 DSN 三者一致）
```

启动（**必须让进程脱离 bash 的进程组** —— 用 `pg_ctl start` 或 `nohup ... &` 起的
进程，会随那次工具调用结束被一起清理）：

```bash
export MSYS_NO_PATHCONV=1
ROOT="C:/Users/ruoxi/.workbuddy/binaries/postgres"
"$ROOT/pgsql/bin/postgres.exe" -D "$ROOT/data" -c listen_addresses=127.0.0.1 -p 5432
# ↑ 作为「后台任务」运行，不要放进一次性的前台调用里
```

初始化（只做一次）与建库：

```bash
printf 'postgres\n' > "$ROOT/pw.txt"
"$ROOT/pgsql/bin/initdb.exe" -D "$ROOT/data" -U postgres --pwfile="$ROOT/pw.txt" \
  -E UTF8 --locale=C && rm -f "$ROOT/pw.txt"
"$ROOT/pgsql/bin/psql.exe" -h 127.0.0.1 -U postgres -c "CREATE DATABASE quant_trading;"
```

> **Redis 未起也能跑手动测试**：`cmd/analysis/*.go` 里零 Redis 引用（redis 只出现在
> `pkg/storage/redis.go` / `pkg/live/redis_order_store.go` / `pkg/marketdata/cached_provider.go`）。
> 只有 `cmd/data` 启动期硬依赖它（`cmd/data/setup.go:146` 连不上直接 Fatal）。
> ~~compose 里的 `depends_on: redis` 只约束容器编排、不约束本地 `go run`~~ ——
> 2026-09-25 起 `redis` 不再是 compose 服务，那条 `depends_on` 已不存在；容器侧的
> 等待改由 `deploy/wait-for-deps.sh` 承担。

#### 2.0.2 原生 Redis 7.4.11（2026-09-25 新增）

「数据库与缓存跑宿主机、服务跑容器」定案之后，Redis 也必须对齐版本 ——
否则会出现「本地通过、容器里失败」。原先是 **tporadowski 的 5.0.14 移植版**
（那个项目已停更在 5.0），现在换装与 compose 同线（`redis:7-alpine` → 7.x）的 **7.4.11**：

```
二进制   C:/Users/ruoxi/.workbuddy/binaries/redis-7.4.11/
数据目录 C:/Users/ruoxi/.workbuddy/binaries/redis-7.4.11/data
连接     redis://127.0.0.1:6379      （无 requirepass，与本项目一贯一致）
```

来源是 `redis-windows/redis-windows` 的 MSYS2 构建（官方 Redis 不支持 Windows）。
旧的 5.0.14 目录 `~/.workbuddy/binaries/redis/` **保留着做回退**，没有删。

> **命令兼容性实测**：全仓用到的 Redis 命令只有
> `Get / Set / Del / Exists / Scan / Keys / Ping`（`pkg/storage/redis.go`、
> `pkg/live/redis_order_store.go`、`pkg/marketdata/cached_provider.go`），
> 都在 5.0 就已存在，5→7 没有破坏性变更。

> ⚠️ **它是 MSYS2 构建，不认 Git Bash 路径**（2026-09-25 实测两种写法都失败）：
> `/c/Users/...` → `can't open config file '/c/Users/...'`（当成 msys 根）；
> `C:/Users/...` → 被当成**相对 cwd 的路径**拼上去，报
> `'/cygdrive/c/.../quant-trading/C:/Users/.../redis.conf'`。
> **唯一稳的姿势是 `cd` 进目录再给相对路径** —— `tools/local-infra.sh` 已经这么写，
> 别「顺手」改成绝对路径。

#### 2.0.3 整栈启停与「起来了」的判据（2026-09-25 新增）

```bash
tools/local-stack.sh {start|stop|status}
```

它是「原生库 + 容器服务」这两半的唯一入口，做四件事：探 Docker 引擎、
`local-infra.sh start`、`docker-compose up -d`、**等健康 + 端口体检**。

> **为什么不能只看 `docker-compose up` 的退出码**：它返回 0 只代表容器被
> **创建**了。应用连不上 PG/Redis 是 `logger.Fatal()` **无重试**
> （`cmd/data/setup.go:137/:146`），容器会立刻退出；健康检查还有 `start_period`，
> up 刚返回时状态还是 `starting`；而且 **Docker Desktop 在本机还会自发退出**
> （2026-09-25 实测：10:24 还在跑，10:27 命名管道就没了）。所以唯一可信的判据是
> **HTTP 探针全通**；`status` 里显示的 `容器=healthy / HTTP=200` 两列要一起看。

> **Docker 引擎探不到就停下并给指引**：那个失败的症状（
> `open //./pipe/dockerDesktopLinuxEngine: The system cannot find the file
> specified`）读起来像路径问题，而真因是引擎没了。启动 Docker Desktop 会拉起
> `wsl.exe`，而它在沙箱的程序黑名单里（报错明写「不可批准、不可绕过」）——
> **脚本无法自救，只有用户能解**，所以它只报错不重试。

`status` 里的**端口体检**是 AUD-13 的运行时那一半：读真实 `netstat`，断言
8080/8081/8082/8085 只绑回环（静态那一半是 `check_deploy_consistency.py` 的
检查 3c）。这一条不是「锦上添花的检查」—— analysis 以 open-access 运行，
容器又必须绑 `0.0.0.0`，「不可从其他主机到达」这条不变量**只**由发布层承担，
见 [ADR-025](adr/adr-025-auth-exposure-publish-layer.md)。

### 2.1 Unit Tests — `pkg/*`

**Strategy Layer:**
- Register mock strategy → resolve by name → verify `GenerateSignals` signature
- Feed known bar series to momentum/value/multi-factor → verify signal direction
- Property: `cash ≥ 0` after every signal generation
- Property: signal weights sum to ≤ 1.0 (no leverage)

**Execution Layer (Tracker):**
- `Signal → Order → Tracker → Position` pipeline
  - Buy 1000 shares → `Position.Quantity == 1000`, `AvgCost` correct
  - Sell 500 → remainder correct
- **T+1 settlement (P0, critical):**
  - Buy on D → attempt sell on D → **blocked** ✅
  - Buy on D → sell on D+1 → **succeeds** ✅
  - Buy on D → sell portion on D+1 → only YD bucket depleted ✅
  - Sell 100 of 500 held (YD: 300, TD: 200) → YD depleted first ✅
- **涨跌停 detection (P0, critical):**
  - Inject mock bar: `(high - prev_close)/prev_close >= 0.10` → buy **blocked** ✅
  - Inject mock bar: `(low - prev_close)/prev_close <= -0.10` → sell **blocked** ✅
  - ST stock with `(high - prev_close)/prev_close >= 0.05` → buy **blocked** ✅
  - Next day after limit-up: open at limit price → verify gap model ✅
- **Commission + stamp tax:**
  - 3 trades (buy, sell, sell) with various sizes → stamp tax **only on sell legs** ✅
  - Very small buy trade → minimum 5 CNY floor applied ✅
  - Transfer fee on both sides verified ✅
- **Slippage:** configured rate applied to every fill ✅
- **Buying power:** order value > available cash → **rejected** ✅
- **Integer rounding:** 183 shares → rounds to 100; 99 shares → **rejected** ✅
- **Partial fill:** available cash covers 50% of order → 50% fill ✅

**Analytics Layer:**
- Feed fixed OHLCV fixture → compute Sharpe, max drawdown, win rate → compare to expected values
- Equity curve: NAV monotonically related to positions + cash ✅

**Data Layer:**
- OHLCV continuity across corporate actions (前复权)
- `stock_fundamentals` schema: required fields non-null
- Trading calendar: CNY 2024 holiday → `is_open=false`

---

### 2.2 Integration Tests

**Service Communication:**
- `analysis-service → data-service`: HTTP call returns OHLCV for known date range
- `analysis-service` in-process `risk.RiskManager`: signal weight adjustment (no cross-service HTTP, ODR-021)
- Health endpoints return 200 on all services

**Data Pipeline:**
- Tushare sync → PostgreSQL → query returns same data (end-to-end integrity)
- Factor cache: pre-computed z-scores match on-the-fly computation

**Backtest Full Pipeline:**
```
Fixed seed + known universe → backtest → equity curve + trade log
↓
Compare to stored fixture ( fixture.json )
↓
Any deviation > 0.01% → FAIL
```

---

### 2.3 Regression Suite

**Purpose:** Ensure backtest results are deterministic. Same inputs → same outputs forever.

**Mechanism:**
- `backtest_jobs` table stores `seed` column
- Golden fixtures stored in `testdata/backtest-fixtures/` (JSON files with known-good NAV curves)
- CI runs: `go test ./pkg/backtest/... -update-fixtures=false` against fixtures

**Fixtures to create (Phase 1 before exit):**
- [ ] `fixture-5yr-500stock-momentum.json` — expected NAV curve, total return, Sharpe, max drawdown
- [ ] `fixture-1yr-single-stock-value.json` — single stock, value strategy
- [ ] `fixture-t+1-enforcement.json` — T+1 edge cases
- [ ] `fixture-zhangting-detection.json` — 涨跌停 boundary cases

**Running the suite:**
```bash
# Run all tests
go test ./...

# Run regression suite only
go test ./pkg/backtest/... -run Regression

# Update fixtures (after verifying new results are correct)
go test ./pkg/backtest/... -run Regression -update-fixtures=true
```

---

### 2.4 Property-Based Tests

Using `testing/quick` or `golang/mock`:

| Property | Test |
|----------|------|
| Cash never negative | Generate random trades; cash always ≥ 0 |
| Position never negative | Generate random buy/sell sequences; quantity always ≥ 0 |
| NAV traceable | NAV = cash + Σ(position_value) at every step |
| Fees always positive | Every trade has fee > 0 |
| T+1 enforced always | 1000 random buy-sell sequences; same-day sells always blocked |

---

### 2.5 Chaos / Reliability Tests

**What to test (Phase 2+):**
- Tushare API fails mid-sync → partial data handled gracefully; retry with backoff
- Redis goes down mid-backtest → fallback to PostgreSQL query (slower, but works)
- Risk service timeout → circuit breaker opens; backtest continues without risk adjustment (fail-safe)
- Docker service crash → health check detects; restart triggered

---

## 3. Phase Gate Tests

These are the **acceptance tests** that must pass before advancing phases. Recorded in `archive/research-2026-Q2/phase-gate-reviews.md`（已归档，只读）。

### Phase 1 Gate (before Phase 2)

| Test | Pass Criterion |
|------|----------------|
| T+1 unit tests | All 5 T+1 cases (above) pass |
| 涨跌停 unit tests | All boundary cases (ST ±5%, gap model) pass |
| Determinism regression | 3 fixtures produce identical results across 3 consecutive runs |
| ~~vnpy drift comparison~~ | ⚠️ **Deprioritized** — requires parquet data not available; replaced by internal determinism + fixture validation |
| Backtest speed | 5yr/500stock backtest: ≤ 5 seconds |
| Test coverage | `go test -cover ./pkg/backtest/...` > 80% |

### Phase 2 Gate (before Phase 3)

| Test | Pass Criterion |
|------|----------------|
| Factor cache integration | Multi-factor 100stock/3yr backtest: ≤ 30 seconds |
| Strategy Copilot E2E | Human submits Chinese → receives code → backtest runs → results displayed (manual test) |
| Copilot acceptance rate | ≥ 30% of generated strategies compile and pass smoke test |
| Walk-forward framework | Framework operational; 3 candidate strategies pass train/validate split |
| Background worker | `POST /backtest` returns `job_id` immediately; worker processes async; results queryable |
| Strategy DB CRUD | Create/read/update/delete strategy via API; YAML round-trip preserved |

---

## 4. Backtest Accuracy Validation (vs vnpy) 📦 *Archived — Reference Only*

> **Status**: This comparison was deprioritized (Phase 1 Gate). Section retained for future reference when parquet data becomes available.

### Methodology for 5% Drift Comparison

**Step 1 — Select reference universe:**
- 50 stocks from CSI 300 constituents (as of 2023-01-01)
- 5-year backtest: 2018-01-01 to 2023-01-01

**Step 2 — Configure identical inputs:**
- Same initial capital: 1,000,000 CNY
- Same strategy: 20-day momentum (top 10 by score, equal weight)
- Same rebalancing: monthly (last trading day of each month)
- Same commission: 0.03% + 0.1% stamp tax (sell-only) + 0.001% transfer fee
- Same slippage: 0% (disable for clean comparison)
- Same T+1 enforcement
- Same 涨跌停 handling

**Step 3 — Run both systems:**

> ⚠️ **下面的命令是示意，不是真实接口**（AUD-44）：analysis-service 是 HTTP 服务，
> `cmd/analysis` 没有 `--strategy` / `--universe` / `--start` / `--end` / `--capital` /
> `--output` 这些 flag；回测是经 `POST /api/backtest` 提交、异步执行、再查结果。

```bash
# Our system
go run ./cmd/analysis/main.go \
  --strategy momentum \
  --universe csi300-2018 \
  --start 2018-01-01 \
  --end 2023-01-01 \
  --capital 1000000 \
  --output json > our-result.json

# vnpy: run equivalent backtest and export results
```

**Step 4 — Compare:**
```python
# compare_backtests.py
import json

with open('our-result.json') as f:
    ours = json.load(f)
with open('vnpy-result.json') as f:
    theirs = json.load(f)

total_return_diff = abs(ours['total_return'] - theirs['total_return'])
annual_return_diff = abs(ours['annual_return'] - theirs['annual_return'])
sharpe_diff = abs(ours['sharpe_ratio'] - theirs['sharpe_ratio'])
max_dd_diff = abs(ours['max_drawdown'] - theirs['max_drawdown'])

print(f"Total return diff: {total_return_diff:.4f}")
print(f"Annual return diff: {annual_return_diff:.4f}")
print(f"Sharpe diff: {sharpe_diff:.4f}")
print(f"Max drawdown diff: {max_dd_diff:.4f}")
```

**Step 5 — Pass criterion:**
- `total_return_diff < 0.05` (5%)
- `max_drawdown_diff < 0.05` (5%)
- `sharpe_diff < 0.3` (0.3 is lenient givenvnpy's approximation)

> ⚠️ **Note:** vnpy itself has approximations (e.g., T+1 handling, 前复权 continuity). The 5% target is a proxy for "results are in the same ballpark." True ground-truth validation requires comparing against a real brokerage account's actual P&L.

---

## 5. AI Evolution Validation Tests

For Phase 3 candidate strategies generated by AI:

| Validation Step | Tool | Pass Criterion |
|----------------|------|----------------|
| Syntax check | `go build -o /dev/null` | Compiles without error |
| Dangerous pattern scan | `go vet` + `internal/sandbox/staticcheck` | No `os.Open`, `os.Remove`, infinite loop patterns |
| Logic guardrails | AI code review (separate LLM call) | No leverage > 1.0, no position limits > 40% |
| Walk-forward validation | Backtest engine (train/validate split) | Must beat benchmark in BOTH train AND validate windows |
| Minimum backtest window | Backtest engine | ≥ 3 years of data required |
| Resource limits | `internal/sandbox/runner`（子进程 + 超时 + 内存上限） | **30s CPU + 1 GiB 内存**（ODR-020 定的值，见 `cmd/analysis/main.go` 的 `sandboxRunnerAdapter`）。POSIX 走 shell wrapper + `setrlimit`，Windows 走 Job Object；映射不到的维度在 Windows 上**降级**而不是假装生效 |

---

## 6. Coverage Targets

> ⚠️ **这些是目标，不是门禁**（AUD-47 复核）：CI **不设覆盖率阈值** ——
> `.github/workflows/ci.yml` 的 go job 只跑 `go build ./...` / `go vet ./...` /
> `GOOS=windows go vet ./...` / `go test ./... -count=1 -race`，**不读
> `coverage.out`**。`make test` 只是**生成报告**（`go test ./... -v
> -coverprofile=coverage.out`），低于任何一行也不会失败。
> 所以下表的数字**不会自动生效**，要让它生效得先把阈值加进 CI。

| Package | Target |
|---------|--------|
| `pkg/backtest` | > 85% line coverage |
| `pkg/strategy/plugins` | > 80% line coverage |
| `pkg/backtest/tracker` | > 90% line coverage |
| `pkg/data` | > 70% line coverage |
| Frontend (web/src) | > 70% line coverage |
| Overall | > 80% line coverage |

Run: `make test`（= `go test ./... -v -coverprofile=coverage.out`）→
`go tool cover -html=coverage.out`

---

## 7. Frontend Testing

### 7.1 Unit Tests — Vitest

**Framework:** Vitest + @vue/test-utils + happy-dom

**Running:**
```bash
cd web
npm test              # Run all unit tests
npm run test:watch    # Watch mode
npm run test:coverage # With coverage report
```

**Test Files:**
| File | Coverage | Description |
|------|----------|-------------|
| `src/utils/format.test.ts` | 28 tests | fmtPercent, fmtNumber, fmtCurrency, formatDate, fmtVolume, fmtMetric |
| `src/stores/backtest.test.ts` | 11 tests | addToHistory, trades mapping, deduplication, clearHistory |
| `src/components/backtest/EquityChart.test.ts` | 11 tests | buildTradeMarkers (buy/sell/short/empty/filter/fallback) |

**Key Test Scenarios:**
- `buildTradeMarkers`: Buy (long), Sell (close), Short directions handled correctly
- `buildTradeMarkers`: Empty date/zero value filtered out
- `buildTradeMarkers`: Fallback to entry_date/exit_date when timestamp missing
- `useBacktestStore`: Trades correctly attached to history items via computed
- `fmtPercent`: Handles null, undefined, NaN, positive, negative, zero

### 7.2 E2E Tests — Playwright

**Framework:** Playwright + Chrome

**Running:**
```bash
cd e2e
npx playwright test                       # Full suite
npx playwright test --project=chromium    # Chromium only（唯一配置的 project）
npx playwright test --grep "Backtest"     # Backtest-related only
```

**Test Suites:** 17 个 spec 文件、**160** 条用例（AUD-47 复核后的实测数）：

| 类别 | spec 文件 | 用例数 |
|---|---|---|
| API 层 | `api-backtest` / `api-strategy` / `api-health` / `api-negative` | 31 |
| 回测引擎 | `backtest-engine` / `critical-e2e` | 40 |
| 数据同步 | `data-sync` / `data-sync-schedule` / `data-sync-error` | 22 |
| Copilot | `copilot` / `copilot-e2e` | 20 |
| 页面 / 导航 | `dashboard` / `screener` / `strategy-selector` / `cross-navigation` / `rbac-open-access` | 35 |
| 视觉回归 | `visual-regression` | 12 |

（`e2e/tests/integration_test.go` 是 Go 写的，不计入上表。）

#### 7.2.1 Go 写的 `e2e/tests`（AUD-52 修后的形态）

`e2e/tests/integration_test.go` 是一个**轻量 HTTP 冒烟套件**，跟着
`go test ./...` 跑，不需要 Node/浏览器。它和 Playwright 那套的**共同前提**是
**栈必须是 open-access**（Playwright 的 `apiRequest()` 不带 Authorization，
`rbac-open-access.spec.ts` 开篇即写明 e2e environment runs with auth DISABLED）。

AUD-52（2026-09-25 修）之前它不是这样工作的：`TestMain` 只探 `/health` 就放行
整套，「服务在」被当成了「服务能用」，于是两条用例长期红得没有信息量（一条拨
已退役的 `:8084`，一条打 `/api/strategies` 拿到 401）。现在门与断言**同源**，
判三档：

| 档 | 判据（就是各用例真正会打的路径） | 动作 |
|---|---|---|
| ① | `/health` 不通 | skip（S7-P0-8 原意：环境没起 → 退出 0） |
| ② | 通了但 `/api/execution/*` 404 / 连不上 | **FAIL** —— 真回归（ODR-021 后的路由必须存在） |
| ③ | 都在但要鉴权（401/403） | skip + 打印怎么改姿势（**形态不匹配 ≠ 缺陷**） |

第 ③ 档打印出来的就是修法：打一个 `AUTH_INSECURE=true` +
`AUTH_INSECURE_EXPOSURE=loopback-published` 的栈（默认 compose 即是，见
[ADR-025](adr/adr-025-auth-exposure-publish-layer.md)）。

**为什么第 ③ 档是 skip 而不是 FAIL**：套件**无法**自取 token —— 系统没有首个
管理员的引导（`CreateUser` 只挂在 `RequireRole(admin)` 后面，鸡生蛋），这是
刻意的。所以「跑了鉴权栈」这件事既不是代码缺陷、也无法自愈，报红只会变成长期
噪声，而长期噪声会训练人忽略红色。

**已知边界**：鉴权开启时**所有** `/api/*` 都回 401（中间件挂在 router 上、
先于路由匹配），包括不存在的路径 —— 所以那种形态下「执行端点是否存在」**判不
出来**，只能判出「要鉴权」然后整体 skip。这条检测只在 open-access 形态下有效，
而那正是套件要跑的形态。

回归护栏在 `e2e/guard/guard_test.go`（独立包，不会被上面的 skip 门带走）：
用 AST 取标识符与字符串字面量，把门的三档判据、探针路径、以及「退役端口不许
回来」都钉住 —— 故意扫的是 AST 而不是原文，否则文件里解释 `:8084` 的**注释**
会被当成违规。破坏验证 5/5（4 红 + 1 绿对照）。
