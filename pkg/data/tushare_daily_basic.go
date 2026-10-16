package data

// 市场活跃度数据层导入面（D5，TASKS.md §一 D5）—— Tushare daily_basic
// （活跃度族） → market_activity 表（Migration 056）。
//
//	ts_code / trade_date / turnover_rate / turnover_rate_f / volume_ratio
//	  → market_activity(symbol, trade_date, turnover_rate, turnover_rate_f,
//	                     volume_ratio, source='tushare', as_of)
//
// 为什么单交易日一次请求（不传 start_date/end_date 拉区间）：官方限量
// 「单次最高 6000 条」，全市场单日约 5500–5600 行，单日请求在限额内且
// 形状可预期；跨日区间请求的行数上限未文档化（可能静默截断——同 fx_daily
// 的前科，见 macro.go 的 SinceYear 注释）。按日迭代由调用方（adapter）
// 负责，每次导入的日期集合显式可控。
//
// 纪律（与 macro.go 的 Fetch* 同款）：
//   - **错误一律向上传播**：call 失败、响应缺列、Save 失败，全部 apperrors
//     包装后 return nil, err。绝不 log 后 continue——那会让「上游挂了」
//     伪装成「导入了 0 行」。
//   - **有行但全被跳过 → fail-loud**（dailyBasicAllSkippedError）：真空响应
//     （节假日 items==0）才是「导入 0 行」的合法场景。
//   - 缺失≠0：turnover_rate 等列为 nil（未发布/停牌）的行**照常入库**
//     （数值列落 NULL，身份列完整——缺失是数据状态，要可见），非 nil 但
//     不可解析/非有限的行跳过并计数（脏值不进库）。
//   - 幂等在存储层：ON CONFLICT DO UPDATE（见 pkg/storage/market_activity.go）。
//   - 溯源列：source 恒为 "tushare"，as_of 取导入时刻。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

// ActivitySourceTushare 是 market_activity source 列的 tushare 取值。
const ActivitySourceTushare = "tushare"

// dailyBasicFields 是请求的列（只取活跃度族三列 + 身份列，压响应体积）。
const dailyBasicFields = "ts_code,trade_date,turnover_rate,turnover_rate_f,volume_ratio"

// MarketActivityStore 是活跃度数据层的写入面（pkg/storage.PostgresStore
// 实现）。TushareStore 内嵌它，导入路径的 Save 契约在编译期钉住。
type MarketActivityStore interface {
	SaveMarketActivityBatch(ctx context.Context, rows []*domain.MarketActivity) error
}

// FetchDailyBasic 拉取**单个交易日**的全市场活跃度观测并落库
// market_activity，返回入库的行。
//
// tradeDate 为 "YYYYMMDD"。节假日/非交易日上游返回空 items —— 合法，
// 返回 (nil, nil)。任一行缺失数值不影响其他行入库（缺失≠0，NULL 落库）；
// 但整行缺身份列（ts_code 空 / trade_date 不可解析）或数值脏 → 跳过并
// 计数，全部跳过 → fail-loud（上游形状变故，拒绝以「导入 0 行」伪装成功）。
// 响应缺任何一列（整个响应没有该列）→ 报错，形状不符契约。
//
// tradeDate 必须是 "YYYYMMDD"：格式不对在**本地**拦下（INVALID_INPUT），
// 不让它变成一次无谓的上游请求 + 上游的口径不明报错。PIT 身份（日期）
// 是这条链路的锚点，格式错误必须点名到参数。
func (c *TushareClient) FetchDailyBasic(ctx context.Context, tradeDate string) ([]domain.MarketActivity, error) {
	if _, err := time.Parse("20060102", tradeDate); err != nil {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput, fmt.Sprintf(
			"daily_basic trade_date 必须是 YYYYMMDD（got %q）", tradeDate))
	}
	resp, err := c.call(ctx, "daily_basic", map[string]interface{}{
		"trade_date": tradeDate,
	}, dailyBasicFields)
	if err != nil {
		return nil, err
	}

	asOf := time.Now().UTC() // 与 normalize 共用同一导入时刻
	rows, skipped, err := normalizeDailyBasic(resp, tradeDate, asOf)
	if err != nil {
		return nil, err
	}
	if err := dailyBasicAllSkippedError(tradeDate, len(resp.Data.Items), skipped); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		// 真空响应（上游确实没返回任何行）——合法。
		return nil, nil
	}

	ptrs := make([]*domain.MarketActivity, len(rows))
	for i := range rows {
		ptrs[i] = &rows[i]
	}
	if err := c.store.SaveMarketActivityBatch(ctx, ptrs); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			fmt.Sprintf("活跃度观测 %s 落库失败", tradeDate), "data.FetchDailyBasic")
	}
	c.logger.Info().Str("trade_date", tradeDate).Int("count", len(rows)).
		Int("skipped", skipped.Skipped()).Msg("daily_basic fetched and saved")
	return rows, nil
}

// ProbeDailyBasic 对单个交易日做一次「只取不落」的连通性探测
// （HealthCheck 用）：发一次 call，成功即返回 nil。不 normalize、不落库
// ——探测不应产生数据副作用。
func (c *TushareClient) ProbeDailyBasic(ctx context.Context, tradeDate string) error {
	_, err := c.call(ctx, "daily_basic", map[string]interface{}{
		"trade_date": tradeDate,
	}, dailyBasicFields)
	return err
}

// dailyBasicSkipped 是 normalizeDailyBasic 的跳过计数。
type dailyBasicSkipped struct {
	BadDate  int // trade_date 不可解析（PIT 身份不完整，跳过该行）
	NilValue int // 三个数值列全为 nil（当日未发布——**照常入库**，不跳过）
	BadValue int // 数值列非 nil 但不可解析/非有限（形状变故，跳过该行）
	BadCode  int // ts_code 为空（身份不完整，跳过该行）
}

// Skipped 是总跳过行数（BadDate + BadValue + BadCode；NilValue 不跳过）。
func (n dailyBasicSkipped) Skipped() int {
	return n.BadDate + n.BadValue + n.BadCode
}

// dailyBasicColumnIndex 在响应的 Fields 头里按**名字（大小写不敏感）**找列
// （同 macroColumnIndex 的口径——cn_pmi 等接口在 fields 为空时返回大写
// 字段名，两条路径都要能定位）。找不到返回 -1。
func dailyBasicColumnIndex(resp *TushareResponse, name string) int {
	lower := make(map[string]int, len(resp.Data.Fields))
	for i, f := range resp.Data.Fields {
		lower[strings.ToLower(f)] = i
	}
	if idx, ok := lower[strings.ToLower(name)]; ok {
		return idx
	}
	return -1
}

// normalizeDailyBasic 把 daily_basic 响应转为 market_activity 行（纯函数）。
//
// 口径：
//   - symbol 取 ts_code 列；trade_date 按行内日期列解析（"20060102"）；
//   - 三个数值列：nil → 指针 nil（缺失≠0，照常入库）；非 nil 但不可解析/
//     非有限 → 跳过该行并计 BadValue（脏值不进库）；
//   - 三列全 nil 的行（NilValue）**不是跳过**——身份完整的「当日未发布」
//     观测照常入库，缺失是数据状态不是脏数据；
//   - 缺任何一列（整个响应没有该列）→ 报错，形状不符契约。
func normalizeDailyBasic(resp *TushareResponse, tradeDate string, asOf time.Time) ([]domain.MarketActivity, dailyBasicSkipped, error) {
	iCode := dailyBasicColumnIndex(resp, "ts_code")
	iDate := dailyBasicColumnIndex(resp, "trade_date")
	iTurnover := dailyBasicColumnIndex(resp, "turnover_rate")
	iTurnoverF := dailyBasicColumnIndex(resp, "turnover_rate_f")
	iVolRatio := dailyBasicColumnIndex(resp, "volume_ratio")
	// 缺列点名（fail-loud 报错要点名缺什么——同 D2 契约的报错纪律）。
	var missing []string
	for _, c := range []struct {
		name string
		idx  int
	}{
		{"ts_code", iCode}, {"trade_date", iDate}, {"turnover_rate", iTurnover},
		{"turnover_rate_f", iTurnoverF}, {"volume_ratio", iVolRatio},
	} {
		if c.idx < 0 {
			missing = append(missing, c.name)
		}
	}
	if len(missing) > 0 {
		return nil, dailyBasicSkipped{}, apperrors.New(apperrors.ErrCodeDataQuality, fmt.Sprintf(
			"daily_basic 响应缺列（%s）：缺 %v，实际 fields=%v",
			tradeDate, missing, resp.Data.Fields))
	}

	var (
		out     []domain.MarketActivity
		skipped dailyBasicSkipped
	)
	for _, item := range resp.Data.Items {
		symbol := tushareCell(item, iCode)
		if symbol == "" {
			skipped.BadCode++
			continue // 身份不完整，不进库
		}
		d, ok := parseTradeDateCell(tushareCell(item, iDate))
		if !ok {
			skipped.BadDate++
			continue // PIT 身份不完整，不进库
		}
		turnover, badT := activityValue(item, iTurnover)
		turnoverF, badTF := activityValue(item, iTurnoverF)
		volRatio, badVR := activityValue(item, iVolRatio)
		if badT || badTF || badVR {
			skipped.BadValue++
			continue // 脏值不进库（nil 已在 activityValue 内区分）
		}
		if turnover == nil && turnoverF == nil && volRatio == nil {
			skipped.NilValue++ // 计数供审计，但**不跳过**
		}
		out = append(out, domain.MarketActivity{
			Symbol:        symbol,
			TradeDate:     d,
			TurnoverRate:  turnover,
			TurnoverRateF: turnoverF,
			VolumeRatio:   volRatio,
			Source:        ActivitySourceTushare,
			AsOf:          asOf,
		})
	}
	return out, skipped, nil
}

// activityValue 取一个可空数值列：nil（或缺列位）→ (nil, false)（缺失）；
// 非 nil 可解析有限 → (值, false)；非 nil 不可解析/非有限 → (nil, true)
// （脏值，调用方跳过该行）。
func activityValue(item []any, idx int) (*float64, bool) {
	if idx < 0 || idx >= len(item) || item[idx] == nil {
		return nil, false
	}
	v, ok := tushareFloatOK(item[idx])
	if !ok {
		return nil, true
	}
	return &v, false
}

// parseTradeDateCell 解析 "YYYYMMDD" 交易日；空串/解析失败 → (零值, false)。
func parseTradeDateCell(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// dailyBasicAllSkippedError 是 fail-loud 闸门：一次响应**有行**却一行
// 都没能入库（全坏身份 / 全坏日期 / 全脏值）时拒绝静默成功——这是上游
// 变故的典型形态（改格式、列整体变空），不是「导入 0 行」的合法场景。
// 真空响应（items==0，节假日）不在此列。
func dailyBasicAllSkippedError(tradeDate string, items int, skipped dailyBasicSkipped) error {
	if items == 0 || skipped.Skipped() < items {
		return nil
	}
	return apperrors.New(apperrors.ErrCodeDataQuality, fmt.Sprintf(
		"活跃度观测 %s：daily_basic 响应返回 %d 行但全部被跳过（坏代码 %d / 坏日期 %d / 脏值 %d）——上游形状/格式变故，拒绝以「导入 0 行」伪装成功",
		tradeDate, items, skipped.BadCode, skipped.BadDate, skipped.BadValue))
}
