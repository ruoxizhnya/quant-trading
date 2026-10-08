package msgbus

import "sort"

// registry.go —— topics.go 注册表在总线里的**可执行镜像**。
//
// ─── 为什么需要它 ─────────────────────────────────────────────────
// topics.go 的 16 个常量是编译期符号，运行期拿不到「全体已注册 topic」这个
// 集合。而 Subscribe / Publish 必须在**订阅期 / 发布期**判出「topic 不在注册表
// 内」（契约冻结的理由见 interfaces.go 的 MsgBus 注释：订阅一个永远收不到
// 消息的 topic，运行期表现为消息丢失且无从发现）。要判就得有一份集合。
//
// ─── 去重还是迁徙 ─────────────────────────────────────────────────
// 第一选择本来是给 topics.go 加一个 `var All = []string{...}`，那样只有一处
// 真相。但 topics.go 是 **K0 冻结的契约文件**（切片的法规文本），本切片的约束
// 是「不改 K0 契约」，所以新增放本文件，而不是改 topics.go。
//
// 代价（必须钉住）：**加常量忘登记**这一类是静默失效——新 topic 会被 Publish
// 拒掉，错误信息还挺清楚，但没人提前知道。故由
// registry_internal_test.go 的 TestTopicSetCoversFrozenConstants 逐个直接
// 引用 topics.go 的每一个常量（本文件与测试里各引用一次），并在
// interfaces_compliance_test.go 的 topicRegistry 之外再钉一次数量。
// 后续切片若把 All 迁进 topics.go，把本文件删掉即可。

// ─── K0 冻结 topic 的可执行镜像 ──────────────────────────────────
//
// 每个键都直接引用 topics.go 的常量（不是字符串字面量）：有人改了常量值，
// 这里跟着走；有人删了常量，本文件编译不过。
var topicSet = map[string]struct{}{
	// ─── 切片 1 的 9 个 ───────────────────────────────────────────
	TopicDataBar:          {},
	TopicExecOrderIntent:  {},
	TopicExecFill:         {},
	TopicPortfolioUpdated: {},
	TopicRiskVerdict:      {},
	TopicRunStart:         {},
	TopicRunDone:          {},
	TopicKernelBoot:       {},
	TopicKernelShutdown:   {},
	// ─── 切片 2 追加的 7 个 ───────────────────────────────────────
	TopicRiskOrderVerdict:    {},
	TopicExecOrderRejected:   {},
	TopicPortfolioFilled:     {},
	TopicReconDiff:           {},
	TopicStrategyStateSaved:  {},
	TopicIndicatorWarmupDone: {},
	TopicExecAlgoChildOrder:  {},
}

// IsRegisteredTopic 判定 topic 是否在冻结注册表内。
//
// 这是 Publish / Subscribe 唯一的准入口径：**topic 必须来自 topics.go，业务
// 代码禁止字面量**（interfaces.go 包注释冻结）。未注册 topic -> 调用方拿到
// error，而不是订阅一个永远不会有消息的空位。
func IsRegisteredTopic(topic string) bool {
	_, ok := topicSet[topic]
	return ok
}

// RegisteredTopics 返回全部已注册 topic（字典序，稳定输出，供装配期自检与
// 诊断打印使用）。返回的是新分配的切片，调用方改动不影响注册表。
func RegisteredTopics() []string {
	out := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		out = append(out, topic)
	}
	sort.Strings(out)
	return out
}
