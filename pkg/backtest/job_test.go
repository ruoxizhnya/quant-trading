package backtest

import (
	"testing"
	"time"
)

// TestParseUniverse tests the universe parsing logic.
func TestParseUniverse(t *testing.T) {
	tests := []struct {
		name     string
		universe string
		wantLen  int
		wantNil  bool
	}{
		{"empty", "", 0, true},
		{"all", "all", 0, true},
		{"single symbol", "000001.SZ", 1, false},
		{"three symbols", "000001.SZ,000002.SZ,000004.SZ", 3, false},
		{"with spaces", "  000001.SZ , 000002.SZ ", 2, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseUniverse(tc.universe)
			if tc.wantNil {
				if got != nil {
					t.Errorf("parseUniverse(%q) = %v, want nil", tc.universe, got)
				}
				return
			}
			if len(got) != tc.wantLen {
				t.Errorf("parseUniverse(%q) len = %d, want %d", tc.universe, len(got), tc.wantLen)
			}
		})
	}
}

// TestRecordToJob tests the record → Job conversion.
func TestRecordToJob(t *testing.T) {
	started := time.Now().Add(-1 * time.Hour)
	completed := time.Now()

	rec := &JobRecord{
		ID:         "job-123",
		StrategyID: "momentum",
		Params:     []byte(`{"lookback":20}`),
		Universe:   "csi300",
		StartDate:  "2018-01-01",
		EndDate:    "2023-01-01",
		Status:     "completed",
		Result:     []byte(`{"total_return":0.35}`),
		ErrorMsg:   "",
		CreatedAt:  time.Now(),
		StartedAt:   &started,
		CompletedAt: &completed,
	}

	job := recordToJob(rec)

	if job.ID != "job-123" {
		t.Errorf("ID = %q, want %q", job.ID, "job-123")
	}
	if job.StrategyID != "momentum" {
		t.Errorf("StrategyID = %q, want %q", job.StrategyID, "momentum")
	}
	if job.Status != "completed" {
		t.Errorf("Status = %q, want %q", job.Status, "completed")
	}
	if job.StartedAt == nil {
		t.Error("StartedAt = nil, want non-nil")
	}
	if job.CompletedAt == nil {
		t.Error("CompletedAt = nil, want non-nil")
	}
}

// TestJobRecordToMap tests the JobRecord → map conversion.
func TestJobRecordToMap(t *testing.T) {
	started := time.Now().Add(-1 * time.Hour)
	rec := &JobRecord{
		ID:         "job-456",
		StrategyID: "value",
		Params:     []byte(`{"factor":"ep"}`),
		Universe:   "csi500",
		StartDate:  "2020-01-01",
		EndDate:    "2022-01-01",
		Status:     "running",
		CreatedAt:  time.Now(),
		StartedAt:   &started,
		CompletedAt: nil,
	}

	m := jobRecordToMap(rec)

	if m["id"] != "job-456" {
		t.Errorf("id = %v, want job-456", m["id"])
	}
	if m["strategy_id"] != "value" {
		t.Errorf("strategy_id = %v, want value", m["strategy_id"])
	}
	if m["status"] != "running" {
		t.Errorf("status = %v, want running", m["status"])
	}
	if m["completed_at"] != nil {
		t.Errorf("completed_at = %v, want nil", m["completed_at"])
	}
}

// TestMapToJobRecord tests the map → JobRecord conversion.
func TestMapToJobRecord(t *testing.T) {
	m := map[string]any{
		"id":           "job-789",
		"strategy_id":  "quality",
		"universe":     "all",
		"start_date":   "2021-01-01",
		"end_date":     "2023-06-01",
		"status":       "pending",
		"params":        []byte(`{}`),
		"result":        []byte(nil),
		"error_msg":     "",
		"created_at":    time.Now(),
		"started_at":    time.Time{},
		"completed_at":  time.Time{},
	}

	rec := mapToJobRecord(m)

	if rec.ID != "job-789" {
		t.Errorf("ID = %q, want job-789", rec.ID)
	}
	if rec.StrategyID != "quality" {
		t.Errorf("StrategyID = %q, want quality", rec.StrategyID)
	}
	if rec.Status != "pending" {
		t.Errorf("Status = %q, want pending", rec.Status)
	}
}

// TestJobRecordRoundTrip tests that a JobRecord survives a round trip through map.
func TestJobRecordRoundTrip(t *testing.T) {
	started := time.Now()
	completed := time.Now()
	orig := &JobRecord{
		ID:         "round-trip-test",
		StrategyID: "momentum",
		Params:     []byte(`{"lookback":20,"threshold":0.05}`),
		Universe:   "csi300",
		StartDate:  "2018-01-01",
		EndDate:    "2023-01-01",
		Status:     "completed",
		Result:     []byte(`{"total_return":0.35,"sharpe":1.2}`),
		ErrorMsg:   "",
		CreatedAt:  time.Now(),
		StartedAt:   &started,
		CompletedAt: &completed,
	}

	m := jobRecordToMap(orig)
	restored := mapToJobRecord(m)

	if restored.ID != orig.ID {
		t.Errorf("ID = %q, want %q", restored.ID, orig.ID)
	}
	if restored.StrategyID != orig.StrategyID {
		t.Errorf("StrategyID = %q, want %q", restored.StrategyID, orig.StrategyID)
	}
	if restored.Status != orig.Status {
		t.Errorf("Status = %q, want %q", restored.Status, orig.Status)
	}
	if restored.StartDate != orig.StartDate {
		t.Errorf("StartDate = %q, want %q", restored.StartDate, orig.StartDate)
	}
}
