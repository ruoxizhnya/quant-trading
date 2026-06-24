// Package hkex implements Hong Kong Stock Connect (港股通) data
// fetching and northbound flow (北向资金) factor calculation.
//
// P2-12: 港股通/北向因子.
//
// The northbound leg of Stock Connect (Shanghai-Hong Kong + Shenzhen-Hong
// Kong) is widely watched as a proxy for "smart money" flow into A-shares.
// This package provides:
//
//   - NorthboundFlow / StockFlow: domain types for daily aggregate flow
//     and per-stock flow.
//   - NorthboundFetcher: the contract every concrete data source must
//     implement (Eastmoney is the default; Tushare / Wind can be added
//     later without touching consumers).
//   - EastmoneyNorthboundFetcher: the Eastmoney push2 implementation.
//   - NorthboundFactor: moving-average / momentum / holding-change / rank
//     / flow-signal calculators that turn raw flows into tradeable factors.
//   - ExchangeRateConverter: HKD↔CNY conversion (mock by default; a live
//     FX source can be plugged in later).
//
// Design notes:
//   - All I/O takes context.Context as the first parameter.
//   - HTTP uses net/http only (no third-party HTTP client).
//   - Logging uses github.com/rs/zerolog, matching the rest of the repo.
//   - Struct fields carry snake_case JSON tags so the same types can be
//     serialized to the API layer without an intermediate DTO.
package hkex

import (
	"context"
	"time"
)

// NorthboundFlow captures the aggregate northbound (北向) capital flow
// for a single trading day.
//
// Units follow the convention used by Eastmoney / Wind terminals:
//   - TotalNetBuy / SHConnectNetBuy / SZConnectNetBuy are in 亿元
//     (100M CNY), matching the headline number quoted by financial media.
//   - TotalBuy / TotalSell are in 万元 (10K CNY) to preserve the
//     granularity the upstream API returns.
//
// Top10Stocks is the list of the day's top-10 traded names; it may be
// empty when the upstream endpoint does not return a breakdown.
type NorthboundFlow struct {
	Date          time.Time   `json:"date"`
	TotalNetBuy   float64    `json:"total_net_buy"`
	SHConnectNetBuy float64  `json:"sh_connect_net_buy"`
	SZConnectNetBuy float64  `json:"sz_connect_net_buy"`
	TotalBuy      float64    `json:"total_buy"`
	TotalSell     float64    `json:"total_sell"`
	Top10Stocks   []StockFlow `json:"top10_stocks"`
}

// StockFlow captures the northbound flow for a single stock on a single
// day (or aggregated over a window, depending on the caller).
//
// Units:
//   - NetBuy / BuyAmount / SellAmount are in 万元 (10K CNY), matching
//     the per-stock granularity returned by the Eastmoney fflow endpoint.
//   - HoldingRatio is a percentage in [0, 100] (e.g. 3.42 means the
//     northbound pool holds 3.42% of the stock's free float).
type StockFlow struct {
	Symbol       string    `json:"symbol"`
	Name         string    `json:"name"`
	NetBuy       float64   `json:"net_buy"`
	BuyAmount    float64   `json:"buy_amount"`
	SellAmount   float64   `json:"sell_amount"`
	HoldingRatio float64   `json:"holding_ratio"`
	Date         time.Time `json:"date"`
}

// NorthboundFetcher is the contract every northbound data source must
// implement. Implementations must be safe for concurrent use: the
// factor calculator may issue parallel FetchStockFlow calls for a basket
// of symbols.
//
// Method semantics:
//
//   - FetchDaily returns the aggregate NorthboundFlow for a single date.
//     If the upstream has no data for that date (holiday, pre-open), the
//     implementation should return (nil, nil) — not an error — so the
//     caller can step back to the previous trading day.
//   - FetchStockFlow returns the per-day StockFlow series for a symbol
//     over [startDate, endDate]. The slice is ordered ascending by Date.
//   - FetchTopHoldings returns the top-`limit` stocks by net buy on the
//     given date. limit<=0 is treated as the implementation default
//     (Eastmoney returns 10).
type NorthboundFetcher interface {
	FetchDaily(ctx context.Context, date time.Time) (*NorthboundFlow, error)
	FetchStockFlow(ctx context.Context, symbol string, startDate, endDate time.Time) ([]StockFlow, error)
	FetchTopHoldings(ctx context.Context, date time.Time, limit int) ([]StockFlow, error)
}

// FlowSignal classifies the strength/direction of northbound flow for a
// single stock on a single day.
//
//   - +1 (StrongInflow):  net buy > +2σ AND holding ratio increasing.
//   -  0 (Neutral):        neither inflow nor outflow threshold met.
//   - -1 (StrongOutflow): net buy < -2σ AND holding ratio decreasing.
//
// The 2σ band is the conventional "smart money" filter used by retail
// northbound-flow screens; it is intentionally simple — callers wanting
// a finer ranking should use NetBuyRank instead.
type FlowSignal int

const (
	FlowSignalNeutral     FlowSignal = 0
	FlowSignalStrongInflow  FlowSignal = 1
	FlowSignalStrongOutflow FlowSignal = -1
)

// String renders a human-readable label for logging / API responses.
func (s FlowSignal) String() string {
	switch s {
	case FlowSignalStrongInflow:
		return "strong_inflow"
	case FlowSignalStrongOutflow:
		return "strong_outflow"
	default:
		return "neutral"
	}
}
