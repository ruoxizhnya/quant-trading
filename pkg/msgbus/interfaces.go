// Package msgbus 是内核模块间消息的命名注册表 + 同步分发总线。
//
// 冻结语义（蓝图 §5 msgbus 行 / §3.2 N2 不抄并发分发）：
//
//   - 同步分发，禁止并发：Publish 内 handler 按注册顺序同步执行，
//     单线程语义，不 goroutine、不缓冲。这是回测确定性的硬前提
//     （nautilus 用 Rust 类型系统强制单线程，我们用契约禁止 +
//     同步实现达到同等效果）。现有 pkg/marketdata/eventbus.go
//     （4 goroutine 抢同一 channel）是旧物，ADR-030 OBS-04 已判定
//     不接线、不照搬，与本包无关。
//
//   - 先记录后分发（BusTap 语义）：消息在派发给订阅者之前经
//     eventstore 落 audit.message_log——订阅者看不到未落库的消息。
//     落库钩子属 eventstore 模块，本包只管分发。
//
//   - topic 名一律来自 topics.go 注册表，业务代码禁止字面量。
//
// K0 切片 1：本文件只冻结接口契约。所有 stub 方法体固定
// panic("contract stub: not implemented")，不含任何实现逻辑；
// K1 实现直接替换 stub。
package msgbus

import (
	"context"
	"time"
)

// Handler 是消息订阅回调。同步执行：Publish 返回前，该 topic 的
// 全部已注册 handler 按注册顺序逐个跑完。
type Handler func(ctx context.Context, msg Message)

// Message 是总线上流动消息的统一抽象。
type Message interface {
	// Topic 返回消息所属 topic，取值必须在 topics.go 注册表内。
	Topic() string
	// Ts 返回消息的业务时间戳：回测=VirtualClock 当前时间（数据时间），
	// 实盘=墙钟。注意不是入队/落库时间。
	Ts() time.Time
	// Payload 返回消息负载。具体结构由各模块的接口契约冻结（切片 2），
	// K0 只冻结消息信封与 topic 名。
	Payload() any
}

// MsgBus 是模块间消息总线。
//
// 冻结语义：
//   - Publish 同步分发：先经 eventstore 落库（BusTap），再按订阅
//     注册顺序逐个同步调用 handler。任一 handler panic 即向上传播
//     （fail-loud），不吞、不重试、不并发。全部 handler 执行完
//     才返回。topic 不在注册表内必须返回 error（理由同 Subscribe）。
//   - Subscribe 不存在的 topic（不在 topics.go 注册表内）返回 error。
//     裁决理由：topic 注册表是冻结契约，订阅未注册 topic 必然是
//     拼写错误或未走变更评审的新 topic——订阅期 fail-loud 优于
//     静默订阅一个永远收不到消息的 topic（运行期消息丢失且无从
//     发现）。Publish 同理。
type MsgBus interface {
	Publish(topic string, payload any) error
	Subscribe(topic string, h Handler) error
}

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// SyncBus 是 K1 进程内同步总线的契约 stub。
type SyncBus struct{}

// Publish 同步分发一条消息（先落库后派发，顺序执行全部 handler）。
func (b *SyncBus) Publish(topic string, payload any) error {
	panic("contract stub: not implemented")
}

// Subscribe 注册订阅；未注册 topic 返回 error。
func (b *SyncBus) Subscribe(topic string, h Handler) error {
	panic("contract stub: not implemented")
}

// Envelope 是基本消息信封的契约 stub。
type Envelope struct{}

// Topic 返回消息 topic。
func (e *Envelope) Topic() string { panic("contract stub: not implemented") }

// Ts 返回业务时间戳。
func (e *Envelope) Ts() time.Time { panic("contract stub: not implemented") }

// Payload 返回消息负载。
func (e *Envelope) Payload() any { panic("contract stub: not implemented") }

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 方法表达式守卫：从 Message/MsgBus 接口删除任一方法，本包
// go build 直接编译失败（防「删方法后测试仍绿」）。
var (
	_ MsgBus  = (*SyncBus)(nil)
	_ Message = (*Envelope)(nil)

	_ func(Message) string    = Message.Topic
	_ func(Message) time.Time = Message.Ts
	_ func(Message) any       = Message.Payload

	_ func(MsgBus, string, any) error     = MsgBus.Publish
	_ func(MsgBus, string, Handler) error = MsgBus.Subscribe
)
