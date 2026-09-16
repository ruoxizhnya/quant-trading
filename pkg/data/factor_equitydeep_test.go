package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data/equitydeep"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Tests for the 桥 B1 vertical fundamentals factors (factor_equitydeep.go).
//
// The arithmetic is asserted against hand-computable fixtures rather than by
// re-running the implementation's own formula: a test that recomputes the
// formula agrees with the code even when both are wrong. Every expected value
// below is therefore derived in a comment next to the fixture it comes from.

const eqdTol = 1e-9

var _ FactorStore = (*eqdMockStore)(nil)

// fixture helpers -----------------------------------------------------------

func eqdQuarterEnd(year, quarter int) time.Time {
	return time.Date(year, time.Month(quarter*3), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1)
}

func eqdPeriod(year, quarter int) reportPeriod {
	return reportPeriod{year: year, quarter: quarter}
}

func eqdPtr(v float64) *float64 { return &v }

func eqdAssertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > eqdTol {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// eqdField builds a statementField from period -> value pairs, stamping each
// reading with an announcement date one month after the period end.
func eqdField(values map[reportPeriod]float64) statementField {
	f := statementField{
		values:  make(map[reportPeriod]float64, len(values)),
		annDate: make(map[reportPeriod]time.Time, len(values)),
	}
	for p, v := range values {
		f.values[p] = v
		f.annDate[p] = eqdQuarterEnd(p.year, p.quarter).AddDate(0, 0, 30)
	}
	return f
}

// eqdRow builds one fundamentals_detail row, announced a month after the
// period end.
func eqdRow(tsCode string, p reportPeriod, fieldCode string, value float64) domain.FundamentalsDetailRow {
	return eqdRowAnnounced(tsCode, p, fieldCode, value, eqdQuarterEnd(p.year, p.quarter).AddDate(0, 0, 30))
}

func eqdRowAnnounced(tsCode string, p reportPeriod, fieldCode string, value float64, annDate time.Time) domain.FundamentalsDetailRow {
	return domain.FundamentalsDetailRow{
		TsCode:       tsCode,
		EndDate:      eqdQuarterEnd(p.year, p.quarter),
		AnnDate:      annDate,
		FieldCode:    fieldCode,
		RawFieldName: fieldCode,
		Value:        eqdPtr(value),
		Unit:         "CNY_100m",
		Source:       "equitydeep:test",
		FetchedAt:    annDate,
		SnapshotURI:  "file:///fixtures/snapshot.json",
	}
}

// eqdFixtureQuarters is 2023Q1..2024Q4: two fiscal years, the shortest history
// that exercises both the TTM bridge and a year-over-year delta.
var eqdFixtureQuarters = []reportPeriod{
	{2023, 1}, {2023, 2}, {2023, 3}, {2023, 4},
	{2024, 1}, {2024, 2}, {2024, 3}, {2024, 4},
}

const (
	eqdFixtureSymbol  = "600519.SH"
	eqdShortSymbol    = "000001.SZ"
	eqdNegativeSymbol = "600000.SH"
	eqdBridgedSymbol  = "000002.SZ"
)

// eqdFixtureSeries is a coherent two-year history for eqdFixtureSymbol. Income
// statement / cash-flow series are year-to-date cumulative, as the field
// dictionary requires; balance-sheet series are point-in-time.
//
// The cumulative series reset at every fiscal year boundary — 2024Q1 is a
// single quarter, not the continuation of 2023Q4. Getting that wrong makes the
// single-quarter differencing look plausible while being wrong, which is
// exactly what this fixture exists to catch.
//
// Values the factor tests rely on, all hand-checkable:
//
//	single-quarter revenue : 100 120 140 160 | 150 200 200 200
//	single-quarter cost    :  80  90  98 104 |  90 110 100  90
//	single-quarter margin  : .20 .25 .30 .35 | .40 .45 .50 .55
//	revenue TTM at 2024Q4  : 750   (Q4 is already the annual reading)
//	cost TTM               : 390 at 2024Q4, 372 at 2023Q4
//	contract liability     :  75 / 750  = 0.1
//	ocf / net profit       : 100 / 125  = 0.8
//	assets / equity        : 1000 / 400 = 2.5
//	inventory turnover     : 390/60 - 372/62 = 6.5 - 6 = 0.5
var eqdFixtureSeries = map[string][]float64{
	equitydeep.FieldTotalRevenue:      {100, 220, 360, 520, 150, 350, 550, 750},
	equitydeep.FieldOperatingCost:     {80, 170, 268, 372, 90, 200, 300, 390},
	equitydeep.FieldContractLiability: {10, 20, 30, 40, 50, 55, 65, 75},
	equitydeep.FieldOCFNet:            {10, 25, 45, 60, 20, 45, 70, 100},
	equitydeep.FieldNetProfitAttr:     {20, 45, 70, 100, 30, 60, 90, 125},
	equitydeep.FieldTotalAssets:       {900, 920, 940, 960, 970, 980, 990, 1000},
	equitydeep.FieldEquityAttr:        {350, 360, 370, 380, 385, 390, 395, 400},
	equitydeep.FieldInventory:         {55, 57, 59, 62, 60, 61, 61, 60},
}

// eqdAsOf is late enough to see every 2024Q4 announcement in the fixture
// (period end + 30 days = 2025-01-30).
var eqdAsOf = time.Date(2025, 4, 30, 0, 0, 0, 0, time.UTC)

func eqdFixtureRows() []domain.FundamentalsDetailRow {
	var rows []domain.FundamentalsDetailRow
	for fieldCode, series := range eqdFixtureSeries {
		for i, p := range eqdFixtureQuarters {
			rows = append(rows, eqdRow(eqdFixtureSymbol, p, fieldCode, series[i]))
		}
	}
	return rows
}

// eqdUnusableRows returns two symbols that every factor must skip: one with a
// single quarter of history, one whose denominators are non-positive or zero.
func eqdUnusableRows() []domain.FundamentalsDetailRow {
	last := eqdPeriod(2024, 4)
	return []domain.FundamentalsDetailRow{
		// Too short to difference a single quarter.
		eqdRow(eqdShortSymbol, last, equitydeep.FieldTotalRevenue, 100),
		eqdRow(eqdShortSymbol, last, equitydeep.FieldOperatingCost, 80),

		// Inputs that would otherwise produce a sign-flipped or undefined
		// factor.
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldTotalRevenue, -100),
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldOperatingCost, 80),
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldNetProfitAttr, -50),
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldOCFNet, 10),
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldTotalAssets, 100),
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldEquityAttr, -10),
		eqdRow(eqdNegativeSymbol, last, equitydeep.FieldInventory, 0),
	}
}

// eqdBridgedRatioRows returns a second symbol whose revenue TTM must be
// bridged across the year boundary: TTM(2024Q2) = 260 + 520 - 220 = 560, so
// the ratio is 112 / 560 = 0.2.
func eqdBridgedRatioRows() []domain.FundamentalsDetailRow {
	return []domain.FundamentalsDetailRow{
		eqdRow(eqdBridgedSymbol, eqdPeriod(2023, 2), equitydeep.FieldTotalRevenue, 220),
		eqdRow(eqdBridgedSymbol, eqdPeriod(2023, 4), equitydeep.FieldTotalRevenue, 520),
		eqdRow(eqdBridgedSymbol, eqdPeriod(2024, 2), equitydeep.FieldTotalRevenue, 260),
		eqdRow(eqdBridgedSymbol, eqdPeriod(2024, 2), equitydeep.FieldContractLiability, 112),
	}
}

// mock store ----------------------------------------------------------------

// eqdMockStore is a FactorStore whose GetFundamentalsDetailAsOf reproduces the
// two storage-side filters of contract C1: the PIT rule (ann_date <= asOf) and
// the field-code filter. Reproducing them makes a factor that asks for the
// wrong field code or the wrong asOf fail visibly instead of silently
// returning a plausible number.
type eqdMockStore struct {
	mu sync.Mutex

	rows      []domain.FundamentalsDetailRow
	detailErr error

	saveErr     error
	saved       []*domain.FactorCacheEntry
	saveCalls   int
	detailCalls int
	lastAsOf    time.Time
	lastFields  []string
}

func (m *eqdMockStore) GetOHLCVForDateRange(context.Context, time.Time, time.Time) ([]domain.OHLCV, error) {
	return nil, nil
}

func (m *eqdMockStore) GetFundamentalsSnapshot(context.Context, time.Time) ([]domain.FundamentalData, error) {
	return nil, nil
}

func (m *eqdMockStore) GetFundamentalsDetailAsOf(_ context.Context, fieldCodes []string, asOf time.Time) ([]domain.FundamentalsDetailRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detailCalls++
	m.lastAsOf = asOf
	m.lastFields = append([]string(nil), fieldCodes...)
	if m.detailErr != nil {
		return nil, m.detailErr
	}
	wanted := make(map[string]bool, len(fieldCodes))
	for _, code := range fieldCodes {
		wanted[code] = true
	}
	var visible []domain.FundamentalsDetailRow
	for _, r := range m.rows {
		if !wanted[r.FieldCode] || r.AnnDate.After(asOf) {
			continue
		}
		visible = append(visible, r)
	}
	return visible, nil
}

func (m *eqdMockStore) GetTradingDays(context.Context, time.Time, time.Time) ([]time.Time, error) {
	return nil, nil
}

func (m *eqdMockStore) SaveFactorCacheBatch(_ context.Context, entries []*domain.FactorCacheEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, entries...)
	return nil
}

func (m *eqdMockStore) entriesBySymbol(factor domain.FactorType) map[string]*domain.FactorCacheEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*domain.FactorCacheEntry)
	for _, e := range m.saved {
		if e.FactorName == factor {
			out[e.Symbol] = e
		}
	}
	return out
}

// rawValues returns every persisted raw value keyed by "factor|symbol" so the
// two ComputeAllFactors paths can be compared without depending on goroutine
// completion order.
func (m *eqdMockStore) rawValues() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]float64, len(m.saved))
	for _, e := range m.saved {
		out[string(e.FactorName)+"|"+e.Symbol] = e.RawValue
	}
	return out
}

func (m *eqdMockStore) savedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.saved)
}

func (m *eqdMockStore) lastRequest() ([]string, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.lastFields...), m.lastAsOf
}

// pure functions: period arithmetic ----------------------------------------

func TestPeriodOf(t *testing.T) {
	cases := []struct {
		name   string
		date   time.Time
		want   reportPeriod
		wantOK bool
	}{
		{"Q1", eqdQuarterEnd(2024, 1), eqdPeriod(2024, 1), true},
		{"Q2", eqdQuarterEnd(2024, 2), eqdPeriod(2024, 2), true},
		{"Q3", eqdQuarterEnd(2024, 3), eqdPeriod(2024, 3), true},
		{"Q4", eqdQuarterEnd(2024, 4), eqdPeriod(2024, 4), true},
		{"mid-year month is rejected", time.Date(2024, time.May, 31, 0, 0, 0, 0, time.UTC), reportPeriod{}, false},
		{"January is rejected", time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC), reportPeriod{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := periodOf(tc.date)
			if ok != tc.wantOK {
				t.Fatalf("periodOf(%s) ok = %v, want %v", tc.date.Format("2006-01-02"), ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("periodOf(%s) = %+v, want %+v", tc.date.Format("2006-01-02"), got, tc.want)
			}
		})
	}
}

func TestShiftQuarters(t *testing.T) {
	cases := []struct {
		name  string
		from  reportPeriod
		delta int
		want  reportPeriod
	}{
		{"Q1 minus one quarter is the previous Q4", eqdPeriod(2024, 1), -1, eqdPeriod(2023, 4)},
		{"Q4 plus one quarter is the next Q1", eqdPeriod(2024, 4), 1, eqdPeriod(2025, 1)},
		{"plus four quarters", eqdPeriod(2024, 2), 4, eqdPeriod(2025, 2)},
		{"minus four quarters", eqdPeriod(2024, 2), -4, eqdPeriod(2023, 2)},
		{"zero is the identity", eqdPeriod(2024, 3), 0, eqdPeriod(2024, 3)},
		{"minus five quarters crosses the year", eqdPeriod(2024, 1), -5, eqdPeriod(2022, 4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shiftQuarters(tc.from, tc.delta); got != tc.want {
				t.Errorf("shiftQuarters(%+v, %d) = %+v, want %+v", tc.from, tc.delta, got, tc.want)
			}
		})
	}
}

// pure functions: statementField --------------------------------------------

func TestStatementFieldTTM(t *testing.T) {
	// YTD(Q2 2024)=300, YTD(Q4 2023)=520, YTD(Q2 2023)=220 -> TTM = 600.
	bridged := eqdField(map[reportPeriod]float64{
		eqdPeriod(2023, 2): 220,
		eqdPeriod(2023, 4): 520,
		eqdPeriod(2024, 2): 300,
	})

	t.Run("Q4 is already the annual reading", func(t *testing.T) {
		f := eqdField(map[reportPeriod]float64{eqdPeriod(2024, 4): 1270})
		got, ok := f.ttm(eqdPeriod(2024, 4))
		if !ok {
			t.Fatal("ttm(Q4) ok = false, want true")
		}
		eqdAssertClose(t, "ttm(Q4)", got, 1270)
	})

	t.Run("Q2 bridges across the year boundary", func(t *testing.T) {
		got, ok := bridged.ttm(eqdPeriod(2024, 2))
		if !ok {
			t.Fatal("ttm(Q2) ok = false, want true")
		}
		eqdAssertClose(t, "ttm(Q2)", got, 600)
	})

	t.Run("missing prior-year Q4 yields false", func(t *testing.T) {
		f := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 2): 220,
			eqdPeriod(2024, 2): 300,
		})
		if _, ok := f.ttm(eqdPeriod(2024, 2)); ok {
			t.Error("ttm(Q2) ok = true without a prior-year annual reading, want false")
		}
	})

	t.Run("missing prior-year same quarter yields false", func(t *testing.T) {
		f := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 4): 520,
			eqdPeriod(2024, 2): 300,
		})
		if _, ok := f.ttm(eqdPeriod(2024, 2)); ok {
			t.Error("ttm(Q2) ok = true without a prior-year Q2, want false")
		}
	})

	t.Run("missing current period yields false", func(t *testing.T) {
		if _, ok := bridged.ttm(eqdPeriod(2024, 3)); ok {
			t.Error("ttm(Q3) ok = true for a period with no reading, want false")
		}
	})
}

func TestStatementFieldSingleQuarter(t *testing.T) {
	// Q1 needs no differencing; Q3 = 360 - 220 = 140.
	f := eqdField(map[reportPeriod]float64{
		eqdPeriod(2023, 1): 100,
		eqdPeriod(2023, 2): 220,
		eqdPeriod(2023, 3): 360,
	})

	t.Run("Q1 is returned as is", func(t *testing.T) {
		got, ok := f.singleQuarter(eqdPeriod(2023, 1))
		if !ok {
			t.Fatal("singleQuarter(Q1) ok = false, want true")
		}
		eqdAssertClose(t, "singleQuarter(Q1)", got, 100)
	})

	t.Run("Q3 differences against Q2", func(t *testing.T) {
		got, ok := f.singleQuarter(eqdPeriod(2023, 3))
		if !ok {
			t.Fatal("singleQuarter(Q3) ok = false, want true")
		}
		eqdAssertClose(t, "singleQuarter(Q3)", got, 140)
	})

	t.Run("missing predecessor yields false", func(t *testing.T) {
		// Q2 is present but Q1 is not, so the quarter cannot be derived.
		orphan := eqdField(map[reportPeriod]float64{eqdPeriod(2023, 2): 220})
		if _, ok := orphan.singleQuarter(eqdPeriod(2023, 2)); ok {
			t.Error("singleQuarter(Q2) ok = true without a Q1 reading, want false")
		}
	})

	t.Run("missing current period yields false", func(t *testing.T) {
		if _, ok := f.singleQuarter(eqdPeriod(2023, 4)); ok {
			t.Error("singleQuarter(Q4) ok = true without a reading, want false")
		}
	})
}

func TestStatementFieldLatestAndHelpers(t *testing.T) {
	t.Run("latest picks the greatest order across years", func(t *testing.T) {
		f := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 4): 372,
			eqdPeriod(2024, 1): 462,
		})
		p, ok := f.latest()
		if !ok {
			t.Fatal("latest() ok = false, want true")
		}
		if p != eqdPeriod(2024, 1) {
			t.Errorf("latest() = %+v, want 2024Q1", p)
		}
		got, ok := latestValue(f)
		if !ok {
			t.Fatal("latestValue() ok = false, want true")
		}
		eqdAssertClose(t, "latestValue()", got, 462)
	})

	t.Run("empty field has no latest", func(t *testing.T) {
		var f statementField
		if _, ok := f.latest(); ok {
			t.Error("latest() ok = true for an empty field, want false")
		}
		if _, ok := latestValue(f); ok {
			t.Error("latestValue() ok = true for an empty field, want false")
		}
		if _, ok := latestTTM(f); ok {
			t.Error("latestTTM() ok = true for an empty field, want false")
		}
	})

	t.Run("latestTTM converts at the latest period", func(t *testing.T) {
		f := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 2): 220,
			eqdPeriod(2023, 4): 520,
			eqdPeriod(2024, 2): 260,
		})
		got, ok := latestTTM(f)
		if !ok {
			t.Fatal("latestTTM() ok = false, want true")
		}
		eqdAssertClose(t, "latestTTM()", got, 560)
	})
}

// pure functions: factor building blocks ------------------------------------

func TestGrossMarginQuarters(t *testing.T) {
	// YTD revenue 100/220/360/520 -> single-quarter 100/120/140/160; YTD cost
	// 80/170/268/372 -> single-quarter 80/90/98/104; margins therefore
	// 0.20/0.25/0.30/0.35, oldest first.
	revenue := eqdField(map[reportPeriod]float64{
		eqdPeriod(2023, 1): 100,
		eqdPeriod(2023, 2): 220,
		eqdPeriod(2023, 3): 360,
		eqdPeriod(2023, 4): 520,
	})
	cost := eqdField(map[reportPeriod]float64{
		eqdPeriod(2023, 1): 80,
		eqdPeriod(2023, 2): 170,
		eqdPeriod(2023, 3): 268,
		eqdPeriod(2023, 4): 372,
	})

	t.Run("returns the window oldest first", func(t *testing.T) {
		got, ok := grossMarginQuarters(revenue, cost, grossMarginTrendQuarters)
		if !ok {
			t.Fatal("grossMarginQuarters ok = false, want true")
		}
		want := []float64{0.20, 0.25, 0.30, 0.35}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			eqdAssertClose(t, fmt.Sprintf("margin[%d]", i), got[i], want[i])
		}
	})

	t.Run("too short a history yields false", func(t *testing.T) {
		partial := eqdField(map[reportPeriod]float64{eqdPeriod(2023, 4): 520})
		if _, ok := grossMarginQuarters(partial, cost, grossMarginTrendQuarters); ok {
			t.Error("grossMarginQuarters ok = true with a single quarter, want false")
		}
	})

	t.Run("gap in the differencing chain yields false", func(t *testing.T) {
		// Q3 is missing, so Q4's single quarter cannot be derived.
		gappy := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 1): 100,
			eqdPeriod(2023, 4): 520,
		})
		if _, ok := grossMarginQuarters(gappy, cost, grossMarginTrendQuarters); ok {
			t.Error("grossMarginQuarters ok = true across a gap, want false")
		}
	})

	t.Run("non-positive quarterly revenue yields false", func(t *testing.T) {
		// YTD(Q2) < YTD(Q1) makes the Q2 single quarter negative.
		shrinking := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 1): 100,
			eqdPeriod(2023, 2): 90,
			eqdPeriod(2023, 3): 200,
			eqdPeriod(2023, 4): 300,
		})
		if _, ok := grossMarginQuarters(shrinking, cost, grossMarginTrendQuarters); ok {
			t.Error("grossMarginQuarters ok = true with non-positive revenue, want false")
		}
	})

	t.Run("no revenue at all yields false", func(t *testing.T) {
		if _, ok := grossMarginQuarters(statementField{}, cost, grossMarginTrendQuarters); ok {
			t.Error("grossMarginQuarters ok = true without revenue, want false")
		}
	})
}

func TestInventoryTurnover(t *testing.T) {
	inventory := eqdField(map[reportPeriod]float64{
		eqdPeriod(2023, 4): 62,
		eqdPeriod(2024, 4): 63.5,
	})
	cost := eqdField(map[reportPeriod]float64{
		eqdPeriod(2023, 4): 372,
		eqdPeriod(2024, 4): 762,
	})

	t.Run("current period", func(t *testing.T) {
		got, ok := inventoryTurnover(inventory, cost, 0)
		if !ok {
			t.Fatal("inventoryTurnover(0) ok = false, want true")
		}
		eqdAssertClose(t, "turnover(2024Q4)", got, 12)
	})

	t.Run("year-ago period", func(t *testing.T) {
		got, ok := inventoryTurnover(inventory, cost, -4)
		if !ok {
			t.Fatal("inventoryTurnover(-4) ok = false, want true")
		}
		eqdAssertClose(t, "turnover(2023Q4)", got, 6)
	})

	t.Run("non-positive inventory yields false", func(t *testing.T) {
		zeroed := eqdField(map[reportPeriod]float64{
			eqdPeriod(2023, 4): 62,
			eqdPeriod(2024, 4): 0,
		})
		if _, ok := inventoryTurnover(zeroed, cost, 0); ok {
			t.Error("inventoryTurnover ok = true with zero inventory, want false")
		}
	})

	t.Run("missing TTM cost leg yields false", func(t *testing.T) {
		partial := eqdField(map[reportPeriod]float64{eqdPeriod(2024, 4): 762})
		if _, ok := inventoryTurnover(inventory, partial, -4); ok {
			t.Error("inventoryTurnover ok = true without the year-ago TTM cost, want false")
		}
	})

	t.Run("no inventory at all yields false", func(t *testing.T) {
		if _, ok := inventoryTurnover(statementField{}, cost, 0); ok {
			t.Error("inventoryTurnover ok = true without inventory, want false")
		}
	})
}

// loadStatementBook ---------------------------------------------------------

func TestLoadStatementBook(t *testing.T) {
	ctx := context.Background()

	t.Run("indexes by symbol then field code", func(t *testing.T) {
		store := &eqdMockStore{rows: eqdFixtureRows()}
		fc := NewFactorComputer(store)
		book, err := fc.loadStatementBook(ctx, eqdAsOf, equitydeep.FieldTotalRevenue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(book.fields) != 1 {
			t.Fatalf("symbols = %d, want 1", len(book.fields))
		}
		field := book.fields[eqdFixtureSymbol][equitydeep.FieldTotalRevenue]
		got, ok := field.valueAt(eqdPeriod(2024, 4))
		if !ok {
			t.Fatal("valueAt(2024Q4) ok = false, want true")
		}
		eqdAssertClose(t, "revenue(2024Q4)", got, 750)

		fields, asOf := store.lastRequest()
		if len(fields) != 1 || fields[0] != equitydeep.FieldTotalRevenue {
			t.Errorf("requested field codes = %v, want [%s]", fields, equitydeep.FieldTotalRevenue)
		}
		if !asOf.Equal(eqdAsOf) {
			t.Errorf("requested asOf = %s, want %s", asOf.Format("2006-01-02"), eqdAsOf.Format("2006-01-02"))
		}
	})

	t.Run("keeps the latest announcement of a restated period", func(t *testing.T) {
		// Announced after the fixture row (2025-01-30) but still before the
		// evaluation date, so it must replace it.
		restated := eqdRowAnnounced(
			eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 9999,
			time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC),
		)
		rows := append(eqdFixtureRows(), restated)
		store := &eqdMockStore{rows: rows}
		fc := NewFactorComputer(store)
		book, err := fc.loadStatementBook(ctx, eqdAsOf, equitydeep.FieldTotalRevenue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, ok := book.fields[eqdFixtureSymbol][equitydeep.FieldTotalRevenue].valueAt(eqdPeriod(2024, 4))
		if !ok {
			t.Fatal("valueAt(2024Q4) ok = false, want true")
		}
		eqdAssertClose(t, "restated revenue(2024Q4)", got, 9999)
	})

	t.Run("drops rows without a value", func(t *testing.T) {
		const symbol = "000004.SZ"
		row := eqdRow(symbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 0)
		row.Value = nil
		store := &eqdMockStore{rows: []domain.FundamentalsDetailRow{row}}
		fc := NewFactorComputer(store)
		book, err := fc.loadStatementBook(ctx, eqdAsOf, equitydeep.FieldTotalRevenue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := book.fields[symbol][equitydeep.FieldTotalRevenue].valueAt(eqdPeriod(2024, 4)); ok {
			t.Error("a row with a nil value produced a reading; nil must not be read as 0")
		}
	})

	t.Run("drops periods that are not quarter ends", func(t *testing.T) {
		const symbol = "000005.SZ"
		row := eqdRow(symbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 100)
		row.EndDate = time.Date(2024, time.May, 31, 0, 0, 0, 0, time.UTC)
		store := &eqdMockStore{rows: []domain.FundamentalsDetailRow{row}}
		fc := NewFactorComputer(store)
		book, err := fc.loadStatementBook(ctx, eqdAsOf, equitydeep.FieldTotalRevenue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(book.fields[symbol]) != 0 {
			t.Errorf("a non-quarter-end row was kept: %+v", book.fields[symbol])
		}
	})

	t.Run("propagates store errors", func(t *testing.T) {
		boom := errors.New("boom")
		store := &eqdMockStore{detailErr: boom}
		fc := NewFactorComputer(store)
		if _, err := fc.loadStatementBook(ctx, eqdAsOf, equitydeep.FieldTotalRevenue); !errors.Is(err, boom) {
			t.Errorf("err = %v, want wrapped %v", err, boom)
		}
	})
}

// citation provenance (ODR-061 切片 C2) --------------------------------------

// Two 64-hex constants chosen so lexicographic order differs from insertion
// order, pinning the sorted output.
const (
	eqdHashLate  = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	eqdHashEarly = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// eqdRowWithSnapshot overrides a fixture row's snapshot_uri so provenance can
// be exercised independently of the default non-archived fixture URI.
func eqdRowWithSnapshot(tsCode string, p reportPeriod, fieldCode string, value float64, snapshotURI string) domain.FundamentalsDetailRow {
	row := eqdRow(tsCode, p, fieldCode, value)
	row.SnapshotURI = snapshotURI
	return row
}

func TestContentHashOfSnapshot(t *testing.T) {
	if got, ok := contentHashOfSnapshot(snapshotURIPrefix + eqdHashEarly); !ok || got != eqdHashEarly {
		t.Errorf("got (%q, %v), want the hash itself", got, ok)
	}
	for _, uri := range []string{"file:///fixtures/snapshot.json", snapshotURIPrefix, "", "x" + snapshotURIPrefix + "abc"} {
		if _, ok := contentHashOfSnapshot(uri); ok {
			t.Errorf("contentHashOfSnapshot(%q) ok = true, want false (prefix is never guessed around)", uri)
		}
	}
}

func TestLoadStatementBookCitation(t *testing.T) {
	ctx := context.Background()

	t.Run("collects the batches of surviving readings, sorted and deduped", func(t *testing.T) {
		// Two revenue periods from two different archived batches, one
		// balance-sheet reading from the first batch again (deduped), and one
		// row whose snapshot is not archived (contributes nothing).
		rows := []domain.FundamentalsDetailRow{
			eqdRowWithSnapshot(eqdFixtureSymbol, eqdPeriod(2023, 4), equitydeep.FieldTotalRevenue, 520, snapshotURIPrefix+eqdHashLate),
			eqdRowWithSnapshot(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 750, snapshotURIPrefix+eqdHashEarly),
			eqdRowWithSnapshot(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldContractLiability, 75, snapshotURIPrefix+eqdHashLate),
			eqdRow(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalAssets, 1000),
		}
		store := &eqdMockStore{rows: rows}
		fc := NewFactorComputer(store)
		book, err := fc.loadStatementBook(ctx, eqdAsOf,
			equitydeep.FieldTotalRevenue, equitydeep.FieldContractLiability, equitydeep.FieldTotalAssets)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := book.hashes[eqdFixtureSymbol]
		want := []string{eqdHashEarly, eqdHashLate}
		if len(got) != len(want) {
			t.Fatalf("hashes = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("hashes[%d] = %s, want %s", i, got[i], want[i])
			}
		}
	})

	t.Run("a non-archived restatement drops the superseded batch", func(t *testing.T) {
		// 2024Q4 revenue is first archived in a batch, then restated from a
		// non-archived source: the surviving reading has no provenance, and
		// citing the earlier batch would name one whose value is no longer in
		// use.
		rows := []domain.FundamentalsDetailRow{
			eqdRowWithSnapshot(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 750, snapshotURIPrefix+eqdHashEarly),
			eqdRowAnnounced(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 9999,
				eqdQuarterEnd(2024, 4).AddDate(0, 0, 60)),
		}
		store := &eqdMockStore{rows: rows}
		fc := NewFactorComputer(store)
		book, err := fc.loadStatementBook(ctx, eqdAsOf, equitydeep.FieldTotalRevenue)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(book.hashes[eqdFixtureSymbol]) != 0 {
			t.Errorf("hashes = %v, want none: the superseded batch must not be cited", book.hashes[eqdFixtureSymbol])
		}
	})
}

func TestComputeVerticalFactorCitation(t *testing.T) {
	ctx := context.Background()

	t.Run("persisted entries cite the batches their readings came from", func(t *testing.T) {
		rows := []domain.FundamentalsDetailRow{
			eqdRowWithSnapshot(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldContractLiability, 75, snapshotURIPrefix+eqdHashLate),
			eqdRowWithSnapshot(eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 750, snapshotURIPrefix+eqdHashEarly),
		}
		store := &eqdMockStore{rows: rows}
		fc := NewFactorComputer(store)
		if err := fc.ComputeContractLiabilityRatioFactor(ctx, eqdAsOf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		entries := store.entriesBySymbol(domain.FactorContractLiabilityRatio)
		e := entries[eqdFixtureSymbol]
		if e == nil {
			t.Fatalf("no entry for %s", eqdFixtureSymbol)
		}
		var refs []struct {
			ContentHash string `json:"content_hash"`
		}
		if err := json.Unmarshal(e.Citation, &refs); err != nil {
			t.Fatalf("citation is not the hash-array form: %v (%s)", err, e.Citation)
		}
		if len(refs) != 2 {
			t.Fatalf("citation = %s, want two hashes", e.Citation)
		}
		if refs[0].ContentHash != eqdHashEarly || refs[1].ContentHash != eqdHashLate {
			t.Errorf("citation = %s, want [%s %s] sorted", e.Citation, eqdHashEarly, eqdHashLate)
		}
		// Only content_hash is stored; the five-tuple expansion is the output
		// face's job (cmd/data getFactorHandler).
		if strings.Contains(string(e.Citation), "source") {
			t.Errorf("stored citation must be hash-only, got %s", e.Citation)
		}
	})

	t.Run("readings without an archived snapshot cite the empty marker", func(t *testing.T) {
		store := &eqdMockStore{rows: eqdFixtureRows()}
		fc := NewFactorComputer(store)
		if err := fc.ComputeGrossMarginTrendFactor(ctx, eqdAsOf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		entries := store.entriesBySymbol(domain.FactorGrossMarginTrend)
		e := entries[eqdFixtureSymbol]
		if e == nil {
			t.Fatalf("no entry for %s", eqdFixtureSymbol)
		}
		if string(e.Citation) != "[]" {
			t.Errorf("citation = %s, want [] (chain not established — explicit marker, never null)", e.Citation)
		}
	})
}

// the five factors ----------------------------------------------------------

func TestComputeGrossMarginTrendFactor(t *testing.T) {
	store := &eqdMockStore{rows: eqdFixtureRows()}
	fc := NewFactorComputer(store)

	if err := fc.ComputeGrossMarginTrendFactor(context.Background(), eqdAsOf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := store.entriesBySymbol(domain.FactorGrossMarginTrend)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[eqdFixtureSymbol]
	if e == nil {
		t.Fatalf("no entry for %s", eqdFixtureSymbol)
	}
	eqdAssertClose(t, "raw value", e.RawValue, 0.05)
	if !e.TradeDate.Equal(eqdAsOf) {
		t.Errorf("trade date = %s, want %s", e.TradeDate.Format("2006-01-02"), eqdAsOf.Format("2006-01-02"))
	}
	// One symbol: zero cross-sectional variance, so the z-score is 0 and the
	// ordinal percentile is 50.
	eqdAssertClose(t, "z-score", e.ZScore, 0)
	eqdAssertClose(t, "percentile", e.Percentile, 50)

	fields, _ := store.lastRequest()
	want := []string{equitydeep.FieldTotalRevenue, equitydeep.FieldOperatingCost}
	if len(fields) != len(want) {
		t.Fatalf("requested field codes = %v, want %v", fields, want)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Fatalf("requested field codes = %v, want %v", fields, want)
		}
	}
}

func TestComputeContractLiabilityRatioFactor(t *testing.T) {
	t.Run("ratio uses the bridged revenue TTM", func(t *testing.T) {
		rows := append(eqdFixtureRows(), eqdBridgedRatioRows()...)
		store := &eqdMockStore{rows: append(rows, eqdUnusableRows()...)}
		fc := NewFactorComputer(store)

		if err := fc.ComputeContractLiabilityRatioFactor(context.Background(), eqdAsOf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		entries := store.entriesBySymbol(domain.FactorContractLiabilityRatio)
		if len(entries) != 2 {
			t.Fatalf("entries = %d, want 2 (the unusable symbols must be skipped)", len(entries))
		}
		eqdAssertClose(t, "600519.SH raw", entries[eqdFixtureSymbol].RawValue, 0.1)
		eqdAssertClose(t, "000002.SZ raw", entries[eqdBridgedSymbol].RawValue, 0.2)
		// Two values 0.1 and 0.2: population stddev 0.05, so z = -1 / +1 and
		// the ordinal percentiles are 25 / 75.
		eqdAssertClose(t, "600519.SH z-score", entries[eqdFixtureSymbol].ZScore, -1)
		eqdAssertClose(t, "000002.SZ z-score", entries[eqdBridgedSymbol].ZScore, 1)
		eqdAssertClose(t, "600519.SH percentile", entries[eqdFixtureSymbol].Percentile, 25)
		eqdAssertClose(t, "000002.SZ percentile", entries[eqdBridgedSymbol].Percentile, 75)
	})

	t.Run("nothing computable means nothing is persisted", func(t *testing.T) {
		store := &eqdMockStore{rows: eqdUnusableRows()}
		fc := NewFactorComputer(store)
		if err := fc.ComputeContractLiabilityRatioFactor(context.Background(), eqdAsOf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n := store.savedCount(); n != 0 {
			t.Errorf("persisted entries = %d, want 0", n)
		}
	})
}

func TestComputeOCFToNetProfitFactor(t *testing.T) {
	store := &eqdMockStore{rows: append(eqdFixtureRows(), eqdUnusableRows()...)}
	fc := NewFactorComputer(store)

	if err := fc.ComputeOCFToNetProfitFactor(context.Background(), eqdAsOf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := store.entriesBySymbol(domain.FactorOCFToNetProfit)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	eqdAssertClose(t, "raw value", entries[eqdFixtureSymbol].RawValue, 0.8)
}

func TestComputeROEDuPontLeverageFactor(t *testing.T) {
	store := &eqdMockStore{rows: append(eqdFixtureRows(), eqdUnusableRows()...)}
	fc := NewFactorComputer(store)

	if err := fc.ComputeROEDuPontLeverageFactor(context.Background(), eqdAsOf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := store.entriesBySymbol(domain.FactorROEDuPontLeverage)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	eqdAssertClose(t, "raw value", entries[eqdFixtureSymbol].RawValue, 2.5)
}

func TestComputeInventoryTurnoverDeltaFactor(t *testing.T) {
	store := &eqdMockStore{rows: append(eqdFixtureRows(), eqdUnusableRows()...)}
	fc := NewFactorComputer(store)

	if err := fc.ComputeInventoryTurnoverDeltaFactor(context.Background(), eqdAsOf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := store.entriesBySymbol(domain.FactorInventoryTurnoverDelta)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	eqdAssertClose(t, "raw value", entries[eqdFixtureSymbol].RawValue, 0.5)
}

// PIT ----------------------------------------------------------------------

// TestVerticalFactorsIgnorePostAsOfRestatement pins the PIT rule from the
// outside: a restatement announced after the evaluation date carries a value
// that would dominate the factor if it leaked in, and the factor is unchanged.
// The row is filtered by the store, so this also pins that the factor passes
// the evaluation date through as asOf rather than dropping the argument.
func TestVerticalFactorsIgnorePostAsOfRestatement(t *testing.T) {
	rows := append(eqdFixtureRows(), eqdRowAnnounced(
		eqdFixtureSymbol, eqdPeriod(2024, 4), equitydeep.FieldTotalRevenue, 99999,
		eqdAsOf.AddDate(0, 1, 0),
	))
	store := &eqdMockStore{rows: rows}
	fc := NewFactorComputer(store)

	if err := fc.ComputeGrossMarginTrendFactor(context.Background(), eqdAsOf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := store.entriesBySymbol(domain.FactorGrossMarginTrend)
	e := entries[eqdFixtureSymbol]
	if e == nil {
		t.Fatalf("no entry for %s", eqdFixtureSymbol)
	}
	eqdAssertClose(t, "raw value", e.RawValue, 0.05)
}

// ComputeAllFactors ---------------------------------------------------------

// eqdAllFactorRows is the union used by the ComputeAllFactors tests: the
// fixture plus a bridged-TTM symbol and two symbols that must be skipped.
func eqdAllFactorRows() []domain.FundamentalsDetailRow {
	rows := eqdFixtureRows()
	rows = append(rows, eqdBridgedRatioRows()...)
	return append(rows, eqdUnusableRows()...)
}

func TestComputeAllFactors_SequentialAndParallelAgree(t *testing.T) {
	// 5 factors x 600519.SH plus contract_liability_ratio for the bridged
	// symbol; nothing else is computable.
	const wantEntries = 6

	ctx := context.Background()
	sequential := &eqdMockStore{rows: eqdAllFactorRows()}
	if err := NewFactorComputer(sequential).ComputeAllFactors(ctx, eqdAsOf, 20, false); err != nil {
		t.Fatalf("sequential: unexpected error: %v", err)
	}
	parallel := &eqdMockStore{rows: eqdAllFactorRows()}
	if err := NewFactorComputer(parallel).ComputeAllFactors(ctx, eqdAsOf, 20, true); err != nil {
		t.Fatalf("parallel: unexpected error: %v", err)
	}

	seqValues, parValues := sequential.rawValues(), parallel.rawValues()
	if len(seqValues) != wantEntries {
		t.Fatalf("sequential entries = %d, want %d", len(seqValues), wantEntries)
	}
	if len(parValues) != wantEntries {
		t.Fatalf("parallel entries = %d, want %d", len(parValues), wantEntries)
	}
	for key, want := range seqValues {
		got, ok := parValues[key]
		if !ok {
			t.Errorf("parallel run is missing %s", key)
			continue
		}
		eqdAssertClose(t, key, got, want)
	}

	// Spot-check the values so a bug that changes both paths identically still
	// fails.
	expected := map[string]float64{
		"gross_margin_trend|" + eqdFixtureSymbol:       0.05,
		"contract_liability_ratio|" + eqdFixtureSymbol: 0.1,
		"contract_liability_ratio|" + eqdBridgedSymbol: 0.2,
		"ocf_to_net_profit|" + eqdFixtureSymbol:        0.8,
		"roe_dupont_leverage|" + eqdFixtureSymbol:      2.5,
		"inventory_turnover_delta|" + eqdFixtureSymbol: 0.5,
	}
	for key, want := range expected {
		got, ok := seqValues[key]
		if !ok {
			t.Errorf("sequential run is missing %s", key)
			continue
		}
		eqdAssertClose(t, key, got, want)
	}
}

func TestComputeAllFactors_PropagatesError(t *testing.T) {
	boom := errors.New("boom")
	ctx := context.Background()

	t.Run("sequential reports the first factor in list order", func(t *testing.T) {
		store := &eqdMockStore{rows: eqdFixtureRows(), detailErr: boom}
		err := NewFactorComputer(store).ComputeAllFactors(ctx, eqdAsOf, 20, false)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want wrapped %v", err, boom)
		}
		// The three cross-sectional factors do not touch fundamentals_detail,
		// so gross_margin_trend is the first one that can fail.
		if !strings.Contains(err.Error(), "gross_margin_trend") {
			t.Errorf("err = %v, want the gross_margin_trend context", err)
		}
	})

	t.Run("parallel reports a vertical factor", func(t *testing.T) {
		store := &eqdMockStore{rows: eqdFixtureRows(), detailErr: boom}
		err := NewFactorComputer(store).ComputeAllFactors(ctx, eqdAsOf, 20, true)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want wrapped %v", err, boom)
		}
		// Which goroutine wins is not deterministic, but it must be one of the
		// five vertical factors.
		if !eqdNamesAnyVerticalFactor(err.Error()) {
			t.Errorf("err = %v, want a vertical-factor context", err)
		}
	})
}

func eqdNamesAnyVerticalFactor(msg string) bool {
	for _, name := range []string{
		string(domain.FactorGrossMarginTrend),
		string(domain.FactorContractLiabilityRatio),
		string(domain.FactorOCFToNetProfit),
		string(domain.FactorROEDuPontLeverage),
		string(domain.FactorInventoryTurnoverDelta),
	} {
		if strings.Contains(msg, name) {
			return true
		}
	}
	return false
}
