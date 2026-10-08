# ADR-029: AI 层对齐 2026 Agent 最佳实践 —— 双轨执行载体、MCP 门面、状态图编排

> **Status**: Proposed
> **Date**: 2026-10-06
> **Category**: Architecture
> **Related**: [ADR-007](adr-007-ai-sandbox.md)（沙箱 · Phase 3 WASM）· [ADR-015](adr-015-ai-agent-architecture.md)（Go-native agents，已弃用）· [ADR-019](adr-019-service-merge-ai-copilot.md) · [ADR-023](adr-023-ai-experimenter-lab.md)（三层模型 · 验证器链 · 校准优于准确）· [ADR-024](adr-024-expression-as-execution-target.md) · [ADR-027](adr-027-modular-decomposition-to-independent-services.md) · [ADR-028](adr-028-tiered-extension-of-strategy-expression-capability.md) · [PRODUCT.md](../PRODUCT.md)
> **Supersedes (部分)**:
> - **ADR-024 的「表达式是唯一执行载体」** —— 改为**双轨**（表达式 + WASM）。但 ADR-024 的**三条理由（起点确定 / 可审阅 / 可复现）与「不做 `plugin.Open`」的裁决全部保留**，见 §2 与 §3
> - **ADR-015**（Go-native agents 作为自主研究层）—— 已由 ODR-046 事实上弃用，本 ADR 正式收口
>
> **Activates**: **ADR-007 Phase 3**（「Optional: WASM sandbox via `wazero`」）—— 从 Optional 转为**轨道 B 的主路径**，收口它与 ADR-024 悬置半年的矛盾（§Context③）
> **Upstream**: 用户（若曦）2026-10-06 提供 [`docs/archive/agent-best-practice-2026-10.md`](../archive/agent-best-practice-2026-10.md) 并授权大规模重设计 + 七项裁决（双轨共存 / 混合模型 / 只 MCP 化 tools 门面 / 外层状态图 + HITL / pgvector 下沉 / 先落 ADR-029）

---

## Context

### 触发点

用户提供了一份 2026 版 Agent 架构最佳实践，核心主张是「**从概率黑魔法到确定性基础设施**」，
并要求据此重设计 AI 部分，提出「底层回测服务可以添加 MCP 接口，或者所有和 AI agent
接触的服务都可以 MCP 化」。

本研究的做法是**先把文档的每条主张与源码现状逐条对照**，而不是照搬。结果分成四类：
**已做对 9 项**（不需要动）、**真缺口 9 项**、**正面冲突 4 项**（已裁决）、
**不适用 2 项**（应拒绝，§9）。

### 实测现状（全部基于源码取证）

**① 「MCP 工具桥」名不副实 —— 全仓零 MCP 协议实现。**

grep `jsonrpc` / `tools/list` / `tools/call` / `modelcontextprotocol` → **唯一命中是一句
测试注释**（`handlers_tools_integration_test.go:267` 里的 `/api/tools/list_factors`）。

实际形态是自定义 REST：`GET /api/tools` 返回 `{tools: []ToolInfo, count: int}`
+ `POST /api/tools/:name` + JWT RBAC + `pkg/tools/sideeffect.go` 副作用分级。

但 [`docs/hermes/config/hermes.yaml:9`](../hermes/config/hermes.yaml) 的注释写的是
「Go 端只通过 **MCP (GET /api/tools)** 暴露工具」，AGENTS.md 写「21 个 MCP 工具」。
**术语与实现不符**，所以「MCP 化」不是扩展，是**从零引入协议**。

**② WASM 沙箱骨架已在，插件协议已定义，但 `wazero` 未入 go.mod。**

`internal/sandbox` 共 **1,653 LOC / 5 个非测试文件**：

```
runner/rlimit_posix.go     142
runner/rlimit_windows.go   200   ← Windows 专门实现
runner/runner.go           508
staticcheck/staticcheck.go 298
wasm/sandbox.go            505
```

[`wasm/sandbox.go:8-16`](../../internal/sandbox/wasm/sandbox.go) 的包注释：

> The production implementation will use **wazero** (a pure-Go WebAssembly runtime);
> **until wazero is added to go.mod**, an `InProcessRuntime` fallback is provided.
> The fallback simulates the WASM memory model in-process (**no real isolation**)
> but exercises the same API surface, so callers can ... **swap in wazero later
> without code changes**.

已定义的插件协议（`wasm/sandbox.go:20-28`）：

```
initialize(params_ptr: i32, params_len: i32) → i32
generate_signals(bars_ptr: i32, bars_len: i32) → i64
```

沙箱能力已有测试覆盖：内存上限（`MemoryLimitExceeded`）、超时（`Timeout` /
`ParentContextCancel`）、越界读保护（`ReadOutOfBounds`）、编译缓存复用（`CompileReuse`）。
`go.mod` 里 grep `wazero|wasmtime|wasmer|extism` → **零命中**。

**③ ADR-007（Accepted）与 ADR-024 矛盾，悬置半年未收口。**

[ADR-007](adr-007-ai-sandbox.md) 状态 **Accepted**（2026-06-11），Phase 3 写
「Optional: WASM sandbox via `wazero` for stronger isolation」，其 Context 明确针对
「AI Evolution Layer generates Go strategy code via LLM and **compiles+runs it**」。
而 ADR-024（2026-09）裁定「LLM 生成的 Go 代码只是 artifact，**不加载、不执行**」。

**两者矛盾但都活着，ADR-024 从未 supersede ADR-007 Phase 3。** 本 ADR 收口这条。

**④ 模型是本地 8B，隐私优先。**

[`hermes.yaml:17-29`](../hermes/config/hermes.yaml)：`provider: ollama` /
`model: hermes-3:8b`，注释「隐私优先：本地 Ollama 部署（**因子公式 / 回测数据不离开本机**）」；
可切云端 `together` / `nous-hermes-3-70b`。`temperature: 0.3`（「因子挖掘需要稳定性
而非创造性」）。

**⑤ 21 个工具全量注入，无动态装载。**

`Registry.List() []ToolInfo`（`pkg/tools/registry.go:76`）返回全部；`GET /api/tools`
**无任何过滤参数**（`handlers_tools_test.go:148-168` 断言的就是「全量返回 + count」）。
配合 ④ 的 8B 模型，context 膨胀是**实打实的伤害**，不是不优雅。

**⑥ 工具命名不统一 —— 9 个点分 vs 12 个下划线。**

| 风格 | 工具 |
|---|---|
| 点分（9） | `backtest.run` `data.ohlcv` `data.stocks` `data.fundamentals` `factor.compute` `factor.evaluate` `factor.hypothesis` `research.profile` `strategy.list` `strategy.get` `monitor.strategy_health` |
| 下划线（12） | `validate_factor` `compute_factor_ic` `list_factors` `list_strategies` `save_factor` `save_strategy` `get_strategy_lineage` `get_market_regime` `summarize_backtest` `walk_forward_validate` |

命名不统一会**直接降低 Skill-RAG 的意图检索质量**（§6）。

**⑦ 两个循环控制器，都不是状态图，且互不相通。**

| | `pkg/ai/loop.Controller` | Hermes |
|---|---|---|
| 位置 | Go 侧 | 外部进程 |
| 提议器 | **`RandomProposer` / `TPEProposer` —— 纯算法，不是 LLM** | LLM |
| 循环控制 | `Config` + `StopReason` + `Stop()` | `max_iterations: 50` |
| 暴露方式 | **只经 `cmd/analysis/handlers_explore.go` 的 REST，不在 tools 里** | `/api/tools/*` |

后果：**Hermes 用不到 Go 侧已写好的 TPE 贝叶斯优化循环**。ADR-023 说「AI 实验员通过
MCP 工具桥操作底座」，但底座里那个确定性优化器**不在工具桥上**。

**⑧ 无 HITL 中断-恢复原语。**

只有**自动**门禁（L1–L4）+ 两层预算 cap。ADR-023 明确 L3 是「唯一需要人在环」的层、
实验室主任职责是「给方向、批阅、拍板」，但**没有「持久化状态 → 等人批准 → 恢复」的机制**。

**⑨ 可观测性分裂在两端，有 rid 无 span。**

Go 侧 `pkg/observability` 有：prometheus `Metrics`（`ObserveBacktest` / `ObserveHTTP` /
**`AddLLMTokens(provider, model, tokens)`** / `SetCacheHitRatio`）+
**`RequestIDMiddleware` / `RequestIDFromContext` / `HTTPTransport.RoundTrip` /
`WithRequestIDHeader`** —— **request-id 已跨服务传播**。但**无 span / trace 树**。
Hermes 侧另有 `trajectory: true`（4 层记忆的 L2 Episodic）—— **两套，互不相通**。

**⑩ 向量存储在 chromadb（Hermes 端），与 Postgres 的 `research.*` 分离。**

`hermes.yaml:45-48`：`vector_store: chromadb` / `vector_dim: 768` / all-MiniLM-L6-v2。
而 `research.{profile,conclusion,question}` 在 Postgres。**同一批研究资产的两个投影
住在两个存储里**，违反 AGENTS.md §9「绝不让同一份数据在两个位置各自存储」。

**⑪ 产业链图谱（GraphRAG 的等价物）未落地。**

ADR-023 已把 EquityDeep 降为数据底座并拆出「产业链图谱（公司-环节归属 / 上下游映射 /
传导指标，可当因子）」，但 `research.*` **只有 profile / conclusion / question 三张表，
无图谱表**。

**⑫ `pkg/ai/prompts` 不存在。**

AGENTS.md §3 目录树列了 `prompts/ # LLM 提示模板`，实测**无此目录或无 `.go` 文件**。
prompt 实际在 `docs/hermes/prompts/quant-research.md`（Hermes 端 markdown）。
这是 AGENTS.md 漂移，顺带修（§10）。

**⑬ 编译链路已完整就位，且 fail-closed。**

[`pkg/strategy/copilot.go`](../../pkg/strategy/copilot.go)：

```go
codeChecker   CodeChecker    // regex-based staticcheck 门禁，nil → run() fails closed
buildExecutor BuildExecutor  // 进程隔离沙箱 runner，nil → fails closed at build step

type BuildExecutor interface {
    Run(ctx context.Context, name string, args []string, workingDir string) (stdout, stderr *bytes.Buffer, err error)
    IsTimeout(err error) bool
}
```

接线在 `cmd/analysis/main.go`(×6) + `setup.go:457`。`WorkingDir` 必须含 `go.mod`
（注释明确「service does NOT auto-detect this — callers MUST set it explicitly」）。
**三个成功率计数器已在跟踪**：

```go
generated  int64 // total generated (LLM called)
buildable  int64 // build succeeded
backtested int64 // backtest produced valid result (≥1 trade)
```

**所以从「真编译校验但不执行」到「编译成 WASM 并执行」，架构增量很小**：
改 build target（`GOOS=wasip1 GOARCH=wasm`，`BuildExecutor.Run` 是通用命令执行器，
改 target 只是改 `args`）+ 加 wazero 依赖 + 改插件协议。

**⑭ 已定义的 WASM 协议有前视漏洞。**

`generate_signals(bars_ptr, bars_len)` 是 host **一次性把全部 bars 写进线性内存**。
沙箱内代码只要读 `bars[len-1]` 就拿到最后一根 —— **回测时那就是未来数据**。

ADR-028 §10 的前视维靠 AST 递归推导 `lookback`；但 WASM 代码图灵完备，
**静态分析不可判定**（停机问题）。所以轨道 B 必须换机制，见 §3。

**⑮ 对照文档，项目已做对 9 项（其中 1 项比文档更强）。**

| 文档主张 | 项目现状 |
|---|---|
| 避免「用 LLM 监控 LLM」，安全逻辑写成确定性节点 | ADR-023 验证器链 **5 维纯计算 + 1 维因果**；ADR-028 再加 **3 维纯计算** |
| 抛弃「共享聊天室」多 Agent | ADR-023 **明确拒绝多 agent 投票与 arbitrator 仲裁** |
| 子 Agent 派发、被调用不自主循环 | ADR-023 验证器 subagent「**被调用、不自主循环**」 |
| 静态确定性沙箱拦截（AST / 语法校验） | `internal/sandbox/staticcheck` 298 LOC + ADR-024 真编译校验 |
| Postgres 统一下沉，不用纯向量 DB | Go 侧全 Postgres，无独立向量库（**但 ⑩ 显示 Hermes 端有 chromadb**） |
| 小模型而非大模型微调 | 本地 `hermes-3:8b` |
| Token 计费/计量 | `AddLLMTokens(provider, model, tokens)` **已实现** |
| 防自主循环累积成本 | **两层预算模型**：session hard cap（$5 / 50 iter / 200 calls）+ per-skill soft default |
| Code-as-a-Tool 的沙箱基础设施 | `internal/sandbox` 1,653 LOC，**含 Windows rlimit 200 行** |
| **（文档没有）** | **ADR-023「目标是可校准，不是准确」** —— 考核校准误差而非推荐准确率。文档全篇讲确定性/SLA/可观测，**未涉及信心校准**。对量化研究这更本质，因为校准良好的概率才能用于配仓位。**本 ADR 保留并强化它，不被文档带偏** |

**结论：主线已对齐，不需要推倒重来。** 本 ADR 是**补缺口 + 收口矛盾**，不是重写。

---

## Decision

### 1. 判据继承

ADR-023 的三条不可动摇：**编排者只有一个** · **能力层拦住 AI 直接摸数据** ·
**校准优于准确**。ADR-024 的三条理由同样保留：**起点确定 · 可审阅 · 可复现**。

本 ADR 的每一项设计都要回答「它如何满足这三条」。§3 的 host API 契约与 §7 的
敏感边界是主要答案。

### 2. 双轨执行载体（用户裁决）

| | **轨道 A：表达式** | **轨道 B：WASM** |
|---|---|---|
| 载体 | expression AST | wasip1 字节码 |
| 适用 | 横截面因子选股、均线/动量/波动率、能用现有 28 个运算表达的 | price action 形态、路径依赖、自定义递推、多序列复杂逻辑、线性离散状态方程 |
| 前视保证 | **AST 静态推导 `lookback`**（ADR-028 §10） | **host API 能力隔离**（§3）—— 物理上拿不到未来数据 |
| 可审阅性 | **高**（一眼看懂） | 中（源码可审，字节码不可读） |
| 递推能力 | 限 L2 算子集 | **任意**（可自己写循环与状态） |
| 生成模型 | 本地 8B 够用 | **云端 70B**（§7） |
| 复现性 | AST + 数据 → 唯一结果 | wazero 确定性 runtime（无 IO / 无时钟 / 无随机源，除非 host 提供） |

**判定规则（可机械执行，避免「什么都走 WASM」）**：

```
默认走轨道 A。
仅当以下两条同时成立才允许轨道 B：
  (1) 意图属于轨道 B 的适用域（price action / 路径依赖 / 自定义递推 / 状态方程）
  (2) 轨道 A 已证明无法表达 —— 即表达式 parse 失败，或需要 ADR-028 附录 B
      标记为「硬缺口」的能力（EWMA/RMA/IIR、OBV 等累积量、最大回撤、
      任意长度 streak、线性状态空间）
```

理由：**轨道 A 的可审阅性与静态前视检查更强、8B 模型成功率更高、成本更低**。
轨道 B 是为轨道 A 的真实能力缺口（ADR-028 附录 B 已量化）准备的，不是默认选项。

**ADR-024 的「不做 `plugin.Open`」裁决完整保留** —— wazero 是纯 Go、无 CGO、
**支持 Windows**，恰好绕开 ADR-024 拒绝 plugin 的核心理由（Windows 不支持
`-buildmode=plugin`）。`rlimit_windows.go` 200 行也证明沙箱这条路早已解决 Windows 兼容。

### 3. WASM host API 契约 —— 前视隔离是唯一可行机制

**核心原则：不靠审阅代码保证安全，靠能力隔离。** WASM 实例只能调用宿主显式导入的
函数；**没有 import 的能力它物理上做不到**。

#### 3.1 修正现有协议的前视漏洞（⑭）

```
✗ 现有：generate_signals(bars_ptr: i32, bars_len: i32) → i64
        host 一次性写入全部 bars → 代码可读 bars[len-1] = 未来数据

✓ 改为：on_bar(t: i32) → i64
        host 逐 bar 调用；数据只能通过 get_bar(t_offset ≤ 0) 取
```

#### 3.2 host → wasm 导入函数（白名单，**这是全部的对外能力**）

```
get_bar(t_offset: i32, field_id: i32) → f64
    // t_offset > 0 直接 trap。field_id 解析走 SeriesSpec 注册表（ADR-028 §5）
get_series_len() → i32                     // 到当前 bar 为止的长度
get_symbol_count() → i32
get_cross_section(t_offset: i32, field_id: i32, idx: i32) → f64
    // 同日横截面，t_offset > 0 trap。让 WASM 也能做 cs_rank 类计算
get_state(key_ptr: i32, key_len: i32) → i64            // 持久化状态读（ptr|len 打包）
set_state(key_ptr, key_len, val_ptr, val_len) → void   // 持久化状态写
emit_signal(symbol_idx: i32, action: i32, strength: f64) → void
    // 逐条发信号，host 可即时校验（副作用分级），而非返回一个大 JSON
log(level: i32, msg_ptr: i32, msg_len: i32) → void
```

**刻意不提供的能力**（缺这些正是安全性的来源）：
文件系统 · 网络 · 时钟 · 随机源 · goroutine/线程 · 进程 · 环境变量。
需要随机数（如蒙特卡洛）时由 host 注入**带种子的确定性 PRNG**，保证可复现。

#### 3.3 wasm → host 导出函数

```
initialize(params_ptr: i32, params_len: i32) → i32    // 保留现有
on_bar(t: i32) → i64                                   // 替代 generate_signals
finalize() → i64                                       // 收尾，冲刷状态
```

#### 3.4 这套契约如何满足 ADR-024 的三条理由

| ADR-024 理由 | 轨道 B 如何满足 |
|---|---|
| **起点确定** | host imports 是**封闭白名单**；`initialize` 的 params 是结构化 JSON；无 IO / 无时钟 / 无随机源 |
| **可审阅** | 审的是**源码 + host API 使用清单**（可静态扫描 wasm 的 import section 得出「这个策略用了哪些能力」），不是字节码 |
| **可复现** | wazero 确定性执行 + host 注入带种子 PRNG + `get_state`/`set_state` 由 host 管理版本 |

**前视保证从「静态可判定」升级为「物理不可能」** —— 这比 ADR-028 §10 的 AST 推导**更强**：
AST 推导依赖算子声明正确，host 隔离不依赖代码写对。

#### 3.5 与 ADR-028 §7 三路一致性的关系

`get_state` / `set_state` 让**状态由 host 管理**，所以：

- 回测（`Batch`）：host 在内存里维护状态，跑完全序列
- 实盘（`Step`）：host 每 bar 调 `on_bar`，状态**序列化持久化**，重启后恢复
- 一致性属性测试：`Batch ≡ Step从零 ≡ Step从持久化恢复`（ADR-028 §7 的三路，原样适用）

### 4. MCP 化：只换 tools 门面的协议（用户裁决）

**范围**：回测服务与 ai-service 的 `/api/tools/*` 从自定义 REST 换成
**真 MCP（JSON-RPC 2.0）**。

**明确不做**：data-service / 策略服务 / reduction-service **不向 Hermes 暴露 MCP**。
它们可以有 MCP Server 供**内部服务间**挂载，但 Hermes 不得直挂。

理由：**协议形态与访问授权是正交的两件事**。MCP 化改的是协议（让工具能被任何 MCP
客户端挂载、跨 Agent 复用），不改「谁能调谁」。ADR-023「能力层存在的意义是拦住 AI
直接读库/读文件」与 ADR-027 §2.6.2「Hermes 只面对 2 个 tools endpoint」**完整保留**。

**保留的既有资产**（这些比协议更重要，不因换协议而丢）：
`pkg/tools/sideeffect.go` 的**副作用分级 fail-closed** · per-tool RBAC ·
`Tool` 接口 + `Registry` + `ToolInfo`。MCP 只是它们外面的一层协议适配。

### 5. 外层确定性状态图 + 节点内有界 Agent（用户裁决）

文档的「终局架构」正好解决 ⑦（两个循环控制器互不相通）与 ⑧（无 HITL）。

```
                    ┌─────────────────────────────────────────────┐
                    │  实验生命周期状态图（确定性 · 状态持久化）    │
                    └─────────────────────────────────────────────┘

  IDLE ──► PROPOSE ──► BACKTEST ──► VALIDATE ──► GATE ──► [HITL?] ──► RECORD ──► ARCHIVE
             │            │            │           │          │           │
          有界 Agent    确定性       确定性      确定性     中断点      确定性
         （迭代上限）                                    （持久化等批准）
             │
             └── 或确定性替代：loop.Controller（TPE / Random）
                                        ▲
                                        └── ⑦ 的修复：TPE 成为 PROPOSE 节点的一个实现
```

| 节点 | 类型 | 说明 |
|---|---|---|
| `PROPOSE` | **有界 Agent** 或 **确定性** | Agent 路径：Hermes 生成假设，**有迭代上限**；确定性路径：复用 `loop.Controller` 的 `TPEProposer` / `RandomProposer`（参数搜索不需要 LLM） |
| `BACKTEST` | 确定性 | 调 `BacktestRunner`（ADR-027 图 B 的 ② 通道） |
| `VALIDATE` | 确定性 | ADR-023 的 5 维 + ADR-028 的 3 维，**全部纯计算** |
| `GATE` | 确定性 | 现有 L1–L4 门禁 |
| **`HITL`** | **中断点** | **状态完整持久化 → 等实验室主任批准 → 恢复**。触发条件可配（验证器通过 / 要花大预算 / 要写入正式因子库 / 轨道 B 代码首次上线） |
| `RECORD` | 确定性 | 写 `ai.experiments`（ADR-027 §Context⑪ 的 experiment DTO） |

**关键设计取舍**：

- **不引入 LangGraph / Temporal** —— 单人自托管（AGENTS.md §1），新增编排引擎是纯负担。
  状态图用 Go 实现 + 状态持久化到 `ai.experiment_runs`（B 类，归 ai-service）
- **`loop.Controller` 复用而非重写** —— 它已经是「确定性提议 + 循环控制 + `StopReason`」，
  正是 `PROPOSE` 节点的确定性实现。**⑦ 的修复就是把它接进状态图**，顺便让它经 tools 门面暴露
- **Hermes 仍是唯一编排者**（ADR-023 不动）—— 状态图不是第二个编排者，它是
  **Hermes 运行的轨道**：Hermes 决定「下一步做什么」，状态图保证「步骤顺序与门禁不可跳过」
- **`HITL` 补上 ADR-023 缺的机制** —— 三角色里「实验室主任批阅拍板」终于有了落点，
  而输入正是 ADR-023 要的「概率估计 + 质疑清单」

### 6. Skill-RAG 动态装载 + 向量下沉 pgvector（用户裁决）

**问题**：⑤ 全量注入 21 个工具 × ④ 8B 模型 = context 膨胀伤害指令遵循。

**方案**：

1. **工具元数据进 Postgres** `ai.tool_catalog`：
   `name · description · description_embedding · side_effect_level · rbac_scope · category · track(A/B)`
2. **检索 = 向量相似度 + SQL 过滤，在同一条查询里**：

```sql
SELECT name, description FROM ai.tool_catalog
WHERE side_effect_level <= $1          -- 副作用分级 fail-closed
  AND $2 = ANY(rbac_scope)             -- per-tool RBAC
  AND track = ANY($3)                  -- 轨道过滤
ORDER BY description_embedding <=> $4  -- pgvector 余弦距离
LIMIT 5;
```

**这是 pgvector 相对 chromadb 的关键优势**：元数据过滤与向量检索**在同一个事务里**，
所以 RBAC 与副作用分级**不可能被绕过**。chromadb 要先向量检索再过滤（两步，有
TOCTOU 风险），或者过滤不了。

3. **命名统一为点分 `<domain>.<verb>`**（修 ⑥）—— 12 个下划线工具改名。
   这不只是美观：**检索质量依赖命名一致性**，`save_factor` 与 `factor.compute`
   在 embedding 空间里会被拉远
4. **chromadb 退役**（修 ⑩）—— 因子 / 研究洞察 / Skill 的向量全部进 pgvector，
   与 `research.*` 同库，符合 AGENTS.md「同一份数据不存两处」
5. Hermes 端 4 层记忆的 **L3 Semantic** 改为查 Go 侧 pgvector，
   **L2 Episodic（trajectory）留在 Hermes 端**（它是 Agent 私有状态，不是研究资产）

**pgvector 是新依赖**，属 AGENTS.md「Ask First」项 —— 但它是 **Postgres 扩展**，
不是新服务，且替换掉一个已有的 chromadb，**净基础设施数量不变**。

### 7. 混合模型：代码上云，数据不出机（用户裁决）

⚠️ **这推翻了 `hermes.yaml:18` 的原始隐私前提**（「因子公式 / 回测数据不离开本机」）。
新边界必须精确，否则「混合」会变成「什么都上云」。

**判据：上云的是「如何算」，留在本机的是「算出什么」。**

| | 云端 70B（`nous-hermes-3-70b`） | 本地 8B（`hermes-3:8b`） |
|---|---|---|
| **职责** | **只做轨道 B 的代码生成与修复** | 编排、轨道 A 表达式生成、结果解读、下一步决策 |
| **输入** | 意图描述 · host API 清单（§3.2）· SeriesSpec 序列名 · **脱敏后的编译错误** | 回测结果的**摘要统计量** · 验证器输出 · 因子假设 |
| **输出** | wasip1 源码 | 表达式 / 决策 / 概率估计 |
| **绝不给** | 行情数据 · 基本面数据 · 持仓 · 回测结果数值 · 标的池 · 账户信息 | —（本机，无限制） |

**三个必须配套的防护**：

1. **编译错误脱敏** —— `go build` 的 stderr 可能含变量名、常量值、文件路径
   （`copilot.go:98` 就记录过一个硬编码 `/Users/ruoxi/...` 路径的历史 bug）。
   上云前必须过一遍脱敏：只保留错误码、行号、类型不匹配的结构信息，
   **剥离标识符字面值与绝对路径**
2. **出站白名单** —— 云端调用的 payload 只允许 §7 表里「输入」那一栏的字段类型，
   由 `sideeffect.go` 同级的出站检查强制（fail-closed）
3. **代码回流校验** —— 云端返回的代码**必须过本地 `codeChecker`（staticcheck）
   + `buildExecutor` 编译**才可用。云端不可信，**它是生成器不是权威**

**为什么值得冒这个隐私变化**：⑬ 显示项目**已在跟踪 `generated` / `buildable` /
`backtested`**。native Go 编译成功率是 WASM 成功率的**上界**（wasip1 约束更严：
无 goroutine 自由、无 syscall、无 net）。**若 native 成功率已经不高，8B 走轨道 B
基本不可行** —— 这是选混合模型的实证理由，不是猜测。

**配套动作**：把三个计数器**落库**（现在只是内存 `atomic.AddInt64`），
按模型分维度统计，作为「哪个模型走哪条轨道」的持续依据。

### 8. 可观测性统一（修 ⑨）

| 层 | 现状 | 目标 |
|---|---|---|
| **rid 传播** | ✅ 已有（`RequestIDMiddleware` + `HTTPTransport` + `WithRequestIDHeader`） | 保留，作为 trace id |
| **span / trace 树** | ❌ 无 | 在 rid 之上加轻量 span（**不引入 OpenTelemetry 全家桶**，单人自托管） |
| **全量日志** | 分裂：Go 侧 zerolog / Hermes 侧 `trajectory` | **异步落库** `ai.experiment_trace`（B 类，ai-service 私有） |
| **主 Agent 读什么** | 无约束 | **只读结构化摘要**（状态 / 交付物指针 / 高概括），全量日志仅供人类 Debug |

这正是文档「可观测性解耦」的主张，且对量化研究特别关键：**一次回测输出可能几万行，
塞进 8B 的 context 就爆了**。ADR-023 的验证器输出「概率估计 + 质疑清单」本来就是
结构化摘要形态，与本节天然契合。

`AddLLMTokens(provider, model, tokens)` 已实现 —— 按 §7 的双模型分维度计量，
即可支撑「代码上云的成本」独立核算。

### 9. 明确不做的事

| 不做 | 理由 | 重评触发条件 |
|---|---|---|
| **API Gateway / LiteLLM Proxy**（文档主张的网关化：计费/限流/DLP/流式缓存） | AGENTS.md §1 + ADR-027 §7「多租户/RBAC/计费/限流/高可用都是纯负担，可砍」。**且预算控制已在 `hermes.yaml` 两层模型、Token 计量已在 `AddLLMTokens`** —— 网关要解决的问题项目已用更轻的方式解决 | 出现多用户或多 Agent 并发抢预算时 |
| **DSPy 自动 Prompt 优化** | DSPy 是 Python 生态，引入 = 加一个 Python 服务；且 prompt 主权在 Hermes 端（`hermes.yaml:10`「Hermes 是唯一工作流存储」），Go 侧 `pkg/ai/prompts` **根本不存在**（⑫）。更根本的：DSPy 优化目标通常是准确率，而 ADR-023 的考核指标是**校准误差** | 若 prompt 迁回 Go 侧且积累了「prompt 版本 ↔ 校准误差」的样本 |
| **全服务 MCP 化 / Hermes 直挂 data-service** | 直接推翻 ADR-023 核心命题与 ADR-027 §2.6.2。见 §4 | — |
| **多 agent 投票 / arbitrator 仲裁 / 共享聊天室** | ADR-023 已明确拒绝（错误相关放大偏差、仲裁层不可解释）；文档同样反对共享聊天室。**两边一致，无需重评** | — |
| **语义缓存（Semantic Cache）** | 文档自己已否定（模糊语义匹配易致脏数据与安全漏洞）。项目的 `factor_cache` 是**精确键**缓存，不是语义缓存，保持原样 | — |
| **LangGraph / Temporal 等编排引擎** | 单人自托管，新增基础设施是纯负担。状态图用 Go 实现 + PG 持久化足够（§5） | 状态图复杂度超出单文件可实现范围时 |
| **模型微调 / SFT** | 文档主张退化为蒸馏；本项目连蒸馏都不做 —— 轨道分工（§7）已解决质量问题，训练数据集与流程对单人实验室投入过大 | 轨道 B 编译成功率长期低于阈值且换模型无效时 |
| **把 `InProcessRuntime` 当生产实现** | 它**无真隔离**（包注释自陈），只是 API surface 的 fallback。轨道 B 上线前必须换 wazero | — |

### 10. 文档一致性收口

本 ADR 落地时须同步（AGENTS.md Rule 1 / Rule 4）：

| 文档 | 修什么 |
|---|---|
| **ADR-024** | 标 **Superseded (部分) by ADR-029**：「表达式是唯一执行载体」改为双轨；**三条理由与「不做 `plugin.Open`」保留** |
| **ADR-007** | Phase 3 从 **Optional** 转为**轨道 B 主路径**；补注「与 ADR-024 的矛盾由 ADR-029 收口」 |
| **ADR-028** | 定位调整为**轨道 A 的能力扩展**；§10 前视维补「轨道 B 走 host API 隔离」；§11 的「L3 WASM 明确不做」**改为指向本 ADR §2**（成本估算原判断有误，见 §Context②⑬） |
| **ADR-027** | observability 说法精确化（**有 rid 传播，无 span**，不是「只有 metrics」）；§2.6 补 MCP 协议化范围；图 B 的 tools 门面标注 MCP |
| **AGENTS.md** | §2「21 个 MCP 工具」→ 说明协议化是本 ADR 的动作；§3 目录树删掉不存在的 `pkg/ai/prompts`（⑫）；§11 ADR 数 24 → 29；§14 Known Issues 补「WASM 沙箱是 fallback 无真隔离」 |
| **`hermes.yaml`** | 第 9 行「通过 MCP (GET /api/tools)」的表述在 §4 落地后才成立；`vector_store: chromadb` 随 §6 改为 pgvector；隐私前提注释随 §7 更新 |

---

## Consequences

### 正面

- **收口两处悬置矛盾** —— ADR-007 Phase 3 与 ADR-024 的半年矛盾（③）；
  「MCP」名不副实（①）
- **ADR-028 附录 B 的 5 类硬缺口全部可解** —— EWMA/RMA/IIR、OBV 等累积量、
  最大回撤、任意长度 streak、线性状态空间，轨道 B 都能写，**不受算子集限制**
- **前视保证变强** —— 轨道 B 的 host API 隔离让前视**物理上不可能**，
  比 AST 静态推导更可靠（不依赖算子声明写对）
- **8B 模型的 context 压力下降** —— Skill-RAG 注入 3–5 个而非 21 个，
  且 pgvector 让 RBAC 与副作用分级**在同一事务内不可绕过**
- **实验室主任的「批阅拍板」有了机制** —— `HITL` 中断-恢复节点（⑧），
  输入正是 ADR-023 要的「概率估计 + 质疑清单」
- **`loop.Controller` 的 TPE 优化器不再闲置** —— 成为 `PROPOSE` 节点的确定性实现（⑦）
- **同一份研究资产不再存两处** —— chromadb 退役，向量进 pgvector 与 `research.*` 同库（⑩）
- **隐私边界比原来更清晰** —— 从模糊的「因子公式不出本机」变成可强制的
  「如何算可上云 / 算出什么不出机」+ 出站白名单 + 错误脱敏 + 回流校验（§7）
- **复用既有资产，增量小** —— 编译链路 fail-closed 已就位（⑬）、
  WASM 骨架 505 行 + 沙箱测试全套已就位（②）、rid 传播已就位（⑨）、
  预算两层模型已就位、`sideeffect.go` 副作用分级已就位
- **不违反 ADR-023** —— 编排者仍只有一个（状态图是 Hermes 的轨道，不是第二个编排者）；
  能力层仍拦住 AI 直接摸数据（§4）；校准优于准确**保留并强化**（§1、§8）

### 代价 / 限制

- **新增两个依赖** —— `wazero`（纯 Go，无 CGO，支持 Windows）+ `pgvector`
  （Postgres 扩展，非新服务）。都属 AGENTS.md「Ask First」项，**已在本 ADR 提出**
- **两套执行载体 = 两套验证路径** —— 轨道 A 走 AST 静态检查，轨道 B 走 host API 隔离
  + import section 扫描。验证器链要同时支持两者，`pkg/validation` 复杂度上升
- **WASM 协议变更是破坏性的** —— `generate_signals(bars_ptr, bars_len)` →
  `on_bar(t)`。现有 `InProcessRuntime` 的测试全部要改
- **双模型运维复杂度** —— 本地 Ollama + 云端 Together 两套凭据、两套超时、
  两套成本核算。且云端不可用时轨道 B **整体不可用**（需 fail-closed 而非静默降级到 8B）
- **隐私边界靠代码强制，不靠约定** —— §7 的三个防护（错误脱敏 / 出站白名单 /
  回流校验）任一条实现有漏，就会静默泄露数据。**需要专门的红队测试**
- **状态图 + 状态持久化是新代码** —— `ai.experiment_runs` 表 + 中断恢复语义
  + HITL 通知机制，都是新增
- **Skill-RAG 引入检索不确定性** —— 选错工具集会让 Agent 卡住。需要 fallback：
  检索结果不足时**允许 Agent 显式请求完整清单**（而非静默失败）
- **工具改名是破坏性的** —— 12 个下划线工具改点分，Hermes 侧 Skill 文档
  （`docs/hermes/skills/autonomous_factor_mining.md`）与验收测试都要同步

### 未解决 / 需后续 ADR

| 项 | 说明 |
|---|---|
| **`buildable/generated` 的实际比率** | §7 的混合模型决策依赖它，但三个计数器**现在只在内存**（`atomic.AddInt64`），未落库。**落地第一步应该是把它们落库并跑一轮真实统计** —— 若 native 成功率已经很低，轨道 B 的推进节奏要重估 |
| **wasip1 的 API 约束清单** | 8B/70B 模型生成的 Go 代码常踩的 wasip1 限制（无 `net`、无 `os/exec`、goroutine 受限、无 cgo）需要整理成**给模型的约束提示**，否则编译失败率会居高不下 |
| **`import section` 静态扫描的实现** | §3.4 说「可审阅 = 审源码 + host API 使用清单」，清单靠扫描 wasm 的 import section 得出。这个扫描器要写，且要能拒绝未申报的 import |
| **产业链图谱（⑪ / GraphRAG）** | ADR-023 已规划但无表。上下游传导指标是 A 股独有价值，且 pgvector 落地后可做实体关系检索。**范围与 schema 需单独 ADR** |
| **状态图的 HITL 通知渠道** | 中断后怎么通知实验室主任？飞书 / 邮件 / 前端红点？单人实验室下最简方案待定 |
| **轨道间的迁移** | 一个轨道 A 的表达式策略随着需求变复杂要迁到轨道 B，有没有自动翻译路径（AST → 等价 WASM 源码骨架）？还是重写？ |
| **`InProcessRuntime` 的退役** | 换成 wazero 后，fallback 是删除还是保留作测试用？保留就有「测试通过但生产无隔离」的风险 |

---

## 落地检查清单

Proposed → Accepted 的判据：

- [ ] 用户（实验室主任）批阅双轨判定规则（§2）与 WASM host API 契约（§3）
- [ ] 用户批阅混合模型的敏感边界（§7）—— **这是对原隐私前提的推翻，需明确签字**
- [ ] **`generated` / `buildable` / `backtested` 三个计数器落库**，跑一轮真实统计，
      确认轨道 B 的推进节奏（§未解决 第 1 项）
- [ ] 新依赖获批：`wazero` + `pgvector`（AGENTS.md「Ask First」）
- [ ] WASM 协议改造完成：`generate_signals` → `on_bar`，**前视漏洞（⑭）关闭并有测试**
- [ ] `InProcessRuntime` 换成 wazero，**真隔离生效**，内存/超时/越界测试全绿
- [ ] host API 白名单实现，`t_offset > 0` trap 有测试；`import section` 扫描器能拒绝未申报 import
- [ ] tools 门面 MCP 化（JSON-RPC 2.0），**副作用分级与 RBAC 完整保留**
- [ ] 工具命名统一为点分，Hermes 侧 Skill 文档与验收测试同步
- [ ] `ai.tool_catalog` + pgvector 落地，Skill-RAG 检索**在同一条 SQL 内完成过滤**
- [ ] chromadb 退役，向量全部进 pgvector
- [ ] 状态图落地：`ai.experiment_runs` 表 + 中断恢复 + `HITL` 节点
- [ ] `loop.Controller` 接入状态图作为 `PROPOSE` 的确定性实现，并经 tools 门面暴露
- [ ] §7 三个防护（错误脱敏 / 出站白名单 / 回流校验）实现**并有红队测试**
- [ ] 可观测性：span 加在 rid 之上；全量日志异步落 `ai.experiment_trace`；主 Agent 只读摘要
- [ ] 文档同步 6 项完成（§10 表）：ADR-007 / ADR-024 / ADR-027 / ADR-028 / AGENTS.md / hermes.yaml
- [ ] 创建 ODR 记录本次重设计（Rule 2：变更开发工具或流程 → Tooling/Process）

---

_Date: 2026-10-06_
_Author: AI 实验员（Hermes）+ 实验室主任（若曦），基于 [`agent-best-practice-2026-10.md`](../archive/agent-best-practice-2026-10.md) 的逐条对照_
_Method: 文档主张 × 源码现状四分类 —— 已做对 9 项（不动）/ 真缺口 9 项（B1–B9）/
正面冲突 4 项（C1–C4，已裁决）/ 不适用 2 项（§9 拒绝）_
_Evidence: `internal/sandbox/{wasm/sandbox.go:8-28, runner/rlimit_windows.go, staticcheck/}` ·
`pkg/tools/{registry.go:76, sideeffect.go, builtin/*}`（21 工具名与命名分裂）·
`pkg/strategy/copilot.go:53-56, 80-95, 98-108, 213-221`（编译链路 + 三计数器）·
`pkg/ai/loop/{loop.go, proposer.go}`（TPE/Random 提议器）·
`pkg/observability/metrics.go`（rid 传播 + AddLLMTokens）·
`docs/hermes/config/hermes.yaml:9, 17-29, 45-48, 56-75` ·
`docs/adr/adr-007-ai-sandbox.md`（Accepted, Phase 3）·
全仓 grep：`jsonrpc|tools/list|tools/call|modelcontextprotocol` → 零命中；
`wazero|wasmtime|wasmer|extism` in go.mod → 零命中_
_Supersedes (部分): ADR-024「表达式是唯一执行载体」· ADR-015。Activates: ADR-007 Phase 3_
