package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeIndexConstituents_DerivesInOutDatesFromSnapshots 钉住 DATA-01 的核心推导：
// index_weight 给的是**定期快照**（index_code, con_code, trade_date, weight），
// 没有进出日期；而 index_constituents 用 in_date/out_date 表达区间。
//
// 构造两期：
//
//	A 只在第一期（20241031）⇒ 已退出，out_date = 20241101（最后出现期的次日，排他上界）
//	B 两期都在           ⇒ 仍在，out_date 零值（落库成 NULL）
//	C 只在最新一期（20241129）⇒ 仍在，in_date = 20241129
func TestNormalizeIndexConstituents_DerivesInOutDatesFromSnapshots(t *testing.T) {
	client := &TushareClient{}

	resp := &TushareResponse{
		Code: 0,
		Data: TushareData{
			Fields: []string{"index_code", "con_code", "trade_date", "weight"},
			Items: [][]any{
				{"000300.SH", "600000.SH", "20241031", 1.5},
				{"000300.SH", "600036.SH", "20241031", 2.5},
				{"000300.SH", "600036.SH", "20241129", 2.7},
				{"000300.SH", "601318.SH", "20241129", 3.1},
			},
		},
	}

	got := client.normalizeIndexConstituents(resp, "000300.SH")
	require.Len(t, got, 3, "三只不同的成分股，按 (symbol) 合并成三行")

	bySymbol := map[string]struct {
		in, out time.Time
		weight  float64
	}{}
	for _, c := range got {
		bySymbol[c.Symbol] = struct {
			in, out time.Time
			weight  float64
		}{c.InDate, c.OutDate, c.Weight}
	}

	first, _ := time.Parse("20060102", "20241031")
	latest, _ := time.Parse("20060102", "20241129")

	a := bySymbol["600000.SH"]
	assert.Equal(t, first, a.in, "A：最早出现的那期为 in_date")
	assert.Equal(t, first.AddDate(0, 0, 1), a.out,
		"A：不在最新一期 ⇒ out_date = 最后出现期的次日（半开区间，保证自己那期仍在池内）")

	b := bySymbol["600036.SH"]
	assert.Equal(t, first, b.in)
	assert.True(t, b.out.IsZero(), "B：仍在最新一期 ⇒ out_date 零值（落库为 NULL）")
	assert.InDelta(t, 2.7, b.weight, 1e-9, "weight 取最新一期")

	c := bySymbol["601318.SH"]
	assert.Equal(t, latest, c.in, "C：只在最新期出现 ⇒ in_date = 最新期")
	assert.True(t, c.out.IsZero())
}

// TestNormalizeIndexConstituents_OldTwoColumnResponseYieldsNothing 是回归护栏：
// 旧代码请求 in_date/out_date 两个 index_weight 根本不存在的字段，Tushare 静默
// 只回两列，normalize 的 len(item) < 4 会把每一行全部跳过 ⇒ 表恒空。
// 现在同样的两列输入必须依旧是「无数据」而不是「造出零值日期的成分行」——
// 因为 trade_date 缺失时无法推导任何区间，编一行进去就是假数据。
func TestNormalizeIndexConstituents_OldTwoColumnResponseYieldsNothing(t *testing.T) {
	client := &TushareClient{}

	resp := &TushareResponse{
		Code: 0,
		Data: TushareData{
			Fields: []string{"index_code", "con_code"},
			Items: [][]any{
				{"000300.SH", "600000.SH"},
				{"000300.SH", "600036.SH"},
			},
		},
	}

	assert.Empty(t, client.normalizeIndexConstituents(resp, "000300.SH"),
		"缺 trade_date 时不得产出成分行（无法推导 in/out 区间）")
	assert.Nil(t, client.normalizeIndexConstituents(nil, "000300.SH"), "nil 响应安全返回 nil")
}
