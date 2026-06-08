package factor

import (
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data/source"
)

func TestSectorRotationFactor_Mapping(t *testing.T) {
	tradeDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := []SectorRow{
		{SectorCode: "BK0001", TradeTime: tradeDate, ChangePct: 0.05},
		{SectorCode: "BK0002", TradeTime: tradeDate, ChangePct: -0.02},
	}
	mapping := map[string]string{
		"600519.SH": "BK0001",
		"000001.SZ": "BK0002",
		"999999.SH": "BK9999", // unknown sector → 0
	}
	factor := SectorRotationFactor(rows, tradeDate, mapping)
	if factor["600519.SH"] != 0.05 {
		t.Errorf("600519 = %v, want 0.05", factor["600519.SH"])
	}
	if factor["000001.SZ"] != -0.02 {
		t.Errorf("000001 = %v, want -0.02", factor["000001.SZ"])
	}
	if factor["999999.SH"] != 0 {
		t.Errorf("999999 (unknown sector) = %v, want 0", factor["999999.SH"])
	}
}

func TestSectorRotationFactor_AsOfFiltering(t *testing.T) {
	// Point-in-time safety: a row with TradeTime > tradeDate must be
	// IGNORED even if it is the "latest" snapshot, otherwise we leak
	// forward-looking data into a backtest.
	tradeDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := []SectorRow{
		{SectorCode: "BK0001", TradeTime: tradeDate.AddDate(0, 0, -1), ChangePct: 0.04}, // valid
		{SectorCode: "BK0001", TradeTime: tradeDate.AddDate(0, 0, 1)},                   // forward-looking, must be ignored
	}
	mapping := map[string]string{"A": "BK0001"}
	factor := SectorRotationFactor(rows, tradeDate, mapping)
	if factor["A"] != 0.04 {
		t.Errorf("A = %v, want 0.04 (forward-looking row should be ignored)", factor["A"])
	}
}

func TestTopMomentumSectors(t *testing.T) {
	tradeDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	rows := []SectorRow{
		{SectorCode: "BK0001", TradeTime: tradeDate, ChangePct: 0.05},
		{SectorCode: "BK0002", TradeTime: tradeDate, ChangePct: 0.03},
		{SectorCode: "BK0003", TradeTime: tradeDate, ChangePct: 0.01},
		{SectorCode: "BK0004", TradeTime: tradeDate, ChangePct: -0.01},
		// Stale entry for BK0001 (newer should win).
		{SectorCode: "BK0001", TradeTime: tradeDate.AddDate(0, 0, -1), ChangePct: 0.99},
	}
	top := TopMomentumSectors(rows, 2)
	if len(top) != 2 {
		t.Fatalf("len(top) = %d, want 2", len(top))
	}
	if top[0].SectorCode != "BK0001" || top[0].ChangePct != 0.05 {
		t.Errorf("top[0] = %+v, want BK0001@0.05 (newer value should win)", top[0])
	}
	if top[1].SectorCode != "BK0002" {
		t.Errorf("top[1] = %+v, want BK0002", top[1])
	}
}

func TestIsSectorRowValid(t *testing.T) {
	now := time.Now()
	cases := []struct {
		row  SectorRow
		want bool
	}{
		{SectorRow{SectorCode: "BK", TradeTime: now, ChangePct: 0.01}, true},
		{SectorRow{SectorName: "NameOnly", TradeTime: now, ChangePct: 0.01}, true},
		{SectorRow{TradeTime: now, ChangePct: 0.01}, false},                  // no code or name
		{SectorRow{SectorCode: "BK", ChangePct: 0.01}, false},                // no time
		{SectorRow{SectorCode: "BK", TradeTime: now, ChangePct: math.NaN()}, false},
	}
	for i, c := range cases {
		if got := IsSectorRowValid(c.row); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestSectorRowsFromPoints_FilterByDataType(t *testing.T) {
	now := time.Now()
	points := []source.UnifiedDataPoint{
		{Symbol: "BK0001", DataType: source.DataTypeSectors, TradeTime: now, Data: map[string]interface{}{
			"sector_code": "BK0001", "sector_name": "白酒", "change_pct": 0.05,
		}},
		{Symbol: "ignored", DataType: source.DataTypeCapitalFlow, TradeTime: now, Data: map[string]interface{}{}},
	}
	rows := SectorRowsFromPoints(points)
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1 (only DataTypeSectors should pass)", len(rows))
	}
	if rows[0].SectorCode != "BK0001" {
		t.Errorf("got %+v, want BK0001", rows[0])
	}
}
