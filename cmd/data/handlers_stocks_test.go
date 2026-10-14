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
	body := stockCountPayload(map[string]int64{"active": 5569, "delisted": 339})
	assert.Equal(t, int64(5908), body["count"], "count 应等于各档之和")
	assert.Equal(t, map[string]int64{"active": 5569, "delisted": 339}, body["by_status"],
		"by_status 应原样带出各档")

	// 空表：by_status 必须是空对象而非 null（前端按对象读）。
	encoded, err := json.Marshal(stockCountPayload(map[string]int64{}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":0,"by_status":{}}`, string(encoded))

	// 反证腿：调用方给 nil（未来新增的错误路径）也不能漏出 null。
	encoded, err = json.Marshal(stockCountPayload(nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":0,"by_status":{}}`, string(encoded))
}

// TestStocksCountHandler_StatusSplitMatchesDatabase 是经 HTTP 面的正证据：
// 端点发出去的拆分逐值等于库里 `GROUP BY status`，且 count 与拆分同源。
//
// oracle 只读**一趟**（原始 GROUP BY，折叠在 Go 里再实现一遍）：本包与 pkg/storage
// 的测试都会在开发库 stocks 里插删探针行，每多一趟读就多一个「两次读数之间被人改过」
// 的假红窗口。语义层面（NULL/空串折同一档、跨档相加）的独立核对在
// pkg/storage/stocks_status_test.go，本条只管**通道**：键名、类型、序列化形状。
func TestStocksCountHandler_StatusSplitMatchesDatabase(t *testing.T) {
	store, err := storage.NewPostgresStore(context.Background(), stocksCountTestDSN)
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
