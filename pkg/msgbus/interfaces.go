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
// 实现（SyncBus / Envelope）。K0-P2-3 按审查裁决改了契约（**签名级变更**）：
//
//   - Handler 加 error 返回：`func(ctx, msg) error`。分发时逐个调用、**不因
//     某个 handler 报错而中断**（一个订阅者失败不该让其他订阅者收不到消息），
//     全部错误聚合上报（errors.Join）。panic 仍不 recover。
//   - Publish 加 ctx：`Publish(ctx, topic, payload) error`。ctx 逐消息透传给
//     handler 与「先记录」这一步，取代原先总线自持的一个 ctx。
//   - Envelope 的字段改为非导出 + 构造函数——消息是既成事实，不该被改。
//   - SyncBus 的时钟改为**构造期强制注入**（NewSyncBus(tap, clk)），不再有
//     隐式墙钟兜底。
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
//
// 返回 error 表示「这个订阅者处理失败了」。语义（K0-P2-3 裁决，写死在注释里
// 免得日后各猜一套）：
//
//   - **不影响其他订阅者**：某个 handler 返回 error，分发循环继续往下走，
//     其余 handler 照常收到这条消息。理由：订阅者之间在契约上是互不认识的，
//     让 A 的失败吃掉 B 的消息等于把 A 的故障扩散成全总线静默丢消息——
//     那正是 BusTap 要防的「看不见的缺口」。
//   - **不影响返回值语义的上半截**：消息**已经落库**、也要**继续分发**，
//     所以 error 只代表「有订阅者没处理好」，不代表「消息没记录」。调用方
//     用 errors.Is 逐个判定自己关心的错误（errors.Join 保留穿透能力）。
//   - **panic 不 recover**：契约不吞异常（fail-loud），handler 自己 panic
//     就让它炸穿 Publish——那是编程错误，不是可恢复的业务失败。
type Handler func(ctx context.Context, msg Message) error

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
//     注册顺序逐个同步调用 handler。handler **返回 error 时继续**调用
//     其余 handler，全部错误聚合后随 Publish 一并返回；handler
//     **panic 则向上传播**（fail-loud），不 recover、不重试、不并发。
//     全部 handler 执行完才返回。topic 不在注册表内必须返回 error
//     （理由同 Subscribe）。
//   - ctx 是**这一次发布**的 ctx：它既传给落库（Tap.Append）也透传给
//     每个 handler。已取消的 ctx 一律「零分发」（见 SyncBus.Publish）。
//   - Subscribe 不存在的 topic（不在 topics.go 注册表内）返回 error。
//     裁决理由：topic 注册表是冻结契约，订阅未注册 topic 必然是
//     拼写错误或未走变更评审的新 topic——订阅期 fail-loud 优于
//     静默订阅一个永远收不到消息的 topic（运行期消息丢失且无从
//     发现）。Publish 同理。
type MsgBus interface {
	Publish(ctx context.Context, topic string, payload any) error
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
	//
	// ctx 由 Publish 逐消息传入（K0-P2-3）：落库是「这一次发布」的一部分，
	// 取消/超时必须与派发共享同一个 ctx，否则会出现「记录被取消但消息照发」
	// 这种 BusTap 破口。
	Append(ctx context.Context, msg Message) error
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

	// ErrContextCanceled：本次发布的 ctx 已取消/超时。裁决（K0-P2-3）：
	// 与「先记录后分发」同向——ctx 取消时**一条都不许派发**，且连落库都不
	// 尝试（落库是这次发布的一部分，取消它才叫取消）。返回前包装 ctx.Err()，
	// 调用方既能 errors.Is(err, ErrContextCanceled) 也能
	// errors.Is(err, context.Canceled / DeadlineExceeded)。
	ErrContextCanceled = errors.New("msgbus: 发布 ctx 已取消或超时——消息既未落库也未派发")
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
	// clk 是消息时间戳的来源，**构造期强制注入，不许为 nil**（见
	// NewSyncBus）。回测必须传 VirtualClock，实盘传 LiveClock。
	clk clock.Clock
	// handlers 是 topic -> 该 topic 的 handler 列表，**切片顺序即注册顺序**。
	handlers map[string][]Handler
}

// NewSyncBus 构造一条同步总线。tap 传 nil 会让 Publish 恒返回 ErrNoTap——
// 那是「先记录后分发」被违反时的正确行为，不是 Bug。
//
// ─── clk 为什么是必填（K0-P2-3：消灭一个静默陷阱）─────────────────
// 旧签名是 NewSyncBus(tap)，clk 为 nil 时**兜底墙钟**。回测装配一旦漏注入
// VirtualClock（漏一行而已，编译照过、运行照跑），每条消息的 ts 都是真实
// 世界时刻：同一份输入两次回放得到两条不同的 ts 序列，audit.message_log
// 没法比对，**BusTap 的审计价值归零——而且全程不报一个错**。这类「看起来
// 正常、价值已归零」的故障比崩掉难查得多。
//
// 所以改成就近 fail-loud：clk 为 nil 直接 panic。想用墙钟就显式写
// NewLiveBus(tap) / NewSyncBus(tap, clock.NewLiveClock())——「我在用墙钟」
// 这件事因此变成一句写在装配处的声明，而不是一个默认值。
func NewSyncBus(tap Tap, clk clock.Clock) *SyncBus {
	if clk == nil {
		panic("msgbus: NewSyncBus 必须传 clock.Clock（回测传 clock.NewVirtualClock(start)，实盘用 NewLiveBus）——" +
			"旧实现 nil 时兜底墙钟，回测误用会让 audit.message_log 落真实世界时刻且静默不报错")
	}
	return &SyncBus{
		tap:      tap,
		clk:      clk,
		handlers: make(map[string][]Handler),
	}
}

// NewLiveBus 实盘形态的便捷构造：时钟显式取 LiveClock（墙钟自走）。
//
// 存在理由是让「用墙钟」这件事在装配处**被念出来**：回测侧写的是
// NewSyncBus(tap, clock.NewVirtualClock(start))，实盘侧写 NewLiveBus(tap)，
// 两种时间观在装配代码里一眼可分——这正是旧「缺省墙钟」版本给不了的东西。
func NewLiveBus(tap Tap) *SyncBus {
	return NewSyncBus(tap, clock.NewLiveClock())
}

// Publish 同步分发一条消息。ctx 是**这一次发布**的 ctx：它传给落库，也原样
// 透传给每个 handler（不再用总线自持的 ctx——那是 K0-P2-3 之前的妥协产物，
// 一次构造定终身，无法 per-message 取消）。
//
// BusTap「**先记录后分发**」的执行顺序，也是本方法唯一的核心不变量：
//
//  1. 校验 topic 在注册表内（否则 ErrUnknownTopic）；
//  2. 校验 Tap 已装配（否则 ErrNoTap）；
//  3. 校验 ctx 未取消/未超时（否则 ErrContextCanceled）—— **零分发**；
//  4. Tap.Append(ctx, msg) —— **记录**；失败即刻 return error，一个 handler
//     都不跑；
//  5. 按注册顺序**同步**逐个调用该 topic 的 handler —— **分发**；某个 handler
//     返回 error **继续**调用其余 handler，错误全部聚合；
//  6. 全部跑完才返回（聚合后的）error；没有 handler 报错时返回 nil。
//
// 为什么顺序不能反 / 不能并发（这两条分别有专门的测试把门）：
//   - **反序**（先分发后记录）：订阅者会看到一条「库里查不到」的消息。BusTap
//     的全部价值在于「订阅者看到的 == 回放出来的」，反序之后回放缺行，而缺口
//     只在事后对账时才发现——那时现场早没了。
//   - **并发**：handler 的执行顺序不再确定 → 同一份输入两次回放得到不同的处理
//     序列 → 回测不可复现。且「落库成功」与「已被处理」之间不再有先后关系，
//     「记录了就一定发生过」这条推论失效。
//
// handler 报错怎么办（K0-P2-3 裁决，替代旧「Handler 无 error 返回值、只能
// panic 传播」的写法）：**继续分发 + 聚合上报**。聚合方式选标准库的
// errors.Join——理由：它的 errors.Is / errors.As **天然穿透**到每一个被聚合的
// 原始 error，调用方能逐个判定自己关心的那一个（例如「风控订阅者处理失败」），
// 而自定聚合类型要自己实现一遍 Is/As 才够用，等于多维护一份真相；且
// errors.Join 的 Error() 是换行拼接的多行文本，诊断时一眼看到全部。
//
// panic 仍然**不 recover**：那是编程错误，不是可恢复的业务失败，契约不吞它。
func (b *SyncBus) Publish(ctx context.Context, topic string, payload any) error {
	if !IsRegisteredTopic(topic) {
		return fmt.Errorf("%w: %q", ErrUnknownTopic, topic)
	}
	if b.tap == nil {
		return ErrNoTap
	}

	// ── ctx 已取消/超时：零分发，连落库都不试 ──────────────────
	// 裁决（K0-P2-3）：与「先记录后分发」同向。取消是调用方对「这一次发布」
	// 的裁决，不是对「派发那一步」的裁决——只跳过派发而仍落库，会写出一条
	// 没人处理的审计行（回放时表现为「记录过但没发生过」，正是 BusTap 要防
	// 的推论失效）；只跳过落库而照发，就是没记录先广播，直接破 BusTap。
	// 所以：**什么都不做，返回 error**。
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w（topic=%s）: %w", ErrContextCanceled, topic, err)
	}

	msg := NewEnvelope(topic, b.now(), payload)

	// ── ① 先记录 ──────────────────────────────────────────────
	// 失败即刻返回：一步都不许往下走。这里是 BusTap 的闸门，任何「记不下来
	// 也先播出去」的 Fallback 都会让 audit.message_log 缺行。
	if err := b.tap.Append(ctx, msg); err != nil {
		return fmt.Errorf("%w（topic=%s）: %w", ErrAppendFailed, topic, err)
	}

	// ── ② 后分发：同步、顺序、不并发 ────────────────────────────
	// 某个 handler 返回 error 时**继续**往下走（理由见 Handler 注释与上面的
	// 裁决），错误收集到 errs 里，最后统一 Join。
	var errs []error
	for _, h := range b.handlers[topic] {
		if err := h(ctx, msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

// now 取「现在」：一律走注入的 Clock（回测=虚拟时间，实盘=墙钟）。clk 由构造
// 函数保证非 nil，故这里不再有墙钟兜底——兜底正是 K0-P2-3 要消灭的静默陷阱
//（见 NewSyncBus 注释）。
func (b *SyncBus) now() time.Time { return b.clk.Now() }

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

	_ func(MsgBus, context.Context, string, any) error = MsgBus.Publish
	_ func(MsgBus, string, Handler) error              = MsgBus.Subscribe

	// Tap（消费方定义的落库窄接口）与 Handler 的签名守卫：改回旧签名
	// （Append(msg)/Handler 无 error 返回）时，这两行编译失败。
	_ func(Tap, context.Context, Message) error = Tap.Append
	_ Handler                                   = func(context.Context, Message) error { return nil }
)
