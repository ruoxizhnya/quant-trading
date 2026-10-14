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

func (r *Router) loadMinute(ctx context.Context, symbol string, start, end time.Time) ([]market.OHLCV, error) {
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
