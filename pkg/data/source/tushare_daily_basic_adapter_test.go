package source

// TushareDailyBasicAdapter（D5 DataTypeDailyBasic）的单元测试：
// SupportedTypes / Schema / nil client 降级 / Fetch 汇总与落库 / 日期范围
// 校验 / HealthCheck 探测 / PIT 声明政策。
//
// Fetch 走真 client 但指向 httptest stub（不碰外网、不碰真库）——Save 由
// 内存替身接收，断言「normalize 后的行确实经 adapter 汇总返回」。

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
	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// activityStubStore 只实现活跃度写入面 + 归档面；嵌入 nil TushareStore，
// 误调其它方法会 panic——那正是想要的信号（同 macroStubStore）。
type activityStubStore struct {
	data.TushareStore
	activity []*domain.MarketActivity
}

func (s *activityStubStore) SaveMarketActivityBatch(_ context.Context, rows []*domain.MarketActivity) error {
	s.activity = append(s.activity, rows...)
	return nil
}

func (s *activityStubStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// dailyBasicStubBody 是最小合法 daily_basic 响应（两行，其中一行数值缺失）。
const dailyBasicStubBody = `{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f","volume_ratio"],"items":[["600519.SH","20261014",1.23,1.5,0.9],["000001.SZ","20261014",null,null,null]]}}`

func newDailyBasicStubClient(t *testing.T, store data.TushareStore) *data.TushareClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dailyBasicStubBody))
	}))
	t.Cleanup(srv.Close)
	return data.NewTushareClient("test-token", srv.URL, 0, store, nil)
}

// newDailyBasicAPIRecordingClient 记录每次请求的 api_name，供「探测不落库」
// 与「按日迭代」的断言使用。
func newDailyBasicAPIRecordingClient(t *testing.T, store data.TushareStore, seen *[]string) *data.TushareClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			APIName string `json:"api_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		*seen = append(*seen, req.APIName)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dailyBasicStubBody))
	}))
	t.Cleanup(srv.Close)
	return data.NewTushareClient("test-token", srv.URL, 0, store, nil)
}

func TestTushareDailyBasicAdapter_NilClientRegistersDisabled(t *testing.T) {
	a := NewTushareDailyBasicAdapter(nil)
	assert.False(t, a.Enabled(), "nil client（token 未配置）→ disabled 降级，不阻断启动")
	assert.Equal(t, "tushare_daily_basic", a.Name())

	_, err := a.Fetch(context.Background(), FetchRequest{
		DataType: DataTypeDailyBasic, StartDate: d5Day(2026, 10, 14), EndDate: d5Day(2026, 10, 14),
	})
	require.Error(t, err, "disabled adapter 的 Fetch 必须报错（Registry 会跳过它）")

	require.Error(t, a.HealthCheck(context.Background()))
}

func TestTushareDailyBasicAdapter_SupportedTypesAndSchema(t *testing.T) {
	a := NewTushareDailyBasicAdapter(newDailyBasicStubClient(t, &activityStubStore{}))
	require.True(t, a.Enabled())

	assert.Equal(t, []string{DataTypeDailyBasic}, a.SupportedTypes())
	assert.Equal(t, AdapterTypeHTTP, a.Type())

	schema, err := a.Schema(DataTypeDailyBasic)
	require.NoError(t, err)
	assert.Equal(t, DataTypeDailyBasic, schema.DataType)
	assert.NotEmpty(t, schema.Fields)
	// trade_date 必须是 date 类型——它是 after_close 声明的可见锚点载体
	// （D2 契约的形态校验会核对这一点）。
	for _, f := range schema.Fields {
		if f.Name == "trade_date" {
			assert.Equal(t, "date", f.Type, "可见锚点 trade_date 必须是 date（契约形态校验）")
		}
	}

	_, err = a.Schema(DataTypeMacro)
	require.Error(t, err, "活跃度 adapter 不服务宏观类型")
	assert.ErrorIs(t, err, ErrUnsupported)
}

// TestTushareDailyBasicAdapter_RegistersUnderContract 是「注册」验收腿：
// adapter 过 D2 契约的 fail-closed 校验并成功进 Registry。
func TestTushareDailyBasicAdapter_RegistersUnderContract(t *testing.T) {
	reg := NewRegistry()
	a := NewTushareDailyBasicAdapter(newDailyBasicStubClient(t, &activityStubStore{}))
	require.NoError(t, reg.Register(a), "合法声明的 adapter 必须注册成功")
	assert.Contains(t, reg.ListAdapters(), "tushare_daily_basic")
}

// TestDailyBasicDeclaration_MustBeAfterClose 是 D5 ⑤2 的 PIT 破坏验证靶子：
// 钉住「市场活跃度 = 收盘后可见」这一语义（蓝图 §4.2 点名的高危场景）。
//
// 破坏腿（改坏 → 红 → 还原 → 绿）：把 declarations.go 里
// TimeSemantics 从 VisibleAfterClose 改成 VisibleCustom（配
// 「盘中即可见」的 fetch_time 表达式）→ 本测试当场红。
// 这不是「抄一遍常量」，是政策钉死：换手率由当日全天成交量统计得出，
// 收盘前不存在；声明成盘中可见 = 把前视洞放回来。
func TestDailyBasicDeclaration_MustBeAfterClose(t *testing.T) {
	a := NewTushareDailyBasicAdapter(newDailyBasicStubClient(t, &activityStubStore{}))
	decls := a.Declarations()
	require.Len(t, decls, 1)

	d := decls[0]
	assert.Equal(t, DataTypeDailyBasic, d.DataType)
	assert.Equal(t, market.VisibleAfterClose, d.TimeSemantics,
		"市场活跃度必须声明 after_close（换手率收盘后才统计得出；改宽 = 前视泄漏）")
	assert.Equal(t, "trade_date", d.VisibleField,
		"可见锚点必须是 schema 里的 trade_date（date 类型）")
	assert.Equal(t, market.FreqSpecDaily, d.Frequency, "换手率是日更聚合数据，频率口径日频")
}

// TestTushareDailyBasicAdapter_FetchAggregatesAndPersists 是「同步」验收腿：
// adapter.Fetch 汇总的 Items ≡ 落库行（同一批），且缺失值在 wire 上可见为
// nil（缺失≠0）。
func TestTushareDailyBasicAdapter_FetchAggregatesAndPersists(t *testing.T) {
	store := &activityStubStore{}
	a := NewTushareDailyBasicAdapter(newDailyBasicStubClient(t, store))

	resp, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeDailyBasic,
		StartDate: d5Day(2026, 10, 14),
		EndDate:   d5Day(2026, 10, 14),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "tushare_daily_basic", resp.Source)
	require.Len(t, resp.Items, 2)

	// 第一行：数值齐全。
	first := resp.Items[0]
	assert.Equal(t, "600519.SH", first.Symbol)
	assert.Equal(t, 1.23, first.Data["turnover_rate"])
	assert.InDelta(t, 1.5, first.Data["turnover_rate_f"], 1e-9)
	assert.Equal(t, true, first.TradeTime.Equal(d5Day(2026, 10, 14)))

	// 第二行：三列全 nil（当日未发布）——身份完整照常返回，缺失在 wire 上
	// 可见为 nil，不折 0。
	second := resp.Items[1]
	assert.Equal(t, "000001.SZ", second.Symbol)
	assert.Nil(t, second.Data["turnover_rate"], "缺失≠0：wire 上是 nil 不是 0")

	// 落库面收到同一批行（adapter 的 Items ≡ 入库的行）。
	require.Len(t, store.activity, 2)
	assert.Equal(t, "600519.SH", store.activity[0].Symbol)
	assert.Nil(t, store.activity[1].TurnoverRate)
}

// TestTushareDailyBasicAdapter_FetchIteratesEachCalendarDay 钉住「一日一请求」：
// 3 天范围 = 3 次调用（非交易日上游返回空 items 也算一次，形状可预期）。
func TestTushareDailyBasicAdapter_FetchIteratesEachCalendarDay(t *testing.T) {
	var seen []string
	a := NewTushareDailyBasicAdapter(newDailyBasicAPIRecordingClient(t, &activityStubStore{}, &seen))

	_, err := a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeDailyBasic,
		StartDate: d5Day(2026, 10, 12),
		EndDate:   d5Day(2026, 10, 14),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"daily_basic", "daily_basic", "daily_basic"}, seen,
		"3 个日历日 → 3 次请求（一日一次，含非交易日）")
}

func TestTushareDailyBasicAdapter_FetchRejectsBadScope(t *testing.T) {
	a := NewTushareDailyBasicAdapter(newDailyBasicStubClient(t, &activityStubStore{}))

	// 无日期范围：不存在隐式「最新一日」的默认值。
	_, err := a.Fetch(context.Background(), FetchRequest{DataType: DataTypeDailyBasic})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StartDate/EndDate required")

	// 非空 Symbols：本单只做全市场截面，per-symbol 历史明确超出范围，
	// fail-loud 而不是静默忽略调用方要的范围。
	_, err = a.Fetch(context.Background(), FetchRequest{
		DataType: DataTypeDailyBasic, Symbols: []string{"600519.SH"},
		StartDate: d5Day(2026, 10, 14), EndDate: d5Day(2026, 10, 14),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Symbols must be empty")

	// 反向区间。
	_, err = a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeDailyBasic,
		StartDate: d5Day(2026, 10, 15), EndDate: d5Day(2026, 10, 14),
	})
	require.Error(t, err)

	// 跨度超上限（配额保护，非上游文档限额）。
	_, err = a.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeDailyBasic,
		StartDate: d5Day(2024, 1, 1),
		EndDate:   d5Day(2026, 10, 14),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")

	// 不支持的类型。
	_, err = a.Fetch(context.Background(), FetchRequest{
		DataType: DataTypeMacro, StartDate: d5Day(2026, 10, 14), EndDate: d5Day(2026, 10, 14),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestTushareDailyBasicAdapter_HealthCheckProbesWithoutPersist(t *testing.T) {
	var seen []string
	store := &activityStubStore{}
	a := NewTushareDailyBasicAdapter(newDailyBasicAPIRecordingClient(t, store, &seen))

	require.NoError(t, a.HealthCheck(context.Background()), "stub 恒 200，探测必须过")
	assert.Equal(t, []string{"daily_basic"}, seen)
	assert.Empty(t, store.activity, "探测不得产生数据副作用（不落库）")
	assert.Equal(t, 200, a.RateLimit().RequestsPerMinute)
}

// d5Day 构造 UTC 自然日（与 adapter 的迭代口径一致）。
func d5Day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
