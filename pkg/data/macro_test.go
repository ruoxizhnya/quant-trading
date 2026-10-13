package data

// 宏观导入面（P2-1）测试：normalize 纯函数 + 错误传播 + 分页窗口。
//
// 错误传播是本文件的红线（同 supply_chain_test.go）：ETL 路径上任何失败
// （HTTP 错、tushare 错误信封、响应形状违约、Save 失败）都必须向上传播——
// 不许 log 后 continue 返回 nil。破坏验证②的 Red 腿指回这里的
// TestFetchMacro*Propagates*。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// ── 测试替身 ─────────────────────────────────────────────────────────────

// recordingMacroStore 记录 macro Save 入参，不碰库。嵌入 TushareStore
// （nil 接口）满足其余方法——误调会 panic，那正是想要的信号。
type recordingMacroStore struct {
	TushareStore
	macro []*domain.MacroSeries
}

func (s *recordingMacroStore) SaveMacroSeriesBatch(_ context.Context, rows []*domain.MacroSeries) error {
	s.macro = append(s.macro, rows...)
	return nil
}

func (s *recordingMacroStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// failingMacroStore 的 macro Save 恒返回同一个错误——证明调用方把错误
// 传上来了，而不是吞掉。
type failingMacroStore struct {
	TushareStore
	err error
}

func (s *failingMacroStore) SaveMacroSeriesBatch(_ context.Context, _ []*domain.MacroSeries) error {
	return s.err
}

func (s *failingMacroStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// ── 指标目录 ─────────────────────────────────────────────────────────────

func TestMacroIndicators_CatalogIsCompleteAndSorted(t *testing.T) {
	got := MacroIndicators()
	want := []string{"CPI_YOY", "LPR_1Y", "LPR_5Y", "M2", "M2_YOY", "PMI", "PPI_YOY", "TSF_INC", "USDCNY_CLOSE"}
	require.Equal(t, want, got, "首版指标目录应恰好是这 9 个（排序稳定视图）")
	for _, ind := range want {
		require.True(t, MacroIndicatorExists(ind), "%s 应在目录内", ind)
	}
	require.False(t, MacroIndicatorExists("NO_SUCH_INDICATOR"))
}

func TestMacroIndicatorSpecs_FieldsAndParamsPerInterface(t *testing.T) {
	// 每条目录条目必须自洽：api/field/freq/unit/date 列齐全，日频带
	// SinceYear（分页窗口），fx_daily 带 ts_code。
	for ind, spec := range macroIndicatorSpecs {
		require.NotEmpty(t, spec.APIName, "%s 缺 APIName", ind)
		require.NotEmpty(t, spec.Field, "%s 缺 Field", ind)
		require.NotEmpty(t, spec.DateField, "%s 缺 DateField", ind)
		require.Contains(t, []string{MacroFreqMonthly, MacroFreqDaily}, spec.Freq, "%s freq 必须是 M/D", ind)
		require.NotEmpty(t, spec.Unit, "%s 缺 Unit", ind)
		require.NotEmpty(t, spec.Fields, "%s 缺 Fields（应只取需要的列）", ind)
		require.Contains(t, spec.Fields, spec.DateField, "%s 的 fields 应含日期列", ind)
	}
	// fx_daily 官方限量单次 1000 行，2019 至今 2400+ 行 → 必须按年分段。
	require.Equal(t, 2019, macroIndicatorSpecs["USDCNY_CLOSE"].SinceYear,
		"fx_daily 必须按年分段（单次限量 1000 行，实测 2019 至今 2410 行）")
	// LPR 相反：shibor_lpr 实测限「1 次/小时」，分段（14 次）必然 40203。
	require.Zero(t, macroIndicatorSpecs["LPR_1Y"].SinceYear,
		"LPR 不分段——shibor_lpr 限 1 次/小时，按年分段必然撞限")
	require.Zero(t, macroIndicatorSpecs["LPR_5Y"].SinceYear)
	require.Equal(t, "USDCNH.FXCM", macroIndicatorSpecs["USDCNY_CLOSE"].Params["ts_code"])
	require.Equal(t, "shibor_lpr", macroIndicatorSpecs["LPR_1Y"].APIName)
	require.Equal(t, "shibor_lpr", macroIndicatorSpecs["LPR_5Y"].APIName,
		"LPR_1Y/LPR_5Y 同接口 → FetchMacroIndicators 会共用一次请求")
}

// ── 分页窗口 ─────────────────────────────────────────────────────────────

func TestMacroFetchWindows_MonthlyIsSingleFullWindow(t *testing.T) {
	spec := macroIndicatorSpecs["CPI_YOY"]
	windows := macroFetchWindows(spec, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	require.Len(t, windows, 1, "月频指标单次全量（历史累计远低于接口限量）")
	require.Empty(t, windows[0], "月频窗口不带日期参数（接口默认全量）")
}

func TestMacroFetchWindows_DailyIsChunkedByYear(t *testing.T) {
	spec := macroIndicatorSpecs["USDCNY_CLOSE"]
	windows := macroFetchWindows(spec, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	// 2019..2026 共 8 段（fx_daily 单次限量 1000 行，每年 ~300 行）。
	require.Len(t, windows, 8, "fx_daily 应按年分段（2019-2026）")
	require.Equal(t, "20190101", windows[0]["start_date"])
	require.Equal(t, "20191231", windows[0]["end_date"])
	require.Equal(t, "20260101", windows[len(windows)-1]["start_date"])
}

// TestMacroFetchWindows_LPRIsSingleCall 钉住「LPR 不分段」这个流控推论：
// shibor_lpr 实测限 1 次/小时（40203），按年分段要 14 次请求 = 必然撞限。
// 破坏腿：谁给 LPR 加回 SinceYear → 本测试红（且真拉会 40203）。
func TestMacroFetchWindows_LPRIsSingleCall(t *testing.T) {
	for _, ind := range []string{"LPR_1Y", "LPR_5Y"} {
		spec := macroIndicatorSpecs[ind]
		windows := macroFetchWindows(spec, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
		require.Len(t, windows, 1, "%s 必须单次全量（shibor_lpr 限 1 次/小时）", ind)
		require.Empty(t, windows[0], "%s 单次全量不带日期参数", ind)
	}
}

func TestMacroFetchWindows_DailyKeepsFixedParams(t *testing.T) {
	spec := macroIndicatorSpecs["USDCNY_CLOSE"]
	windows := macroFetchWindows(spec, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	require.Len(t, windows, 8, "2019-2026 共 8 段")
	for _, w := range windows {
		require.Equal(t, "USDCNH.FXCM", w["ts_code"], "分段窗口必须保留接口级固定参数")
	}
}

// ── normalize ────────────────────────────────────────────────────────────

// cn_cpi 响应样例（官方文档数据样例口径，2026-10-12 实测同款字段）。
const cnCpiBody = `{"code":0,"msg":"","data":{"fields":["month","nt_val","nt_yoy","nt_mom","nt_accu","town_val","town_yoy","town_mom","town_accu","cnt_val","cnt_yoy","cnt_mom","cnt_accu"],"items":[
	["202608",100.8,0.8,0.4,0.9,100.8,0.8,0.4,100.9,100.7,0.7,0.4,100.7],
	["202607",100.5,0.5,-0.1,0.9,100.5,0.5,-0.1,101.0,100.4,0.4,-0.2,100.7],
	["202606",null,null,null,null,null,null,null,null,null,null,null,null]
]}}`

func TestNormalizeMacroSeries_MonthlyTakesFirstDayOfMonth(t *testing.T) {
	resp := decodeTushareBody(t, cnCpiBody)
	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

	res, err := normalizeMacroSeries(resp, macroIndicatorSpecs["CPI_YOY"], MacroSourceTushare, asOf)
	require.NoError(t, err)
	require.Len(t, res.Rows, 2, "数值为 null 的行跳过（不补 0——缺失值折成 0 等于编数据）")
	require.Equal(t, 1, res.SkippedNilValue, "R2：nil 值行的跳过计数（cnCpiBody 第三行全 null）")

	require.Equal(t, "CPI_YOY", res.Rows[0].Indicator)
	require.Equal(t, MacroFreqMonthly, res.Rows[0].Freq)
	require.Equal(t, MacroSourceTushare, res.Rows[0].Source)
	require.Equal(t, "%", res.Rows[0].Unit)
	require.Equal(t, asOf, res.Rows[0].AsOf)

	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), res.Rows[0].ObsDate, "月频 YYYYMM 取当月首日")
	require.InDelta(t, 0.8, res.Rows[0].Value, 1e-12)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), res.Rows[1].ObsDate)
	require.InDelta(t, 0.5, res.Rows[1].Value, 1e-12)
}

// cn_pmi 不传 fields 时响应字段名为大写（2026-10-12 实测）——normalize
// 必须大小写不敏感定位。
const cnPmiUpperBody = `{"code":0,"msg":"","data":{"fields":["MONTH","PMI010802","PMI010000","ID","CREATE_BY"],"items":[
	["202609",51.2,50.1,"x","y"],
	["202608",51.0,49.8,"x","y"]
]}}`

func TestNormalizeMacroSeries_PMIUppercaseFieldsMatchCaseInsensitively(t *testing.T) {
	resp := decodeTushareBody(t, cnPmiUpperBody)
	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

	res, err := normalizeMacroSeries(resp, macroIndicatorSpecs["PMI"], MacroSourceTushare, asOf)
	require.NoError(t, err, "大写字段名（MONTH/PMI010000）必须能定位——实测 cn_pmi 不传 fields 时就是这个形状")
	require.Len(t, res.Rows, 2)
	require.Equal(t, "PMI", res.Rows[0].Indicator)
	require.Equal(t, "点", res.Rows[0].Unit)
	require.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), res.Rows[0].ObsDate)
	require.InDelta(t, 50.1, res.Rows[0].Value, 1e-12)
}

// shibor_lpr 日频样例（官方文档口径）。
const shiborLprBody = `{"code":0,"msg":"","data":{"fields":["date","1y","5y"],"items":[
	["20260820",3.0,3.5],
	["20260720",3.0,3.5]
]}}`

func TestNormalizeMacroSeries_DailyKeepsNaturalDay(t *testing.T) {
	resp := decodeTushareBody(t, shiborLprBody)
	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

	res, err := normalizeMacroSeries(resp, macroIndicatorSpecs["LPR_1Y"], MacroSourceTushare, asOf)
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	require.Equal(t, MacroFreqDaily, res.Rows[0].Freq)
	require.Equal(t, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), res.Rows[0].ObsDate, "日频取自然日")
	require.InDelta(t, 3.0, res.Rows[0].Value, 1e-12)
}

func TestNormalizeMacroSeries_RejectsMissingKeyColumns(t *testing.T) {
	// 响应里没有 date 列（形状违约）——必须报错，不许静默返回空。
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["month","nt_val"],"items":[["202608",100.8]]}}`)
	_, err := normalizeMacroSeries(resp, macroIndicatorSpecs["CPI_YOY"], MacroSourceTushare, time.Now().UTC())
	require.Error(t, err, "缺数值列必须报错（形状不符契约）")
	require.Contains(t, err.Error(), "nt_yoy")
}

func TestNormalizeMacroSeries_SkipsRowsWithBadObsDate(t *testing.T) {
	// 日期解析失败的行跳过（PIT 身份不完整不进库，同 normalizeETFPortfolio
	// 先例）；好行照常入库，跳过计数分列如实上报（R2）。
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["month","nt_yoy"],"items":[
		["202608",0.8],
		["not-a-month",0.7],
		["",0.6],
		["202605",null]
	]}}`)
	res, err := normalizeMacroSeries(resp, macroIndicatorSpecs["CPI_YOY"], MacroSourceTushare, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, res.Rows, 1, "坏行跳过、好行入库")
	require.Equal(t, 2, res.SkippedBadDate, "坏日期行数（PIT 身份不完整）")
	require.Equal(t, 1, res.SkippedNilValue, "nil 值行数（未发布）")
	require.Equal(t, 3, res.Skipped(), "总跳过 = 坏日期 + nil 值")
	require.InDelta(t, 0.8, res.Rows[0].Value, 1e-12)
}

// TestNormalizeMacroSeries_SkipsUnparseableValues 钉住 P2-1-R3 R1：数值列
// **非 nil 但不可解析**（上游把 number 改成字符串/非标量）的行跳过并单独计数，
// 绝不静默折 0（缺失值折 0 等于编数据）。
func TestNormalizeMacroSeries_SkipsUnparseableValues(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["month","nt_yoy"],"items":[
		["202608",0.8],
		["202607","N/A"],
		["202606","13,42"],
		["202605","--"],
		["202604",true],
		["202603",[1,2]]
	]}}`)
	res, err := normalizeMacroSeries(resp, macroIndicatorSpecs["CPI_YOY"], MacroSourceTushare, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, res.Rows, 1, "只有可解析的行入库")
	require.Equal(t, 5, res.SkippedBadValue, "不可解析串 / 千分位 / 占位符 / bool / 数组 均计入坏值")
	require.Equal(t, 0, res.SkippedNilValue, "本响应无 nil 值列，不该误计 nil")
	require.InDelta(t, 0.8, res.Rows[0].Value, 1e-12)
}

// TestFetchMacroIndicator_FailsLoudWhenAllValuesUnparseable：有行但数值列全部
// 不可解析 ⇒ fail-loud（口径与既有 nil 全跳过一致）。
func TestFetchMacroIndicator_FailsLoudWhenAllValuesUnparseable(t *testing.T) {
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t,
		`{"code":0,"msg":"","data":{"fields":["month","nt_yoy"],"items":[["202608","N/A"],["202607","--"]]}}`, 200), rec)

	rows, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.Error(t, err, "有行但全被跳过必须 fail-loud")
	require.Nil(t, rows)
	require.Contains(t, err.Error(), "坏值", "错误文案应分别暴露坏值计数")
	require.Empty(t, rec.macro, "错误路径不许落库")
}

// TestEqualMacroParams_NonScalarDoesNotPanic 是 P2-1-R3 R2 的目录自检：
// Params 的 value 是非可比较类型（slice/map）时，改前用 != 会 runtime panic
// （"comparing uncomparable type"），改后 DeepEqual 安全且能判不等。
func TestEqualMacroParams_NonScalarDoesNotPanic(t *testing.T) {
	a := map[string]interface{}{"codes": []string{"a", "b"}, "n": 1}
	b := map[string]interface{}{"codes": []string{"a", "b"}, "n": 1}
	c := map[string]interface{}{"codes": []string{"a", "c"}, "n": 1}

	require.NotPanics(t, func() { _ = equalMacroParams(a, b) }, "非标量 value 不得 panic")
	require.True(t, equalMacroParams(a, b), "内容相同的非标量应判等")
	require.False(t, equalMacroParams(a, c), "内容不同的非标量应判不等")
	require.False(t, equalMacroParams(a, map[string]interface{}{}), "长度不同判不等")

	// 嵌套 map 同样走 DeepEqual，不得 panic。
	m1 := map[string]interface{}{"o": map[string]any{"k": []int{1}}}
	m2 := map[string]interface{}{"o": map[string]any{"k": []int{1}}}
	require.NotPanics(t, func() { _ = equalMacroParams(m1, m2) })
	require.True(t, equalMacroParams(m1, m2))
}

// ── ETL 错误传播（破坏验证②的 Red 腿） ──────────────────────────────────

// macroOKBody 是任意指标可用的最小合法响应。
const macroOKBody = `{"code":0,"msg":"","data":{"fields":["month","nt_yoy"],"items":[["202608",0.8]]}}`

func TestFetchMacroIndicator_SavesNormalizedRows(t *testing.T) {
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, macroOKBody, 200), rec)

	rows, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, rec.macro, 1, "normalize 后的行必须落到 store")
	require.Equal(t, "CPI_YOY", rec.macro[0].Indicator)
	require.Equal(t, MacroSourceTushare, rec.macro[0].Source)
	require.False(t, rec.macro[0].AsOf.IsZero(), "as_of 必须取导入时刻")
}

func TestFetchMacroIndicator_PropagatesCallError(t *testing.T) {
	// tushare 错误信封（40203 形状）——必须向上传播，不许吞。
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, `{"code":40203,"msg":"nope"}`, 200), rec)

	rows, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.Error(t, err, "上游错误信封必须传播（吞错会让「上游挂了」伪装成「导入 0 行」）")
	require.Nil(t, rows)
	require.Empty(t, rec.macro, "错误路径不许落库")
}

func TestFetchMacroIndicator_PropagatesTransportError(t *testing.T) {
	// HTTP 500 —— 同样必须传播。
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, `{"code":0}`, http.StatusInternalServerError), rec)

	_, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.Error(t, err)
	require.Empty(t, rec.macro)
}

func TestFetchMacroIndicator_PropagatesSaveError(t *testing.T) {
	sentinel := errSentinel
	rec := &failingMacroStore{err: sentinel}
	client := newSupplyChainTestClient(stubTushare(t, macroOKBody, 200), rec)

	_, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.Error(t, err, "Save 失败必须传播")
	require.ErrorIs(t, err, sentinel, "传播的应是包装后的原错误（%w 链可追溯）")
}

func TestFetchMacroIndicator_PropagatesShapeViolation(t *testing.T) {
	// code:0 但字段形状不符契约（缺 nt_yoy 列）——data-quality 错误必须传播。
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, `{"code":0,"msg":"","data":{"fields":["month"],"items":[["202608"]]}}`, 200), rec)

	_, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.Error(t, err, "形状违约必须传播")
	require.Contains(t, err.Error(), "cn_cpi")
	require.Empty(t, rec.macro)
}

func TestFetchMacroIndicator_RejectsUnknownIndicator(t *testing.T) {
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, macroOKBody, 200), rec)

	_, err := client.FetchMacroIndicator(context.Background(), "NOT_AN_INDICATOR")
	require.Error(t, err, "目录外指标必须被拒（fail-loud，不静默拉全量）")
	require.Contains(t, err.Error(), "未知宏观指标")
}

// stubTushareByAPI 起一个按 api_name 分发的 tushare stub（多指标导入需要
// 各接口各自的字段形状：PPI 的数值列是 ppi_yoy，不是 nt_yoy）。
func stubTushareByAPI(t *testing.T, bodies map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			APIName string `json:"api_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		body, ok := bodies[req.APIName]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":-1,"msg":"stub: unknown api_name"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchMacroIndicators_SequentialAndPropagates(t *testing.T) {
	// 两个指标顺序拉取（各 1 行）；第二个失败时整体报错（第一个的成果也
	// 不返回——调用方无从判断半途状态，宁可重来）。
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushareByAPI(t, map[string]string{
		"cn_cpi": macroOKBody,
		"cn_ppi": `{"code":0,"msg":"","data":{"fields":["month","ppi_yoy"],"items":[["202608",-1.5]]}}`,
	}), rec)

	rows, err := client.FetchMacroIndicators(context.Background(), "CPI_YOY", "PPI_YOY")
	require.NoError(t, err)
	require.Len(t, rows, 2, "两个指标各 1 行")
	require.Len(t, rec.macro, 2, "两个指标的行都落到了 store")

	_, err = client.FetchMacroIndicators(context.Background(), "CPI_YOY", "NOT_AN_INDICATOR")
	require.Error(t, err)
}

func TestProbeMacro_DoesNotPersist(t *testing.T) {
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, macroOKBody, 200), rec)

	require.NoError(t, client.ProbeMacro(context.Background(), "CPI_YOY"))
	require.Empty(t, rec.macro, "探测不许产生数据副作用（只取不落）")

	require.Error(t, client.ProbeMacro(context.Background(), "NOT_AN_INDICATOR"))
}

// TestFetchMacroIndicators_SharesOneRequestPerAPI 钉住「同一 tushare 接口的
// 多个 indicator 共用一次请求」：LPR_1Y / LPR_5Y 同 shibor_lpr（实测限
// 1 次/小时），M2 / M2_YOY 同 cn_m。分两次拉的话，shibor_lpr 的第二次请求
// 必然 40203——所以这不是优化，是流控所迫。
//
// 破坏腿：把 FetchMacroIndicators 改回逐个 FetchMacroIndicator →
// calls["shibor_lpr"] 变 2，本测试红（且真拉会 40203）。
func TestFetchMacroIndicators_SharesOneRequestPerAPI(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	bodies := map[string]string{
		"shibor_lpr": `{"code":0,"msg":"","data":{"fields":["date","1y","5y"],"items":[["20260820",3.0,3.5]]}}`,
		"cn_m":       `{"code":0,"msg":"","data":{"fields":["month","m2","m2_yoy"],"items":[["202608",3568083.6,7.5]]}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			APIName string `json:"api_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		calls[req.APIName]++
		mu.Unlock()
		body, ok := bodies[req.APIName]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(srv.URL, rec)

	rows, err := client.FetchMacroIndicators(context.Background(), "LPR_1Y", "LPR_5Y", "M2", "M2_YOY")
	require.NoError(t, err)
	require.Len(t, rows, 4, "4 个 indicator 各 1 行（同接口的从同一份响应各取各的列）")
	require.Len(t, rec.macro, 4, "全部落库")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls["shibor_lpr"],
		"LPR_1Y/LPR_5Y 必须共用一次 shibor_lpr 请求（该接口限 1 次/小时）")
	require.Equal(t, 1, calls["cn_m"], "M2/M2_YOY 必须共用一次 cn_m 请求")

	// 两个 indicator 从同一份响应各取自己的列。
	byIndicator := map[string]float64{}
	for _, r := range rows {
		byIndicator[r.Indicator] = r.Value
	}
	require.InDelta(t, 3.0, byIndicator["LPR_1Y"], 1e-12)
	require.InDelta(t, 3.5, byIndicator["LPR_5Y"], 1e-12)
	require.InDelta(t, 3568083.6, byIndicator["M2"], 1e-6)
	require.InDelta(t, 7.5, byIndicator["M2_YOY"], 1e-12)
}

// ── R1：探测只发一次请求（P2-1-R2） ─────────────────────────────────────

// TestProbeMacro_ChunkedIndicatorSendsSingleRequest 钉住 R1：探测的语义是
// 连通性，不按数据量放大——USDCNY_CLOSE 导入要 8 段（2019–2026），探测
// 必须只发 1 次请求。旧行为（遍历全部窗口）会让每次 HealthCheck 白吃
// fx_daily（2000 积分档）配额。
//
// 破坏腿：ProbeMacro 改回遍历 macroFetchWindows 全量 → calls 变 8，本测试红。
func TestProbeMacro_ChunkedIndicatorSendsSingleRequest(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"fields":["trade_date","bid_close"],"items":[["20260102",6.95]]}}`))
	}))
	t.Cleanup(srv.Close)

	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(srv.URL, rec)
	require.NoError(t, client.ProbeMacro(context.Background(), "USDCNY_CLOSE"))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls,
		"USDCNY_CLOSE 探测只许发 1 次请求（全窗口导入是 8 次——探测不许按数据量放大）")
	require.Empty(t, rec.macro, "探测不许产生数据副作用")
}

// ── R2：跳过计数与 fail-loud（P2-1-R2） ─────────────────────────────────

// TestFetchMacroIndicator_FailsLoudWhenAllRowsSkipped 是 R2 的 Red 腿：
// 响应有行但值列全 nil（上游列整体变空的典型形态）→ 必须报错，不许以
// 「导入 0 行」静默成功。
//
// 破坏腿：去掉 macroAllSkippedError 闸门 → 本测试红（返回 nil, nil）。
func TestFetchMacroIndicator_FailsLoudWhenAllRowsSkipped(t *testing.T) {
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t,
		`{"code":0,"msg":"","data":{"fields":["month","nt_yoy"],"items":[["202608",null],["202607",null]]}}`, 200), rec)

	rows, err := client.FetchMacroIndicator(context.Background(), "CPI_YOY")
	require.Error(t, err, "有行但全被跳过必须 fail-loud（上游变故不许伪装成导入 0 行）")
	require.Nil(t, rows)
	require.Contains(t, err.Error(), "CPI_YOY")
	require.Contains(t, err.Error(), "全部被跳过")
	require.Empty(t, rec.macro, "错误路径不许落库")
}

// TestFetchMacroIndicators_FailsLoudWhenOneIndicatorAllSkipped：共享响应的
// 组里一个 indicator 的列全 nil（另一个正常）——点名报错那个 indicator。
func TestFetchMacroIndicators_FailsLoudWhenOneIndicatorAllSkipped(t *testing.T) {
	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushareByAPI(t, map[string]string{
		"cn_m": `{"code":0,"msg":"","data":{"fields":["month","m2","m2_yoy"],"items":[["202608",3568083.6,null]]}}`,
	}), rec)

	_, err := client.FetchMacroIndicators(context.Background(), "M2", "M2_YOY")
	require.Error(t, err, "M2 有行、M2_YOY 列全 nil——必须 fail-loud")
	require.Contains(t, err.Error(), "M2_YOY", "错误必须点名断掉的 indicator")
	require.Empty(t, rec.macro, "错误路径不许落库")
}

// ── R3：组不变量与目录自检（P2-1-R2） ───────────────────────────────────

// TestSpecsCanShareResponse_DetectsInconsistentInvariants：fields / 日期列 /
// 日期格式 / 固定参数 / 窗口序列任一不一致都要被捕获并点名原因。
func TestSpecsCanShareResponse_DetectsInconsistentInvariants(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	base := macroIndicatorSpecs["M2"]

	fieldsDiff := base
	fieldsDiff.Indicator, fieldsDiff.Field, fieldsDiff.Fields = "M2_X", "m2_x", "month,m2_x"
	share, reason := specsCanShareResponse([]macroIndicatorSpec{base, fieldsDiff}, now)
	require.False(t, share)
	require.Contains(t, reason, "fields", "原因应点名 fields 不同")

	layoutDiff := base
	layoutDiff.Indicator, layoutDiff.DateLayout = "M2_X", "20060102"
	share, reason = specsCanShareResponse([]macroIndicatorSpec{base, layoutDiff}, now)
	require.False(t, share)
	require.Contains(t, reason, "日期格式")

	paramsDiff := macroIndicatorSpecs["USDCNY_CLOSE"]
	paramsDiff.Indicator = "USDCNY_FAKE"
	paramsDiff.Params = map[string]interface{}{"ts_code": "EURUSD.FXCM"}
	share, reason = specsCanShareResponse([]macroIndicatorSpec{macroIndicatorSpecs["USDCNY_CLOSE"], paramsDiff}, now)
	require.False(t, share)
	require.Contains(t, reason, "固定参数")

	winDiff := base
	winDiff.Indicator, winDiff.SinceYear = "M2_WIN", 2024 // 月频全量 → 3 段窗口
	share, reason = specsCanShareResponse([]macroIndicatorSpec{base, winDiff}, now)
	require.False(t, share)
	require.Contains(t, reason, "窗口")

	// 真实目录的两个共享组必须通过（自洽）。
	share, reason = specsCanShareResponse([]macroIndicatorSpec{
		macroIndicatorSpecs["LPR_1Y"], macroIndicatorSpecs["LPR_5Y"],
	}, now)
	require.True(t, share, "LPR 组应可共享：%s", reason)
	share, reason = specsCanShareResponse([]macroIndicatorSpec{
		macroIndicatorSpecs["M2"], macroIndicatorSpecs["M2_YOY"],
	}, now)
	require.True(t, share, "M2 组应可共享：%s", reason)
}

// TestMacroIndicatorSpecs_GroupInvariantsHold 是目录自检（R3）：按 APIName
// 分组的每组 spec 必须能共用一次响应。未来给同接口加指标时若配错
// fields/params/窗口，这里红——把「目录配置不自洽」拦在测试层，而不是
// 运行时伪装成「响应形状不符契约」。
func TestMacroIndicatorSpecs_GroupInvariantsHold(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	groups := map[string][]macroIndicatorSpec{}
	for _, spec := range macroIndicatorSpecs {
		groups[spec.APIName] = append(groups[spec.APIName], spec)
	}
	shared := 0
	for api, specs := range groups {
		if len(specs) < 2 {
			continue
		}
		shared++
		share, reason := specsCanShareResponse(specs, now)
		require.True(t, share, "同接口 %s 的 spec 无法共用响应：%s", api, reason)
	}
	require.Equal(t, 2, shared, "首版目录有且仅有 cn_m 与 shibor_lpr 两个共享接口组")
}

// TestFetchMacroIndicators_RejectsInconsistentCatalogGroup：人为往目录塞一个
// 与 M2 不变量冲突的 spec——FetchMacroIndicators 必须报「目录配置不自洽」
// （点名根因），而不是发完请求后在 normalize 报「响应形状不符契约」
// （错误归因）。校验必须发生在发请求/落库之前。
func TestFetchMacroIndicators_RejectsInconsistentCatalogGroup(t *testing.T) {
	const fake = "M2_FAKE"
	orig, had := macroIndicatorSpecs[fake]
	macroIndicatorSpecs[fake] = macroIndicatorSpec{
		Indicator: fake, APIName: "cn_m", Field: "m2_fake",
		Freq: MacroFreqMonthly, Unit: "%", DateField: "month", DateLayout: "200601",
		Fields: "month,m2_fake", // 与 M2 的 fields 不同 → 不能共用一次响应
	}
	t.Cleanup(func() {
		if had {
			macroIndicatorSpecs[fake] = orig
			return
		}
		delete(macroIndicatorSpecs, fake)
	})

	rec := &recordingMacroStore{}
	client := newSupplyChainTestClient(stubTushare(t, macroOKBody, 200), rec)
	_, err := client.FetchMacroIndicators(context.Background(), "M2", fake)
	require.Error(t, err, "组不变量不一致必须报错，不许静默退化逐条拉")
	require.Contains(t, err.Error(), "配置不自洽", "错误必须点名根因是指标目录，不是上游")
	require.Empty(t, rec.macro, "报错发生在发请求/落库之前")
}

// errSentinel 是错误传播测试用的哨兵错误。
var errSentinel = &macroSentinelError{}

type macroSentinelError struct{}

func (e *macroSentinelError) Error() string { return "macro sentinel save failure" }
