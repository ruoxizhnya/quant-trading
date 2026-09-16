package data

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data/equitydeep"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// ─ 桥 B1 纵向基本面因子 (ADR-022 计算面, TASKS.md EQD-P1-2) ─────────────────
//
// The five factors in this file read the same source as the cross-sectional
// factors in factor.go — fundamentals_detail rows — and end in the same place
// (a cross-sectional z-score in factor_cache), but they differ in one decisive
// way: they are **vertical** (one stock across N quarters), so each factor is
// computed per symbol by walking that symbol's own reporting history.
//
// Two conventions come from the frozen contracts and are deliberately not
// re-derived here:
//
//   - PIT (contracts/fundamentals_detail.schema.sql): only rows announced on
//     or before the evaluation date may be read. The filter lives in
//     storage.GetFundamentalsDetailAsOf, so nothing in this file may substitute
//     end_date for it.
//   - statement basis (contracts/field_dictionary.yaml): income-statement and
//     cash-flow items are **cumulative within the fiscal year** (Q1 = 3 months,
//     Q2 = 6 months, … Q4 = the full year), while balance-sheet items are
//     point-in-time. Every period conversion below follows from that split.
//
// A stock that cannot support a factor (too short a history, a missing bridge
// leg, a non-positive denominator) is skipped rather than approximated: a
// substitute value would mix fiscal semantics into the factor and be
// indistinguishable from a real signal downstream.

// grossMarginTrendQuarters is the window RESEARCH.md §3.4 prescribes for
// gross_margin_trend ("毛利率 4 季线性斜率").
const grossMarginTrendQuarters = 4

// reportPeriod is a fiscal period coordinate. The fiscal year is the calendar
// year (A-share convention) and contract end_date is always a quarter end, so
// (year, quarter) is derivable from end_date alone.
type reportPeriod struct {
	year    int
	quarter int
}

// order is a total order over reportPeriod, so a larger value is always a later
// period regardless of year. It is also directly invertible (see shiftQuarters).
func (p reportPeriod) order() int { return p.year*4 + p.quarter - 1 }

// periodOf derives the fiscal period of a report end date. A month that is not
// a quarter end is rejected rather than rounded: a producer emitting monthly
// rows would otherwise contribute partial quarters to a TTM sum without any
// visible trace.
func periodOf(endDate time.Time) (reportPeriod, bool) {
	switch endDate.Month() {
	case time.March:
		return reportPeriod{year: endDate.Year(), quarter: 1}, true
	case time.June:
		return reportPeriod{year: endDate.Year(), quarter: 2}, true
	case time.September:
		return reportPeriod{year: endDate.Year(), quarter: 3}, true
	case time.December:
		return reportPeriod{year: endDate.Year(), quarter: 4}, true
	default:
		return reportPeriod{}, false
	}
}

// shiftQuarters moves a period by delta quarters, normalizing the coordinate
// (Q1 minus one quarter is Q4 of the previous year).
func shiftQuarters(p reportPeriod, delta int) reportPeriod {
	idx := p.order() + delta
	return reportPeriod{year: idx / 4, quarter: idx%4 + 1}
}

// statementField holds one stock's readings for one field code, keyed by fiscal
// period. Values are already normalized to the dictionary base unit by the
// ingest pipeline (contract C1-a unit_scale), so no unit handling happens here.
//
// annDate records which announcement supplied each value: when a period has
// been restated, the later announcement is the one the market saw, and it is
// still PIT-safe as long as it is <= the evaluation date (enforced by the
// storage query, not by this type).
//
// provenance records, for each surviving period, the ingest.raw batch
// (content hash) the reading came from, following the same winner rule as
// annDate. It is the per-period coordinate the JSON Pointer layer refines to
// later; the per-symbol batch set written into factor_cache is its union.
type statementField struct {
	values     map[reportPeriod]float64
	annDate    map[reportPeriod]time.Time
	provenance map[reportPeriod]string
}

func (f statementField) valueAt(p reportPeriod) (float64, bool) {
	v, ok := f.values[p]
	return v, ok
}

// latest returns the most recent period that has a reading.
func (f statementField) latest() (reportPeriod, bool) {
	var best reportPeriod
	found := false
	for p := range f.values {
		if !found || p.order() > best.order() {
			best, found = p, true
		}
	}
	return best, found
}

// ttm returns the trailing-twelve-month value of a cumulative field at period p.
// Because source values are year-to-date, the annual reading is simply Q4 and
// any other quarter bridges across the year boundary:
//
//	TTM(Qn, Y) = YTD(Qn, Y) + YTD(Q4, Y-1) − YTD(Qn, Y-1)
//
// A missing bridge leg yields ok=false; the factor is then skipped for that
// symbol rather than approximated, because a substitute (doubling a half-year,
// say) would silently change what the number means.
func (f statementField) ttm(p reportPeriod) (float64, bool) {
	current, ok := f.valueAt(p)
	if !ok {
		return 0, false
	}
	if p.quarter == 4 {
		return current, true
	}
	priorYearAnnual, ok := f.valueAt(reportPeriod{year: p.year - 1, quarter: 4})
	if !ok {
		return 0, false
	}
	priorYearSame, ok := f.valueAt(reportPeriod{year: p.year - 1, quarter: p.quarter})
	if !ok {
		return 0, false
	}
	return current + priorYearAnnual - priorYearSame, true
}

// singleQuarter returns the value attributable to one quarter, derived from the
// cumulative readings by differencing against the immediately preceding quarter
// of the same fiscal year. Q1 needs no differencing; a missing predecessor means
// the quarter cannot be derived.
func (f statementField) singleQuarter(p reportPeriod) (float64, bool) {
	current, ok := f.valueAt(p)
	if !ok {
		return 0, false
	}
	if p.quarter == 1 {
		return current, true
	}
	prev, ok := f.valueAt(reportPeriod{year: p.year, quarter: p.quarter - 1})
	if !ok {
		return 0, false
	}
	return current - prev, true
}

// latestValue returns the most recent reading of a point-in-time field.
func latestValue(field statementField) (float64, bool) {
	p, ok := field.latest()
	if !ok {
		return 0, false
	}
	return field.valueAt(p)
}

// latestTTM returns the trailing-twelve-month value of a cumulative field at its
// most recent period.
func latestTTM(field statementField) (float64, bool) {
	p, ok := field.latest()
	if !ok {
		return 0, false
	}
	return field.ttm(p)
}

// statementBook indexes PIT-filtered readings by symbol, then by field code:
// book.fields["600519.SH"][equitydeep.FieldEquityAttr]. book.hashes carries,
// per symbol, the deduplicated content hashes of the ingest.raw batches that
// supplied the symbol's surviving readings — the batch coordinates written
// into factor_cache.citation (ODR-061 切片 C2).
type statementBook struct {
	fields map[string]map[string]statementField
	hashes map[string][]string
}

// snapshotURIPrefix marks a fundamentals_detail row whose snapshot is archived
// in ingest.raw; the remainder of the URI is the batch's content hash.
const snapshotURIPrefix = "ingest.raw:"

// contentHashOfSnapshot extracts the ingest.raw content hash from a
// snapshot_uri of the form "ingest.raw:<64hex>". ok=false for any other URI
// form: such a row simply carries no provenance — the prefix is never guessed
// around, and no coordinate is invented.
func contentHashOfSnapshot(uri string) (string, bool) {
	hash, ok := strings.CutPrefix(uri, snapshotURIPrefix)
	if !ok || hash == "" {
		return "", false
	}
	return hash, true
}

// citationJSON marshals content hashes into the stored citation form,
// [{"content_hash":"<64hex>"}...], sorted for determinism. An empty set
// marshals as [] — the explicit "chain not established" marker, never null.
func citationJSON(hashes []string) json.RawMessage {
	if len(hashes) == 0 {
		return json.RawMessage("[]")
	}
	refs := make([]struct {
		ContentHash string `json:"content_hash"`
	}, len(hashes))
	for i, hash := range hashes {
		refs[i].ContentHash = hash
	}
	b, err := json.Marshal(refs)
	if err != nil {
		// Unreachable for a slice of string-only structs; degrade to the
		// explicit empty marker rather than emitting a malformed citation.
		return json.RawMessage("[]")
	}
	return json.RawMessage(b)
}

// loadStatementBook reads the given field codes as of asOf and indexes them.
//
// Restatements produce several rows for one (symbol, period, field) — one per
// announcement — and only the latest announcement may be used, so the builder
// keeps the row with the greater ann_date. Rows without a value are dropped: a
// source-side missing reading must not be read as zero, since every factor
// below divides or differences its inputs.
//
// Provenance follows the same winner rule: a surviving reading carries its
// batch hash only when its snapshot_uri points at ingest.raw. A restatement
// from a non-archived source supersedes the earlier batch — the reading's hash
// is dropped rather than kept, so the citation never names a batch whose value
// is no longer in use.
func (f *FactorComputer) loadStatementBook(ctx context.Context, asOf time.Time, fieldCodes ...string) (statementBook, error) {
	rows, err := f.store.GetFundamentalsDetailAsOf(ctx, fieldCodes, asOf)
	if err != nil {
		return statementBook{}, fmt.Errorf("load fundamentals_detail: %w", err)
	}
	book := statementBook{
		fields: make(map[string]map[string]statementField, len(rows)),
		hashes: make(map[string][]string),
	}
	for _, r := range rows {
		if r.Value == nil {
			continue
		}
		period, ok := periodOf(r.EndDate)
		if !ok {
			continue
		}
		fields := book.fields[r.TsCode]
		if fields == nil {
			fields = make(map[string]statementField)
			book.fields[r.TsCode] = fields
		}
		field := fields[r.FieldCode]
		if field.values == nil {
			field.values = make(map[reportPeriod]float64)
			field.annDate = make(map[reportPeriod]time.Time)
			field.provenance = make(map[reportPeriod]string)
		}
		if prev, seen := field.annDate[period]; seen && !r.AnnDate.After(prev) {
			continue
		}
		field.values[period] = *r.Value
		field.annDate[period] = r.AnnDate
		if hash, ok := contentHashOfSnapshot(r.SnapshotURI); ok {
			field.provenance[period] = hash
		} else {
			delete(field.provenance, period)
		}
		fields[r.FieldCode] = field
	}
	// Union the surviving readings' batch hashes per symbol, deduplicated and
	// sorted: wider than the minimal set (not every surviving period is
	// consumed by a formula), but no coordinate is invented (ODR-061 §5).
	for symbol, fields := range book.fields {
		seen := make(map[string]bool)
		var hashes []string
		for _, field := range fields {
			for _, hash := range field.provenance {
				if !seen[hash] {
					seen[hash] = true
					hashes = append(hashes, hash)
				}
			}
		}
		if len(hashes) > 0 {
			sort.Strings(hashes)
			book.hashes[symbol] = hashes
		}
	}
	return book, nil
}

// saveVerticalFactor cross-sectionally normalizes one vertical factor's raw
// values and persists them. The pipeline tail is identical for all five factors
// and identical in behaviour to the three cross-sectional factors in factor.go,
// so it is kept in one place rather than copied five times.
//
// sourceHashes maps symbol → the ingest.raw batches its readings came from and
// is the only provenance this pipeline writes (ODR-061 切片 C2); symbols
// without a hash set cite '[]'.
func (f *FactorComputer) saveVerticalFactor(ctx context.Context, date time.Time, factor domain.FactorType, sourceHashes map[string][]string, rawValues map[string]float64) error {
	if len(rawValues) == 0 {
		f.logger.Warn().
			Time("date", date).
			Str("factor", string(factor)).
			Msg("No factor values computed (insufficient fundamentals_detail history)")
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
			Citation:   citationJSON(sourceHashes[symbol]),
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save %s factor_cache: %w", factor, err)
	}
	f.logger.Info().
		Time("date", date).
		Str("factor", string(factor)).
		Int("stocks", len(entries)).
		Msg("Vertical factor computed and cached")
	return nil
}

// ComputeGrossMarginTrendFactor computes the 4-quarter slope of single-quarter
// gross margin, per stock:
//
//	margin(q) = (revenue(q) − cost(q)) / revenue(q)     ← single quarter, not YTD
//	factor    = Slope([margin(q-3) … margin(q)])        ← margin points / quarter
//
// The slope is taken over single-quarter margins on purpose: since revenue and
// cost are cumulative, differencing first is what makes the four readings
// comparable — a slope over YTD margins would mostly measure the passage of the
// fiscal year rather than a change in profitability.
//
// A positive value means margin expansion. Stocks without four consecutive
// quarters (or with a non-positive quarterly revenue) are skipped: that is a
// data-availability outcome, not an error.
func (f *FactorComputer) ComputeGrossMarginTrendFactor(ctx context.Context, date time.Time) error {
	book, err := f.loadStatementBook(ctx, date, equitydeep.FieldTotalRevenue, equitydeep.FieldOperatingCost)
	if err != nil {
		return fmt.Errorf("load fundamentals_detail for gross margin trend: %w", err)
	}
	rawValues := make(map[string]float64)
	for symbol, fields := range book.fields {
		margins, ok := grossMarginQuarters(
			fields[equitydeep.FieldTotalRevenue],
			fields[equitydeep.FieldOperatingCost],
			grossMarginTrendQuarters,
		)
		if !ok {
			continue
		}
		rawValues[symbol] = statistics.Slope(margins)
	}
	return f.saveVerticalFactor(ctx, date, domain.FactorGrossMarginTrend, book.hashes, rawValues)
}

// grossMarginQuarters returns the single-quarter gross margins of the n
// consecutive quarters ending at the latest available revenue period, oldest
// first. ok=false means the stock cannot support the factor: no revenue at all,
// a quarter whose revenue is missing or non-positive (the margin would be
// undefined or sign-flipped), or a gap in the differencing chain.
func grossMarginQuarters(revenue, cost statementField, n int) ([]float64, bool) {
	latest, ok := revenue.latest()
	if !ok {
		return nil, false
	}
	// Walk backwards from the latest period so a stock that stopped reporting
	// still gets its most recent window.
	margins := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		period := shiftQuarters(latest, -i)
		rev, ok := revenue.singleQuarter(period)
		if !ok || rev <= 0 {
			return nil, false
		}
		costOfSales, ok := cost.singleQuarter(period)
		if !ok {
			return nil, false
		}
		margins = append(margins, (rev-costOfSales)/rev)
	}
	// Collected newest→oldest; statistics.Slope is direction-sensitive.
	for i, j := 0, len(margins)-1; i < j; i, j = i+1, j-1 {
		margins[i], margins[j] = margins[j], margins[i]
	}
	return margins, true
}

// ComputeContractLiabilityRatioFactor computes 合同负债 / 营业总收入(TTM) per stock.
//
// Contract liabilities are money customers have already paid, so the ratio is a
// direct read on channel power (RESEARCH.md §3.4, 疑点 Q1): a high ratio means
// the company is financed by its customers rather than financing them.
//
// The numerator is the latest balance-sheet reading and the denominator the TTM
// revenue, both as of the same evaluation date and both PIT-filtered upstream.
// A non-positive TTM revenue is skipped, since it would make the ratio's sign a
// statement about the denominator rather than about channel power.
func (f *FactorComputer) ComputeContractLiabilityRatioFactor(ctx context.Context, date time.Time) error {
	book, err := f.loadStatementBook(ctx, date, equitydeep.FieldContractLiability, equitydeep.FieldTotalRevenue)
	if err != nil {
		return fmt.Errorf("load fundamentals_detail for contract liability ratio: %w", err)
	}
	rawValues := make(map[string]float64)
	for symbol, fields := range book.fields {
		liability, ok := latestValue(fields[equitydeep.FieldContractLiability])
		if !ok {
			continue
		}
		revenue, ok := latestTTM(fields[equitydeep.FieldTotalRevenue])
		if !ok || revenue <= 0 {
			continue
		}
		rawValues[symbol] = liability / revenue
	}
	return f.saveVerticalFactor(ctx, date, domain.FactorContractLiabilityRatio, book.hashes, rawValues)
}

// ComputeOCFToNetProfitFactor computes 经营现金流(TTM) / 归母净利润(TTM) per stock —
// the cash-conversion quality of reported earnings (RESEARCH.md §3.4).
//
// Both legs are cumulative (cash-flow statement and income statement), so both
// are converted to TTM before dividing. A non-positive TTM net profit is
// skipped rather than inverted: the ratio's sign would then be driven by the
// denominator, which makes a cross-sectional z-score meaningless — the same
// reason the PE-based value factor requires PE > 0.
func (f *FactorComputer) ComputeOCFToNetProfitFactor(ctx context.Context, date time.Time) error {
	book, err := f.loadStatementBook(ctx, date, equitydeep.FieldOCFNet, equitydeep.FieldNetProfitAttr)
	if err != nil {
		return fmt.Errorf("load fundamentals_detail for ocf to net profit: %w", err)
	}
	rawValues := make(map[string]float64)
	for symbol, fields := range book.fields {
		ocf, ok := latestTTM(fields[equitydeep.FieldOCFNet])
		if !ok {
			continue
		}
		netProfit, ok := latestTTM(fields[equitydeep.FieldNetProfitAttr])
		if !ok || netProfit <= 0 {
			continue
		}
		rawValues[symbol] = ocf / netProfit
	}
	return f.saveVerticalFactor(ctx, date, domain.FactorOCFToNetProfit, book.hashes, rawValues)
}

// ComputeROEDuPontLeverageFactor computes the equity multiplier of the DuPont
// decomposition, 总资产 / 归母净资产, from the latest balance sheet as of date.
//
// It is the leverage leg of ROE = margin × turnover × leverage, kept as its own
// factor so that a levered ROE can be told apart from an efficient one
// (RESEARCH.md §3.4). Both inputs are point-in-time, so no TTM conversion
// applies. A non-positive book equity (an insolvent balance sheet) flips the
// sign and is therefore skipped.
func (f *FactorComputer) ComputeROEDuPontLeverageFactor(ctx context.Context, date time.Time) error {
	book, err := f.loadStatementBook(ctx, date, equitydeep.FieldTotalAssets, equitydeep.FieldEquityAttr)
	if err != nil {
		return fmt.Errorf("load fundamentals_detail for roe dupont leverage: %w", err)
	}
	rawValues := make(map[string]float64)
	for symbol, fields := range book.fields {
		totalAssets, ok := latestValue(fields[equitydeep.FieldTotalAssets])
		if !ok {
			continue
		}
		equity, ok := latestValue(fields[equitydeep.FieldEquityAttr])
		if !ok || equity <= 0 {
			continue
		}
		rawValues[symbol] = totalAssets / equity
	}
	return f.saveVerticalFactor(ctx, date, domain.FactorROEDuPontLeverage, book.hashes, rawValues)
}

// ComputeInventoryTurnoverDeltaFactor computes the year-over-year change in
// inventory turnover per stock:
//
//	turnover(p) = 营业成本(TTM at p) / 存货(p)
//	factor      = turnover(current) − turnover(current − 4 quarters)
//
// Inventory is a point-in-time balance and cost of sales is cumulative, hence
// the TTM leg. The delta form (rather than the level) is what RESEARCH.md §3.4
// asks for: it isolates an abnormal inventory build-up from a company's
// structural turnover level (疑点 Q2).
//
// Turnover is measured against the period-end balance rather than a two-point
// average: the contract stores one reading per period, and averaging would need
// an opening balance that the first period of a history does not have. A
// non-positive inventory would flip the sign and is skipped.
func (f *FactorComputer) ComputeInventoryTurnoverDeltaFactor(ctx context.Context, date time.Time) error {
	book, err := f.loadStatementBook(ctx, date, equitydeep.FieldInventory, equitydeep.FieldOperatingCost)
	if err != nil {
		return fmt.Errorf("load fundamentals_detail for inventory turnover delta: %w", err)
	}
	rawValues := make(map[string]float64)
	for symbol, fields := range book.fields {
		inventory, cost := fields[equitydeep.FieldInventory], fields[equitydeep.FieldOperatingCost]
		current, ok := inventoryTurnover(inventory, cost, 0)
		if !ok {
			continue
		}
		yearAgo, ok := inventoryTurnover(inventory, cost, -4)
		if !ok {
			continue
		}
		rawValues[symbol] = current - yearAgo
	}
	return f.saveVerticalFactor(ctx, date, domain.FactorInventoryTurnoverDelta, book.hashes, rawValues)
}

// inventoryTurnover returns the inventory turnover of one period, `offset`
// quarters away from the latest inventory reading (0 = that reading).
// ok=false means the period's inventory or TTM cost is missing, or the
// inventory is non-positive.
func inventoryTurnover(inventory, cost statementField, offset int) (float64, bool) {
	latest, ok := inventory.latest()
	if !ok {
		return 0, false
	}
	period := shiftQuarters(latest, offset)
	inventoryValue, ok := inventory.valueAt(period)
	if !ok || inventoryValue <= 0 {
		return 0, false
	}
	costTTM, ok := cost.ttm(period)
	if !ok {
		return 0, false
	}
	return costTTM / inventoryValue, true
}
