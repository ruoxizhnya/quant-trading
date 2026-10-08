// K1 切片 1：topic 注册表镜像的完整性护栏（**内部测试包**，为的是直接看到
// 未导出的 topicSet）。
//
// 为什么需要它：topics.go 里的 16 个常量与 registry.go 的 topicSet 是两份
// 真相。新增一个 topic 常量却忘了在 registry.go 登记，症状是「Publish 一个新
// topic 被拒」——错误信息清楚，但发现得晚。这条护栏把它提前到编译/测试期。
package msgbus

import "testing"

// TestTopicSetCoversFrozenConstants 逐个直接引用 topics.go 的每一个冻结常量。
//
// 与 interfaces_compliance_test.go 的 topicRegistry 是**两个独立文件**各自
// 引用一次，故意重复：那边是 K0 契约的护栏（不许删/改值），这边是 K1 注册表
// 镜像的护栏（不许漏登记）。任一常量被删除，两个文件都会编译失败。
func TestTopicSetCoversFrozenConstants(t *testing.T) {
	frozen := []string{
		// ─── 切片 1 的 9 个 ───────────────────────────────────────
		TopicDataBar,
		TopicExecOrderIntent,
		TopicExecFill,
		TopicPortfolioUpdated,
		TopicRiskVerdict,
		TopicRunStart,
		TopicRunDone,
		TopicKernelBoot,
		TopicKernelShutdown,
		// ─── 切片 2 追加的 7 个 ───────────────────────────────────
		TopicRiskOrderVerdict,
		TopicExecOrderRejected,
		TopicPortfolioFilled,
		TopicReconDiff,
		TopicStrategyStateSaved,
		TopicIndicatorWarmupDone,
		TopicExecAlgoChildOrder,
	}
	const wantRegistered = 16

	if len(frozen) != wantRegistered {
		t.Fatalf("本文件引用的冻结常量数 = %d, want %d", len(frozen), wantRegistered)
	}
	if len(topicSet) != wantRegistered {
		t.Errorf("topicSet 有 %d 项, want %d（新增 topic 必须同时登记到 pkg/msgbus/registry.go 的 topicSet）",
			len(topicSet), wantRegistered)
	}
	for _, topic := range frozen {
		if _, ok := topicSet[topic]; !ok {
			t.Errorf("topic %q 已冻结但未登记进 topicSet——Publish/Subscribe 会把它当成未注册 topic 拒绝", topic)
		}
	}
	for topic := range topicSet {
		found := false
		for _, f := range frozen {
			if f == topic {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("topicSet 里的 %q 不在冻结常量清单中（多出来的条目，多半是拼写变体）", topic)
		}
	}
}

// TestRegisteredTopicsIsSortedAndUniq RegisteredTopics() 的输出要稳定（字典序、
// 去重），否则拿它做装配自检/诊断输出的代码会得到抖动的结果。
func TestRegisteredTopicsIsSortedAndUniq(t *testing.T) {
	got := RegisteredTopics()
	if len(got) != len(topicSet) {
		t.Errorf("RegisteredTopics() 返回 %d 项, want %d（有重复？）", len(got), len(topicSet))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("RegisteredTopics() 未严格字典序: [%d]=%q >= [%d]=%q", i-1, got[i-1], i, got[i])
		}
	}
	// 返回值是拷贝：改它不该动注册表。
	if len(got) > 0 {
		got[0] = "tampered"
		if _, ok := topicSet["tampered"]; ok {
			t.Error("改动 RegisteredTopics() 的返回值影响了内部注册表——没拷贝")
		}
	}
}

// TestIsRegisteredTopicRejectsLookalikes 判 topic 只看完整匹配：
// 大小写变体、前后缀扩展都不是「注册过的Topic」。
func TestIsRegisteredTopicRejectsLookalikes(t *testing.T) {
	lookalikes := []string{
		"",
		"DATA.BAR",
		"data.Bar",
		"data.bar ",
		" data.bar",
		"data.ba",
		"data.barr",
		"exec.fill.v2",
	}
	for _, topic := range lookalikes {
		if IsRegisteredTopic(topic) {
			t.Errorf("IsRegisteredTopic(%q) = true, want false（注册表只认 topics.go 的原值）", topic)
		}
	}
	for _, topic := range RegisteredTopics() {
		if !IsRegisteredTopic(topic) {
			t.Errorf("IsRegisteredTopic(%q) = false, want true（自己列出来的必须认）", topic)
		}
	}
}
