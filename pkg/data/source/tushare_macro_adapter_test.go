package source

// TushareMacroAdapter（P2-1 DataTypeMacro）的单元测试：SupportedTypes /
// Schema / nil client 降级 / Fetch 汇总 / HealthCheck 探测。
//
// Fetch 走真 client 但指向 httptest stub（不碰外网、不碰真库）——Save 由
// 内存替身接收，断言「normalize 后的行确实经 adapter 汇总返回」。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// macroStubStore 只实现宏观写入面 + 归档面；嵌入 nil TushareStore，误调
// 其它方法会 panic——那正是想要的信号（同 pkg/data 的 recordingMacroStore）。
type macroStubStore struct {
	data.TushareStore
	macro []*domain.MacroSeries
}

func (s *macroStubStore) SaveMacroSeriesBatch(_ context.Context, rows []*domain.MacroSeries) error {
	s.macro = append(s.macro, rows...)
	return nil
}

func (s *macroStubStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// macroStubBody 是最小合法 cn_cpi 响应。
const macroStubBody = `{"code":0,"msg":"","data":{"fields":["month","nt_yoy"],"items":[["202608",0.8]]}}`

// macroStubBodiesPerAPI 按 api_name 分发的最小合法响应——全目录导入测试
// 需要每个接口各自的字段形状（USDCNY_CLOSE 的日期列是 trade_date，不是 month）。
var macroStubBodiesPerAPI = map[string]string{
	"cn_cpi":     macroStubBody,
	"cn_ppi":     `{"code":0,"msg":"","data":{"fields":["month","ppi_yoy"],"items":[["202608",-1.5]]}}`,
	"cn_pmi":     `{"code":0,"msg":"","data":{"fields":["month","pmi010000"],"items":[["202608",50.1]]}}`,
	"cn_m":       `{"code":0,"msg":"","data":{"fields":["month","m2","m2_yoy"],"items":[["202608",3568083.6,7.5]]}}`,
	"sf_month":   `{"code":0,"msg":"","data":{"fields":["month","inc_month"],"items":[["202608",16577.0]]}}`,
	"shibor_lpr": `{"code":0,"msg":"","data":{"fields":["date","1y","5y"],"items":[["20260820",3.0,3.5]]}}`,
	"fx_daily":   `{"code":0,"msg":"","data":{"fields":["trade_date","bid_close"],"items":[["20260131",6.95447]]}}`,
}

func newMacroStubClient(t *testing.T, store data.TushareStore) *data.TushareClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(macroStubBody))
	}))
	t.Cleanup(srv.Close)
	return data.NewTushareClient("test-token", srv.URL, 0, store, nil)
}

// newMacroCatalogStubClient 起一个按 api_name 分发的 tushare stub。
func newMacroCatalogStubClient(t *testing.T, store data.TushareStore) *data.TushareClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			APIName string `json:"api_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		body, ok := macroStubBodiesPerAPI[req.APIName]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":-1,"msg":"stub: unknown api_name"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return data.NewTushareClient("test-token", srv.URL, 0, store, nil)
}

func TestTushareMacroAdapter_NilClientRegistersDisabled(t *testing.T) {
	a := NewTushareMacroAdapter(nil)
	assert.False(t, a.Enabled(), "nil client（token 未配置）→ disabled 降级，不阻断启动")
	assert.Equal(t, "tushare_macro", a.Name())

	_, err := a.Fetch(context.Background(), FetchRequest{DataType: DataTypeMacro})
	require.Error(t, err, "disabled adapter 的 Fetch 必须报错（Registry 会跳过它）")

	require.Error(t, a.HealthCheck(context.Background()))
}

func TestTushareMacroAdapter_SupportedTypesAndSchema(t *testing.T) {
	a := NewTushareMacroAdapter(newMacroStubClient(t, &macroStubStore{}))
	require.True(t, a.Enabled())

	assert.Equal(t, []string{DataTypeMacro}, a.SupportedTypes())
	assert.Equal(t, AdapterTypeHTTP, a.Type())

	schema, err := a.Schema(DataTypeMacro)
	require.NoError(t, err)
	assert.Equal(t, DataTypeMacro, schema.DataType)
	assert.NotEmpty(t, schema.Fields)

	_, err = a.Schema(DataTypeOHLCDaily)
	require.Error(t, err, "宏观 adapter 不服务行情类型")
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestTushareMacroAdapter_FetchAggregatesPersistedRows(t *testing.T) {
	store := &macroStubStore{}
	a := NewTushareMacroAdapter(newMacroStubClient(t, store))

	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType: DataTypeMacro,
		Symbols:  []string{"CPI_YOY"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "tushare_macro", resp.Source)
	require.Len(t, resp.Items, 1)

	item := resp.Items[0]
	assert.Equal(t, "CPI_YOY", item.Symbol)
	assert.Equal(t, 0.8, item.Data["value"])
	assert.Equal(t, "tushare", item.Data["source"])

	// 落库面也收到了同一批行（adapter 的 Items ≡ 入库的行）。
	require.Len(t, store.macro, 1)
	assert.Equal(t, "CPI_YOY", store.macro[0].Indicator)
}

func TestTushareMacroAdapter_FetchRejectsUnsupportedType(t *testing.T) {
	a := NewTushareMacroAdapter(newMacroStubClient(t, &macroStubStore{}))
	_, err := a.Fetch(context.Background(), FetchRequest{DataType: DataTypeOHLCDaily, Symbols: []string{"600519.SH"}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestTushareMacroAdapter_FetchEmptySymbolsMeansWholeCatalog(t *testing.T) {
	store := &macroStubStore{}
	a := NewTushareMacroAdapter(newMacroCatalogStubClient(t, store))

	// 空 Symbols = 全目录（9 个 indicator / 7 个接口）。日频指标按年分段
	// （LPR 2013 起 14 段、fx 2019 起 8 段），所以总行数 > 9——断言的语义
	// 是「9 个 indicator 都被拉到」，用 distinct indicator 集合表达。
	resp, err := a.Fetch(context.Background(), FetchRequest{DataType: DataTypeMacro})
	require.NoError(t, err)

	got := make(map[string]bool, len(resp.Items))
	for _, item := range resp.Items {
		got[item.Symbol] = true
	}
	assert.Len(t, got, len(data.MacroIndicators()), "全目录的每个 indicator 都应被拉到")
	assert.Equal(t, len(resp.Items), len(store.macro), "返回行 ≡ 入库行")
}

func TestTushareMacroAdapter_HealthCheckProbes(t *testing.T) {
	a := NewTushareMacroAdapter(newMacroStubClient(t, &macroStubStore{}))
	require.NoError(t, a.HealthCheck(context.Background()), "stub 恒 200，探测必须过")
	assert.Equal(t, 200, a.RateLimit().RequestsPerMinute)
}

// TestMacroDeclaration_AnchorIsConservativeAsOf 是宏观可见锚点收紧（2026-10-16
// 用户裁决 A）的破坏验证靶子，同 D5 的 TestDailyBasicDeclaration_MustBeAfterClose：
// 钉住「宏观可见锚点 = as_of（导入时刻，保守）」这一**政策**，而不是抄一遍常量。
//
// 为什么不能用 obs_date：obs_date 是**观测日**（月频取当月首日），真实发布日
// **晚于**它（9 月 CPI 10 月中才发布）。拿 obs_date 当可见锚点 = 声明「观测日
// 当天即可见」，任何按它做 PIT 过滤的消费方一接线即前视。as_of 是导入时刻，
// ≥ 真实发布日 ⇒ 按 as_of 过滤不早于真实可见时刻（保守）。
//
// 破坏腿（改坏 → 红 → 还原 → 绿）：把 declarations.go 的 VisibleField 从
// "as_of" 改回 "obs_date" → 本测试当场红。
func TestMacroDeclaration_AnchorIsConservativeAsOf(t *testing.T) {
	a := NewTushareMacroAdapter(newMacroStubClient(t, &macroStubStore{}))
	decls := a.Declarations()
	require.Len(t, decls, 1)

	d := decls[0]
	assert.Equal(t, DataTypeMacro, d.DataType)
	assert.Equal(t, market.VisibleOnAnnouncementDay, d.TimeSemantics,
		"宏观是披露驱动事件流，语义仍是 announcement_day（换的只是承载可见时刻的字段）")
	assert.Equal(t, "as_of", d.VisibleField,
		"可见锚点必须是 as_of（导入时刻，保守）：obs_date 是观测日、早于真实发布日，拿它当可见点 = 前视")
	assert.Equal(t, market.FreqSpecEvent, d.Frequency, "宏观是事件流，频率口径 event")

	// 锚点字段必须真的在 wire schema 里、且类型能承载时间（date/datetime）——
	// 否则 validateVisibleAnchor 在注册期就会拒收这条声明。
	schema, err := a.Schema(DataTypeMacro)
	require.NoError(t, err)
	var asOf *SchemaField
	for i := range schema.Fields {
		if schema.Fields[i].Name == "as_of" {
			asOf = &schema.Fields[i]
		}
	}
	require.NotNil(t, asOf, "锚点 as_of 必须在 wire schema 里（否则注册期拒收）")
	assert.Equal(t, "datetime", asOf.Type, "as_of 是导入时刻，类型 datetime")
}
