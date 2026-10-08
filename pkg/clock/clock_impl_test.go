// K1 切片 1：pkg/clock 实现的行为测试。
//
// 与 interfaces_compliance_test.go 的分工：那边钉的是「结构有没有被改坏」
// （方法集合、Mode 取值顺序），这边钉的是「实现行为对不对」。
//
// 核心不变量：**回测确定性**——VirtualClock 的时间序列只由 Advance 的调用
// 序列决定，与墙钟、时区、构造顺序毫无关系。同一切片的全部 git 历史都跑
// 出同一份结果，这条才成立。
package clock_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
)

// ─── VirtualClock ────────────────────────────────────────────────

// TestVirtualClockZeroValueStartsAtEpoch 钉死零值的起始时间：Unix epoch。
//
// 「零值是确定的起始时间」写进了 structs 的注释里，注释不执行——把它变成
// 断言，改了就得回来改这个文件（还得走变更评审）。
func TestVirtualClockZeroValueStartsAtEpoch(t *testing.T) {
	var c clock.VirtualClock
	const want = int64(0)
	if got := c.Now().UTC().UnixNano(); got != want {
		t.Errorf("零值 VirtualClock.Now() = %d ns since epoch (%s), want %d ns (Unix epoch)",
			got, c.Now().Format(time.RFC3339Nano), want)
	}
	if !c.Now().Equal(time.Unix(0, 0).UTC()) {
		t.Errorf("零值 VirtualClock.Now() = %s, want 1970-01-01T00:00:00Z", c.Now().Format(time.RFC3339Nano))
	}
}

// TestVirtualClockStartIsDeterministic 确定性本身：两个独立零值时钟、两条
// 相同 Advance 序列 → 同一串时间戳。这是回测可复现的最小证据。
func TestVirtualClockStartIsDeterministic(t *testing.T) {
	sequence := []time.Time{
		time.Date(2200, 1, 5, 9, 30, 0, 0, time.UTC),
		time.Date(2200, 1, 5, 9, 31, 0, 0, time.UTC),
		time.Date(2200, 1, 6, 9, 30, 0, 0, time.UTC),
	}
	replay := func() []time.Time {
		var c clock.VirtualClock
		out := []time.Time{c.Now()}
		for _, ts := range sequence {
			if err := c.Advance(ts); err != nil {
				t.Fatalf("Advance(%s) 意外失败: %v", ts, err)
			}
			out = append(out, c.Now())
		}
		return out
	}
	first, second := replay(), replay()
	if len(first) != len(second) {
		t.Fatalf("两条同序列回放长度不同: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if !first[i].Equal(second[i]) {
			t.Errorf("第 %d 步不确定: 第一次 %s, 第二次 %s", i, first[i], second[i])
		}
	}
}

// TestVirtualClockAdvanceForward 正向推进：Now 反映最新虚拟时间。
func TestVirtualClockAdvanceForward(t *testing.T) {
	c := clock.NewVirtualClock(time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC))
	start := c.Now()

	next := start.Add(15 * time.Minute)
	if err := c.Advance(next); err != nil {
		t.Fatalf("Advance 正向推进失败: %v", err)
	}
	if !c.Now().Equal(next) {
		t.Errorf("Advance 后 Now() = %s, want %s", c.Now(), next)
	}

	// 连续推进到更晚的时刻仍然合法（单调性的另一半）。
	later := next.AddDate(1, 0, 0)
	if err := c.Advance(later); err != nil {
		t.Fatalf("Advance 连续推进失败: %v", err)
	}
	if !c.Now().Equal(later) {
		t.Errorf("二次 Advance 后 Now() = %s, want %s", c.Now(), later)
	}
}

// TestVirtualClockAdvanceRewindReturnsError —— 本任务验收第 4 条。
// 传入更早的时间必须返回 error，errors.Is 能命中哨兵 ErrTimeRewind。
func TestVirtualClockAdvanceRewindReturnsError(t *testing.T) {
	start := time.Date(2200, 6, 1, 12, 0, 0, 0, time.UTC)
	c := clock.NewVirtualClock(start)

	err := c.Advance(start.Add(-time.Nanosecond))
	if err == nil {
		t.Fatalf("Advance 到早 1ns 的时刻返回 nil，want error（时间倒退必须 fail-loud）")
	}
	if !errors.Is(err, clock.ErrTimeRewind) {
		t.Errorf("error = %v, want errors.Is(err, clock.ErrTimeRewind) 成立", err)
	}

	// 失败不得留下痕迹：倒了的时间不能悄悄生效。
	if !c.Now().Equal(start) {
		t.Errorf("失败的 Advance 改变了时钟状态: Now() = %s, want 仍是 %s", c.Now(), start)
	}

	// 回到起点之后仍能正常推进——失败路径没把时钟锁死。
	if err := c.Advance(start.Add(time.Second)); err != nil {
		t.Errorf("倒退被拒后，正向推进也失败了: %v", err)
	}
}

// TestVirtualClockAdvanceEqualTimestampIsNoOp 同一时刻重复推进视为合法：
// 同一时间戳可能连来两根 bar / 两笔 tick，拦下来属于误伤正常车流。
func TestVirtualClockAdvanceEqualTimestampIsNoOp(t *testing.T) {
	start := time.Date(2200, 6, 1, 12, 0, 0, 0, time.UTC)
	c := clock.NewVirtualClock(start)

	if err := c.Advance(start); err != nil {
		t.Errorf("Advance 到等于当前虚拟时间的时刻返回 %v, want nil（非递减，含相等）", err)
	}
	if !c.Now().Equal(start) {
		t.Errorf("Now() = %s, want %s", c.Now(), start)
	}
}

// TestVirtualClockIsUnaffectedByWallClock VirtualClock 与墙钟无关：构造后
// 真实世界流逝若干毫秒，虚拟时间一步不动。这是「回测不受运行时刻影响」
// 的直接证据。
func TestVirtualClockIsUnaffectedByWallClock(t *testing.T) {
	c := clock.NewVirtualClock(time.Date(2200, 3, 1, 0, 0, 0, 0, time.UTC))
	before := c.Now()
	time.Sleep(2 * time.Millisecond)
	if !c.Now().Equal(before) {
		t.Errorf("墙钟流逝后虚拟时间自己动了: %s → %s", before, c.Now())
	}
}

// TestVirtualClockModeBacktest 回测时钟的 Mode 必然是 ModeBacktest——
// 模块靠它自查当前跑在哪套时间观里。
func TestVirtualClockModeBacktest(t *testing.T) {
	var zero clock.VirtualClock
	if got := zero.Mode(); got != clock.ModeBacktest {
		t.Errorf("零值 VirtualClock.Mode() = %d, want ModeBacktest(%d)", got, clock.ModeBacktest)
	}
	c := clock.NewVirtualClock(time.Unix(0, 0).UTC())
	if got := c.Mode(); got != clock.ModeBacktest {
		t.Errorf("NewVirtualClock().Mode() = %d, want ModeBacktest(%d)", got, clock.ModeBacktest)
	}
}

// TestVirtualClockRejectsRewindFromZeroValue 零值时钟（起点 epoch）往前推
// 同样被拒：epoch 之后任何更早的时刻都是倒退，边界处不能有例外。
func TestVirtualClockRejectsRewindFromZeroValue(t *testing.T) {
	var c clock.VirtualClock
	if err := c.Advance(time.Unix(0, 0).UTC().Add(-time.Second)); !errors.Is(err, clock.ErrTimeRewind) {
		t.Errorf("从零值往前推 = %v, want errors.Is(err, clock.ErrTimeRewind)", err)
	}
}

// TestVirtualClockNormalizesToUTC 不丢时刻、不丢精度即可：时区只是表示层，
// 内部一律 UTC。调用方拿回的 time.Time 表示的必须是同一个瞬时。
func TestVirtualClockNormalizesToUTC(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	localStamp := time.Date(2200, 6, 1, 20, 0, 0, 0, shanghai) // UTC 同瞬时是 12:00
	c := clock.NewVirtualClock(localStamp)

	if !c.Now().Equal(localStamp) {
		t.Errorf("Now() = %s, want 与 %s 同一瞬时（时区归一到 UTC）", c.Now(), localStamp)
	}
	if c.Now().Location() != time.UTC {
		t.Errorf("Now().Location() = %v, want UTC（回测时间统一 UTC，避免序列化/比对受时区影响）",
			c.Now().Location())
	}
}

// ─── LiveClock ───────────────────────────────────────────────────

// TestLiveClockNowIsWallClock LiveClock 必须跟着真实墙钟走，且读数落在
// 测试自身的真实时间窗内（不是某个冻结的假值）。
func TestLiveClockNowIsWallClock(t *testing.T) {
	c := clock.NewLiveClock()
	before := time.Now()
	got := c.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("LiveClock.Now() = %s, 未落在 [%s, %s] 之间——不是墙钟", got, before, after)
	}

	// 墙钟自走：不调 Advance，时间也得往前。
	first := c.Now()
	deadline := time.Now().Add(2 * time.Second)
	for !c.Now().After(first) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !c.Now().After(first) {
		t.Errorf("LiveClock 的时间没有自走（两次读数都是 %s）——那是 VirtualClock 的行为", first)
	}
}

// TestLiveClockAdvanceReturnsError 实盘时钟不可被程序推进。
func TestLiveClockAdvanceReturnsError(t *testing.T) {
	var zero clock.LiveClock
	err := zero.Advance(time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("LiveClock.Advance 返回 nil, want error（墙钟不可被推进）")
	}
	if !errors.Is(err, clock.ErrLiveClockImmutable) {
		t.Errorf("error = %v, want errors.Is(err, clock.ErrLiveClockImmutable) 成立", err)
	}

	if c := clock.NewLiveClock(); c.Advance(time.Now()) == nil {
		t.Error("NewLiveClock().Advance 返回 nil, want error")
	}
}

// TestLiveClockModeLive 实盘时钟的 Mode 必然是 ModeLive。
func TestLiveClockModeLive(t *testing.T) {
	var zero clock.LiveClock
	if got := zero.Mode(); got != clock.ModeLive {
		t.Errorf("零值 LiveClock.Mode() = %d, want ModeLive(%d)", got, clock.ModeLive)
	}
	if got := clock.NewLiveClock().Mode(); got != clock.ModeLive {
		t.Errorf("NewLiveClock().Mode() = %d, want ModeLive(%d)", got, clock.ModeLive)
	}
}

// ─── 替换性：两种时钟共用调用方 ──────────────────────────────────

// TestClockIsSubstitutable 回测-实盘同构的最小形态：同一段「调用方代码」
// 在两种 Clock 下都能跑通，调用方不做类型分支。这个 panic-for-nil 之外的
// 全部差异都只来自 Mode()。
func TestClockIsSubstitutable(t *testing.T) {
	virtual := clock.NewVirtualClock(time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC))
	live := clock.NewLiveClock()

	for _, tc := range []struct {
		name string
		c    clock.Clock
		want clock.Mode
	}{
		{"回测", virtual, clock.ModeBacktest},
		{"实盘", live, clock.ModeLive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Mode(); got != tc.want {
				t.Errorf("Mode() = %d, want %d", got, tc.want)
			}
			if tc.c.Now().IsZero() {
				t.Error("Now() 返回零值 time.Time——没有时间源")
			}
		})
	}

	// VirtualClock 能被推进，LiveClock 不能：这个差异就是两种时间观的全部。
	if err := virtual.Advance(time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Errorf("VirtualClock.Advance 失败: %v", err)
	}
	if err := live.Advance(time.Now()); err == nil {
		t.Error("LiveClock.Advance 成功, want error")
	}
}
