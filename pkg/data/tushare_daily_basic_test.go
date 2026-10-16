package data

// 市场活跃度导入面（D5，pkg/data/tushare_daily_basic.go）的单元测试。
//
// 覆盖三类口径：
//   - 缺失≠0：三列全 nil 的行**照常入库**（指针 nil，不折 0）；
//   - 脏值不进库：非 nil 但不可解析/非有限的行跳过并计数；
//   - fail-loud：响应缺列报错；有行但全被跳过报错（拒绝以「导入 0 行」
//     伪装成功）；Save/call 错误向上传播。
//
// 用手算夹具而非「重跑实现自己的公式」：期望值都在注释里独立算一遍。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// d5RecStore 记录活跃度 Save 入参，不碰库（同 macro 的 recordingMacroStore）。
type d5RecStore struct {
	TushareStore
	activity []*domain.MarketActivity
}

func (s *d5RecStore) SaveMarketActivityBatch(_ context.Context, rows []*domain.MarketActivity) error {
	s.activity = append(s.activity, rows...)
	return nil
}

func (s *d5RecStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// d5FailStore 的活跃度 Save 恒失败——证明错误被上传而不是吞掉。
type d5FailStore struct {
	TushareStore
	err error
}

func (s *d5FailStore) SaveMarketActivityBatch(_ context.Context, _ []*domain.MarketActivity) error {
	return s.err
}

func (s *d5FailStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// d5Body 是 daily_basic 响应夹具（2026-10-14 实测字段形状）：
//   - 行 1：数值齐全；
//   - 行 2：三列全 null（当日未发布）——照常入库，指针 nil；
//   - 行 3：turnover_rate 为 "N/A"（形状变故）——脏值，跳过；
//   - 行 4：ts_code 为空——身份不完整，跳过；
//   - 行 5：trade_date 不可解析——PIT 身份不完整，跳过。
const d5Body = `{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f","volume_ratio"],"items":[
	["600519.SH","20261014",1.23,1.5,0.9],
	["000001.SZ","20261014",null,null,null],
	["000002.SZ","20261014","N/A",1.1,0.8],
	["","20261014",2.0,2.1,1.2],
	["600000.SH","2026-10-14",3.0,3.1,1.3]
]}}`

func TestNormalizeDailyBasic_MissingIsVisibleDirtyIsSkipped(t *testing.T) {
	resp := decodeTushareBody(t, d5Body)
	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

	rows, skipped, err := normalizeDailyBasic(resp, "20261014", asOf)
	require.NoError(t, err)

	require.Len(t, rows, 2, "身份完整的行入库：数值齐全 1 行 + 三列全 nil 1 行")
	require.Equal(t, 1, skipped.NilValue, "三列全 nil 的行计数（供审计）但不跳过")
	require.Equal(t, 1, skipped.BadValue, "「N/A」是脏值，跳过")
	require.Equal(t, 1, skipped.BadCode, "空 ts_code 跳过")
	require.Equal(t, 1, skipped.BadDate, "日期不可解析跳过（PIT 身份不完整）")
	require.Equal(t, 3, skipped.Skipped(), "总跳过 = BadValue + BadCode + BadDate（NilValue 不算）")

	// 行 1：数值齐全，指针解出。
	first := rows[0]
	require.Equal(t, "600519.SH", first.Symbol)
	require.Equal(t, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), first.TradeDate)
	require.NotNil(t, first.TurnoverRate)
	require.InDelta(t, 1.23, *first.TurnoverRate, 1e-12)
	require.InDelta(t, 1.5, *first.TurnoverRateF, 1e-12)
	require.InDelta(t, 0.9, *first.VolumeRatio, 1e-12)
	require.Equal(t, ActivitySourceTushare, first.Source)
	require.Equal(t, asOf, first.AsOf)

	// 行 2：缺失≠0——指针 nil（不是 0.0），但行本身入库（缺失是数据状态）。
	second := rows[1]
	require.Equal(t, "000001.SZ", second.Symbol)
	require.Nil(t, second.TurnoverRate, "缺失≠0：nil 指针，绝不是 0.0")
	require.Nil(t, second.TurnoverRateF)
	require.Nil(t, second.VolumeRatio)
}

func TestNormalizeDailyBasic_MissingColumnIsContractViolation(t *testing.T) {
	// volume_ratio 列整体缺失 → 形状不符契约，fail-loud（不是「跳过」）。
	body := `{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f"],"items":[["600519.SH","20261014",1.23,1.5]]}}`
	_, _, err := normalizeDailyBasic(decodeTushareBody(t, body), "20261014", time.Now().UTC())
	require.Error(t, err)
	require.Contains(t, err.Error(), "缺列")
	require.Contains(t, err.Error(), "volume_ratio")
}

func TestFetchDailyBasic_SavesNormalizedRows(t *testing.T) {
	rec := &d5RecStore{}
	client := newSupplyChainTestClient(stubTushare(t, d5Body, 200), rec)

	rows, err := client.FetchDailyBasic(context.Background(), "20261014")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Len(t, rec.activity, 2, "normalize 后的行必须落到 store（Items ≡ 入库行）")
	require.Equal(t, ActivitySourceTushare, rec.activity[0].Source)
	require.False(t, rec.activity[0].AsOf.IsZero(), "as_of 必须取导入时刻")
	require.Nil(t, rec.activity[1].TurnoverRate, "缺失列以 NULL 落库")
}

func TestFetchDailyBasic_EmptyResponseIsLegal(t *testing.T) {
	// 节假日：上游返回空 items —— 合法空，不报错。
	body := `{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f","volume_ratio"],"items":[]}}`
	rec := &d5RecStore{}
	client := newSupplyChainTestClient(stubTushare(t, body, 200), rec)

	rows, err := client.FetchDailyBasic(context.Background(), "20261001")
	require.NoError(t, err)
	require.Nil(t, rows)
	require.Empty(t, rec.activity, "空响应不落库")
}

// TestFetchDailyBasic_FailsLoudWhenAllRowsSkipped 钉住 fail-loud 闸门：
// 有行但全部身份不完整 → 报错（拒绝以「导入 0 行」伪装成功）。
// 破坏腿：把 dailyBasicAllSkippedError 摘掉 → 本测试红（静默成功）。
func TestFetchDailyBasic_FailsLoudWhenAllRowsSkipped(t *testing.T) {
	body := `{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f","volume_ratio"],"items":[["","20261014",1.0,1.0,1.0],["600519.SH","20991340",1.0,1.0,1.0]]}}`
	rec := &d5RecStore{}
	client := newSupplyChainTestClient(stubTushare(t, body, 200), rec)

	_, err := client.FetchDailyBasic(context.Background(), "20261014")
	require.Error(t, err, "有行但全跳过必须 fail-loud")
	require.Contains(t, err.Error(), "全部被跳过")
	require.Empty(t, rec.activity)
}

func TestFetchDailyBasic_PropagatesCallError(t *testing.T) {
	rec := &d5RecStore{}
	client := newSupplyChainTestClient(stubTushare(t, `{"code":-1,"msg":"upstream down"}`, 200), rec)

	_, err := client.FetchDailyBasic(context.Background(), "20261014")
	require.Error(t, err, "上游错误必须向上传播，不许伪装成 0 行")
	require.Empty(t, rec.activity)
}

func TestFetchDailyBasic_PropagatesSaveError(t *testing.T) {
	saveErr := errors.New("db is down")
	client := newSupplyChainTestClient(stubTushare(t, d5Body, 200), &d5FailStore{err: saveErr})

	_, err := client.FetchDailyBasic(context.Background(), "20261014")
	require.Error(t, err)
	require.True(t, errors.Is(err, saveErr), "落库失败必须上传（含原始错误链），got: %v", err)
}

// TestProbeDailyBasic_NoSideEffects 钉住探测语义：只取不落。
func TestProbeDailyBasic_NoSideEffects(t *testing.T) {
	rec := &d5RecStore{}
	client := newSupplyChainTestClient(stubTushare(t, d5Body, 200), rec)

	require.NoError(t, client.ProbeDailyBasic(context.Background(), "20261014"))
	require.Empty(t, rec.activity, "探测不得落库")
}

// TestNormalizeDailyBasic_TurnoverZeroStaysZero 钉住「缺失≠0 的反面」：
// 真实的 0 换手是有意义观测，必须原样保留（不得被当成缺失）。
// 用 0.0（非 null）表达——与 nil 走的是两条路。
func TestNormalizeDailyBasic_TurnoverZeroStaysZero(t *testing.T) {
	body := `{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f","volume_ratio"],"items":[["600519.SH","20261014",0.0,0.0,0.0]]}}`
	rows, skipped, err := normalizeDailyBasic(decodeTushareBody(t, body), "20261014", time.Now().UTC())
	require.NoError(t, err)
	require.Empty(t, skipped.Skipped())
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].TurnoverRate, "0.0 是有意义观测，不是缺失")
	require.InDelta(t, 0.0, *rows[0].TurnoverRate, 1e-12)
	require.Zero(t, skipped.NilValue, "0.0 不触发 nil 计数")
	require.False(t, strings.Contains(rows[0].Symbol, " "), "symbol 不应含空白")
}
