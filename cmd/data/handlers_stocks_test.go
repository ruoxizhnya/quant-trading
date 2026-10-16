package main

// DATA-02: 票池状态拆分的只读通道。
//
// 这块缺的不是数据（stocks 有 status 与 delist_date），是**HTTP 面**：SPA 能走到的
// 只有 `GET /api/stocks/count`，而它只回 `{"count":5908}` —— 这 5908 含已退市，
// 叫"在市股票数"就是假数字。于是前端只能挂"缺读数通道"的空态，也不许自己
// `5908 - 339`（同一概念两处真相）。
//
// 钉三件事：
//  1. 端点确实把拆分发出去了，且逐值等于库里的 `GROUP BY status`（工单 ③ 口径）。
//  2. count 与 by_status 是**同一趟查询**的产物：count == 各档之和，不是第二次 COUNT(*)。
//  3. 空表/无行时 `by_status` 序列化成 `{}` 而不是 `null` —— 前端类型按对象读，
//     拿到 null 会在客户端炸或者被当成"没有这个字段"。

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// stocksCountTestDSN 与本包 handlers_ingest_test.go 的 ingestTestDSN 同源：
// 连不上开发库就 skip，不 red。
const stocksCountTestDSN = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"

func TestStockCountPayload_Shape(t *testing.T) {
	t.Parallel()

	// 有行：count 由各档求和导出。
	body := stockCountPayload(map[string]int64{"active": 5569, "delisted": 339}, ptrString("2026-09-24"))
	assert.Equal(t, int64(5908), body["count"], "count 应等于各档之和")
	assert.Equal(t, map[string]int64{"active": 5569, "delisted": 339}, body["by_status"],
		"by_status 应原样带出各档")

	// latest_date 有读数时原样带出（不加工、不截断）—— 按**序列化结果**断言，
	// 而不是 gin.H 里的动态类型：契约是 JSON，不是 Go 值。
	encodedWithDate, err := json.Marshal(stockCountPayload(
		map[string]int64{"active": 1}, ptrString("2026-09-24")))
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":1,"by_status":{"active":1},"latest_date":"2026-09-24"}`,
		string(encodedWithDate))

	// 空表：by_status 必须是空对象而非 null（前端按对象读）。
	encoded, err := json.Marshal(stockCountPayload(map[string]int64{}, ptrString("2026-09-24")))
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":0,"by_status":{},"latest_date":"2026-09-24"}`, string(encoded))

	// 反证腿：调用方给 nil（未来新增的错误路径）也不能漏出 null。
	encoded, err = json.Marshal(stockCountPayload(nil, ptrString("2026-09-24")))
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":0,"by_status":{},"latest_date":"2026-09-24"}`, string(encoded))
}

// TestStockCountPayload_LatestDateKeyAlwaysPresent 是 DATA-03 (a1) 的形状腿：
// **没有读数时字段必须显式为 null，而不是整个键消失**。
//
// 缺陷形状就是「键缺席」：前端类型声明了 `latest_date: string`，实测拿到
// undefined，而 HTTP 面上「查失败」「表是空的」「没实现」三种情形挤在同一个
// 缺席里，谁也分辨不了。守这条的是本腿 —— 把 payload 改回
// `if latestDate != nil { resp[...] = ... }`（即旧形状）它就红。
func TestStockCountPayload_LatestDateKeyAlwaysPresent(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(stockCountPayload(map[string]int64{"active": 1}, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":1,"by_status":{"active":1},"latest_date":null}`, string(encoded),
		"无读数时 latest_date 必须是显式 null（键恒在），不是缺席")

	// 键存在性与值分离：JSON 里能数出这个键，序列化不是 `{}` 少一项。
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &raw))
	_, ok := raw["latest_date"]
	assert.True(t, ok, "latest_date 这个键必须恒在；缺席就等于契约说谎（DATA-03 的缺陷形状）")
}

func ptrString(v string) *string { return &v }

// TestStocksCountHandler_StatusSplitMatchesDatabase 是经 HTTP 面的正证据：
// 端点发出去的拆分逐值等于库里 `GROUP BY status`，且 count 与拆分同源。
//
// oracle 只读**一趟**（原始 GROUP BY，折叠在 Go 里再实现一遍）：本包与 pkg/storage
// 的测试都会在开发库 stocks 里插删探针行，每多一趟读就多一个「两次读数之间被人改过」
// 的假红窗口。语义层面（NULL/空串折同一档、跨档相加）的独立核对在
// pkg/storage/stocks_status_test.go，本条只管**通道**：键名、类型、序列化形状。
func TestStocksCountHandler_StatusSplitMatchesDatabase(t *testing.T) {
	store, err := storage.NewPostgresStoreConnected(context.Background(), stocksCountTestDSN)
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	t.Cleanup(store.Close)
	ctx := context.Background()

	router := gin.New()
	router.GET("/stocks/count", stocksCountHandler(store))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stocks/count", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Count    int64            `json:"count"`
		ByStatus map[string]int64 `json:"by_status"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())

	// status 原始列可空，如实接 NullString；NULL 与 '' 折进 unknown 一档并**相加**。
	rows, err := store.DB().Query(ctx, `SELECT status, COUNT(*) FROM stocks GROUP BY status`)
	require.NoError(t, err)
	want := map[string]int64{}
	for rows.Next() {
		var status sql.NullString
		var n int64
		require.NoError(t, rows.Scan(&status, &n))
		key := "unknown"
		if status.Valid && status.String != "" {
			key = status.String
		}
		want[key] += n
	}
	rows.Close()
	require.NoError(t, rows.Err())

	if len(want) == 0 {
		t.Skip("stocks 表为空，通道核对无从谈起（AUD-51：前置与断言读的都是 stocks）")
	}

	assert.Equal(t, want, body.ByStatus, "by_status 应逐值等于库里的 GROUP BY，响应体：%s", w.Body.String())

	// count 与拆分同源：等于 oracle 各档之和，也等于自己响应体里各档之和。
	total := int64(0)
	for _, n := range want {
		total += n
	}
	assert.Equal(t, total, body.Count, "count 应等于票池全表行数（含已退市）")

	bodySum := int64(0)
	for _, n := range body.ByStatus {
		bodySum += n
	}
	assert.Equal(t, body.Count, bodySum, "count 与 by_status 必须同源：响应体内各档之和应等于 count")
}

// TestStocksCountHandler_LatestDatePresentAndMatchesDatabase 是 DATA-03 (a1) 的
// HTTP 面正证据：`latest_date` 这个键**在**，且逐值等于库里的 MAX(trade_date)。
//
// 工单 ② 说的「这一趟就没成」在 2026-10-16 被实测订正成更准的形状：查询本身成功，
// 是 handler 把 `MAX(trade_date)`（date 类型）扫进 `**string`，pgx 直接拒
// （`cannot scan date (OID 1082) in binary format into **string`），而那条 error
// 被丢弃 ⇒ 这个字段**在任何库里都不可能出现过**。本条红就等于该形状复发。
//
// 两趟读（读数 + oracle）之间有并发同步推进 MAX 的假红窗口，与上面 DATA-02 那条
// 同一取舍：自灌探针会把**全表** MAX 推后，带偏 pkg/data 里读全局 max 的
// TestAdjustmentBasis，代价更大。
func TestStocksCountHandler_LatestDatePresentAndMatchesDatabase(t *testing.T) {
	store, err := storage.NewPostgresStoreConnected(context.Background(), stocksCountTestDSN)
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	t.Cleanup(store.Close)
	ctx := context.Background()

	var oracle *string
	require.NoError(t, store.DB().QueryRow(ctx,
		`SELECT to_char(MAX(trade_date), 'YYYY-MM-DD') FROM ohlcv_daily_qfq`).Scan(&oracle))
	if oracle == nil {
		t.Skip("前置：ohlcv_daily_qfq 为空（新库未灌数据）——断言读的就是这东西，与前置同源（AUD-51）")
	}

	router := gin.New()
	router.GET("/stocks/count", stocksCountHandler(store))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stocks/count", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 用 map 解，才能区分「键缺席」与「键为 null」—— 结构体解出来两者都是零值，
	// 而 DATA-03 的缺陷形状恰恰是「缺席」。
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw), w.Body.String())
	value, ok := raw["latest_date"]
	require.True(t, ok, "响应里必须有 latest_date 这个键（SPEC 声明了它）；实际 body：%s", w.Body.String())
	assert.NotEqual(t, "null", string(value),
		"库里有行情日期时该字段不该是 null，否则读失败与空表又被混成一格；实际 body：%s", w.Body.String())

	var got string
	require.NoError(t, json.Unmarshal(value, &got))
	assert.Equal(t, *oracle, got, "端点发出去的 latest_date 逐值等于库里的 MAX(trade_date)")
}

// TestStocksCountHandler_ReadFailureFailsLoud 钉「读失败要响」：请求 ctx 已取消时
// 端点回 500，而不是把一个读失败的字段静默缺席后回 200。
//
// ⚠️ 分工写清，别夸本条：**这条抓不到「只把 latest_date 的 error 丢掉」这一种改坏**。
// ctx 取消时两趟读都会失败，从外部区分不了是哪趟；且健康的库里丢掉 error 后行为等价
// （无 error 可读）。那一面由三处守：库侧 TestLatestQuoteDate_PropagatesQueryError
// （error 必须上抛，不被折成 nil）+ 上面那条逐值核对（读失败 ⇒ 值不可能对）+
// 活进程真实失败路径（隔离库里 DROP 表后 curl 必须 500，见 DATA-03 台账）。
//
// 本条**不断言失败文案**：ctx 取消先撞在票池拆分那一趟（:90，既有调用点走 Wrap ⇒
// 通用文案），点名「哪一步」那条腿由只有一趟读的 /market/index 守
// （TestMarketIndexHandler_ReadFailureFailsLoud），不在这里冒充覆盖。
// 「Wrap 在 5xx 丢弃 message / FailCause 留住静态文案」这件事本身，由 pkg/httpserver
// 的 TestWrap_At5xxDiscardsTheMessage 与 TestFailCause_KeepsStageTextAt5xx 两条腿钉。
func TestStocksCountHandler_ReadFailureFailsLoud(t *testing.T) {
	store, err := storage.NewPostgresStoreConnected(context.Background(), stocksCountTestDSN)
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	t.Cleanup(store.Close)

	router := gin.New()
	router.GET("/stocks/count", stocksCountHandler(store))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/stocks/count", nil).WithContext(cancelled)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"读失败不得回 200 并少一个字段（那正是 DATA-03 的缺陷形状）；实际 body：%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "by_status",
		"失败响应不得带半份成功数据；实际 body：%s", w.Body.String())
}
