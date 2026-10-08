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
