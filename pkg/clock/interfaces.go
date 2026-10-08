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
// K0 切片 1：本文件只冻结接口契约。所有 stub 方法体固定
// panic("contract stub: not implemented")，不含任何实现逻辑；
// K1 实现直接替换 stub。
package clock

import "time"

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

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// VirtualClock 是回测时钟的契约 stub：时间由数据迭代器推进，只准非递减。
type VirtualClock struct{}

// Now 返回虚拟当前时间。
func (c *VirtualClock) Now() time.Time { panic("contract stub: not implemented") }

// Advance 推进虚拟时间；ts 早于当前时间返回 error（禁止倒退）。
func (c *VirtualClock) Advance(ts time.Time) error { panic("contract stub: not implemented") }

// Mode 恒返回 ModeBacktest。
func (c *VirtualClock) Mode() Mode { panic("contract stub: not implemented") }

// LiveClock 是实盘时钟的契约 stub：墙钟自走，不可被外部推进。
type LiveClock struct{}

// Now 返回墙钟时间。
func (c *LiveClock) Now() time.Time { panic("contract stub: not implemented") }

// Advance 在墙钟上无意义，恒返回 error。
func (c *LiveClock) Advance(ts time.Time) error { panic("contract stub: not implemented") }

// Mode 恒返回 ModeLive。
func (c *LiveClock) Mode() Mode { panic("contract stub: not implemented") }

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
