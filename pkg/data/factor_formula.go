package data

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// ─── D3 公式型内核：叶子/公式因子走 pkg/expression 引擎现算 ────────────────
//
// 与代码型内核（Go 出口）的关系：同一Registry 分派，同一条产出尾部
// （ZScore/PercentileRank → factor_cache）。公式型不物化中间序列——
// 每个计算日按定义现算，定义带版本哈希，复现靠「同定义同快照」。
//
// 对拍承诺（D3 ⑤1 数值回归红线）：公式版与原代码版**同输入同输出**。
// 原版的三个隐含行为在公式侧的复现机制：
//
//   - 输入域过滤（PE>0 / ROE>0 / 价格>0）→ Definition.Validity；
//   - 历史不足（动量不足 20 根 bar）→ ts_delay 窗口不足给 NaN → 缺失；
//   - universe 只含数据来源出现过的股票 → provider 按 InputFields
//     来源表定 universe，混源时并集 + 缺数据 NaN（缺失≠0）。
//
// 缺失≠0：任何 NaN（公式值或有效性值）→ 该股票当日不产出 entry，
// 与「跳过」在产出语义上一致，绝不以 0 顶包。

// marketFieldSet / fundamentalsFieldSet 与 expression fieldRegistry 的
// 来源类别同域（OBS-08 后字段能力由 provider 自报，这里按 InputFields
// 判断该拉哪张表）。
var (
	marketFieldSet = map[string]bool{
		"open": true, "high": true, "low": true, "close": true,
		"volume": true, "turnover": true,
	}
	fundamentalsFieldSet = map[string]bool{
		"pe": true, "pb": true, "ps": true, "roe": true, "roa": true,
		"revenue": true, "profit": true,
	}
)

// factorSeriesProvider 把 FactorStore 的日频数据适配成 expression 的
// DataProvider。一次求值前拉全窗口数据，求值中不再碰 store（无 IO）。
type factorSeriesProvider struct {
	symbols []string
	series  map[string]map[string][]float64 // field → symbol → series（末点=date 当日）
	fields  []string
}

func (p *factorSeriesProvider) GetSymbols() []string { return p.symbols }

func (p *factorSeriesProvider) Fields() []string { return p.fields }

func (p *factorSeriesProvider) GetField(symbol, field string, _ int) ([]float64, error) {
	bySymbol, ok := p.series[field]
	if !ok {
		return nil, fmt.Errorf("field %q not loaded in provider", field)
	}
	series, ok := bySymbol[symbol]
	if !ok {
		// 该 symbol 无此字段数据 → 全 NaN 序列（缺失≠0，不是 error：
		// 混源 universe 下「有行情没财报」是常态）。
		return []float64{math.NaN()}, nil
	}
	return series, nil
}

// loadFactorSeries 按定义的 InputFields 来源拉取窗口数据。
//
// universe 规则：全 market 字段 → OHLCV 出现过的股票；全 fundamentals
// 字段 → 财报快照里的股票；混源 → 并集（缺的那侧由 GetField 给 NaN，
// 产出端即缺失，与原版「各因子只遍历自己来源」一致）。
func loadFactorSeries(ctx context.Context, store FactorStore, def domain.FactorDefinition, date time.Time) (*factorSeriesProvider, error) {
	wantMarket, wantFundamentals := false, false
	for _, f := range def.InputFields {
		switch {
		case marketFieldSet[f]:
			wantMarket = true
		case fundamentalsFieldSet[f]:
			wantFundamentals = true
		default:
			// code/pending 型不走本路径；formula 型字段已在 NewRegistry
			// 校验过 ⊆ 词表，到这里只剩两类来源。
			return nil, fmt.Errorf("factor %q: input field %q has no data source mapping", def.Name, f)
		}
	}
	p := &factorSeriesProvider{
		series: make(map[string]map[string][]float64),
		fields: def.InputFields,
	}
	symbolSet := make(map[string]bool)
	if wantMarket {
		start := date.AddDate(0, 0, -def.Lookback)
		bars, err := store.GetOHLCVForDateRange(ctx, start, date)
		if err != nil {
			return nil, fmt.Errorf("load OHLCV for formula factor %q: %w", def.Name, err)
		}
		// 按 symbol 收集，保持引擎要的「field → symbol → series」形状；
		// 序列按日期升序（末点即 date 当日）。
		bySymbol := make(map[string][]domain.OHLCV)
		for _, b := range bars {
			bySymbol[b.Symbol] = append(bySymbol[b.Symbol], b)
		}
		for field := range marketFieldSet {
			if !containsStr(def.InputFields, field) {
				continue
			}
			fieldSeries := make(map[string][]float64, len(bySymbol))
			for symbol, sb := range bySymbol {
				sort.Slice(sb, func(i, j int) bool { return sb[i].Date.Before(sb[j].Date) })
				vals := make([]float64, len(sb))
				for i, b := range sb {
					vals[i] = fieldValue(b, field)
				}
				fieldSeries[symbol] = vals
				symbolSet[symbol] = true
			}
			p.series[field] = fieldSeries
		}
	}
	if wantFundamentals {
		snap, err := store.GetFundamentalsSnapshot(ctx, date)
		if err != nil {
			return nil, fmt.Errorf("load fundamentals for formula factor %q: %w", def.Name, err)
		}
		for field := range fundamentalsFieldSet {
			if !containsStr(def.InputFields, field) {
				continue
			}
			fieldSeries := make(map[string][]float64, len(snap))
			for _, fd := range snap {
				v, ok := fundamentalFieldValue(fd, field)
				if !ok {
					continue // nil = 缺失，不进序列（该 symbol 此字段无数据）
				}
				fieldSeries[fd.TsCode] = []float64{v}
				symbolSet[fd.TsCode] = true
			}
			p.series[field] = fieldSeries
		}
	}
	for s := range symbolSet {
		p.symbols = append(p.symbols, s)
	}
	sort.Strings(p.symbols)
	return p, nil
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func fieldValue(b domain.OHLCV, field string) float64 {
	switch field {
	case "open":
		return b.Open
	case "high":
		return b.High
	case "low":
		return b.Low
	case "close":
		return b.Close
	case "volume":
		return b.Volume
	case "turnover":
		return b.Turnover
	default:
		return math.NaN()
	}
}

func fundamentalFieldValue(fd domain.FundamentalData, field string) (float64, bool) {
	var p *float64
	switch field {
	case "pe":
		p = fd.PE
	case "pb":
		p = fd.PB
	case "ps":
		p = fd.PS
	case "roe":
		p = fd.ROE
	case "roa":
		p = fd.ROA
	case "revenue":
		p = fd.Revenue
	case "profit":
		p = fd.NetProfit
	default:
		return 0, false
	}
	if p == nil {
		return 0, false // 缺失≠0：nil 不当 0
	}
	return *p, true
}

// computeFormulaRawValues 求值一条 leaf/formula 型定义，返回当日原始值
// （未做横截面标准化）。NaN / 域外（Validity 不通过）/ 无数据 → 不进
// 结果集（缺失≠0）。
func computeFormulaRawValues(ctx context.Context, store FactorStore, def domain.FactorDefinition, date time.Time) (map[string]float64, error) {
	provider, err := loadFactorSeries(ctx, store, def, date)
	if err != nil {
		return nil, err
	}
	evaluator := expression.NewEvaluator(provider)

	formulaExpr, err := expression.NewParser().Parse(def.Formula)
	if err != nil {
		return nil, fmt.Errorf("factor %q: formula does not parse: %w", def.Name, err)
	}
	// 求值期再过一次 L1：注册表加载期已校验，这里防的是「定义对象被
	 // 绕过 NewRegistry 直接构造」的路径。
	if err := formulaExpr.Validate(); err != nil {
		return nil, fmt.Errorf("factor %q: formula fails L1 gate: %w", def.Name, err)
	}
	formulaSeries, err := evaluator.Evaluate(formulaExpr.AST, def.Lookback)
	if err != nil {
		return nil, fmt.Errorf("factor %q: formula evaluation failed: %w", def.Name, err)
	}

	var validitySeries map[string][]float64
	if def.Validity != "" {
		validityExpr, err := expression.NewParser().Parse(def.Validity)
		if err != nil {
			return nil, fmt.Errorf("factor %q: validity does not parse: %w", def.Name, err)
		}
		if err := validityExpr.Validate(); err != nil {
			return nil, fmt.Errorf("factor %q: validity fails L1 gate: %w", def.Name, err)
		}
		validitySeries, err = evaluator.Evaluate(validityExpr.AST, def.Lookback)
		if err != nil {
			return nil, fmt.Errorf("factor %q: validity evaluation failed: %w", def.Name, err)
		}
	}

	raw := make(map[string]float64)
	for _, symbol := range provider.symbols {
		series := formulaSeries[symbol]
		if len(series) == 0 {
			continue
		}
		v := series[len(series)-1] // 末点 = date 当日
		if math.IsNaN(v) {
			continue // 缺失≠0
		}
		if validitySeries != nil {
			vs := validitySeries[symbol]
			if len(vs) == 0 {
				continue
			}
			mask := vs[len(vs)-1]
			if math.IsNaN(mask) || mask == 0 {
				continue // 输入落在数学定义域外 → 缺失，不是 0 分
			}
		}
		raw[symbol] = v
	}
	return raw, nil
}

// saveFormulaFactor 落库公式型因子产出。Citation 不设（nil）——与横截面
// 三因子原版逐字一致：OHLCV/财报快照没有 ingest.raw 批次的 provenance
// 坐标，'[]' 是纵向因子才有意义的「链未建立」显式标记。
func (f *FactorComputer) saveFormulaFactor(ctx context.Context, date time.Time, factor domain.FactorType, rawValues map[string]float64) error {
	if len(rawValues) == 0 {
		f.logger.Warn().
			Time("date", date).
			Str("factor", string(factor)).
			Msg("No factor values computed (formula factor: no symbol passed validity)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	entries := make([]*domain.FactorCacheEntry, 0, len(rawValues))
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: factor,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save %s factor_cache: %w", factor, err)
	}
	f.logger.Info().
		Time("date", date).
		Str("factor", string(factor)).
		Int("stocks", len(entries)).
		Msg("Formula factor computed and cached")
	return nil
}

// ComputeFormulaFactor 按注册表定义现算一个 leaf/formula 型因子。
// 供 registry 驱动的批处理与 handler 按名调用。
func (f *FactorComputer) ComputeFormulaFactor(ctx context.Context, def domain.FactorDefinition, date time.Time) error {
	switch def.Kernel {
	case domain.KernelLeaf, domain.KernelFormula:
	default:
		return fmt.Errorf("factor %q: kernel %s is not a formula kernel", def.Name, def.Kernel)
	}
	raw, err := computeFormulaRawValues(ctx, f.store, def, date)
	if err != nil {
		return err
	}
	return f.saveFormulaFactor(ctx, date, def.Name, raw)
}
