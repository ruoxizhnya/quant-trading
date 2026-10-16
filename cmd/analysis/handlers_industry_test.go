package main

// IndustryHandler（P2-2 /api/industry/* 只读查询）的契约测试。
//
// 覆盖验收⑥的三种形态：正常查（200 + 数据）、空结果（200 + 空数组）、
// 非法参数（400，先于任何库访问）。另加两条 DB-free 的装配/可用性断言：
// nil store panic（接线 bug）与零值 pool 503（未接库不 panic）。
//
// 真库纪律同 pkg/strategy/signal_store_test.go：自己的数据自己造、自己验、
// 自己删（独享 source 标记），**库不在就红不 skip**——本族用例自灌自证，
// 红必须指向「库没起」这个真实原因。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// indSource 生成本用例独享的 source 标记（清理只删这个标记的行）。
func indSource() string { return "itest-" + uuid.NewString() }

// indCleanup 删四张表里本标记的行。
func indCleanup(t *testing.T, store *storage.PostgresStore, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, table := range []string{
		"quant.sw_industry_tree",
		"quant.supply_chain_edge",
		"quant.industry_vitality",
		"quant.etf_constituent",
	} {
		if _, err := store.DB().Exec(ctx, "DELETE FROM "+table+" WHERE source = $1", source); err != nil {
			t.Errorf("清理 %s 残留失败（source=%s）: %v", table, source, err)
		}
	}
}

// newIndustryTestRouter 建真库 store（不迁移入口，DATA-09；Migration 049 由启动路径落库），
// 挂上 IndustryHandler。库不在就红不 skip（本族用例自灌自证）。
func newIndustryTestRouter(t *testing.T) (*gin.Engine, *storage.PostgresStore) {
	t.Helper()
	store, err := storage.NewPostgresStoreConnected(context.Background(),
		"postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable")
	if err != nil {
		t.Fatalf("连接测试库失败（库不在就红不 skip——先起 PG 再跑）: %v", err)
	}
	t.Cleanup(store.Close)
	router := gin.New()
	NewIndustryHandler(store, zerolog.Nop()).RegisterRoutes(router)
	return router, store
}

// industryGet 发一次 GET 并解出 {total, data}。
func industryGet(t *testing.T, router *gin.Engine, path string) (*httptest.ResponseRecorder, struct {
	Total int               `json:"total"`
	Data  []json.RawMessage `json:"data"`
}) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(w, req)

	var body struct {
		Total int               `json:"total"`
		Data  []json.RawMessage `json:"data"`
	}
	if w.Code == http.StatusOK {
		require.NoErrorf(t, json.Unmarshal(w.Body.Bytes(), &body), "200 响应体应是 JSON: %s", w.Body.String())
	}
	return w, body
}

// ─── DB-free：装配与可用性 ──────────────────────────────────────────────

// TestNewIndustryHandler_PanicsOnNilStore nil store = 接线 bug，构造期炸
// （与 NewEvidenceHandler 同款 fail-loud 契约）。
func TestNewIndustryHandler_PanicsOnNilStore(t *testing.T) {
	assert.Panics(t, func() {
		NewIndustryHandler(nil, zerolog.Nop())
	})
}

// TestIndustryHandler_NilPoolReturns503 零值 store（未接库，pool 为 nil）：
// 路由照常注册，请求回 503 而不是 panic——「没库」是部署态，不是崩溃理由。
func TestIndustryHandler_NilPoolReturns503(t *testing.T) {
	router := gin.New()
	NewIndustryHandler(&storage.PostgresStore{}, zerolog.Nop()).RegisterRoutes(router)

	for _, path := range []string{
		"/api/industry/tree",
		"/api/industry/edges?downstream=x",
		"/api/industry/vitality?industry_code=x",
		"/api/industry/etf/510300.SH/constituents",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code,
			"零值 pool 的 %s 应回 503（不是 panic，也不是 200 空数据）", path)
	}
}

// ─── 行业树 ─────────────────────────────────────────────────────────────

func TestIndustryHandler_TreeRoundtripAndFilters(t *testing.T) {
	router, store := newIndustryTestRouter(t)
	source := indSource()
	t.Cleanup(func() { indCleanup(t, store, source) })

	l1 := source + "-L1"
	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.SaveIndustryTreeBatch(context.Background(), []*domain.SWIndustryNode{
		{Code: l1, ParentCode: "", Level: 1, Name: "测试一级", Source: source, AsOf: asOf},
		{Code: source + "-L2a", ParentCode: l1, Level: 2, Name: "测试二级甲", IsLeaf: true, Source: source, AsOf: asOf},
		{Code: source + "-L2b", ParentCode: l1, Level: 2, Name: "测试二级乙", IsLeaf: true, Source: source, AsOf: asOf},
	}))

	// 正常查：全量树（含库里别人的行），本用例节点必须在。
	w, body := industryGet(t, router, "/api/industry/tree")
	require.Equal(t, http.StatusOK, w.Code)
	require.GreaterOrEqual(t, body.Total, 3, "全量至少含本用例 3 个节点")
	var sawL1, sawL2a bool
	for _, raw := range body.Data {
		var n domain.SWIndustryNode
		require.NoError(t, json.Unmarshal(raw, &n))
		if n.Code == l1 {
			sawL1 = true
			require.Equal(t, 1, n.Level)
			require.Equal(t, "测试一级", n.Name)
			require.False(t, n.IsLeaf)
			require.Equal(t, source, n.Source, "source 溯源列应透出")
		}
		if n.Code == source+"-L2a" {
			sawL2a = true
			require.True(t, n.IsLeaf)
		}
	}
	require.True(t, sawL1 && sawL2a, "入库的节点应能通过 API 读回")

	// 按 level 过滤。
	w, body = industryGet(t, router, "/api/industry/tree?level=1")
	require.Equal(t, http.StatusOK, w.Code)
	for _, raw := range body.Data {
		var n domain.SWIndustryNode
		require.NoError(t, json.Unmarshal(raw, &n))
		require.Equal(t, 1, n.Level, "level=1 不应混入其它层级")
	}

	// 按 parent_code 过滤（显式查子节点）。
	w, body = industryGet(t, router, "/api/industry/tree?parent_code="+l1)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 2, body.Total, "该父下应恰有两个子节点")

	// 空结果：不存在的父 → 200 + 空数组（不是 null，不是 404）。
	w, body = industryGet(t, router, "/api/industry/tree?parent_code=NO_SUCH_PARENT")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 0, body.Total)
	require.NotNil(t, body.Data, "空结果的 data 应是 [] 而不是 null")
	require.Contains(t, w.Body.String(), `"data":[]`, "空结果序列化为空数组")
}

func TestIndustryHandler_TreeBadLevelReturns400(t *testing.T) {
	router, _ := newIndustryTestRouter(t)
	for _, q := range []string{"abc", "0", "-1", "1.5"} {
		w, _ := industryGet(t, router, "/api/industry/tree?level="+q)
		assert.Equal(t, http.StatusBadRequest, w.Code, "level=%s 应 400", q)
	}
}

// ─── 上下游映射 ─────────────────────────────────────────────────────────

func TestIndustryHandler_EdgesRoundtripAndGuards(t *testing.T) {
	router, store := newIndustryTestRouter(t)
	source := indSource()
	t.Cleanup(func() { indCleanup(t, store, source) })

	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.SaveSupplyChainEdgeBatch(context.Background(), []*domain.SupplyChainEdge{
		{Upstream: source + "-stk", Downstream: source + "-ind", Relation: domain.RelationIndustryMember, Source: source, AsOf: asOf},
		{Upstream: source + "-stk", Downstream: source + "-ind2", Relation: domain.RelationIndustryMember, Source: source, AsOf: asOf},
	}))

	// 正常查：按 downstream。
	w, body := industryGet(t, router, "/api/industry/edges?downstream="+source+"-ind")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, body.Total)
	var e domain.SupplyChainEdge
	require.NoError(t, json.Unmarshal(body.Data[0], &e))
	require.Equal(t, source+"-stk", e.Upstream)
	require.Equal(t, domain.RelationIndustryMember, e.Relation)

	// 按 upstream 查两条。
	_, body = industryGet(t, router, "/api/industry/edges?upstream="+source+"-stk")
	require.Equal(t, 2, body.Total)

	// 空结果：200 + []。
	w, body = industryGet(t, router, "/api/industry/edges?downstream=NO_SUCH")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 0, body.Total)
	require.Contains(t, w.Body.String(), `"data":[]`)

	// 非法参数：无过滤 → 400（边表不接受全表扫描）。
	w, _ = industryGet(t, router, "/api/industry/edges")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ─── 行业景气 ───────────────────────────────────────────────────────────

func TestIndustryHandler_VitalityRoundtripAndGuards(t *testing.T) {
	router, store := newIndustryTestRouter(t)
	source := indSource()
	t.Cleanup(func() { indCleanup(t, store, source) })

	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.SaveIndustryVitalityBatch(context.Background(), []*domain.IndustryVitality{
		{IndustryCode: source + "-ind", Metric: "gross_margin", ObsDate: time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), Value: 13.42, Unit: "%", Source: source, AsOf: asOf},
		{IndustryCode: source + "-ind", Metric: "gross_margin", ObsDate: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC), Value: 14.01, Unit: "%", Source: source, AsOf: asOf},
		{IndustryCode: source + "-ind", Metric: "revenue_yoy", ObsDate: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC), Value: -2.31, Unit: "%", Source: source, AsOf: asOf},
	}))

	// 正常查：按行业。
	w, body := industryGet(t, router, "/api/industry/vitality?industry_code="+source+"-ind")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 3, body.Total)
	var v domain.IndustryVitality
	require.NoError(t, json.Unmarshal(body.Data[0], &v))
	require.Equal(t, "gross_margin", v.Metric)
	require.InDelta(t, 13.42, v.Value, 1e-12)
	require.Equal(t, "%", v.Unit)

	// 按指标 + 日期区间。
	_, body = industryGet(t, router,
		"/api/industry/vitality?industry_code="+source+"-ind&metric=gross_margin&start_date=2200-01-02&end_date=2200-01-02")
	require.Equal(t, 1, body.Total, "指标+单日区间应只剩一条")

	// 空结果：200 + []。
	w, body = industryGet(t, router, "/api/industry/vitality?industry_code=NO_SUCH_IND")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 0, body.Total)
	require.Contains(t, w.Body.String(), `"data":[]`)

	// 非法参数 400：缺 industry_code / 坏日期 / start>end。
	for _, path := range []string{
		"/api/industry/vitality",
		"/api/industry/vitality?industry_code=" + source + "-ind&start_date=2026/01/01",
		"/api/industry/vitality?industry_code=" + source + "-ind&end_date=not-a-date",
		"/api/industry/vitality?industry_code=" + source + "-ind&start_date=2200-01-02&end_date=2200-01-01",
	} {
		w, _ = industryGet(t, router, path)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s 应 400", path)
	}
}

// ─── ETF 成分 ───────────────────────────────────────────────────────────

func TestIndustryHandler_ETFConstituentsRoundtripAndGuards(t *testing.T) {
	router, store := newIndustryTestRouter(t)
	source := indSource()
	t.Cleanup(func() { indCleanup(t, store, source) })

	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	etfCode := source + "-etf"
	require.NoError(t, store.SaveETFConstituentBatch(context.Background(), []*domain.ETFConstituent{
		{ETFCode: etfCode, Symbol: "600519.SH", Weight: 5.23, ObsDate: time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), Source: source, AsOf: asOf},
		{ETFCode: etfCode, Symbol: "000001.SZ", Weight: 4.11, ObsDate: time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), Source: source, AsOf: asOf},
		{ETFCode: etfCode, Symbol: "600519.SH", Weight: 5.90, ObsDate: time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC), Source: source, AsOf: asOf},
	}))

	// 正常查：默认最新报告期。
	w, body := industryGet(t, router, "/api/industry/etf/"+etfCode+"/constituents")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, body.Total, "默认只给最新报告期")
	var c domain.ETFConstituent
	require.NoError(t, json.Unmarshal(body.Data[0], &c))
	require.Equal(t, "600519.SH", c.Symbol)
	require.InDelta(t, 5.90, c.Weight, 1e-12)

	// 显式指定旧报告期（PIT）。
	_, body = industryGet(t, router, "/api/industry/etf/"+etfCode+"/constituents?obs_date=2200-01-01")
	require.Equal(t, 2, body.Total, "旧报告期应读到当时的两行")

	// 空结果：200 + []。
	w, body = industryGet(t, router, "/api/industry/etf/NO_SUCH_ETF/constituents")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 0, body.Total)
	require.Contains(t, w.Body.String(), `"data":[]`)

	// 非法参数 400：坏 obs_date。
	w, _ = industryGet(t, router, "/api/industry/etf/"+etfCode+"/constituents?obs_date=01-02")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
