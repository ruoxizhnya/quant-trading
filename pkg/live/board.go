// Package live — A-share board classification.
//
// P1-5 (ODR-018) — A 股价格笼子校验前置: 把 ts_code 分类到板块,
// 不同板块的笼子规则和涨跌幅不同。
//
// 分类规则参考:
//   - 上交所/深交所 主板 (MainBoardSH, MainBoardSZ): 60xxxx.SH, 601xxx.SH,
//     603xxx.SH, 605xxx.SH, 000xxx.SZ, 001xxx.SZ, 002xxx.SZ。
//     涨跌幅 ±10%; 自 2023-08 起实施"价格笼子", 申报价偏离最优价 ±2%
//     (但涨跌幅位置除外)。
//   - 创业板 (ChiNext, 300xxx.SZ): 涨跌幅 ±20%; 2020-08 注册制改革后
//     不再适用 2% 价格笼子 (但有 ±20% 笼子)。
//   - 科创板 (STAR, 688xxx.SH): 涨跌幅 ±20%; 同 ChiNext, 仅有 ±20% 笼子。
//   - 北交所 (BSE, 8xxxxx.BJ, 4xxxxx.BJ): 涨跌幅 ±30%; 注册制无 2% 笼子。
//   - ETF (159xxx.SZ, 510xxx-588xxx.SH): 涨跌幅 ±10%。
//   - LOF / closed-end fund: 涨跌幅 ±10%。
//   - 债券: 涨跌幅 ±10% (但价格笼子规则略有不同, 本实现统一为 ±10% + 不启用 2% 笼子)。
//
// 注: 新股上市前 5 个交易日无涨跌幅限制; ST 股票 ±5%; *ST 股票 ±5%。
// 这两类特殊标记需要外部 metadata (symbol → is_st / is_new), 不在
// 本模块职责范围内, 留待风控/合规模块 (P2-5) 处理。
package live

import (
	"strings"
)

// Board enumerates A-share trading boards. Each board has its own
// daily price limit and (for some) a "price cage" rule that further
// constrains limit-order prices relative to the current best quote.
type Board string

const (
	// BoardMainBoardSH — Shanghai Main Board (上交所主板).
	BoardMainBoardSH Board = "main_sh"
	// BoardMainBoardSZ — Shenzhen Main Board (深交所主板, 含 002 中小板).
	BoardMainBoardSZ Board = "main_sz"
	// BoardChiNext — 创业板 (Shenzhen, 300xxx).
	BoardChiNext Board = "chinext"
	// BoardSTAR — 科创板 (Shanghai, 688xxx).
	BoardSTAR Board = "star"
	// BoardBSE — 北交所 (Beijing Stock Exchange, 8xxxxx / 4xxxxx).
	BoardBSE Board = "bse"
	// BoardETF — 交易型开放式基金.
	BoardETF Board = "etf"
	// BoardBond — 国债 / 地方债 / 公司债.
	BoardBond Board = "bond"
	// BoardIndex — 指数 (如 000300.SH, 399001.SZ).
	BoardIndex Board = "index"
	// BoardFundLOF — LOF 基金 (16xxxx.SZ, 50xxxx.SH).
	BoardFundLOF Board = "lof"
	// BoardUnknown — 未知/无法识别 (默认 ±10% 限制 + 主板笼子, 偏保守).
	BoardUnknown Board = "unknown"
)

// String returns the canonical name of the board.
func (b Board) String() string { return string(b) }

// DailyPriceLimit returns the ±daily price limit (e.g. 0.10 for ±10%).
// Limit up = prev_close * (1 + limit), limit down = prev_close * (1 - limit).
//
// 新股 / ST 股票的 special case 由调用方在 metadata 中另行处理。
func (b Board) DailyPriceLimit() float64 {
	switch b {
	case BoardChiNext, BoardSTAR:
		return 0.20
	case BoardBSE:
		return 0.30
	case BoardMainBoardSH, BoardMainBoardSZ, BoardETF, BoardFundLOF, BoardBond:
		return 0.10
	default:
		return 0.10
	}
}

// HasCageRule reports whether the board applies a "price cage" rule
// that further constrains limit-order prices relative to the best bid/ask.
//
// 只有上交所/深交所主板在 2023-08 后启用了 ±2% 价格笼子。
// 创业板/科创板/北交所因涨跌幅本身较宽 (±20% / ±30%), 不再叠加
// 2% 笼子, 仅由日涨跌幅约束。
func (b Board) HasCageRule() bool {
	switch b {
	case BoardMainBoardSH, BoardMainBoardSZ:
		return true
	default:
		return false
	}
}

// CagePercent returns the cage width (e.g. 0.02 for ±2%). Only boards
// with HasCageRule() return a meaningful value; others return 0.
func (b Board) CagePercent() float64 {
	if !b.HasCageRule() {
		return 0
	}
	return 0.02
}

// ClassifySymbol classifies a ts_code (e.g. "000001.SZ") into a Board.
//
// Implementation notes:
//   - We accept the canonical "ts_code" form (6-digit code + '.' + exchange).
//   - Funds / bonds / indices are best-effort — a more authoritative
//     classification requires the `stocks` table (see pkg/storage/stocks.go).
//   - Empty / malformed symbols return BoardUnknown rather than erroring,
//     so the caller (price-cage validator) can apply the conservative
//     ±10% + ±2% defaults.
func ClassifySymbol(tsCode string) Board {
	code, exch, ok := splitTsCode(tsCode)
	if !ok {
		return BoardUnknown
	}
	if len(code) < 6 {
		return BoardUnknown
	}
	prefix3 := code[:3]
	prefix1 := code[:1]

	switch exch {
	case "SH":
		return classifySH(code, prefix3, prefix1)
	case "SZ":
		return classifySZ(code, prefix3, prefix1)
	case "BJ":
		return classifyBJ(code, prefix1)
	default:
		return BoardUnknown
	}
}

func classifySH(code, prefix3, prefix1 string) Board {
	// 科创板 688xxx
	if prefix3 == "688" {
		return BoardSTAR
	}
	// 上交所主板: 600/601/603/605
	if prefix3 == "600" || prefix3 == "601" || prefix3 == "603" || prefix3 == "605" {
		return BoardMainBoardSH
	}
	// ETF: 510xxx-589xxx (常见 510, 511, 512, 513, 515, 516, 517, 518, 588)
	if prefix3 == "510" || prefix3 == "511" || prefix3 == "512" || prefix3 == "513" ||
		prefix3 == "515" || prefix3 == "516" || prefix3 == "517" || prefix3 == "518" ||
		prefix3 == "588" {
		return BoardETF
	}
	// LOF / 场外基金: 50xxxx
	if prefix3 == "500" || prefix3 == "501" {
		return BoardFundLOF
	}
	// 国债 / 地方债 / 公司债 / 可转债: 10xxxx, 11xxxx, 12xxxx, 13xxxx
	if prefix1 == "1" {
		return BoardBond
	}
	// 指数: 000xxx (000300.SH 沪深300 等)
	if prefix3 == "000" {
		return BoardIndex
	}
	// B 股: 9xxxxx (面值 USD)
	if prefix1 == "9" {
		return BoardMainBoardSH // 退市风险, 暂归主板
	}
	_ = code
	return BoardUnknown
}

func classifySZ(code, prefix3, prefix1 string) Board {
	// 创业板 300xxx
	if prefix3 == "300" {
		return BoardChiNext
	}
	// 深交所主板: 000xxx, 001xxx, 002xxx (002 原中小板, 2021 合并入主板)
	if prefix3 == "000" || prefix3 == "001" || prefix3 == "002" {
		return BoardMainBoardSZ
	}
	// ETF: 159xxx
	if prefix3 == "159" {
		return BoardETF
	}
	// LOF: 16xxxx
	if prefix3 == "160" || prefix3 == "161" || prefix3 == "162" || prefix3 == "163" ||
		prefix3 == "164" || prefix3 == "165" || prefix3 == "166" || prefix3 == "167" ||
		prefix3 == "168" || prefix3 == "169" {
		return BoardFundLOF
	}
	// 债券: 10xxxx, 11xxxx, 12xxxx
	if prefix1 == "1" {
		return BoardBond
	}
	// 指数: 399xxx
	if prefix3 == "399" {
		return BoardIndex
	}
	// B 股: 2xxxxx
	if prefix1 == "2" {
		return BoardMainBoardSZ
	}
	_ = code
	return BoardUnknown
}

func classifyBJ(code, prefix1 string) Board {
	// 北交所: 8xxxxx, 4xxxxx, 92xxxx (老三板)
	if prefix1 == "8" || prefix1 == "4" || prefix1 == "9" {
		return BoardBSE
	}
	_ = code
	return BoardUnknown
}

// splitTsCode splits a ts_code like "000001.SZ" into (code, exchange, ok).
// The exchange is normalized to uppercase; the code is stripped of any
// leading zero padding issues by leaving it as the raw 6-digit string.
func splitTsCode(tsCode string) (string, string, bool) {
	tsCode = strings.TrimSpace(tsCode)
	if tsCode == "" {
		return "", "", false
	}
	// Accept both "000001.SZ" and "000001SZ".
	var code, exch string
	if idx := strings.LastIndex(tsCode, "."); idx > 0 {
		code = tsCode[:idx]
		exch = strings.ToUpper(tsCode[idx+1:])
	} else if len(tsCode) >= 8 {
		code = tsCode[:len(tsCode)-2]
		exch = strings.ToUpper(tsCode[len(tsCode)-2:])
	} else {
		return "", "", false
	}
	if !isAllDigits(code) {
		return "", "", false
	}
	if exch != "SH" && exch != "SZ" && exch != "BJ" {
		return "", "", false
	}
	return code, exch, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
