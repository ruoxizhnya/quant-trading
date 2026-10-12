package main

// MacroHandler（P2-1 /api/macro/series 只读查询）的契约测试。
//
// 覆盖验收⑥的三种形态：正常查（200 + 数据）、空结果（200 + 空数组）、
// 非法参数（400，先于任何库访问）。另加两条 DB-free 的装配/可用性断言：
// nil store panic（接线 bug）与零值 pool 503（未接库不 panic）。
//
// 真库纪律同 handlers_industry_test.go：自己的数据自己造、自己验、自己删
// （独享 source 标记），**库不在就红不 skip**——本族用例自灌自证，红必须
// 指向「库没起」这个真实原因。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// macroSource 生成本用例独享的 source 标记（清理只删这个标记的行）。
func macroSource() string { return "itest-" + uuid.NewString() }

// macroCleanup 删 quant.macro_series 里本标记的行。
func macroCleanup(t *testing.T, store *storage.PostgresStore, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.DB().Exec(ctx, "DELETE FROM quant.macro_series WHERE source = $1", source); err != nil {
		t.Errorf("清理 quant.macro_series 残留失败（source=%s）: %v", source, err)
	}
}

// newMacroTestRouter 建真库 store（含 migrate——Migration 050 随之执行），
// 挂上 MacroHandler。库不在就红不 skip（本族用例自灌自证）。
func newMacroTestRouter(t *testing.T) (*gin.Engine, *storage.PostgresStore) {
	t.Helper()
	store, err := storage.NewPostgresStore(context.Background(),
		"postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable")
	if err != nil {
		t.Fatalf("连接测试库失败（库不在就红不 skip——先起 PG 再跑）: %v", err)
	}
	t.Cleanup(store.Close)
	router := gin.New()
	NewMacroHandler(store, zerolog.Nop()).RegisterRoutes(router)
	return router, store
}

// macroGet 发一次 GET 并解出 {total, data, limit, truncated, hint}
// （后三者是 R4 新增的 P2-1-R2 字段）。
func macroGet(t *testing.T, router *gin.Engine, path string) (*httptest.ResponseRecorder, struct {
	Total     int               `json:"total"`
	Data      []json.RawMessage `json:"data"`
	Limit     int               `json:"limit"`
	Truncated bool              `json:"truncated"`
	Hint      string            `json:"hint"`
}) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(w, req)

	var body struct {
		Total     int               `json:"total"`
		Data      []json.RawMessage `json:"data"`
		Limit     int               `json:"limit"`
		Truncated bool              `json:"truncated"`
		Hint      string            `json:"hint"`
	}
	if w.Code == http.StatusOK {
		require.NoErrorf(t, json.Unmarshal(w.Body.Bytes(), &body), "200 响应体应是 JSON: %s", w.Body.String())
	}
	return w, body
}

// ─── DB-free：装配与可用性 ──────────────────────────────────────────────

// TestNewMacroHandler_PanicsOnNilStore nil store = 接线 bug，构造期炸
// （与 NewIndustryHandler / NewEvidenceHandler 同款 fail-loud 契约）。
func TestNewMacroHandler_PanicsOnNilStore(t *testing.T) {
	assert.Panics(t, func() {
		NewMacroHandler(nil, zerolog.Nop())
	})
}

// TestMacroHandler_NilPoolReturns503 零值 store（未接库，pool 为 nil）：
// 路由照常注册，请求回 503 而不是 panic——「没库」是部署态，不是崩溃理由。
func TestMacroHandler_NilPoolReturns503(t *testing.T) {
	router := gin.New()
	NewMacroHandler(&storage.PostgresStore{}, zerolog.Nop()).RegisterRoutes(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/macro/series?indicator=CPI_YOY", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"零值 pool 的 /api/macro/series 应回 503（不是 panic，也不是 200 空数据）")
}

// ─── 契约三形态：正常 / 空结果 / 非法参数 ───────────────────────────────

func TestMacroHandler_SeriesRoundtripAndFilters(t *testing.T) {
	router, store := newMacroTestRouter(t)
	source := macroSource()
	t.Cleanup(func() { macroCleanup(t, store, source) })
	ctx := context.Background()

	rows := []*domain.MacroSeries{
		{Indicator: source + "-CPI_YOY", Freq: "M", ObsDate: time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), Value: 0.8, Unit: "%", Source: source, AsOf: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC)},
		{Indicator: source + "-CPI_YOY", Freq: "M", ObsDate: time.Date(2200, 2, 1, 0, 0, 0, 0, time.UTC), Value: 0.5, Unit: "%", Source: source, AsOf: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC)},
		{Indicator: source + "-CPI_YOY", Freq: "M", ObsDate: time.Date(2200, 3, 1, 0, 0, 0, 0, time.UTC), Value: 0.3, Unit: "%", Source: source, AsOf: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	require.NoError(t, store.SaveMacroSeriesBatch(ctx, rows))

	// 正常查：全量三条，按 obs_date 降序（R4：最新在前）+ 回显生效 limit。
	w, body := macroGet(t, router, "/api/macro/series?indicator="+source+"-CPI_YOY")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 3, body.Total)
	require.Len(t, body.Data, 3)
	assert.Contains(t, string(body.Data[0]), `"value":0.3`, "降序：最新一行（3 月）在前")
	assert.Equal(t, storage.MacroSeriesDefaultLimit, body.Limit, "未传 limit 时响应应回显默认值")
	assert.False(t, body.Truncated, "3 行 < 默认 limit，不应标记截断")

	// 日期区间过滤（闭区间）。
	w, body = macroGet(t, router, "/api/macro/series?indicator="+source+"-CPI_YOY&start_date=2200-02-01&end_date=2200-02-01")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, body.Total, "闭区间应只剩 2 月一条")
	assert.Contains(t, string(body.Data[0]), `"value":0.5`)

	// freq 过滤：不传则 M/D 都收；传 D 收不到月频行。
	w, body = macroGet(t, router, "/api/macro/series?indicator="+source+"-CPI_YOY&freq=D")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, body.Total)
	assert.Contains(t, w.Body.String(), `"data":[]`,
		"空结果应是空数组形状（JSON null 会让严格客户端炸）")
}

func TestMacroHandler_MissingIndicatorReturns400(t *testing.T) {
	router, _ := newMacroTestRouter(t)

	w, _ := macroGet(t, router, "/api/macro/series")
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"indicator 必填——缺它必须 400，且先于任何库访问")
	assert.Contains(t, w.Body.String(), "indicator")
}

func TestMacroHandler_BadDateReturns400(t *testing.T) {
	router, _ := newMacroTestRouter(t)

	for _, path := range []string{
		"/api/macro/series?indicator=CPI_YOY&start_date=2026/08/01",
		"/api/macro/series?indicator=CPI_YOY&end_date=08012026",
		"/api/macro/series?indicator=CPI_YOY&start_date=2026-09-01&end_date=2026-08-01",
	} {
		w, _ := macroGet(t, router, path)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s 应回 400", path)
	}
}

// TestMacroHandler_LimitParam：R4 的 HTTP 契约——limit 生效（降序取最新）、
// 达上限标记 truncated + hint、非法 limit（0/负数/非数字/超上限）一律 400。
func TestMacroHandler_LimitParam(t *testing.T) {
	router, store := newMacroTestRouter(t)
	source := macroSource()
	t.Cleanup(func() { macroCleanup(t, store, source) })
	ctx := context.Background()

	rows := make([]*domain.MacroSeries, 0, 3)
	for i := 0; i < 3; i++ {
		rows = append(rows, &domain.MacroSeries{
			Indicator: source + "-CPI_YOY", Freq: "M",
			ObsDate: time.Date(2200, time.Month(1+i), 1, 0, 0, 0, 0, time.UTC),
			Value:   float64(i), Unit: "%", Source: source,
			AsOf: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC),
		})
	}
	require.NoError(t, store.SaveMacroSeriesBatch(ctx, rows))

	// limit=2：返回最新 2 行（降序）+ truncated + hint。
	w, body := macroGet(t, router, "/api/macro/series?indicator="+source+"-CPI_YOY&limit=2")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, body.Total)
	assert.Equal(t, 2, body.Limit)
	assert.True(t, body.Truncated, "len(rows)==limit 时应标记可能截断（OPTION-A：不隐式嗅探）")
	assert.NotEmpty(t, body.Hint, "truncated 时应带日期下钻提示")
	assert.Contains(t, string(body.Data[0]), `"value":2`, "降序：最新一行在前")

	// limit=5：未达上限 → 不截断、无 hint。
	w, body = macroGet(t, router, "/api/macro/series?indicator="+source+"-CPI_YOY&limit=5")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 3, body.Total)
	assert.Equal(t, 5, body.Limit)
	assert.False(t, body.Truncated)
	assert.Empty(t, body.Hint)

	// 非法 limit → 400（0 是显式非法值——「未传」才是默认语义）。
	for _, bad := range []string{"0", "-1", "abc", strconv.Itoa(storage.MacroSeriesMaxLimit + 1)} {
		w, _ := macroGet(t, router, "/api/macro/series?indicator="+source+"-CPI_YOY&limit="+bad)
		assert.Equal(t, http.StatusBadRequest, w.Code, "limit=%s 应回 400", bad)
	}
}

func TestMacroHandler_UnknownIndicatorEmptyResult(t *testing.T) {
	// 目录外 indicator 不是 400——指标目录会增长（pkg/data 的
	// macroIndicatorSpecs 是唯一真相，handler 不复制一份）。空结果是
	// 合法答案：200 + 空数组。
	router, _ := newMacroTestRouter(t)

	w, body := macroGet(t, router, "/api/macro/series?indicator=NO_SUCH_INDICATOR")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, body.Total)
	assert.Empty(t, body.Data)
}
