// topics.go —— 模块间消息的命名注册表（switchboard 模式）。
//
// ─── 落位修正（对蓝图笔误级设计误差的落地修正，K0 冻结） ──────────
// 蓝图（docs/design/kernel/target-architecture-modular-kernel.md
// §3.1 C3 与 §5.1 消息契约行）写的落位是 pkg/kernel/topics.go。
// 但 Go 的包级依赖是全量的：kernel 依赖 msgbus（Kernel.Bus() 返回
// msgbus.MsgBus），若 topic 注册表放在 pkg/kernel，则任何引用 topic
// 常量的包（含 msgbus 自身的实现与测试）都必须 import kernel，
// 形成 kernel → msgbus → kernel 的导入环。故注册表落位本文件
// （pkg/msgbus/topics.go）。纯落位修正，不改变蓝图任何语义。
//
// ─── 命名规范（冻结） ────────────────────────────────────────────
// topic 命名固定为 <module>.<event>：
//   - 全小写，点分隔；
//   - 段内多词用下划线连接（如 exec.order_intent 的 order_intent）；
//   - <module> 必须是蓝图 §5 模块矩阵中的模块名（或 run/kernel 这类
//     内核级前缀）；
//   - 正则校验：^[a-z]+(\.[a-z_]+)+$，由
//     interfaces_compliance_test.go 的 TestTopicNamingConvention 强制。
//
// 新增/改名 topic = 修改冻结契约，必须走变更评审（蓝图 §5.1：契约
// 先冻结再并行）。payload 结构由各模块接口契约冻结（切片 2），
// 本表只冻结「名字」。
//
// ─── 切片 2 追加（2026-10-08） ────────────────────────────────────
// 本表现在共 16 个常量：切片 1 的 9 个（下表第一组，名与值**不得改动**）
// + 切片 2 追加的 7 个（第二组，服务 data-engine 之外的 6 个业务模块）。
// 只追加，不改既有 9 个常量的名与值。

package msgbus

// K0 切片 1 冻结的 topic 常量。
const (
	// TopicDataBar：bar 到达。发布者 data-engine（回测=快照迭代，
	// 实盘=feed 聚合）；订阅者 strategy-runtime / indicators / exec-algo。
	TopicDataBar = "data.bar"

	// TopicExecOrderIntent：策略 → 风控的订单意图。
	TopicExecOrderIntent = "exec.order_intent"

	// TopicExecFill：成交回报。发布者 exec-engine；订阅者 portfolio /
	// exec-algo / strategy-runtime。
	TopicExecFill = "exec.fill"

	// TopicPortfolioUpdated：持仓/现金/净值更新完成。发布者 portfolio。
	TopicPortfolioUpdated = "portfolio.updated"

	// TopicRiskVerdict：订单前置风控裁决（放行/拒绝）。发布者 risk-engine。
	TopicRiskVerdict = "risk.verdict"

	// TopicRunStart：一次回测/实盘 run 开始。
	TopicRunStart = "run.start"

	// TopicRunDone：一次 run 结束。
	TopicRunDone = "run.done"

	// TopicKernelBoot：内核 Boot 完成。发布者 kernel。
	TopicKernelBoot = "kernel.boot"

	// TopicKernelShutdown：内核开始 Shutdown。发布者 kernel。
	TopicKernelShutdown = "kernel.shutdown"
)

// K0 切片 2 追加的 topic 常量（7 个，服务切片 2 的 7 个业务模块）。
//
// 两条命名裁决（沿用 <module>.<event>，不新增规范）：
//
//  1. TopicRiskOrderVerdict = "risk.order_verdict" **不复用**切片 1 的
//     risk.verdict：后者是通用风控裁决通道（未限定裁决对象，留给盘后 /
//     regime 级裁决），前者专指订单报送前挂点
//     risk.RiskEngine.CheckOrder 的**订单级**裁决（payload = OrderIntent
//     + Verdict）。两者语义层不同，且既有 risk.verdict 的名与值禁止
//     改动，故新增而非改义。
//  2. TopicExecAlgoChildOrder = "execalgo.child_order"：模块名 exec-algo
//     带连字符，而命名规范正则 ^[a-z]+(\.[a-z_]+)+$ 不允许连字符，
//     故按既有先例（data-engine 模块 → "data.bar"）去掉连字符写作
//     execalgo。
const (
	// TopicRiskOrderVerdict：订单前置风控裁决（CheckOrder）。
	// 发布者 risk-engine；订阅者 exec-engine / exec-algo。
	TopicRiskOrderVerdict = "risk.order_verdict"

	// TopicExecOrderRejected：订单被风控拒绝（未报送 broker）。
	// 发布者 exec-engine；订阅者 strategy-runtime / 审计。
	TopicExecOrderRejected = "exec.order_rejected"

	// TopicPortfolioFilled：成交已计入组合（ApplyFill 完成）。
	// 发布者 portfolio。与 exec.fill 的区别：exec.fill 是「成交发生」，
	// portfolio.filled 是「账已记完」——下游只在后者之后读持仓才安全。
	TopicPortfolioFilled = "portfolio.filled"

	// TopicReconDiff：对账发现差异。发布者 exec-engine（Reconciler）。
	TopicReconDiff = "recon.diff"

	// TopicStrategyStateSaved：流式策略状态已落 quant.strategy_state。
	// 发布者 strategy-runtime（BarHandler.SaveState 之后）。
	TopicStrategyStateSaved = "strategy.state_saved"

	// TopicIndicatorWarmupDone：有状态算子预热完成（可开始产出有效值）。
	// 发布者 indicators。
	TopicIndicatorWarmupDone = "indicator.warmup_done"

	// TopicExecAlgoChildOrder：执行算法拆出的子订单。
	// 发布者 exec-algo；订阅者 risk-engine（每片都过风控）/ exec-engine。
	TopicExecAlgoChildOrder = "execalgo.child_order"
)
