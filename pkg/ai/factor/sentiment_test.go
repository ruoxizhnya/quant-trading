package factor

import (
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data/source"
)

func TestSentimentFactor_HotSearchRank1(t *testing.T) {
	ref := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := []HotSearchRow{
		{Symbol: "A", TradeTime: ref, Rank: 1},
		{Symbol: "B", TradeTime: ref, Rank: 20},
		{Symbol: "C", TradeTime: ref, Rank: 100}, // out of top-20 → ignored
	}
	factor := SentimentFactor(rows, ref)
	if factor["A"] < 0.95 {
		t.Errorf("A (rank 1) = %v, want ~1.0", factor["A"])
	}
	if factor["B"] < 0.0 || factor["B"] > 0.1 {
		t.Errorf("B (rank 20) = %v, want ~0.0", factor["B"])
	}
	if _, ok := factor["C"]; ok {
		t.Errorf("C (rank 100) should be ignored, got %v", factor["C"])
	}
}

func TestSentimentFactor_NewsSentimentMapping(t *testing.T) {
	ref := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := []HotSearchRow{
		{Symbol: "POS", TradeTime: ref, Title: "stock soars", Sentiment: 1.0},  // bullish → 1.0
		{Symbol: "NEU", TradeTime: ref, Title: "earnings report", Sentiment: 0.0}, // neutral → 0.5
		{Symbol: "NEG", TradeTime: ref, Title: "stock tumbles", Sentiment: -1.0}, // bearish → 0.0
	}
	factor := SentimentFactor(rows, ref)
	if math.Abs(factor["POS"]-1.0) > 1e-9 {
		t.Errorf("POS = %v, want 1.0", factor["POS"])
	}
	if math.Abs(factor["NEU"]-0.5) > 1e-9 {
		t.Errorf("NEU = %v, want 0.5", factor["NEU"])
	}
	if math.Abs(factor["NEG"]-0.0) > 1e-9 {
		t.Errorf("NEG = %v, want 0.0", factor["NEG"])
	}
}

func TestSentimentFactor_TimeDecay(t *testing.T) {
	ref := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	// Each symbol has TWO events: a recent rank-1 hit and a stale
	// rank-20 mention. The proportion of high-rank vs low-rank
	// contributions should be HIGHER for the more recent symbol
	// because the stale event carries less weight.
	rows := []HotSearchRow{
		// OLD: stale rank-1 hit (day -7, low weight) + fresh rank-20
		// mention (day 0, full weight). The fresh-but-low-rank
		// event dominates.
		{Symbol: "OLD", TradeTime: ref.AddDate(0, 0, -7), Rank: 1},
		{Symbol: "OLD", TradeTime: ref, Rank: 20},
		// NEW: fresh rank-1 hit + fresh rank-20 mention. The
		// high-rank event dominates.
		{Symbol: "NEW", TradeTime: ref, Rank: 1},
		{Symbol: "NEW", TradeTime: ref, Rank: 20},
	}
	factor := SentimentFactor(rows, ref)
	if factor["NEW"] <= factor["OLD"] {
		t.Errorf("NEW (%v) should be > OLD (%v); time decay should let the recent high-rank hit dominate", factor["NEW"], factor["OLD"])
	}
}

func TestSentimentFactor_ClipsToUnitInterval(t *testing.T) {
	ref := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	// Pathological input: 100 rank-1 events should still cap at 1.
	rows := make([]HotSearchRow, 100)
	for i := range rows {
		rows[i] = HotSearchRow{Symbol: "X", TradeTime: ref, Rank: 1}
	}
	factor := SentimentFactor(rows, ref)
	if factor["X"] > 1.0 {
		t.Errorf("X = %v, want <= 1.0", factor["X"])
	}
}

func TestSentimentFactor_EmptyInput(t *testing.T) {
	factor := SentimentFactor(nil, time.Now())
	if len(factor) != 0 {
		t.Errorf("len = %d, want 0", len(factor))
	}
}

func TestTopBuzzSymbols(t *testing.T) {
	ref := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := []HotSearchRow{
		{Symbol: "A", TradeTime: ref, Rank: 1},
		{Symbol: "B", TradeTime: ref, Rank: 2},
		{Symbol: "C", TradeTime: ref, Rank: 3},
	}
	top := TopBuzzSymbols(rows, ref, 2)
	if len(top) != 2 {
		t.Fatalf("len = %d, want 2", len(top))
	}
	if top[0] != "A" {
		t.Errorf("top[0] = %q, want A", top[0])
	}
	if top[1] != "B" {
		t.Errorf("top[1] = %q, want B", top[1])
	}
}

func TestHotSearchFromPoints(t *testing.T) {
	now := time.Now()
	points := []source.UnifiedDataPoint{
		{
			Symbol:    "A",
			TradeTime: now,
			DataType:  source.DataTypeHotSearch,
			Data:      map[string]interface{}{"rank": float64(5), "keyword": "test", "heat": 100.0},
		},
		{
			Symbol:    "B",
			TradeTime: now,
			DataType:  source.DataTypeNews,
			Data:      map[string]interface{}{"title": "earnings beat", "sentiment": 0.7},
		},
		{
			// Wrong data type should be ignored.
			Symbol:    "C",
			TradeTime: now,
			DataType:  source.DataTypeOHLCDaily,
			Data:      map[string]interface{}{},
		},
	}
	rows := HotSearchFromPoints(points)
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
}

func TestIsHotSearchRowValid(t *testing.T) {
	now := time.Now()
	cases := []struct {
		row  HotSearchRow
		want bool
	}{
		{HotSearchRow{Symbol: "A", TradeTime: now, Heat: 1}, true},
		{HotSearchRow{TradeTime: now, Heat: 1}, false},                    // no symbol
		{HotSearchRow{Symbol: "A", Heat: 1}, false},                      // no time
		{HotSearchRow{Symbol: "A", TradeTime: now, Heat: math.NaN()}, false},
	}
	for i, c := range cases {
		if got := IsHotSearchRowValid(c.row); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}
