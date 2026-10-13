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
//     挂了」伪装成「导入了 0 行」（破坏验证②的 Red 腿）。P2-1-R2 起同一
//     纪律覆盖「响应有行但全被跳过」：跳过计数随 normalize 返回，全跳过
//     fail-loud（macroAllSkippedError），部分跳过留 Info 日志。
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
	"reflect"
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
// R2（P2-1-R2）补强：某次响应**有行但全部被跳过**（坏日期 / 值列全
// nil）同样报错——「0 行」只有在**上游确实没返回任何行**时才是合法答案
// （见 macroAllSkippedError）。
func (c *TushareClient) FetchMacroIndicator(ctx context.Context, indicator string) ([]domain.MacroSeries, error) {
	spec, ok := macroIndicatorSpecs[indicator]
	if !ok {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			fmt.Sprintf("未知宏观指标 %q（目录：%v）", indicator, MacroIndicators()))
	}

	now := time.Now().UTC() // R6：as_of 与窗口计算共用同一时间点
	asOf := now
	windows, err := macroWindowsOrErr(spec, now)
	if err != nil {
		return nil, err
	}
	var all []domain.MacroSeries
	for _, params := range windows {
		resp, err := c.call(ctx, spec.APIName, params, spec.Fields)
		if err != nil {
			return nil, err
		}
		res, err := normalizeMacroSeries(resp, spec, MacroSourceTushare, asOf)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
				fmt.Sprintf("%s 响应形状不符合契约（缺 %s/%s 列）", spec.APIName, spec.DateField, spec.Field),
				"data.FetchMacroIndicator")
		}
		if err := macroAllSkippedError(spec.APIName, spec.Indicator, len(resp.Data.Items), res); err != nil {
			return nil, err
		}
		logMacroSkipped(c, spec.Indicator, spec.APIName, res)
		all = append(all, res.Rows...)
	}

	if len(all) == 0 {
		// 所有响应都是真空（items==0）——上游确实没返回任何行，合法。
		// （「有行但全被跳过」已在循环内 fail-loud。）
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
// 分组键是 APIName。组不变量（fields / 日期列 / 日期格式 / 固定参数 /
// 分段窗口）必须逐项一致（R3）——用 specs[0].Fields 请求来的响应要能
// 喂给组内每个 spec。不一致时报错点名「指标目录配置不自洽」：那是静态
// 目录 bug，不许静默退化逐条拉（对 shibor_lpr 这类流控接口，退化本身
// 就是故障；且错误会伪装成「响应形状不符契约」，把根因埋掉）。
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

	now := time.Now().UTC() // R6：as_of 与全部分组窗口共用同一时间点
	asOf := now
	var all []domain.MacroSeries
	for _, api := range order {
		specs := groups[api]
		if len(specs) > 1 {
			// R3：共用一次响应前先验组不变量。不一致 = 目录配置不自洽
			// （静态 bug）→ 报错点名，不退化、不伪装成上游过错。
			if share, reason := specsCanShareResponse(specs, now); !share {
				return nil, apperrors.New(apperrors.ErrCodeInternal, fmt.Sprintf(
					"宏观指标目录配置不自洽：同接口 %s 的 %v 无法共用一次响应（%s）——修正 pkg/data/macro.go 的 macroIndicatorSpecs，不要改这里的错误信息",
					api, apiIndicatorNames(specs), reason))
			}
		}

		windows, err := macroWindowsOrErr(specs[0], now)
		if err != nil {
			return nil, err
		}
		var batch []domain.MacroSeries
		for _, params := range windows {
			resp, err := c.call(ctx, api, params, specs[0].Fields)
			if err != nil {
				return nil, err
			}
			for _, spec := range specs {
				res, err := normalizeMacroSeries(resp, spec, MacroSourceTushare, asOf)
				if err != nil {
					return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
						fmt.Sprintf("%s 响应形状不符合契约（缺 %s/%s 列）", api, spec.DateField, spec.Field),
						"data.FetchMacroIndicators")
				}
				if err := macroAllSkippedError(api, spec.Indicator, len(resp.Data.Items), res); err != nil {
					return nil, err
				}
				logMacroSkipped(c, spec.Indicator, api, res)
				batch = append(batch, res.Rows...)
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

// specsCanShareResponse 报告一组同接口 spec 能否共用一次响应（R3）。
// 除窗口序列一致（sameWindows）外，组不变量必须逐项一致——用 specs[0]
// 的 fields 请求来的响应，要能喂给组内每一个 spec：
//   - Fields：响应列形状（列不同 → 另一 spec 会报「缺列」，错误信息误导）；
//   - DateField / DateLayout：obs_date 的解析口径；
//   - Params：接口级固定参数（如 fx_daily 的 ts_code，不同标的不能同拉）。
//
// 不一致返回 (false, 原因)。这是静态目录配置 bug（不是运行时上游变故）：
// 调用方报错点名「目录配置不自洽」，不许静默退化逐条拉。
func specsCanShareResponse(specs []macroIndicatorSpec, now time.Time) (bool, string) {
	base := specs[0]
	for _, spec := range specs[1:] {
		switch {
		case spec.Fields != base.Fields:
			return false, fmt.Sprintf("%s 与 %s 的 fields 不同（%q vs %q）",
				base.Indicator, spec.Indicator, base.Fields, spec.Fields)
		case spec.DateField != base.DateField:
			return false, fmt.Sprintf("%s 与 %s 的日期列不同（%q vs %q）",
				base.Indicator, spec.Indicator, base.DateField, spec.DateField)
		case spec.DateLayout != base.DateLayout:
			return false, fmt.Sprintf("%s 与 %s 的日期格式不同（%q vs %q）",
				base.Indicator, spec.Indicator, base.DateLayout, spec.DateLayout)
		case !equalMacroParams(spec.Params, base.Params):
			return false, fmt.Sprintf("%s 与 %s 的固定参数不同（%v vs %v）",
				base.Indicator, spec.Indicator, base.Params, spec.Params)
		}
	}
	if !sameWindows(specs, now) {
		return false, fmt.Sprintf("%v 的分段窗口序列不一致（检查 SinceYear）",
			apiIndicatorNames(specs))
	}
	return true, ""
}

// equalMacroParams 报告两组接口级固定参数是否相等（nil 与空 map 等值）。
func equalMacroParams(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		// P2-1-R3 R2 · Params 的 value 是 interface{}：用 != 比较非可比较类型
		// （slice/map/func）会 runtime panic（"comparing uncomparable type"），
		// 绕过 apperrors 链路。DeepEqual 对非标量安全且能判不等。
		if !ok || !reflect.DeepEqual(bv, av) {
			return false
		}
	}
	return true
}

// sameWindows 报告一组 spec 的分段窗口序列是否一致（能否共用一次响应）。
// now 由调用方注入（R6）：同一次导入内 as_of 与窗口计算共用一个时间点，
// 跨年边界瞬间不再存在「两次 time.Now 结果不同」的缝隙。
func sameWindows(specs []macroIndicatorSpec, now time.Time) bool {
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
				// P2-1-R3 R2 · 同 equalMacroParams：窗口参数值也是 interface{}，
				// 用 != 比较非标量会 panic。DeepEqual 安全。
				if ov, ok := other[i][k]; !ok || !reflect.DeepEqual(ov, v) {
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
// 发首窗口一次 call，成功即返回 nil。不 normalize、不落库——探测不应
// 产生数据副作用。
//
// R1（P2-1-R2）：只发**第一个**窗口。探测的语义是连通性，不按数据量
// 放大——此前遍历全部窗口会让 USDCNY_CLOSE（2019–2026 分 8 段）每次
// 探测发 8 次请求，白吃 fx_daily（2000 积分档）配额。同接口同鉴权下，
// 单一窗口的成败足以判定连通性。
func (c *TushareClient) ProbeMacro(ctx context.Context, indicator string) error {
	spec, ok := macroIndicatorSpecs[indicator]
	if !ok {
		return apperrors.New(apperrors.ErrCodeInvalidInput,
			fmt.Sprintf("未知宏观指标 %q（目录：%v）", indicator, MacroIndicators()))
	}
	windows, err := macroWindowsOrErr(spec, time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = c.call(ctx, spec.APIName, windows[0], spec.Fields)
	return err
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

// macroWindowsOrErr 取 spec 的请求窗口；空序列是目录配置错误（SinceYear
// 晚于当前年份——导入与探测都会静默什么都不做），fail-loud。
func macroWindowsOrErr(spec macroIndicatorSpec, now time.Time) ([]map[string]interface{}, error) {
	windows := macroFetchWindows(spec, now)
	if len(windows) == 0 {
		return nil, apperrors.New(apperrors.ErrCodeInternal, fmt.Sprintf(
			"宏观指标 %s 无可用请求窗口（SinceYear=%d 晚于当前年份 %d，目录配置不自洽）",
			spec.Indicator, spec.SinceYear, now.Year()))
	}
	return windows, nil
}

// ── normalize（纯函数：TushareResponse → domain 行） ─────────────────────

// normalizedMacroRows 是 normalizeMacroSeries 的产出：Rows 为可入库行，
// 两个跳过计数解释「为什么少了行」——「坏日期」（上游改格式/空日期）与
// 「数值 nil」（未发布）是两种不同变故，诊断时不能混（R2，P2-1-R2）。
type normalizedMacroRows struct {
	Rows            []domain.MacroSeries
	SkippedBadDate  int // obs_date 解析失败（PIT 身份不完整）
	SkippedNilValue int // 数值列 nil / 行缺列（未发布，不补 0）
	SkippedBadValue int // 数值列非 nil 但不可解析/非有限（形状变故，P2-1-R3 R1）
}

// Skipped 是总跳过行数（坏日期 + 数值 nil + 坏值）。
func (n normalizedMacroRows) Skipped() int {
	return n.SkippedBadDate + n.SkippedNilValue + n.SkippedBadValue
}

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
//   - 该列**非 nil 但不可解析/非有限**（"N/A" / "13,42" / "--" / bool / 数组）
//     的行同样跳过并单独计数（P2-1-R3 R1）——静默折 0 与上一条同罪；
//   - obs_date 解析失败的行跳过（PIT 身份不完整不进库，同
//     normalizeETFPortfolio 先例）；
//   - 缺日期列或数值列（整个响应没有该列）→ 报错，形状不符契约；
//   - 跳过行数随返回值计数（R2）：调用方在「有行但全被跳过」时
//     fail-loud，部分跳过留 Info 日志。
func normalizeMacroSeries(resp *TushareResponse, spec macroIndicatorSpec, source string, asOf time.Time) (normalizedMacroRows, error) {
	iDate := macroColumnIndex(resp, spec.DateField)
	iVal := macroColumnIndex(resp, spec.Field)
	if iDate < 0 || iVal < 0 {
		return normalizedMacroRows{}, fmt.Errorf("%s 响应缺 %s/%s 列（实际 fields=%v）",
			spec.APIName, spec.DateField, spec.Field, resp.Data.Fields)
	}

	var out normalizedMacroRows
	for _, item := range resp.Data.Items {
		obs, ok := parseMacroObsDate(tushareCell(item, iDate), spec.DateLayout)
		if !ok {
			out.SkippedBadDate++
			continue // 日期身份不完整，不进库
		}
		if iVal >= len(item) || item[iVal] == nil {
			out.SkippedNilValue++
			continue // 数值未发布，不进库（不补 0）
		}
		value, ok := tushareFloatOK(item[iVal])
		if !ok {
			// P2-1-R3 R1：非 nil 但不可解析/非有限（上游把 value 列从 JSON
			// number 改成 "N/A" / "13,42" / "--" 或非标量）——静默折 0 等于
			// 编数据（与本函数注释自述一致）。计入跳过，不落库，不进 Value。
			out.SkippedBadValue++
			continue
		}
		out.Rows = append(out.Rows, domain.MacroSeries{
			Indicator: spec.Indicator,
			Freq:      spec.Freq,
			ObsDate:   obs,
			Value:     value,
			Unit:      spec.Unit,
			Source:    source,
			AsOf:      asOf,
		})
	}
	return out, nil
}

// macroAllSkippedError 是 R2 的 fail-loud 闸门：一次响应**有行**却一行
// 都没能入库（全坏日期 / 值列全 nil）时拒绝静默成功——这是上游变故的
// 典型形态（改格式、列整体变空），不是「导入 0 行」的合法场景。真空
// 响应（items==0）不在此列：那是上游确实没有数据。
func macroAllSkippedError(api, indicator string, items int, res normalizedMacroRows) error {
	if items == 0 || len(res.Rows) > 0 {
		return nil
	}
	return apperrors.New(apperrors.ErrCodeDataQuality, fmt.Sprintf(
		"宏观指标 %s：%s 响应返回 %d 行但全部被跳过（坏日期 %d / 数值 nil %d / 坏值 %d）——上游形状/格式变故，拒绝以「导入 0 行」伪装成功",
		indicator, api, items, res.SkippedBadDate, res.SkippedNilValue, res.SkippedBadValue))
}

// logMacroSkipped 在 normalize 跳过了行时记一条 Info 日志（R2）：部分
// 跳过本身可能是合法状态（尾行未发布），但跳过率异常是上游变故的前兆，
// 必须留痕以便审计。
func logMacroSkipped(c *TushareClient, indicator, api string, res normalizedMacroRows) {
	if res.Skipped() == 0 {
		return
	}
	c.logger.Info().Str("indicator", indicator).Str("api", api).
		Int("rows", len(res.Rows)).
		Int("skipped_bad_date", res.SkippedBadDate).
		Int("skipped_nil_value", res.SkippedNilValue).
		Int("skipped_bad_value", res.SkippedBadValue).
		Msg("macro rows skipped during normalize")
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
