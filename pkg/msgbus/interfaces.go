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
// K0 切片 1 冻结了本包的接口契约（Message / MsgBus / Handler；stub 方法体固定
// panic("contract stub: not implemented")）。K1 切片 1 已把 stub 替换为真实
// 实现（SyncBus / Envelope），契约半个字未改：
//
//   - MsgBus 的方法集合、Handler 的签名（**无 error 返回值**）照旧；
//   - Envelope 的字段改为非导出 + 构造函数——消息是既成事实，不该被改。
//
// ─── 导入方向（写装配前先看这条） ──────────────────────────────────
// pkg/eventstore import 本包（Message 接口与 Envelope 在这），所以本包
// **不得** import pkg/eventstore —— 那会是导入环。后果见 Tap 的注释。
package msgbus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
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

// ─── K1 实现（替换 K0 的 panic stub，契约不变） ─────────────────────

// Tap 是 Publish 的「先记录」落点——BusTap 语义那一半契约在总线上的投影。
//
// ─── 为什么类型不是 eventstore.EventStore ─────────────────────────
// 任务书写的是 NewSyncBus(store eventstore.EventStore)。照写会导致
// **导入环**：pkg/eventstore 必须 import pkg/msgbus（它的 Append/Replay 签名
// 用 msgbus.Message），若本包再 import pkg/eventstore，Go 直接拒绝编译。这
// 与 topics.go 头部记的那次「注册表落位修正」（kernel ↔ msgbus 环）是同一类
// 错误，只是这次环藏在消息模块之间。
//
// Go 的正解是「**消费方定义接口**」：总线只需要「能把消息记下去」这一件事，
// 就把这**一件事**定义成本接口（一个方法）。eventstore.EventStore 有三个方
// 法，天然满足 Tap（结构化类型，无需声明实现关系），由 bus_test.go 里的
// 编译期断言 `var _ msgbus.Tap = (*eventstore.PGEventStore)(nil)` 钉住。
//
// 副作用是好的：单元测试可以用内存实现替代真库，不必碰 PostgreSQL。
type Tap interface {
	// Append 落库一条消息。返回非 nil error 时，Publish **必须中止派发**。
	Append(msg Message) error
}

// 哨兵 error：调用方用 errors.Is 判定，不要比字符串。
var (
	// ErrUnknownTopic：topic 不在 topics.go 注册表内。
	ErrUnknownTopic = errors.New("msgbus: topic 不在 pkg/msgbus/topics.go 注册表内（业务代码禁止字面量）")

	// ErrNilHandler：Subscribe 收到 nil handler。
	ErrNilHandler = errors.New("msgbus: handler 为 nil")

	// ErrNoTap：总线没有装配落库钩子。BusTap 的第一原则是「不记录就不许
	// 派发」，缺了 Tap 的总线只能拒绝发布——绝不能退化成「纯内存广播」，
	// 那会让 audit.message_log 静默缺行。
	ErrNoTap = errors.New("msgbus: 总线未装配 Tap（eventstore），拒绝发布以免消息无审计地消失")

	// ErrAppendFailed：落库失败，消息未派发。包装 Tap 返回的原始 error。
	ErrAppendFailed = errors.New("msgbus: 先记录后分发——落库失败，消息未派发给任何订阅者")
)

// SyncBus 是进程内同步总线（K1 实现）。
//
// **禁止并发分发**：Publish 在自己的 goroutine 里、按注册顺序、逐个调用该
// topic 的 handler，全部跑完才返回。理由（蓝图 §3.2 N2 / interfaces.go 包
// 注释）：回测确定性来自确定的处理顺序——nautilus 用 Rust 的类型系统强制单
// 线程，我们用「契约禁止 + 同步实现」达到同等效果。pkg/marketdata 那个
// 4 goroutine 抢同一 channel 的 EventBus（ADR-030 OBS-04）是旧物，不接线、
// 不照搬。
//
// 无全局单例（ADR-027）：Tap 由构造函数注入，进程里可以有多个总线实例。
type SyncBus struct {
	// tap 是「先记录」的落点，见 Tap 注释。为 nil 时 Publish 一律返回
	// ErrNoTap（构造期不报错是因为 constructor 签名不带 error；发布期拒绝
	// 比静默广播安全）。
	tap Tap
	// clk 是消息时间戳的来源。为 nil 时兜底用墙钟——**仅限实盘**。
	// 回测必须注入 VirtualClock，否则每条消息的 ts 都是真实世界时刻：
	// 同一份输入两次回放得到两条不同的 ts 序列，audit.message_log 无法比对，
	// 也就是 BusTap 的审计价值归零。见 NewSyncBusWithClock 注释。
	clk clock.Clock
	// ctx 透传给 handler。契约里 Publish 不带 ctx，故总线持有自己的一个：
	// 用法是把「本次 run 的生命周期 ctx」在构造时交给总线。
	ctx context.Context
	// handlers 是 topic -> 该 topic 的 handler 列表，**切片顺序即注册顺序**。
	handlers map[string][]Handler
}

// NewSyncBus 构造一条同步总线。tap 传 nil 会让 Publish 恒返回 ErrNoTap——
// 那是「先记录后分发」被违反时的正确行为，不是 Bug。
//
// 时间戳来源缺省是**墙钟**。回测请务必用 NewSyncBusWithClock 注入
// VirtualClock，否则消息 ts 会随真实时间漂移。
func NewSyncBus(tap Tap) *SyncBus {
	return &SyncBus{
		tap:      tap,
		handlers: make(map[string][]Handler),
		ctx:      context.Background(),
	}
}

// NewSyncBusWithClock 同上，额外指定时间源。**回测走这个**：把内核的
// VirtualClock 传进来，消息 ts 才等于数据时间而不是真实世界时刻。
//
// clk 传 nil 等价于 NewSyncBus。
func NewSyncBusWithClock(tap Tap, clk clock.Clock) *SyncBus {
	b := NewSyncBus(tap)
	b.clk = clk
	return b
}

// Publish 同步分发一条消息。
//
// BusTap「**先记录后分发**」的执行顺序，也是本方法唯一的核心不变量：
//
//  1. 校验 topic 在注册表内（否则 ErrUnknownTopic）；
//  2. Tap.Append(msg) —— **记录**；失败即刻 return error，一个 handler 都不跑；
//  3. 按注册顺序**同步**逐个调用该 topic 的 handler —— **分发**；
//  4. 全部跑完才返回 nil。
//
// 为什么顺序不能反 / 不能并发（这两条分别有专门的测试把门）：
//   - **反序**（先分发后记录）：订阅者会看到一条「库里查不到」的消息。BusTap
//     的全部价值在于「订阅者看到的 == 回放出来的」，反序之后回放缺行，而缺口
//     只在事后对账时才发现——那时现场早没了。
//   - **并发**：handler 的执行顺序不再确定 → 同一份输入两次回放得到不同的处理
//     序列 → 回测不可复现。且「落库成功」与「已被处理」之间不再有先后关系，
//     「记录了就一定发生过」这条推论失效。
//
// handler 报错怎么办：**冻结契约里 Handler 没有 error 返回值**
// （type Handler func(ctx, msg)），所以本方法没有「聚合 handler 错误」这条路
// 可走——返回值 nil 只代表「已记录 + 已分发」。契约原文规定 handler panic 即
// 向上传播（fail-loud，不吞、不重试、不并发），本实现照办：**不 recover**。
// 若后续评审希望「一个 handler 失败不影响其他订阅者 + 聚合上报」，那得先把
// Handler 的签名改成 `func(ctx, msg) error`（属契约变更，本切片不做）。
// 详见今次报告的「歧义与裁决」。
func (b *SyncBus) Publish(topic string, payload any) error {
	if !IsRegisteredTopic(topic) {
		return fmt.Errorf("%w: %q", ErrUnknownTopic, topic)
	}
	if b.tap == nil {
		return ErrNoTap
	}

	msg := NewEnvelope(topic, b.now(), payload)

	// ── ① 先记录 ──────────────────────────────────────────────
	// 失败即刻返回：一步都不许往下走。这里是 BusTap 的闸门，任何「记不下来
	// 也先播出去」的 Fallback 都会让 audit.message_log 缺行。
	if err := b.tap.Append(msg); err != nil {
		return fmt.Errorf("%w（topic=%s）: %w", ErrAppendFailed, topic, err)
	}

	// ── ② 后分发：同步、顺序、不并发 ────────────────────────────
	for _, h := range b.handlers[topic] {
		h(b.dispatchCtx(), msg)
	}
	return nil
}

// Subscribe 注册订阅。topic 不在注册表内返回 ErrUnknownTopic——订阅一个永远
// 收不到消息的 topic，运行期症状是「消息静默丢失」，订阅期直接拒绝更容易定位。
//
// 同一 topic 多次订阅都保留，执行顺序 = 订阅顺序（契约：「按注册优先级」）。
func (b *SyncBus) Subscribe(topic string, h Handler) error {
	if !IsRegisteredTopic(topic) {
		return fmt.Errorf("%w: %q", ErrUnknownTopic, topic)
	}
	if h == nil {
		return ErrNilHandler
	}
	b.handlers[topic] = append(b.handlers[topic], h)
	return nil
}

// now 取「现在」：注入了 Clock 就用它（回测=虚拟时间，实盘=墙钟），没注入就
// 退化为墙钟 UTC（仅实盘可接受，见字段注释）。
func (b *SyncBus) now() time.Time {
	if b.clk != nil {
		return b.clk.Now()
	}
	return time.Now().UTC()
}

// dispatchCtx 给 handler 用的 ctx。构造时没给就用 Background——handler 自己
// 负责在其上派生超时。
func (b *SyncBus) dispatchCtx() context.Context {
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

// Envelope 是基本消息信封。
//
// 字段非导出 + 构造函数：消息是「已经发生的事实」，发出去之后不该被任何人
// （包括发布方自己持有的指针）改写——尤其 **ts 是回测锚点**，被改一次整条
// run 的时间序列就歪了。要构造请用 NewEnvelope。
type Envelope struct {
	topic string
	ts    time.Time
	body  any
}

// NewEnvelope 构造一条消息。payload 原样持有，**不在这里序列化**——序列化是
// eventstore 落库时的事（那里才需要 JSON），总线只搬运对象本身：进程内分发
// 一份消息拷贝都不该做，否则回测里慢一倍，语义上还会让人以为 handler 拿到
// 的是快照。
func NewEnvelope(topic string, ts time.Time, payload any) *Envelope {
	return &Envelope{topic: topic, ts: ts, body: payload}
}

// Topic 返回消息 topic。
func (e *Envelope) Topic() string { return e.topic }

// Ts 返回业务时间戳：回测 = VirtualClock 当前值（数据时间），实盘 = 墙钟。
// 注意不是入队 / 落库时间。
func (e *Envelope) Ts() time.Time { return e.ts }

// Payload 返回消息负载。回放（eventstore.Replay）返回的 Envelope 里，它是一
// 段 json.RawMessage——调用方负责反序列化成自己的类型。
func (e *Envelope) Payload() any { return e.body }

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
