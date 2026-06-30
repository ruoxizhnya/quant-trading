// Package marketdata — real-time quote type.
//
// S7-P1-2 (ODR-043): Quote previously lived in pkg/live and was imported
// by pkg/marketdata (realtime_provider.go), creating a reverse dependency
// (marketdata → live). Quote is a market-data concept, so it belongs here
// in the lower layer. pkg/live re-exports it via a type alias for backward
// compatibility.
package marketdata

import "time"

// Quote represents a real-time market quote.
//
// This type is the canonical quote representation shared between the
// marketdata layer (providers, buses) and the live layer (data feeds,
// order matching). Both pkg/live and pkg/marketdata use this struct;
// pkg/live exposes it as `live.Quote` via a type alias.
type Quote struct {
	Symbol    string    `json:"symbol"`
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    int64     `json:"volume"`
	Bid       float64   `json:"bid"`
	Ask       float64   `json:"ask"`
}
