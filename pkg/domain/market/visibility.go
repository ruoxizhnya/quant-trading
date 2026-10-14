package market

import "time"

// 规范层时间语义（可见时间点，point-in-time）——ADR-031 蓝图 §4.2 红线：
// 每条数据必须自带「什么时候才可见」，否则即数据泄漏，且极难事后发现
// （回测曲线照样漂亮）。本文件是 Go 侧口径的唯一真相。

// AsiaShanghai 是 A 股交易时区（UTC+8 固定偏移）。
//
// 用 FixedZone 而不是 time.LoadLocation：中国 1991 年后无夏令时，固定
// 偏移语义等价，且不依赖运行时 zoneinfo——Windows 开发机与精简 CI 容器
// 默认没有 tzdata，LoadLocation("Asia/Shanghai") 会直接失败。
var AsiaShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

// DailyVisibleAt 返回日线 bar 的可见时间点：交易日 15:00（收盘，
// Asia/Shanghai）。
//
// 入参按 UTC 零点解释的交易日（PG DATE 经 pgx 扫进 time.Time 的形态）——
// 只取它的日历日期分量，时段与位置另建。实际 Tushare 日更数据落库更晚，
// 15:00 是「收盘后可见」的规范口径（保守：不早于真实可见时刻）。
func DailyVisibleAt(tradeDate time.Time) time.Time {
	y, m, d := tradeDate.Date()
	return time.Date(y, m, d, 15, 0, 0, 0, AsiaShanghai)
}

// IntradayVisibleAt 返回日内 bar 的可见时间点 = **bar 周期结束时刻**。
//
// 1 分钟 bar（周期 1 分钟） stamped 10:00 → 10:01 可见；15m bucket
// stamped 09:30（周期 15 分钟）→ 09:45 可见。公式与蓝图 §4.3 的
// 「10:00 的 1 分钟线 10:01 才可见」逐字同构：可见 = 周期结束，不提前。
func IntradayVisibleAt(barStart time.Time, minutes int) time.Time {
	return barStart.Add(time.Duration(minutes) * time.Minute)
}
