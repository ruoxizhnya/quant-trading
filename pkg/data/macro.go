package data

// 宏观数据层导入面（P2-1，TASKS.md §五 遗留待裁决）。
//
// Tushare 宏观接口 → quant.macro_series（Migration 050）：
//
//	cn_cpi      → CPI_YOY / cn_ppi → PPI_YOY / cn_pmi → PMI
//	cn_m        → M2 + M2_YOY / sf_month → TSF_INC
//	shibor_lpr  → LPR_1Y + LPR_5Y / fx_daily → USDCNY_CLOSE
//
// 纪律（与 supply_chain.go 的 Fetch* 同款）：
//   - **错误一律向上传播**：call 失败、响应形状不符契约、Save 失败，全部
//     apperrors 包装后 return nil, err。绝不 log 后 continue——那会让「上游
//     挂了」伪装成「导入了 0 行」（破坏验证②的 Red 腿）。
//   - 幂等在存储层：ON CONFLICT DO UPDATE（见 pkg/storage/macro.go），同
//     唯一键重导刷新行而不插新行。
//   - 溯源列：tushare 导入的 source 恒为 "tushare"，as_of 取导入时刻。
//
// 接口核验（2026-10-12，官方文档页 + 本仓 token 实测，全部 code:0）：
//   cn_cpi doc_id=228（600 积分）/ cn_ppi 245（600）/ cn_pmi 325（5000）/
//   cn_m 242（600）/ sf_month 310（2000）/ shibor_lpr 151（120）/
//   fx_daily 179（2000）。第三方映射表（GitHub aigroup-market-mcp）的
//   doc_id 与积分多处过期，未采信——「凭文档臆断」正是 P2-8 40203 前科
//   的教训，一切以官方文档页 + 实测为准。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

// MacroSourceTushare 是 quant.macro_series source 列的 tushare 取值。
const MacroSourceTushare = "tushare"

// 频率常量（quant.macro_series.freq）。
const (
	MacroFreqMonthly = "M" // 月频（CPI/PPI/PMI/M2/社融）
	MacroFreqDaily   = "D" // 日频（LPR 报价 / 汇率）
)

// macroIndicatorSpec 描述一个宏观指标如何从 Tushare 取数。
//
// DateLayout 决定 obs_date 的解析口径：月频 "200601"（取当月首日），
// 日频 "20060102"（自然日）。SinceYear 非零表示日频指标按年分段请求
// （fx_daily 官方限量单次 1000 行，2019 至今累计 2400+ 行，一次拉全
// 会静默丢数据——实测当前 API 不截断，但不依赖未文档化行为）。
// 分段会放大请求数，因此**只对确实需要分段且流控允许的接口开**
// （shibor_lpr 是 1 次/小时，实测 40203 → 不开，见 LPR_1Y 条目）。
type macroIndicatorSpec struct {
	Indicator  string
	APIName    string
	Field      string                 // 响应里的数值列（大小写不敏感匹配）
	Freq       string                 // M / D
	Unit       string                 // % / 亿元 / 点 / 汇率
	DateField  string                 // 响应里的日期列（month / date）
	DateLayout string                 // 200601 / 20060102
	Fields     string                 // fields 参数（只取需要的列，压响应体积）
	Params     map[string]interface{} // 接口级固定参数（fx_daily 的 ts_code）
	SinceYear  int                    // 日频分段起始年（0 = 一次全量）
}

// macroIndicatorSpecs 是首版指标目录（9 个 indicator / 7 个 tushare 接口）。
// 键为 indicator 代码，即 quant.macro_series.indicator 的取值域。
var macroIndicatorSpecs = map[string]macroIndicatorSpec{
	"CPI_YOY": {
		Indicator: "CPI_YOY", APIName: "cn_cpi", Field: "nt_yoy",
		Freq: MacroFreqMonthly, Unit: "%", DateField: "month", DateLayout: "200601",
		Fields: "month,nt_val,nt_yoy",
	},
	"PPI_YOY": {
		Indicator: "PPI_YOY", APIName: "cn_ppi", Field: "ppi_yoy",
		Freq: MacroFreqMonthly, Unit: "%", DateField: "month", DateLayout: "200601",
		Fields: "month,ppi_yoy",
	},
	"PMI": {
		// cn_pmi 不传 fields 时响应字段名为大写（MONTH/PMI010000，实测），
		// 传 fields（小写）则按小写返回；normalize 侧大小写不敏感匹配，
		// 两条路径都成立。
		Indicator: "PMI", APIName: "cn_pmi", Field: "pmi010000",
		Freq: MacroFreqMonthly, Unit: "点", DateField: "month", DateLayout: "200601",
		Fields: "month,pmi010000",
	},
	"M2": {
		Indicator: "M2", APIName: "cn_m", Field: "m2",
		Freq: MacroFreqMonthly, Unit: "亿元", DateField: "month", DateLayout: "200601",
		Fields: "month,m2,m2_yoy",
	},
	"M2_YOY": {
		Indicator: "M2_YOY", APIName: "cn_m", Field: "m2_yoy",
		Freq: MacroFreqMonthly, Unit: "%", DateField: "month", DateLayout: "200601",
		Fields: "month,m2,m2_yoy",
	},
	"TSF_INC": {
		Indicator: "TSF_INC", APIName: "sf_month", Field: "inc_month",
		Freq: MacroFreqMonthly, Unit: "亿元", DateField: "month", DateLayout: "200601",
		Fields: "month,inc_month",
	},
	"LPR_1Y": {
		// ⚠️ SinceYear=0（一次全量），**不按年分段**：实测 shibor_lpr 的流控是
		// 「1 次/小时」（2026-10-12 真拉时返回 40203「您访问接口(shibor_lpr)
		// 频率超限(1次/小时)」）。日频按年分段要 14 次请求，必然撞限；单次
		// 限量 4000 行（≈18 年历史）足够覆盖 LPR（始于 2013-10）。
		// 与 LPR_5Y 同接口 → FetchMacroIndicators 会共用一次请求（见该函数）。
		Indicator: "LPR_1Y", APIName: "shibor_lpr", Field: "1y",
		Freq: MacroFreqDaily, Unit: "%", DateField: "date", DateLayout: "20060102",
		Fields: "date,1y,5y",
	},
	"LPR_5Y": {
		Indicator: "LPR_5Y", APIName: "shibor_lpr", Field: "5y",
		Freq: MacroFreqDaily, Unit: "%", DateField: "date", DateLayout: "20060102",
		Fields: "date,1y,5y",
	},
	"USDCNY_CLOSE": {
		// fx_daily 的 trade_date 为 GMT 口径（官方文档明示「比北京时间晚
		// 一天」），原样入库不做时区转换——转换是解读不是事实，且宏观日频
		// 序列连续、整体偏移一天不改变序列形态；溯源以原值为准。
		Indicator: "USDCNY_CLOSE", APIName: "fx_daily", Field: "bid_close",
		Freq: MacroFreqDaily, Unit: "汇率", DateField: "trade_date", DateLayout: "20060102",
		Fields:    "trade_date,bid_close",
		Params:    map[string]interface{}{"ts_code": "USDCNH.FXCM"},
		SinceYear: 2019, // USDCNH.FXCM 实测数据始于 20190102
	},
}

// MacroIndicators 返回排序后的指标代码列表（目录的稳定视图，供只读 API
// 文档、adapter 全量导入与测试使用）。
func MacroIndicators() []string {
	out := make([]string, 0, len(macroIndicatorSpecs))
	for k := range macroIndicatorSpecs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MacroIndicatorExists 报告 indicator 是否在首版目录内。
func MacroIndicatorExists(indicator string) bool {
	_, ok := macroIndicatorSpecs[indicator]
	return ok
}

// MacroStore 是宏观数据层的写入面（pkg/storage.PostgresStore 实现）。
// TushareStore 内嵌它，导入路径的 Save 契约在编译期钉住。
type MacroStore interface {
	SaveMacroSeriesBatch(ctx context.Context, rows []*domain.MacroSeries) error
}

// FetchMacroIndicator 拉取一个宏观指标的全量历史并落库
// quant.macro_series，返回入库的观测行。
//
// 月频指标一次请求拿全量（各接口限量 2000-5000 行，历史累计均远低于）；
// 日频指标按年分段（见 macroIndicatorSpec.SinceYear 注释）。任一段失败
// 即整体报错——上游故障必须传播，不许伪装成「导入了 0 行」。
func (c *TushareClient) FetchMacroIndicator(ctx context.Context, indicator string) ([]domain.MacroSeries, error) {
	spec, ok := macroIndicatorSpecs[indicator]
	if !ok {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			fmt.Sprintf("未知宏观指标 %q（目录：%v）", indicator, MacroIndicators()))
	}

	asOf := time.Now().UTC()
	var all []domain.MacroSeries
	for _, params := range macroFetchWindows(spec, time.Now().UTC()) {
		resp, err := c.call(ctx, spec.APIName, params, spec.Fields)
		if err != nil {
			return nil, err
		}
		rows, err := normalizeMacroSeries(resp, spec, MacroSourceTushare, asOf)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
				fmt.Sprintf("%s 响应形状不符合契约（缺 %s/%s 列）", spec.APIName, spec.DateField, spec.Field),
				"data.FetchMacroIndicator")
		}
		all = append(all, rows...)
	}

	if len(all) == 0 {
		return nil, nil
	}
	ptrs := make([]*domain.MacroSeries, len(all))
	for i := range all {
		ptrs[i] = &all[i]
	}
	if err := c.store.SaveMacroSeriesBatch(ctx, ptrs); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			fmt.Sprintf("宏观指标 %s 落库失败", indicator), "data.FetchMacroIndicator")
	}
	c.logger.Info().Str("indicator", indicator).Str("api", spec.APIName).
		Int("count", len(all)).Msg("macro indicator fetched and saved")
	return all, nil
}

// FetchMacroIndicators 拉取多个指标。
//
// **同一 tushare 接口的多个 indicator 共用一次请求**：M2 / M2_YOY 同 cn_m，
// LPR_1Y / LPR_5Y 同 shibor_lpr。这不是优化，是流控所迫——实测 shibor_lpr
// 限「1 次/小时」（40203），把 LPR_1Y 和 LPR_5Y 分两次拉的话，第二次必然
// 撞限，目录里就有一个指标永远拉不到。分组后一次响应喂两个 spec，行数不变。
//
// 分组键是 APIName：同接口的两个 spec 的窗口序列必须一致（首版目录里
// 确实一致——都是月频全量或都是日频全量），不一致时按单条目退化为逐条拉取。
// 接口间顺序执行（不并发）：tushare 流控按 token 计，并发只会更快撞限。
// 任一接口失败即整体报错。
func (c *TushareClient) FetchMacroIndicators(ctx context.Context, indicators ...string) ([]domain.MacroSeries, error) {
	if len(indicators) == 0 {
		indicators = MacroIndicators()
	}

	// 按 APIName 分组，保持 indicators 的首现顺序（结果顺序稳定）。
	var order []string
	groups := map[string][]macroIndicatorSpec{}
	for _, ind := range indicators {
		spec, ok := macroIndicatorSpecs[ind]
		if !ok {
			return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
				fmt.Sprintf("未知宏观指标 %q（目录：%v）", ind, MacroIndicators()))
		}
		if _, seen := groups[spec.APIName]; !seen {
			order = append(order, spec.APIName)
		}
		groups[spec.APIName] = append(groups[spec.APIName], spec)
	}

	asOf := time.Now().UTC()
	var all []domain.MacroSeries
	for _, api := range order {
		specs := groups[api]
		if len(specs) > 1 && !sameWindows(specs) {
			// 窗口不一致无法共用响应 → 退化为逐条拉取（结果等价，多花请求）。
			for _, spec := range specs {
				rows, err := c.FetchMacroIndicator(ctx, spec.Indicator)
				if err != nil {
					return nil, err
				}
				all = append(all, rows...)
			}
			continue
		}

		var batch []domain.MacroSeries
		for _, params := range macroFetchWindows(specs[0], time.Now().UTC()) {
			resp, err := c.call(ctx, api, params, specs[0].Fields)
			if err != nil {
				return nil, err
			}
			for _, spec := range specs {
				rows, err := normalizeMacroSeries(resp, spec, MacroSourceTushare, asOf)
				if err != nil {
					return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
						fmt.Sprintf("%s 响应形状不符合契约（缺 %s/%s 列）", api, spec.DateField, spec.Field),
						"data.FetchMacroIndicators")
				}
				batch = append(batch, rows...)
			}
		}
		if len(batch) == 0 {
			continue
		}
		ptrs := make([]*domain.MacroSeries, len(batch))
		for i := range batch {
			ptrs[i] = &batch[i]
		}
		if err := c.store.SaveMacroSeriesBatch(ctx, ptrs); err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
				fmt.Sprintf("宏观指标 %v 落库失败", apiIndicatorNames(specs)), "data.FetchMacroIndicators")
		}
		c.logger.Info().Str("api", api).Int("count", len(batch)).
			Msg("macro indicators fetched and saved")
		all = append(all, batch...)
	}
	return all, nil
}

// sameWindows 报告一组 spec 的分段窗口序列是否一致（能否共用一次响应）。
func sameWindows(specs []macroIndicatorSpec) bool {
	now := time.Now().UTC()
	base := macroFetchWindows(specs[0], now)
	for _, spec := range specs[1:] {
		other := macroFetchWindows(spec, now)
		if len(base) != len(other) {
			return false
		}
		for i := range base {
			if len(base[i]) != len(other[i]) {
				return false
			}
			for k, v := range base[i] {
				if ov, ok := other[i][k]; !ok || ov != v {
					return false
				}
			}
		}
	}
	return true
}

// apiIndicatorNames 取一组 spec 的 indicator 名（错误信息里点名用）。
func apiIndicatorNames(specs []macroIndicatorSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Indicator)
	}
	return out
}

// ProbeMacro 对指标做一次「只取不落」的连通性探测（HealthCheck 用）：
// 按目录发一次 call，成功即返回 nil。不 normalize、不落库——探测不应
// 产生数据副作用。
func (c *TushareClient) ProbeMacro(ctx context.Context, indicator string) error {
	spec, ok := macroIndicatorSpecs[indicator]
	if !ok {
		return apperrors.New(apperrors.ErrCodeInvalidInput,
			fmt.Sprintf("未知宏观指标 %q（目录：%v）", indicator, MacroIndicators()))
	}
	for _, params := range macroFetchWindows(spec, time.Now().UTC()) {
		if _, err := c.call(ctx, spec.APIName, params, spec.Fields); err != nil {
			return err
		}
	}
	return nil
}

// macroFetchWindows 构造一次导入所需的全部请求参数：
//   - 月频（SinceYear==0）：单次全量请求，params 只含接口级固定参数；
//   - 日频（SinceYear>0）：按年分段 [SinceYear, 当前年]，每段一个请求，
//     params 叠加 start_date/end_date（YYYYMMDD）。
func macroFetchWindows(spec macroIndicatorSpec, now time.Time) []map[string]interface{} {
	base := map[string]interface{}{}
	for k, v := range spec.Params {
		base[k] = v
	}
	if spec.SinceYear == 0 {
		return []map[string]interface{}{base}
	}
	windows := make([]map[string]interface{}, 0, now.Year()-spec.SinceYear+1)
	for y := spec.SinceYear; y <= now.Year(); y++ {
		w := map[string]interface{}{
			"start_date": fmt.Sprintf("%04d0101", y),
			"end_date":   fmt.Sprintf("%04d1231", y),
		}
		for k, v := range base {
			w[k] = v
		}
		windows = append(windows, w)
	}
	return windows
}

// ── normalize（纯函数：TushareResponse → domain 行） ─────────────────────

// macroColumnIndex 在响应的 Fields 头里按**名字（大小写不敏感）**找列：
// names 靠前的优先，与列在响应里的实际顺序无关。大小写不敏感是因为
// cn_pmi 等接口在 fields 参数为空时返回大写字段名（MONTH/PMI010000，
// 实测），传 fields 时按小写返回——两条路径都要能定位。
// 全部找不到返回 -1。缺关键列由调用方报「形状不符契约」（fail-loud，
// 不静默跳行）。
func macroColumnIndex(resp *TushareResponse, names ...string) int {
	lower := make(map[string]int, len(resp.Data.Fields))
	for i, f := range resp.Data.Fields {
		lower[strings.ToLower(f)] = i
	}
	for _, n := range names {
		if idx, ok := lower[strings.ToLower(n)]; ok {
			return idx
		}
	}
	return -1
}

// normalizeMacroSeries 把宏观接口响应转为 quant.macro_series 行。
// 口径：
//   - obs_date 按 spec.DateLayout 解析；月频 YYYYMM 取当月首日；
//   - value 取 spec.Field 列；该列为 nil（未发布/未公布）的行跳过——
//     缺失值折成 0 等于编数据（同 tushare.go 的 P2-10 教训）；
//   - obs_date 解析失败的行跳过（PIT 身份不完整不进库，同
//     normalizeETFPortfolio 先例）；
//   - 缺日期列或数值列（整个响应没有该列）→ 报错，形状不符契约。
func normalizeMacroSeries(resp *TushareResponse, spec macroIndicatorSpec, source string, asOf time.Time) ([]domain.MacroSeries, error) {
	iDate := macroColumnIndex(resp, spec.DateField)
	iVal := macroColumnIndex(resp, spec.Field)
	if iDate < 0 || iVal < 0 {
		return nil, fmt.Errorf("%s 响应缺 %s/%s 列（实际 fields=%v）",
			spec.APIName, spec.DateField, spec.Field, resp.Data.Fields)
	}

	var rows []domain.MacroSeries
	for _, item := range resp.Data.Items {
		obs, ok := parseMacroObsDate(tushareCell(item, iDate), spec.DateLayout)
		if !ok {
			continue // 日期身份不完整，不进库
		}
		if iVal >= len(item) || item[iVal] == nil {
			continue // 数值未发布，不进库（不补 0）
		}
		rows = append(rows, domain.MacroSeries{
			Indicator: spec.Indicator,
			Freq:      spec.Freq,
			ObsDate:   obs,
			Value:     tushareCellFloat(item, iVal),
			Unit:      spec.Unit,
			Source:    source,
			AsOf:      asOf,
		})
	}
	return rows, nil
}

// parseMacroObsDate 解析宏观观测日期：月频 "200601" → 当月首日；
// 日频 "20060102" → 自然日。空串/解析失败 → (零值, false)。
func parseMacroObsDate(s, layout string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
