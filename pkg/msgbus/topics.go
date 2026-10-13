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
//   - 正则校验：^[a-z]+(\.[a-z_]+)+$，由 topic() 在登记时强制（见下）。
//
// 新增/改名 topic = 修改冻结契约，必须走变更评审（蓝图 §5.1：契约
// 先冻结再并行）。payload 结构由各模块接口契约冻结（切片 2），
// 本表只冻结「名字」。
//
// ─── 切片 2 追加（2026-10-08） ────────────────────────────────────
// 本表现在共 16 个条目：切片 1 的 9 个（下表第一组，名与值**不得改动**）
// + 切片 2 追加的 7 个（第二组，服务 data-engine 之外的 6 个业务模块）。
// 只追加，不改既有 9 个常量的名与值。
//
// ─── K0-P2-3b 收敛（2026-10-13）：声明即登记，一处真相 ─────────────
// 此前 topics.go 的 16 个常量与 registry.go 的 topicSet map 是**两份真相**
// ——加常量忘登记是静默失效（新 topic 被 Publish 拒掉，错误清楚但没人提前
// 知道）。Go 里「常量已声明但忘登记进列表」**无法**成为编译错误（声明与
// 列表是两个语法位点，没有编译期耦合），除非把「声明」本身变成「登记」。
// 故本文件把每个 topic 的声明写成 TopicX = topic("x.y")：topic() 在包
// 初始化期校验命名规范、查重并登记进 registry。由此：
//
//   - 「新增 topic 未登记」**不可能发生**（声明语句就是登记动作），
//     比编译期报错更强；绕过 topic() 写裸字符串属于违规写法，由
//     topics_internal_test.go 的 AST 护栏兜底；
//   - 注册表本体（All 的等价集合）= 下方 registry map，就地一份，
//     IsRegisteredTopic / RegisteredTopics 基于它实现（原 registry.go
//     已删除）。
//
// 常量改为 var 的取舍（必须钉住）：var 与 const 的差异是导出符号可被
// 外部包重新赋值——对冻结契约是理论风险。但换来的是结构性消除「双真相
// 静默失效」（真实发生过的工作方式故障），且篡改 var 会被 IsRegisteredTopic
// 当场拒掉（registry 里的值不随赋值走，Publish 立刻 fail-loud）。
// 名与值本身不变更（16 个、值同 K0 冻结），冻结面由
// interfaces_compliance_test.go 的 frozenTopicValues 继续钉值。
package msgbus

import (
	"regexp"
	"sort"
)

// topicNamePattern 是命名规范的正则（同 interfaces_compliance_test.go
// 的 TestTopicNamingConvention 冻结的那条）。topic() 函数体引用它，Go 的
// 初始化依赖分析会把这算作依赖边（函数体内的引用同样计入），故本声明
// 无论写在哪里都会先于 topic 变量块初始化。
var topicNamePattern = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)

// registry 是全体已注册 topic 的唯一真相（K0-P2-3b）。
//
// 无初始化器是刻意的：零值 nil map 不需要初始化时机，由 topic() 懒建，
// 因此不依赖任何声明顺序、也不存在初始化覆盖问题。注册只发生在包
// 初始化期（单线程），此后只读。
var registry map[string]struct{}

// topic 声明一个 topic 并同时登记（声明即登记）。
//
// 校验在这里而不是等到 Publish/Subscribe：命名违规、重复登记都是**登记
// 期错误**，构造期 panic 把故障钉在离错误最近处（与 K0-P2-3 ③ 的
// NewSyncBus clk 注入 panic 同一裁决）——带着违规 topic 起进程，比在
// 一次运行到一半的 Publish 时才发现要好。
func topic(name string) string {
	if !topicNamePattern.MatchString(name) {
		panic("msgbus: topic 名不符合 ^[a-z]+(\\.[a-z_]+)+$: " + name)
	}
	if registry == nil {
		registry = make(map[string]struct{}, 16)
	}
	if _, dup := registry[name]; dup {
		panic("msgbus: topic 重复登记: " + name)
	}
	registry[name] = struct{}{}
	return name
}

// K0 切片 1 冻结的 topic（声明即登记）。
var (
	// TopicDataBar：bar 到达。发布者 data-engine（回测=快照迭代，
	// 实盘=feed 聚合）；订阅者 strategy-runtime / indicators / exec-algo。
	TopicDataBar = topic("data.bar")

	// TopicExecOrderIntent：策略 → 风控的订单意图。
	TopicExecOrderIntent = topic("exec.order_intent")

	// TopicExecFill：成交回报。发布者 exec-engine；订阅者 portfolio /
	// exec-algo / strategy-runtime。
	TopicExecFill = topic("exec.fill")

	// TopicPortfolioUpdated：持仓/现金/净值更新完成。发布者 portfolio。
	TopicPortfolioUpdated = topic("portfolio.updated")

	// TopicRiskVerdict：订单前置风控裁决（放行/拒绝）。发布者 risk-engine。
	TopicRiskVerdict = topic("risk.verdict")

	// TopicRunStart：一次回测/实盘 run 开始。
	TopicRunStart = topic("run.start")

	// TopicRunDone：一次 run 结束。
	TopicRunDone = topic("run.done")

	// TopicKernelBoot：内核 Boot 完成。发布者 kernel。
	TopicKernelBoot = topic("kernel.boot")

	// TopicKernelShutdown：内核开始 Shutdown。发布者 kernel。
	TopicKernelShutdown = topic("kernel.shutdown")
)

// K0 切片 2 追加的 topic（7 个，服务切片 2 的 7 个业务模块）。
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
var (
	// TopicRiskOrderVerdict：订单前置风控裁决（CheckOrder）。
	// 发布者 risk-engine；订阅者 exec-engine / exec-algo。
	TopicRiskOrderVerdict = topic("risk.order_verdict")

	// TopicExecOrderRejected：订单被风控拒绝（未报送 broker）。
	// 发布者 exec-engine；订阅者 strategy-runtime / 审计。
	TopicExecOrderRejected = topic("exec.order_rejected")

	// TopicPortfolioFilled：成交已计入组合（ApplyFill 完成）。
	// 发布者 portfolio。与 exec.fill 的区别：exec.fill 是「成交发生」，
	// portfolio.filled 是「账已记完」——下游只在后者之后读持仓才安全。
	TopicPortfolioFilled = topic("portfolio.filled")

	// TopicReconDiff：对账发现差异。发布者 exec-engine（Reconciler）。
	TopicReconDiff = topic("recon.diff")

	// TopicStrategyStateSaved：流式策略状态已落 quant.strategy_state。
	// 发布者 strategy-runtime（BarHandler.SaveState 之后）。
	TopicStrategyStateSaved = topic("strategy.state_saved")

	// TopicIndicatorWarmupDone：有状态算子预热完成（可开始产出有效值）。
	// 发布者 indicators。
	TopicIndicatorWarmupDone = topic("indicator.warmup_done")

	// TopicExecAlgoChildOrder：执行算法拆出的子订单。
	// 发布者 exec-algo；订阅者 risk-engine（每片都过风控）/ exec-engine。
	TopicExecAlgoChildOrder = topic("execalgo.child_order")
)

// IsRegisteredTopic 判定 topic 是否在冻结注册表内。
//
// 这是 Publish / Subscribe 唯一的准入口径：**topic 必须来自 topics.go，业务
// 代码禁止字面量**（interfaces.go 包注释冻结）。未注册 topic -> 调用方拿到
// error，而不是订阅一个永远不会有消息的空位。
func IsRegisteredTopic(name string) bool {
	_, ok := registry[name]
	return ok
}

// RegisteredTopics 返回全部已注册 topic（字典序，稳定输出，供装配期自检与
// 诊断打印使用）。返回的是新分配的切片，调用方改动不影响注册表。
func RegisteredTopics() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
