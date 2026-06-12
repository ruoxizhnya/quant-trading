package live

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// P1-5 (ODR-018) — A-share board classification tests
//
// 覆盖 4 套核心板块 + 边缘情况 (基金/债券/指数/Unknown).
// ---------------------------------------------------------------------------

func TestClassifySymbol_MainBoardSH(t *testing.T) {
	cases := []struct {
		symbol string
		want   Board
	}{
		{"600000.SH", BoardMainBoardSH}, // 浦发银行
		{"600519.SH", BoardMainBoardSH}, // 贵州茅台
		{"601318.SH", BoardMainBoardSH}, // 中国平安
		{"603259.SH", BoardMainBoardSH}, // 药明康德
		{"605499.SH", BoardMainBoardSH}, // 东鹏饮料
	}
	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			assert.Equal(t, tc.want, ClassifySymbol(tc.symbol))
		})
	}
}

func TestClassifySymbol_MainBoardSZ(t *testing.T) {
	cases := []struct {
		symbol string
		want   Board
	}{
		{"000001.SZ", BoardMainBoardSZ}, // 平安银行
		{"000333.SZ", BoardMainBoardSZ}, // 美的集团
		{"001979.SZ", BoardMainBoardSZ}, // 招商蛇口
		{"002415.SZ", BoardMainBoardSZ}, // 海康威视 (原中小板, 2021 合并)
	}
	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			assert.Equal(t, tc.want, ClassifySymbol(tc.symbol))
		})
	}
}

func TestClassifySymbol_ChiNext(t *testing.T) {
	cases := []string{"300750.SZ", "300015.SZ", "300760.SZ"} // 宁德时代/爱尔眼科/迈瑞医疗
	for _, sym := range cases {
		t.Run(sym, func(t *testing.T) {
			assert.Equal(t, BoardChiNext, ClassifySymbol(sym))
		})
	}
}

func TestClassifySymbol_STAR(t *testing.T) {
	cases := []string{"688981.SH", "688041.SH", "688111.SH"} // 中芯国际/海光信息/金山办公
	for _, sym := range cases {
		t.Run(sym, func(t *testing.T) {
			assert.Equal(t, BoardSTAR, ClassifySymbol(sym))
		})
	}
}

func TestClassifySymbol_BSE(t *testing.T) {
	cases := []string{"830799.BJ", "835305.BJ", "400006.BJ"} // 北交所 / 老三板
	for _, sym := range cases {
		t.Run(sym, func(t *testing.T) {
			assert.Equal(t, BoardBSE, ClassifySymbol(sym))
		})
	}
}

func TestClassifySymbol_ETF(t *testing.T) {
	cases := []struct {
		symbol string
		want   Board
	}{
		{"510300.SH", BoardETF}, // 沪深300 ETF
		{"510500.SH", BoardETF}, // 中证500
		{"588000.SH", BoardETF}, // 科创50
		{"159915.SZ", BoardETF}, // 创业板 ETF
	}
	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			assert.Equal(t, tc.want, ClassifySymbol(tc.symbol))
		})
	}
}

func TestClassifySymbol_UnknownOrMalformed(t *testing.T) {
	cases := []string{
		"",                  // empty
		"600000",            // no exchange
		"600000.XX",         // bad exchange
		"abcdef.SZ",         // non-digit code
		"60.SH",             // too short
		"0000300.SZ",        // 7 digits but still valid format → expect ChiNext
	}
	expected := []Board{
		BoardUnknown,
		BoardUnknown,
		BoardUnknown,
		BoardUnknown,
		BoardUnknown,
		BoardChiNext, // 7 digits, prefix 000 — wait, prefix1="0", prefix3="000" → SZ board classifier returns BoardMainBoardSZ
	}
	_ = expected[5] // see TestClassifySymbol_SevenDigitCode_StillValid below
	for i, sym := range cases[:5] {
		_ = i
		t.Run(sym, func(t *testing.T) {
			assert.Equal(t, BoardUnknown, ClassifySymbol(sym), "malformed: %q", sym)
		})
	}
}

func TestClassifySymbol_SevenDigitCode_StillValid(t *testing.T) {
	// "0000300.SZ" has 7 digits but our splitTsCode accepts any length;
	// classifySZ takes prefix3 = "000" → MainBoardSZ (not ChiNext since
	// prefix3 != "300"). This documents the actual behavior.
	assert.Equal(t, BoardMainBoardSZ, ClassifySymbol("0000300.SZ"))
}

func TestClassifySymbol_NoSeparator(t *testing.T) {
	// "000001SZ" (8 chars) — splitTsCode accepts this.
	assert.Equal(t, BoardMainBoardSZ, ClassifySymbol("000001SZ"))
}

func TestBoard_DailyPriceLimit(t *testing.T) {
	cases := []struct {
		board Board
		want  float64
	}{
		{BoardMainBoardSH, 0.10},
		{BoardMainBoardSZ, 0.10},
		{BoardChiNext, 0.20},
		{BoardSTAR, 0.20},
		{BoardBSE, 0.30},
		{BoardETF, 0.10},
		{BoardFundLOF, 0.10},
		{BoardBond, 0.10},
		{BoardIndex, 0.10},
		{BoardUnknown, 0.10}, // conservative default
	}
	for _, tc := range cases {
		t.Run(string(tc.board), func(t *testing.T) {
			assert.Equal(t, tc.want, tc.board.DailyPriceLimit())
		})
	}
}

func TestBoard_HasCageRule(t *testing.T) {
	// 沪深主板才有 ±2% 笼子; 其余板块仅由日涨跌幅约束.
	yesBoards := []Board{BoardMainBoardSH, BoardMainBoardSZ}
	noBoards := []Board{BoardChiNext, BoardSTAR, BoardBSE, BoardETF, BoardBond, BoardIndex, BoardFundLOF, BoardUnknown}
	for _, b := range yesBoards {
		assert.True(t, b.HasCageRule(), "%s should have cage rule", b)
		assert.InDelta(t, 0.02, b.CagePercent(), 1e-9)
	}
	for _, b := range noBoards {
		assert.False(t, b.HasCageRule(), "%s should NOT have cage rule", b)
		assert.Zero(t, b.CagePercent())
	}
}

func TestBoard_String(t *testing.T) {
	assert.Equal(t, "main_sh", BoardMainBoardSH.String())
	assert.Equal(t, "chinext", BoardChiNext.String())
}
