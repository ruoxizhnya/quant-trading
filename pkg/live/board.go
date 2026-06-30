// Package live — A-share board classification (backward-compat aliases).
//
// S7-P1-2 (ODR-043): Board type, constants, ClassifySymbol, and the
// classification helpers have moved to pkg/marketdata (the lower layer).
// This file re-exports them via aliases so existing `live.Board`,
// `live.BoardChiNext`, `live.ClassifySymbol` references continue to work
// without modification. The canonical definitions live in
// pkg/marketdata/board.go.
//
// Both pkg/live (price cage validation) and pkg/compliance (investor
// suitability) consume Board; moving it to marketdata breaks the
// compliance → live reverse dependency while keeping the correct
// dependency direction (live → marketdata, compliance → marketdata).
package live

import "github.com/ruoxizhnya/quant-trading/pkg/marketdata"

// Board is an alias for marketdata.Board. See marketdata.Board for the
// canonical definition and documentation.
type Board = marketdata.Board

const (
	// BoardMainBoardSH — Shanghai Main Board (上交所主板).
	BoardMainBoardSH = marketdata.BoardMainBoardSH
	// BoardMainBoardSZ — Shenzhen Main Board (深交所主板, 含 002 中小板).
	BoardMainBoardSZ = marketdata.BoardMainBoardSZ
	// BoardChiNext — 创业板 (Shenzhen, 300xxx).
	BoardChiNext = marketdata.BoardChiNext
	// BoardSTAR — 科创板 (Shanghai, 688xxx).
	BoardSTAR = marketdata.BoardSTAR
	// BoardBSE — 北交所 (Beijing Stock Exchange, 8xxxxx / 4xxxxx).
	BoardBSE = marketdata.BoardBSE
	// BoardETF — 交易型开放式基金.
	BoardETF = marketdata.BoardETF
	// BoardBond — 国债 / 地方债 / 公司债.
	BoardBond = marketdata.BoardBond
	// BoardIndex — 指数 (如 000300.SH, 399001.SZ).
	BoardIndex = marketdata.BoardIndex
	// BoardFundLOF — LOF 基金 (16xxxx.SZ, 50xxxx.SH).
	BoardFundLOF = marketdata.BoardFundLOF
	// BoardUnknown — 未知/无法识别 (默认 ±10% 限制 + 主板笼子, 偏保守).
	BoardUnknown = marketdata.BoardUnknown
)

// ClassifySymbol delegates to marketdata.ClassifySymbol. It is kept here
// as a thin wrapper so existing `live.ClassifySymbol(...)` callers do not
// need to update their imports. The classification logic lives in
// pkg/marketdata.
func ClassifySymbol(tsCode string) Board {
	return marketdata.ClassifySymbol(tsCode)
}
