package source

// D2-R1 ①：财报 connector 的 `ann_date`（披露日）透出链路。
//
// 缺口（D2 审查登记）：声明用保留记号 VisibleFieldAnnDate，但 connector 的
// wire 输出里**看不到**披露日——`fundamentalToMap` 无 ann_date、
// `Schema(DataTypeFundamental)` 也无该字段，锚点悬空、不可实测。
//
// 本文件钉住三件事：
//   1. 映射层：ann_date 透传（nil = 未披露 → 留空为 nil，**不折 0 / 不用零值时间**）；
//   2. schema 层：ann_date 作为 date 字段进 wire schema（非必填）；
//   3. 端到端：DataItem 里可见锚点（Data["ann_date"]）与 TradeTime
//      （报告期截止日）**不同物**——两者相同即前视洞复现（三季报 9/30 截止、
//      10/25 披露，用 TradeTime 当可见点等于 9/30 就可交易）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// fundamentalStubStore 只实现财报写入面 + 归档面；嵌入 nil TushareStore，
// 误调其它方法 panic（同包内其它 stub 的口径）。
type fundamentalStubStore struct {
	data.TushareStore
	saved []*domain.Fundamental
}

func (s *fundamentalStubStore) SaveFundamentalBatch(_ context.Context, records []*domain.Fundamental) error {
	s.saved = append(s.saved, records...)
	return nil
}

func (s *fundamentalStubStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// finaIndicatorBody 造一个 fina_indicator 响应。列顺序必须与
// data.TushareClient.normalizeFundamentals 的按位读取一致：
// 0=ts_code 1=ann_date 2=end_date 3..=pe/pb/ps/roe/roa/debt_to_equity/
// gross_margin/net_margin/revenue/net_profit/total_assets/total_liab。
// annDate 传空串 = 上游未给披露日。
func finaIndicatorBody(annDate, endDate string) string {
	item := []interface{}{
		"600519.SH", annDate, endDate,
		25.1, 8.2, 7.9, 26.3, 18.4, 0.35, 91.2, 52.7, 1200000.0, 620000.0, 3000000.0, 900000.0,
	}
	raw, _ := json.Marshal(item)
	return `{"code":0,"msg":"","data":{"fields":["ts_code","ann_date","end_date","pe","pb","ps","roe","roa","debt_to_equity","gross_margin","net_margin","revenue","net_profit","total_assets","total_liab"],"items":[` + string(raw) + `]}}`
}

func newFundamentalStubClient(t *testing.T, body string) (*data.TushareClient, *fundamentalStubStore) {
	t.Helper()
	store := &fundamentalStubStore{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return data.NewTushareClient("test-token", srv.URL, 0, store, nil), store
}

// TestFundamentalToMap_AnnDateNilSemantics 钉住映射层的缺失语义。
func TestFundamentalToMap_AnnDateNilSemantics(t *testing.T) {
	annDate := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	t.Run("披露日存在则原样透传", func(t *testing.T) {
		m := fundamentalToMap(domain.Fundamental{
			Symbol: "600519.SH", Date: endDate, AnnDate: &annDate,
		})
		got, ok := m["ann_date"]
		require.True(t, ok, "ann_date 必须进 wire map（D2-R1 的锚点实测载体）")
		p, isPtr := got.(*time.Time)
		require.True(t, isPtr, "ann_date 应是 *time.Time（与 PE/PB 等可空字段同口径），got %T", got)
		require.NotNil(t, p)
		assert.True(t, p.Equal(annDate), "ann_date = 披露日，got %v", p)
		// 与报告期截止日必须不同物——相同即锚点失效。
		assert.False(t, p.Equal(endDate), "披露日不得等于报告期截止日（本夹具刻意区分）")
	})

	t.Run("未披露留空为 nil 而不是零值时间", func(t *testing.T) {
		m := fundamentalToMap(domain.Fundamental{Symbol: "600519.SH", Date: endDate, AnnDate: nil})
		got, ok := m["ann_date"]
		require.True(t, ok, "键恒在（schema 声明的字段，形状稳定）")
		assert.Nil(t, got, "nil = 未披露，绝不折成 0 或 0001-01-01")
	})
}

// TestTushareAdapter_FundamentalSchemaProjectsAnnDate 钉住 wire schema 投影：
// ann_date 作为 date 字段存在且非必填（缺失≠0 的前提），并与声明的锚点同名。
func TestTushareAdapter_FundamentalSchemaProjectsAnnDate(t *testing.T) {
	a := NewTushareAdapter(nil)

	schema, err := a.Schema(DataTypeFundamental)
	require.NoError(t, err)

	var found *SchemaField
	for i := range schema.Fields {
		if schema.Fields[i].Name == "ann_date" {
			found = &schema.Fields[i]
		}
	}
	require.NotNil(t, found, "D2-R1 起 wire schema 必须投影 ann_date（锚点从悬空变可实测）")
	assert.Equal(t, "date", found.Type, "锚点载体必须是 date/datetime")
	assert.False(t, found.Required, "披露日缺失时留空——必填会把「未披露」逼成编造")

	// 声明与投影同名：锚点记号 = schema 字段名（validateReservedAnchorProjection 据此执法）。
	var decl ConnectorDeclaration
	for _, d := range a.Declarations() {
		if d.DataType == DataTypeFundamental {
			decl = d
		}
	}
	require.Equal(t, VisibleFieldAnnDate, decl.VisibleField)
	assert.Equal(t, VisibleFieldAnnDate, found.Name, "保留记号与投影字段同名，优先级见 contract.go")
}

// TestTushareAdapter_FetchFundamentals_AnnDateIsTheAnchorNotTradeTime 是端到端
// PIT 断言：三季报（9/30 截止、10/25 披露）——DataItem 的可见锚点必须是 10/25，
// TradeTime（报告期截止日）仍是 9/30，两者不同物。破坏腿：把 Data["ann_date"]
// 改成 f.Date（用报告期冒充披露日）→ 本测试红。
func TestTushareAdapter_FetchFundamentals_AnnDateIsTheAnchorNotTradeTime(t *testing.T) {
	client, store := newFundamentalStubClient(t, finaIndicatorBody("20261025", "20260930"))
	a := NewTushareAdapter(client)

	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeFundamental,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, resp.Items, 1)

	item := resp.Items[0]
	wantAnn := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	// 报告期截止日仍由 TradeTime 承载（D2 契约：不能拿它当可见点）。
	assert.True(t, item.TradeTime.Equal(wantEnd), "TradeTime = 报告期截止日，got %v", item.TradeTime)

	// 可见锚点在 wire 上：披露日。
	rawAnn, ok := item.Data["ann_date"]
	require.True(t, ok, "DataItem.Data 必须带 ann_date（D2-R1）")
	ann, isPtr := rawAnn.(*time.Time)
	require.True(t, isPtr, "ann_date 应是 *time.Time，got %T", rawAnn)
	require.NotNil(t, ann)
	assert.True(t, ann.Equal(wantAnn), "可见锚点 = 披露日 2026-10-25，got %v", ann)
	assert.False(t, ann.Equal(item.TradeTime), "锚点与 TradeTime 相同 = 前视洞复现")

	// 落库面（legacy 路径）收到同一批行——两条路径的披露日口径一致。
	require.Len(t, store.saved, 1)
	require.NotNil(t, store.saved[0].AnnDate)
	assert.True(t, store.saved[0].AnnDate.Equal(wantAnn))
}

// TestTushareAdapter_FetchFundamentals_NilAnnDateStaysNil 端到端缺失语义：
// 上游未给披露日 → wire 上 ann_date 为 nil（不是零值时间、不是 0）。
func TestTushareAdapter_FetchFundamentals_NilAnnDateStaysNil(t *testing.T) {
	client, _ := newFundamentalStubClient(t, finaIndicatorBody("", "20260930"))
	a := NewTushareAdapter(client)

	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeFundamental,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, resp.Items, 1)

	got, ok := resp.Items[0].Data["ann_date"]
	require.True(t, ok, "键恒在")
	assert.Nil(t, got, "未披露 → nil（缺失≠0）")
}
