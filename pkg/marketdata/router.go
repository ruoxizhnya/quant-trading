package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// 物理表名 —— 规范层之外唯一允许直白写出这两个名字的地方（遗留基线
// 登记见 internal/repoguard/canonical_layer_test.go）。
const (
	tableDaily  = "ohlcv_daily_qfq"
	tableMinute = "ohlcv_minute"
)

// Router 透明路由：调用方声明频率，不感知存在哪张表。
type Router struct {
	pool *pgxpool.Pool
}

// NewRouter 用现有连接池构造路由。池的构造与生命周期归调用方
// （连接池是应用层资源，ADR-031 资源归属裁决同款语义）。
func NewRouter(pool *pgxpool.Pool) *Router {
	return &Router{pool: pool}
}

// Bars 按声明频率取数（含时间语义字段填充）。
//
// start/end 语义：1d 路由按交易日闭区间（trade_date BETWEEN）；
// 1m/15m 路由按时刻闭区间（ts BETWEEN），15m 额外聚合。
//
// 跨度边界（REV-01）：1m/15m 单次请求的跨度上限见 maxIntradaySpan，超限直接报错
// 而不是截断或硬扛；1d 不受该上限约束（日线单票量级可接受）。
//
// 返回 bar 的 Frequency 恒等于入参 freq、VisibleAt 按规范层时间语义
// 填充（1d=当日 15:00 收盘 CST；日内=周期结束时刻）——测试钉住，
// 改错即红（D1 破坏验证①）。
func (r *Router) Bars(ctx context.Context, freq market.Frequency, symbol string, start, end time.Time) ([]market.OHLCV, error) {
	switch freq {
	case market.Freq1d:
		return r.loadDaily(ctx, symbol, start, end)
	case market.Freq1m:
		return r.loadMinute(ctx, symbol, start, end)
	case market.Freq15m:
		minutes, err := r.loadMinute(ctx, symbol, start, end)
		if err != nil {
			return nil, err
		}
		return Aggregate(minutes, market.Freq15m)
	default:
		return nil, fmt.Errorf("marketdata: unsupported frequency %q (known: 1m/15m/1d) —— 拒绝静默回退", string(freq))
	}
}

func (r *Router) loadDaily(ctx context.Context, symbol string, start, end time.Time) ([]market.OHLCV, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT symbol, trade_date, open, high, low, close, volume, turnover, trade_days
		FROM `+tableDaily+`
		WHERE symbol = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC
	`, symbol, start, end)
	if err != nil {
		return nil, fmt.Errorf("marketdata: query daily bars: %w", err)
	}
	defer rows.Close()

	var results []market.OHLCV
	for rows.Next() {
		var o market.OHLCV
		if err := rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &o.Volume, &o.Turnover, &o.TradeDays); err != nil {
			return nil, fmt.Errorf("marketdata: scan daily bar: %w", err)
		}
		o.Frequency = market.Freq1d
		o.VisibleAt = market.DailyVisibleAt(o.Date)
		results = append(results, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("marketdata: daily rows: %w", err)
	}
	return results, nil
}

// maxIntradaySpan 是日内频率（1m/15m）单次请求允许的最大自然日跨度（REV-01）。
//
// 为什么要限：loadMinute 把 [start,end] 内该标的的全部分钟行一次性 Scan 进
// []market.OHLCV，峰值内存随行数线性涨，而行数 ≈ 每交易日 bar 数 × 交易日数 ——
// 没有任何 LIMIT / 分页 / 跨度校验时，这个数只由调用方传进来的区间决定。
//
// 上限取 **366 自然日**（≈ 244 交易日 ≈ A 股 1m 的 5.9 万行 ≈ 8MB 单标的一次请求），
// 不是随手挑的：再小（如工单举例的 60 天）会让 D6「多频率进回测」的 15m 视野没法用，
// 一个满足不了任何真实用法的上限，下一个用它的人只会把它调大或删除 —— 那比没有
// 护栏更糟。代价写清：单次请求最坏 8MB 级而不是 1.4MB 级，需要多年分钟视野的调用方
// 必须自己切片分次取。
//
// 按自然日而非行数：③ 给的是跨度上限或流式两条路，跨度这条能在**查库前**判定、
// 零数据即可测（ohlcv_minute 现状 0 行，行数上限当场无法证伪）；但它对每日 bar
// 数不同的市场是同一把尺（A 股 240 根/日、美股 390 根/日，D7/D9 国外线到位后
// 同一跨度的行数会差 ~1.6 倍）—— 这个含义写在这里，别当成行数保证。
const maxIntradaySpan = 366 * 24 * time.Hour

// checkIntradaySpan 是日内路径的跨度守卫。
//
// 拒绝而不是截断：静默把「十年」改成「366 天」会让调用方以为拿到了全区间，
// 与 Router 既有 fail-loud 风格（未知频率直接报错）一致。
//
// end 早于 start 不在这里判：那是另一个问题，本单只收「无上限 materialize」，
// 负跨度由 SQL 的 BETWEEN 自然给出空集。
func checkIntradaySpan(start, end time.Time) error {
	span := end.Sub(start)
	if span <= maxIntradaySpan {
		return nil
	}
	return fmt.Errorf(
		"marketdata: 日内频率（1m/15m）请求跨度 %s 超过单次上限 %s（%d 自然日）—— "+
			"请切片后分次取数；本层拒绝静默截断，也不做无上限全量加载",
		// 天数从常量导出，不写死字面量：否则改了 maxIntradaySpan 之后，报错里
		// 会和紧挨着打印的那个 Duration 各说各话。
		span, maxIntradaySpan, int64(maxIntradaySpan.Hours()/24))
}

func (r *Router) loadMinute(ctx context.Context, symbol string, start, end time.Time) ([]market.OHLCV, error) {
	// 1m 与 15m 都经本函数取分钟行（15m 再交 Aggregate），所以守卫挂在这里
	// 结构性地覆盖两条路径 —— 挂在 Bars 的 switch 分支上就要记住两处。
	if err := checkIntradaySpan(start, end); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT symbol, ts, open, high, low, close, volume, amount
		FROM `+tableMinute+`
		WHERE symbol = $1 AND ts >= $2 AND ts <= $3
		ORDER BY ts ASC
	`, symbol, start, end)
	if err != nil {
		return nil, fmt.Errorf("marketdata: query minute bars: %w", err)
	}
	defer rows.Close()

	var results []market.OHLCV
	for rows.Next() {
		var (
			o      market.OHLCV
			volume int64
			amount *float64
		)
		if err := rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &volume, &amount); err != nil {
			return nil, fmt.Errorf("marketdata: scan minute bar: %w", err)
		}
		o.Volume = float64(volume)
		if amount != nil {
			o.Turnover = *amount
		}
		o.Frequency = market.Freq1m
		o.VisibleAt = market.IntradayVisibleAt(o.Date, 1)
		results = append(results, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("marketdata: minute rows: %w", err)
	}
	return results, nil
}
