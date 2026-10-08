// Package clock 提供内核的「现在」抽象——回测与实盘同构的第一替换点
// （蓝图 §4.1 替换点①，对应 nautilus clock_factory）。
//
// 替换边界语义（冻结）：
//   - 回测 = VirtualClock：时间由数据迭代器驱动推进（数据驱动），
//     同一输入必然产生同一时间序列——回测确定性由此保证；
//   - 实盘 = LiveClock：「现在」即操作系统墙钟，自走，不可外部推进。
//
// 同一份策略/模块代码在两种时间观下零改动，仅由 Kernel 装配时替换
// Clock 实现。这是「回测-实盘同构」的全部秘密之一：替换的是实现，
// 不是调用方。
//
// K0 切片 1 冻结了本包的接口契约（Clock / Mode；stub 方法体固定
// panic("contract stub: not implemented")）。K1 切片 1 已把 stub 替换为
// 真实实现：契约半个字没改，只填了方法体，并补了构造函数与哨兵 error。
package clock

import (
	"errors"
	"fmt"
	"time"
)

// Mode 标识时钟的时间观。取值顺序（iota）是冻结契约：
// ModeBacktest=0、ModeLive=1，禁止重排或插入新值。
type Mode int

const (
	// ModeBacktest 回测模式：VirtualClock，数据驱动推进。
	ModeBacktest Mode = iota
	// ModeLive 实盘模式：LiveClock，墙钟自走。
	ModeLive
)

// Clock 是内核唯一的时间源。任何模块需要「现在」都必须经本接口，
// 禁止直接调用 time.Now()（那等于私接墙钟，破坏回测确定性）。
//
// 方法语义（冻结）：
//   - Now()：VirtualClock 返回虚拟当前时间（由 Advance 推进）；
//     LiveClock 返回墙钟。
//   - Advance(ts)：VirtualClock 专用——时间只准非递减，ts 早于当前
//     虚拟时间时返回 error（时间倒退=破坏确定性，fail-loud）；
//     在 LiveClock 上调用一律返回 error（墙钟不可被外部推进）。
//   - Mode()：返回当前时间观，供模块自查。
type Clock interface {
	Now() time.Time
	Advance(ts time.Time) error
	Mode() Mode
}

// ─── K1 实现（替换 K0 的 panic stub，契约不变） ─────────────────────

// 哨兵 error：调用方用 errors.Is 判定，不要比字符串。
var (
	// ErrTimeRewind：试图把 VirtualClock 推进到一个早于当前虚拟时间的
	// 时刻。时间倒退会让「同一份输入产出同一份结果」这一确定性前提失效
	// ——回测里第二次读到的 bar 比第一次旧，任何有状态算子都会被污染，
	// 且污染发生在别的模块里，现场离根因很远。所以这里必须 fail-loud。
	ErrTimeRewind = errors.New("clock: 虚拟时间只准非递减（倒退会破坏回测确定性）")

	// ErrLiveClockImmutable：在 LiveClock 上调 Advance。墙钟不是我们能
	// 推进的东西——调用方多半是把回测代码原样跑到实盘了，或者是装配时
	// 拿错了 Clock 实现。两者都是真错误，静默成功会把「时间观用错」
	// 藏进正确-looking 的行情里。
	ErrLiveClockImmutable = errors.New("clock: LiveClock 不可被程序推进（墙钟自走）")
)

// VirtualClock 是回测时钟：时间由数据迭代器推进，只准非递减。
//
// 内部状态是「距 Unix epoch 的纳秒数」而不是 time.Time，理由有二：
//  1. int64 的零值就是 epoch，所以零值 +VirtualClock{} 的起始时间是一个
//     **确定的、不依赖墙钟也不依赖构造顺序** 的时刻（1970-01-01T00:00:00Z）。
//     换成 time.Time 存，零值是 0001-01-01T00:00:00Z，语义含糊（分不清
//     「起点就是它」和「还没初始化」），且没法保证没人塞进一个带单调时钟
//     读数的值进来。
//  2. 纳秒整数是全序的，比较与赋值不牵扯时区/单调时钟，比 time.Time 的
//     Before/After 更不容易写出边界错。
//
// 起始时间（epoch）是契约的一部分：由
// TestVirtualClockZeroValueStartsAtEpoch 钉死，改动须走变更评审。
type VirtualClock struct {
	nanos int64
}

// NewVirtualClock 从给定起点建一个回测时钟。start 归一到 UTC 后取整到
// 纳秒；回测里通常传数据窗口的第一根 bar 的时间戳。
func NewVirtualClock(start time.Time) *VirtualClock {
	return &VirtualClock{nanos: start.UTC().UnixNano()}
}

// Now 返回虚拟当前时间（恒定 UTC，由 Advance 推进，与墙钟无关）。
func (c *VirtualClock) Now() time.Time { return time.Unix(0, c.nanos).UTC() }

// Advance 推进虚拟时间。
//
// 只准非递减：ts 早于当前虚拟时间时返回包装了 ErrTimeRewind 的 error，
// 且**不修改**当前时间（失败不得留下痕迹——半推进比不推进更难查）。
// ts 等于当前时间视为合法 no-op：数据迭代器完全可能连推两根同一时刻的
// bar（同秒多笔 tick），把它判成错误属于把正常车流拦下来。
func (c *VirtualClock) Advance(ts time.Time) error {
	n := ts.UTC().UnixNano()
	if n < c.nanos {
		return fmt.Errorf("clock: 无法把虚拟时间从 %s 推进到 %s：%w",
			c.Now().Format(time.RFC3339Nano), ts.UTC().Format(time.RFC3339Nano), ErrTimeRewind)
	}
	c.nanos = n
	return nil
}

// Mode 恒返回 ModeBacktest。
func (c *VirtualClock) Mode() Mode { return ModeBacktest }

// LiveClock 是实盘时钟：墙钟自走，不可被外部推进。
//
// 无状态故无字段——零值 +LiveClock{} 与 NewLiveClock() 完全等价，两种写法
// 都可以用。
type LiveClock struct{}

// NewLiveClock 建一个实盘时钟（等价于 &LiveClock{}，只为装配处可读性存在）。
func NewLiveClock() *LiveClock { return &LiveClock{} }

// Now 返回墙钟时间。注意：内核模块**禁止**直接调 time.Now()，必须经
// Clock 接口拿时间（interfaces.go 顶部注释），否则等于私接墙钟。
func (c *LiveClock) Now() time.Time { return time.Now() }

// Advance 在墙钟上无意义，恒返回包装了 ErrLiveClockImmutable 的 error。
func (c *LiveClock) Advance(ts time.Time) error {
	return fmt.Errorf("clock: 无法把墙钟推进到 %s：%w",
		ts.UTC().Format(time.RFC3339Nano), ErrLiveClockImmutable)
}

// Mode 恒返回 ModeLive。
func (c *LiveClock) Mode() Mode { return ModeLive }

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 前两行是既有样板（pkg/strategy/interfaces.go 模式）：stub 漂移出
// 接口时 go build 失败。
//
// 方法表达式守卫（后三行）防止「从接口删方法后 build 仍绿」：
// 从 Clock 接口删除任一方法，Clock.<Method> 即未定义，本包
// go build 直接编译失败——这是 K0 验收破坏验证 a 的护栏。
var (
	_ Clock = (*VirtualClock)(nil)
	_ Clock = (*LiveClock)(nil)

	_ func(Clock) time.Time        = Clock.Now
	_ func(Clock, time.Time) error = Clock.Advance
	_ func(Clock) Mode             = Clock.Mode
)
