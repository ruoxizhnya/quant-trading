package market

import (
	"testing"
	"time"
)

// 分钟粒度 PIT 闸门（D6）：可见时间点的**判定**与填充同文件同语义。
//
// 钉死蓝图 §4.3 裁定 4 的原句口径：「10:00 的 1 分钟线 10:01 才可见」——
// 即 10:00 bar 在 10:00:30 不可见、10:01:00 可见。这两条是 D6 验收①
// 的机械判据，改闸门口径（如把 <= 写成 <、或拿周期起点当可见点）
// 都会当场变红。

// cst 构造 Asia/Shanghai 时刻（测试专用，与 aggregate 测试同款约定）。
func cst(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, AsiaShanghai)
}

func TestOHLCV_VisibleBy_MinuteGranularity(t *testing.T) {
	// 10:00 的 1m bar：周期 10:00–10:01 ⇒ 可见点 10:01。
	bar := OHLCV{
		Symbol:    "000001.SZ",
		Date:      cst(2024, 1, 2, 10, 0),
		Frequency: Freq1m,
		VisibleAt: IntradayVisibleAt(cst(2024, 1, 2, 10, 0), 1),
	}
	if got := bar.VisibleAt; !got.Equal(cst(2024, 1, 2, 10, 1)) {
		t.Fatalf("VisibleAt = %s, want 10:01", got.In(AsiaShanghai))
	}

	cases := []struct {
		name string
		asOf time.Time
		want bool
	}{
		// 验收①原句：10:00:30 不可见。
		{"bar_stamp_plus_30s", cst(2024, 1, 2, 10, 0).Add(30 * time.Second), false},
		// 边界前一纳秒仍不可见（不提前）。
		{"one_ns_before_visible", cst(2024, 1, 2, 10, 1).Add(-time.Nanosecond), false},
		// 验收①原句：10:01 可见（边界 inclusive）。
		{"visible_at_exact", cst(2024, 1, 2, 10, 1), true},
		{"after_visible", cst(2024, 1, 2, 15, 0), true},
		// 当天更早当然不可见。
		{"before_bar", cst(2024, 1, 2, 9, 45), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bar.VisibleBy(tc.asOf); got != tc.want {
				t.Fatalf("VisibleBy(%s) = %v, want %v", tc.asOf.In(AsiaShanghai), got, tc.want)
			}
		})
	}
}

// 15m bucket 同构：09:30 bucket 的可见点是 09:45（周期结束）。
func TestOHLCV_VisibleBy_15mBucket(t *testing.T) {
	bar := OHLCV{
		Symbol:    "000001.SZ",
		Date:      cst(2024, 1, 2, 9, 30),
		Frequency: Freq15m,
		VisibleAt: IntradayVisibleAt(cst(2024, 1, 2, 9, 30), 15),
	}
	if !bar.VisibleAt.Equal(cst(2024, 1, 2, 9, 45)) {
		t.Fatalf("VisibleAt = %s, want 09:45", bar.VisibleAt.In(AsiaShanghai))
	}
	if bar.VisibleBy(cst(2024, 1, 2, 9, 44)) {
		t.Fatal("09:44 不应看到 09:30 bucket")
	}
	if !bar.VisibleBy(cst(2024, 1, 2, 9, 45)) {
		t.Fatal("09:45 应看到 09:30 bucket")
	}
}

// 日线 bar：当日 15:00 收盘可见（DailyVisibleAt 口径），15:00 前不可见。
func TestOHLCV_VisibleBy_Daily(t *testing.T) {
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bar := OHLCV{
		Symbol:    "000001.SZ",
		Date:      day,
		Frequency: Freq1d,
		VisibleAt: DailyVisibleAt(day),
	}
	if !bar.VisibleAt.Equal(cst(2024, 1, 2, 15, 0)) {
		t.Fatalf("VisibleAt = %s, want 15:00 CST", bar.VisibleAt.In(AsiaShanghai))
	}
	if bar.VisibleBy(cst(2024, 1, 2, 14, 59)) {
		t.Fatal("14:59 不应看到当日日线")
	}
	if !bar.VisibleBy(cst(2024, 1, 2, 15, 0)) {
		t.Fatal("15:00 应看到当日日线")
	}
}

// 零值 VisibleAt（未声明）恒不可见：未声明的可见性是泄漏，
// 不是「永远可见」。
func TestOHLCV_VisibleBy_ZeroVisibleAtNeverVisible(t *testing.T) {
	bar := OHLCV{Symbol: "000001.SZ", Date: cst(2024, 1, 2, 10, 0)}
	if bar.VisibleBy(cst(2026, 1, 1, 0, 0)) {
		t.Fatal("未声明 VisibleAt 的 bar 在任何时刻都不得判可见")
	}
}
