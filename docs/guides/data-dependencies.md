---
status: evergreen
last-verified: 2026-09-23
verified-by: 读取 `cmd/data/registry_init.go` / `pkg/data/tushare.go` / `config/data-service.yaml` /
  `.env.example` / `docker-compose.yml` 核对，Alpha Vantage 免费额度引自其定价页；
  **2026-09-23 真部署 + 真同步实测**：容器内 curl 逐个接口验权限
  （`daily`/`adj_factor`/`stock_basic` 可用；`stk_factor`/`stk_factor_pro` 返回 **40203**），
  qfq 换算式取自 Tushare 官方 `adj_factor` 接口文档示例 4 并用官方算例做绝对值断言。
  **本文件初版里「`stk_factor_pro` 多要 4 个 `*_hfq` 字段即可」的方案已被实测推翻，
  P2-8 一节已整段重写 —— 那是从接口文档推断的，没考虑权限。**
---

# P2 数据部分：外部依赖与凭据清单

> 回答两个问题：**P2 剩下的四项数据任务各需要什么外部依赖**、**要配哪些 key**。
> 部署侧的环境变量名见 [deploy-config.md](deploy-config.md)；任务状态见 [TASKS.md](../TASKS.md)。

---

## 一句话结论

**真正的凭据只有一个：`TUSHARE_TOKEN`。** 它不填，P2 的四项一项都验不了。
其余要么**已有槽位待填**（`ALPHA_VANTAGE_KEY`）、要么**免费无需 key**（yahoo / eastmoney /
xueqiu / juchao）、要么缺的是**代码依赖而不是 key**（mootdx 缺 Go SDK）。

而且**「跨境」那一半其实已经做完了** —— 见下表。

---

## 一、现状：registry 里已接线 9 个 adapter

注册点 `cmd/data/registry_init.go`，全部在启动期注册；缺依赖的注册为
**disabled**（降级运行而不是启动失败），所以「没配 key」不会让服务起不来，
只会让 Fetch 时那条链整条走空。

| adapter | 数据类型 | 凭据 | 现状 |
|---|---|---|---|
| `tushare` | `ohlcv_daily` / `fundamentals` | **`TUSHARE_TOKEN`** | 已接线；token 空 → disabled |
| `eastmoney` | `capital_flow` | 无 | 默认启用 |
| `eastmoney_sectors` | `sectors` / `stock_sector` | 无 | 默认启用 |
| `eastmoney_toplist` | `top_list` / `limit_up_pool` | 无 | 默认启用 |
| `juchao` | `announcements` | 无 | 默认启用 |
| `xueqiu` | `news` / `hot_search` | 无 | 默认启用 |
| `yahoo_finance` | `global_ohlcv` | 无 | 默认启用 —— **跨境 primary** |
| `alpha_vantage` | `global_ohlcv` | `ALPHA_VANTAGE_KEY` | 已接线；无 key → disabled —— **跨境 secondary** |
| `mootdx` | `realtime_quote` / `ohlcv_minute` | 无 key，**缺 SDK** | 恒 disabled（go.mod 里没有 `go-mootdx`） |

**未接线**：`pkg/data/source/hkex`（港股源）—— 代码完整、零调用方，登记在
`internal/repoguard` 的 `unwiredPackages` 白名单里。要港股行情先接它，不用新 key。

每个 adapter 都有独立 kill switch（`DATA_DISABLE_*`，见 `registry_init.go` 的
`envDisabled`），不需要改代码就能单独关掉一个源。

---

## 二、四项 P2 各需要什么

### P2-1 补宏观 / 跨境数据源 — **跨境已完成，宏观是真空白**

- **跨境（美股等境外行情）**：**已做完**。链已配
  `global_ohlcv: [yahoo_finance, alpha_vantage]`（yahoo 免 key 为主、alpha 为辅）。
  登记里「新建 adapter」这个说法对跨境这一半**已经过时**。
- **宏观（汇率 / 利率 / 大宗）**：**真空白**。`pkg/data/source/adapter.go` 的
  DataType 常量里只有行情/资金流/财务/板块/公告/新闻，**没有任何宏观类型**。
  所以宏观要新增 DataType 常量 + adapter，不是填个 key 就能用。

候选源与代价：

| 源 | 覆盖 | 凭据 | 代价 |
|---|---|---|---|
| Tushare 宏观接口（`cn_cpi` 一类） | 国内宏观 | **复用 `TUSHARE_TOKEN`** | 0 个新 key，但吃积分 |
| Alpha Vantage | FX / 大宗 / 美债收益率 | **复用 `ALPHA_VANTAGE_KEY`** 槽位 | 0 个新 key；**免费档 25 req/day**（历史上 500→100→25 一路收紧），只够探测，不够批量 |
| FRED（美联储） | 美国利率 / 宏观 | 免费注册拿 key | 1 个新 key，免费 |
| World Bank | 跨境宏观 | 无 | 0 个新 key，免费 |

**结论**：选 Tushare 路线 → **新增 0 个 key**；选 FRED → 新增 1 个免费 key。

### P2-2 产业链数据底座 `query_supply_chain(name)` — **新增 0 个 key**

ADR-023 要的是「公司-环节归属 + 上下游映射 + 传导指标」，**没有哪个公开 API 直接
给这个**（给的都是行业标签，而 ADR-023 明确说「最有价值的形态是传导信号，而非行业
标签」）。所以它是**构建**任务不是**接入**任务：

- 骨架可复用已有的 tushare 行业分类 + eastmoney 板块（两个源都已在链上）
- 环节/上下游关系可由 AI 生成草案再人工审 —— 那只用到**已存在的 `AI_API_KEY`**
- 因此**不产生任何新凭据**

### P2-8 复权口径 hfq 对照 — **已换成 daily + adj_factor，hfq 落库仍待做**

> ⚠️ **本节已被实测推翻并重写（2026-09-23）。** 原方案写的是「同一个
> `stk_factor_pro` 多请求 4 个 `*_hfq` 字段即可」—— 那是**基于接口文档推断的，
> 没有考虑权限**。真跑起来返回 **40203「没有接口(stk_factor_pro)访问权限」**，
> 行情一只都进不来（同步 job 100% 失败）。**别再按旧方案动手。**

实测的权限边界（容器内 curl，同一个 token）：

| 接口 | 结果 |
|---|---|
| `daily`（不复权行情） | ✅ |
| `adj_factor`（复权因子） | ✅ |
| `stock_basic` | ✅ |
| `stk_factor` / `stk_factor_pro` | ❌ **40203 无权限** |

**40203 ≠ token 无效**（那才是 40101）。token 是好的，`stock_basic` 同步成功
（5907 只）就是证据 —— 只是那个专业版接口没开通。

**现已改为 `daily` + `adj_factor` 自己算复权**（`pkg/data/tushare.go`），
口径取自 Tushare 官方 `adj_factor` 接口文档示例 4：

```
前复权价 = 当日价格 × 当日复权因子 / 最新复权因子     (qfq)
后复权价 = 当日价格 × 当日复权因子                   (hfq，基准为上市日)
```

这个改法**比原方案更好**：官方自己说 `stk_factor` 的 qfq 是「从最新交易日往前复权的
**历史快照，数据不更新**」，而 `adj_factor` 是每日更新的因子（盘前 8:30-9:30 入库），
自己算可以避开快照问题。代价是每次取数要两次请求（daily + adj_factor）。

**P2-8 剩下的部分**：qfq 已落 `ohlcv_daily_qfq`（回测口径不变），**hfq 还没落库** ——
表结构只有一套价格列。做 P2-8 时需要加表/加列并**重跑一次同步**；建议先算好再落，
免得多跑一轮全量（`daily` + `adj_factor` 两个都有权限，一次拉取就能同时算出两套价）。

积分（`adj_factor` 官方原文）：**2000 积分起，5000 以上可高频调取** —— 与
`stk_factor`（5000 起）相比门槛低一档。

### P2-13 验证器接真实回测取证 — **新增 0 个 key**

不需要新数据源。两样前置：① 库里有真数据（同一个 `TUSHARE_TOKEN`）；
② 六维里的**因果维**要调模型 → 用到 `AI_API_KEY`。统计/经济/稳健/偏差/冗余五维
是纯计算，不需要任何凭据。

---

## 三、凭据清单（写进 `.env`；模板见仓库根 `.env.example`）

| 变量 | 必需性 | 用途 | 谁读它 | 现状 |
|---|---|---|---|---|
| **`TUSHARE_TOKEN`** | **阻塞项** | A 股行情 / 财务 / 复权因子 | `tushare.token`（只能从 env 注入） | **空** |
| `JWT_SECRET` | 必填，否则拒绝启动 | 鉴权 | `auth.jwt_secret` | 需 `openssl rand -hex 32` |
| `DATABASE_PASSWORD` | 必填 | 数据库（**唯一**一个密码变量） | `database.password` | 默认 `postgres` |
| `AI_API_KEY` + `AI_API_URL` | 可选 | 因果维 / AI 实验员 | `pkg/ai/client.go` 直读 env | 空 |
| `ALPHA_VANTAGE_KEY` | 可选 | 跨境行情 fallback | `alpha_vantage.api_key` | 空 |

读取路径的两条坑（都已修，别踩回去）：

- `tushare.token` **只能**从 env 注入。`config/data-service.yaml` 里留空串是**有意**的：
  早先写字面量 `${TUSHARE_TOKEN}`，而这个仓库没有展开器，于是「未配置」伪装成「配了一
  个内容为 `${TUSHARE_TOKEN}` 的 token」→ 40101 静默失败。
- `ALPHA_VANTAGE_KEY` 同时存在 env 与 viper 两个入口，**viper（配置文件）优先**，
  两者都设且不同时会打 warn（见 `registry_init.go` 的 CR-54 注释）。

---

## 四、缺的不是 key 而是代码依赖

- **mootdx**：需要 Go SDK `github.com/qmaru/go-mootdx` 进 `go.mod`，再加一个真实
  transport 实现（当前传 `nil`）。它是「实时/分钟行情」链上的 primary —— 不接它，
  `realtime_quote` 与 `ohlcv_minute` 两条链实际只有 eastmoney 一个源在跑。
  **这是代码工作量，不是凭据问题。**

---

## 五、待核实（不假装知道）

- `stk_factor_pro`（pro 版）的积分门槛 —— 上文数字取自 `stk_factor` 官方文档，
  pro 版可能不同。**拿真 token 调一次即可确认。**
- 各 Tushare 宏观接口的准确名称与积分要求 —— 本文只确认了 `cn_cpi` 存在
  （doc_id=228），其余**未逐条核对**，动手前查官方文档。
