package main

// DATA-07 (a2)：`GET /market/index` 的响应从来没有 volume/amount，
// 而库里 `ohlcv_daily_qfq` 就有 `volume`（股）与 `turnover`（CNY）两列 —— 缺的是**通道**，
// 不是数据（ui 那两格因此恒 '-'）。
//
// 本文件钉三层，各自守什么写清（DATA-02 自审的教训：护栏不靠叙述自证）：
//   1. `marketIndexPayload` 纯映射：键名、**单位原样透出**（不 ×100 / 不 ÷1e8，
//      DATA-04/05 已把库里口径定成股 / CNY，换算一次就有两处真相）、
//      NULL turnover 读成 null 而不是 0、无 prev close 时 change/pct 为 null
//      （「缺失不等于 0%」——把无从算起的涨跌幅显示成 +0.00% 是假数字）。
//   2. HTTP 面通道：请求真的把 store 的读数摊出去，且库里没有指数行时如实回
//      `{"indices":[]}` —— 不编造条目（工单 ② 实测：三个指数代码 0 行）。
//   3. 读失败要响：库侧失败 ⇒ 500，不是回一份缺字段的 200。
//
// 端到端「响应 == 库逐字段」的真值腿在隔离库上做（一次性 PG 5433 + 自建指数行 +
// curl），因为它要的是**指数代码**有行，而共享开发库里那些代码 0 行，插进去会
// 让并发跑的因子/票池读数把指数当股票看见。库侧逐字段腿见
// pkg/storage/market_readout_test.go。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func TestMarketIndexPayload_PassesUnitsThroughAndKeepsNull(t *testing.T) {
	t.Parallel()

	amount := 375461676.1428
	bars := []storage.MarketIndexBar{
		{Code: "000300.SH", Close: 4439.1443, Volume: 151965787, Turnover: &amount, PrevClose: ptrF(4400.5)},
		{Code: "000001.SH", Close: 3000, Volume: 200000000, Turnover: nil, PrevClose: nil},
	}

	raw, err := json.Marshal(marketIndexPayload(bars))
	require.NoError(t, err)

	var body struct {
		Indices []struct {
			Code   string   `json:"code"`
			Name   string   `json:"name"`
			Close  float64  `json:"close"`
			Change *float64 `json:"change"`
			Pct    *float64 `json:"pct"`
			Volume float64  `json:"volume"`
			Amount *float64 `json:"amount"`
		} `json:"indices"`
	}
	require.NoError(t, json.Unmarshal(raw, &body), string(raw))
	require.Len(t, body.Indices, 2)

	first := body.Indices[0]
	assert.Equal(t, "000300.SH", first.Code)
	assert.Equal(t, "沪深300", first.Name, "代码→中文名由 core 给，前端不再各自维护一份")
	assert.InDelta(t, 4439.1443, first.Close, 1e-9)
	// 单位原样：库里的股 / CNY 直接是响应的 volume / amount。
	assert.InDelta(t, 151965787, first.Volume, 1e-6,
		"volume 必须原样透出（股）；除以 1e8 或乘 100 都是第二处换算，DATA-04/05 已定口径")
	require.NotNil(t, first.Amount, "turnover 有值时 amount 必须给出去（这正是 DATA-07 缺的通道）")
	assert.InDelta(t, amount, *first.Amount, 1e-6,
		"amount 必须原样透出 CNY；换算成亿元是展示层的事（ui format.ts 的 fmtAmount 自己除 1e8）")
	require.NotNil(t, first.Change)
	assert.InDelta(t, 38.6443, *first.Change, 1e-6)
	require.NotNil(t, first.Pct)
	assert.InDelta(t, (4439.1443-4400.5)/4400.5*100, *first.Pct, 1e-9)

	second := body.Indices[1]
	assert.Nil(t, second.Amount,
		"turnover 为 NULL 时 amount 必须是 null，不能读成 0 —— 0 会被界面显示成「成交额 0 元」这个假数字")
	assert.Nil(t, second.Change, "没有上一期收盘时涨跌幅无从算起，必须是 null 而不是 0（+0.00% 是假数字）")
	assert.Nil(t, second.Pct)
	assert.InDelta(t, 200000000, second.Volume, 1e-6)
}

// TestMarketIndexHandler_ChannelAndHonestEmpty 走 HTTP 面：通道在，且库里
// 没有指数行时如实回空数组（工单 ② 的实测形状：`{"indices":[]}`）。
func TestMarketIndexHandler_ChannelAndHonestEmpty(t *testing.T) {
	store, err := storage.NewPostgresStoreConnected(context.Background(), stocksCountTestDSN)
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	t.Cleanup(store.Close)

	router := gin.New()
	router.GET("/market/index", marketIndexHandler(store))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/market/index", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Indices []map[string]json.RawMessage `json:"indices"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())

	// 键恒在，且 indices 为数组不是 null（ui api-health.spec.ts 也断 Array）。
	require.NotNil(t, body.Indices, "indices 必须序列化成数组（空也是 []，不是 null）")

	// 库里那三个指数代码当前 0 行 ⇒ 空数组是**诚实读数**，不是缺陷；
	// 若将来灌进指数日线，条目必须带 volume/amount 两个键 —— 这条把形状钉住。
	for _, entry := range body.Indices {
		for _, key := range []string{"code", "name", "close", "change", "pct", "volume", "amount"} {
			_, ok := entry[key]
			assert.True(t, ok, "每个指数读数必须带 %q 键（DATA-07 的通道）；实际条目：%v", key, entry)
		}
	}
}

// TestMarketIndexHandler_ReadFailureFailsLoud 钉失败面：库侧读失败 ⇒ 500，
// 不回一份「indices 缺席所以界面空白」的 200。
//
// ⚠️ 分工：本条守的是「handler 不把库侧 error 折成空数组」。它抓不到单位换算错误
// （那是纯映射腿的事），也抓不到「prev close 查询单独失败」（同一条 SQL 一趟取回，
// 结构上不存在半途状态）。
func TestMarketIndexHandler_ReadFailureFailsLoud(t *testing.T) {
	store, err := storage.NewPostgresStoreConnected(context.Background(), stocksCountTestDSN)
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	t.Cleanup(store.Close)

	router := gin.New()
	router.GET("/market/index", marketIndexHandler(store))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/market/index", nil).WithContext(cancelled)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"读失败必须回 500；回 200 加空数组就是把「读失败」显示成「今天没有行情」；实际 body：%s", w.Body.String())
}

func ptrF(v float64) *float64 { return &v }
