package marketdata

import (
	"fmt"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// 聚合口径（docs/SPEC.md「规范层 Schema」节是文档真相，本文件是代码真相）。
//
// 主口径：**开 = 首笔开，高 = 期间最高，低 = 期间最低，收 = 末笔收，
// 量 = 期间合计**（turnover 同比合计）。
//
// 边界三细则（原蓝图 §9 #2 的归属地，2026-10-14 落定在此）：
//  1. 午休断点：15m bucket 按会话内时钟对齐（每日 16 个：09:30/09:45/
//     …/11:15 | 13:00/…/14:45），**不得跨午休**。
//  2. 集合竞价：09:15–09:25 开盘竞价成交并入首个上午 bucket（开价取
//     竞价成交价）；14:57–15:00 收盘竞价成交并入末 bucket。
//  3. 停牌日处理：当日无 1m bar → 不产出聚合 bar（缺失，不零填充）；
//     半日停牌 → 已覆盖 bucket 照常产出、未覆盖 bucket 缺失。
//
// 另外：日内出现于交易时段之外的 bar（盘前/午休/盘后打点）→ fail-loud
// 报错，不静默丢弃——静默丢 bar 等于悄悄改口径。

// A 股连续竞价时段边界（Asia/Shanghai，分钟计数 0..1440）。
const (
	auctionOpenMin    = 9*60 + 15  // 09:15 开盘集合竞价开始
	morningOpenMin    = 9*60 + 30  // 09:30
	morningCloseMin   = 11*60 + 30 // 11:30
	afternoonOpenMin  = 13 * 60    // 13:00
	afternoonCloseMin = 15 * 60    // 15:00
	bucketMinutes     = 15
)

// Aggregate 把 1m bar 聚合到 target（15m 或 1d）。输入 bar 的 Frequency
// 必须声明为 1m —— 频率声明与数据形态不符即拒（fail-loud），这是
// 「频率语义改错会被抓住」的第二道闸（第一道是路由测试）。
func Aggregate(bars []market.OHLCV, target market.Frequency) ([]market.OHLCV, error) {
	if target != market.Freq15m && target != market.Freq1d {
		return nil, fmt.Errorf("marketdata: 聚合目标只支持 15m/1d（输入恒为 1m），got %q", string(target))
	}
	for i, b := range bars {
		if b.Frequency != market.Freq1m {
			return nil, fmt.Errorf(
				"marketdata: Aggregate 输入 bar[%d]（%s %s）频率声明为 %q，要求 %q —— 声明与数据形态不符，拒收",
				i, b.Symbol, b.Date.Format(time.RFC3339), string(b.Frequency), string(market.Freq1m))
		}
	}
	if len(bars) == 0 {
		return nil, nil
	}

	sorted := make([]market.OHLCV, len(bars))
	copy(sorted, bars)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Symbol != sorted[j].Symbol {
			return sorted[i].Symbol < sorted[j].Symbol
		}
		return sorted[i].Date.Before(sorted[j].Date)
	})

	type acc struct {
		open, close      float64
		high, low        float64
		volume, turnover float64
		count            int
	}
	type bucketKey struct {
		symbol string
		start  time.Time
	}

	buckets := make(map[bucketKey]*acc, len(sorted))
	var keys []bucketKey
	for _, b := range sorted {
		var (
			start time.Time
			err   error
		)
		if target == market.Freq15m {
			start, err = bucketStart15m(b.Date)
		} else {
			start, err = bucketStartDaily(b.Date)
		}
		if err != nil {
			return nil, err
		}
		k := bucketKey{symbol: b.Symbol, start: start}
		a, ok := buckets[k]
		if !ok {
			// sorted by ts → 首见即首笔
			buckets[k] = &acc{open: b.Open, close: b.Close, high: b.High, low: b.Low, volume: b.Volume, turnover: b.Turnover, count: 1}
			keys = append(keys, k)
			continue
		}
		// sorted by ts → 后来的是更晚的 bar：收=末笔
		a.close = b.Close
		if b.High > a.high {
			a.high = b.High
		}
		if b.Low < a.low {
			a.low = b.Low
		}
		a.volume += b.Volume
		a.turnover += b.Turnover
		a.count++
	}

	out := make([]market.OHLCV, 0, len(keys))
	for _, k := range keys {
		a := buckets[k]
		bar := market.OHLCV{
			Symbol:   k.symbol,
			Date:     k.start,
			Open:     a.open,
			High:     a.high,
			Low:      a.low,
			Close:    a.close,
			Volume:   a.volume,
			Turnover: a.turnover,
			// TradeDays 不填（0）：该字段无消费方，Tushare 日线路径同样
			// 不填（pkg/data/tushare.go 注释 "not available from daily"），
			// 不为聚合 bar 发明新语义。
			// LimitUp/LimitDown 不推导（恒 false）：涨跌停判定需要日线
			// 参考价，属 D6 消费侧按需计算，不在这里拍。
			Frequency: target,
		}
		if target == market.Freq15m {
			bar.VisibleAt = market.IntradayVisibleAt(k.start, bucketMinutes)
		} else {
			bar.VisibleAt = market.DailyVisibleAt(k.start)
		}
		out = append(out, bar)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Date.Before(out[j].Date)
	})
	return out, nil
}

// bucketStart15m 返回 1m bar 所属 15m bucket 的起点（Asia/Shanghai 时刻）。
//
// bucket 按会话内时钟对齐：上午 09:30 起每 15 分钟一个（11:15 bucket
// 止于 11:30），下午 13:00 起每 15 分钟一个（14:45 bucket 止于 15:00）。
// 开盘集合竞价打点（09:15–09:25）并入 09:30 bucket；收盘集合竞价打点
// （14:57–15:00，含 15:00:00 收盘价打点）并入 14:45 bucket。
func bucketStart15m(ts time.Time) (time.Time, error) {
	t := ts.In(market.AsiaShanghai)
	if err := checkInSession(t); err != nil {
		return time.Time{}, err
	}
	hm := t.Hour()*60 + t.Minute()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.AsiaShanghai)

	var startMin int
	switch {
	case hm < morningOpenMin:
		// 开盘集合竞价区间（09:15–09:29）→ 首个上午 bucket
		startMin = morningOpenMin
	case hm < morningCloseMin:
		startMin = morningOpenMin + (hm-morningOpenMin)/bucketMinutes*bucketMinutes
	case hm < afternoonCloseMin:
		startMin = afternoonOpenMin + (hm-afternoonOpenMin)/bucketMinutes*bucketMinutes
	default:
		// hm == afternoonCloseMin：15:00:00 收盘价打点归末 bucket
		startMin = afternoonCloseMin - bucketMinutes
	}
	return day.Add(time.Duration(startMin) * time.Minute), nil
}

// bucketStartDaily 返回 1m bar 所属交易日的日 bucket 起点。
// 用 UTC 零点（与 ohlcv_daily_qfq 的 DATE 扫描形态一致），保证聚合
// 1d 与日线表路由两种 1d 口径的 Date 字段可比。
func bucketStartDaily(ts time.Time) (time.Time, error) {
	t := ts.In(market.AsiaShanghai)
	if err := checkInSession(t); err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
}

// checkInSession 拒绝交易时段之外的 bar（盘前/午休/盘后打点）。
// 09:15–09:29 是开盘集合竞价区间、14:57–15:00 是收盘集合竞价区间，
// 都算交易时段内（打点并入相邻 bucket，见 bucketStart15m）。
func checkInSession(t time.Time) error {
	hm := t.Hour()*60 + t.Minute()
	switch {
	case hm < auctionOpenMin:
		return fmt.Errorf("marketdata: bar %s 早于 09:15 开盘集合竞价，不在 A 股交易时段内", t.Format(time.RFC3339))
	case hm < morningCloseMin:
		return nil // 09:15–11:29：集合竞价 + 上午连续竞价
	case hm < afternoonOpenMin:
		return fmt.Errorf("marketdata: bar %s 落在午休（11:30–13:00），A 股此时段不成交", t.Format(time.RFC3339))
	case hm <= afternoonCloseMin:
		return nil // 13:00–15:00：下午连续竞价 + 收盘集合竞价（含 15:00:00 收盘打点）
	default:
		return fmt.Errorf("marketdata: bar %s 晚于 15:00 收盘，不在 A 股交易时段内", t.Format(time.RFC3339))
	}
}
