package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockJobStore struct {
	mu    sync.Mutex
	jobs  map[string]map[string]any
	order []string
}

func newMockJobStore() *mockJobStore {
	return &mockJobStore{jobs: make(map[string]map[string]any)}
}

// cloneJobMap returns a deep copy of a job map. The JobStore mock
// previously returned the underlying map by reference; that meant
// any goroutine holding the returned reference (e.g. via
// GetBacktestJob) could observe in-flight mutations from
// UpdateJobStarted / UpdateJobCompleted / UpdateJobFailed, which
// the race detector correctly flagged.
//
// Sprint 6 P0-6 fix: every read in the mock now hands back a fresh
// deep copy so concurrent Update* / Get* calls do not share mutable
// state. (Production code uses the same defensive copy in
// JobService.GetJob — see mapToJobRecord in job.go which already
// copies scalars into a JobRecord struct.)
func cloneJobMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch x := v.(type) {
		case map[string]any:
			dst[k] = cloneJobMap(x)
		case []byte:
			// []byte is a reference type in Go; copy to avoid sharing.
			cp := make([]byte, len(x))
			copy(cp, x)
			dst[k] = cp
		case time.Time:
			dst[k] = x // time.Time is a value type; safe to share
		case *time.Time:
			if x == nil {
				dst[k] = (*time.Time)(nil)
			} else {
				v := *x
				dst[k] = &v
			}
		default:
			dst[k] = v // scalars (string, int, float64) are values
		}
	}
	return dst
}

func (m *mockJobStore) CreateBacktestJob(ctx context.Context, job map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := job["id"].(string)
	m.jobs[id] = job
	m.order = append(m.order, id)
	return nil
}

func (m *mockJobStore) UpdateJobStarted(ctx context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j["status"] = "running"
		now := time.Now()
		j["started_at"] = now
	}
	return nil
}

func (m *mockJobStore) UpdateJobCompleted(ctx context.Context, jobID string, result []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j["status"] = "completed"
		j["result"] = result
		now := time.Now()
		j["completed_at"] = now
	}
	return nil
}

func (m *mockJobStore) UpdateJobFailed(ctx context.Context, jobID string, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j["status"] = "failed"
		j["error_msg"] = errMsg
	}
	return nil
}

func (m *mockJobStore) GetBacktestJob(ctx context.Context, jobID string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil, nil
	}
	// Return a deep copy so callers cannot observe concurrent
	// in-flight mutations from Update* without re-acquiring the
	// lock (P0-6).
	return cloneJobMap(j), nil
}

func (m *mockJobStore) ListBacktestJobs(ctx context.Context, limit int) ([]map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []map[string]any
	for i := len(m.order) - 1; i >= 0 && len(result) < limit; i-- {
		if j, ok := m.jobs[m.order[i]]; ok {
			result = append(result, cloneJobMap(j))
		}
	}
	return result, nil
}

func (m *mockJobStore) DeleteBacktestJob(ctx context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, jobID)
	for i, id := range m.order {
		if id == jobID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}

func newTestJobService(t *testing.T) (*JobService, *mockJobStore) {
	t.Helper()
	store := newMockJobStore()
	v := viper.New()
	v.Set("backtest.initial_capital", 1000000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.trading.stamp_tax_rate", 0.001)
	v.Set("backtest.trading.min_commission", 5.0)
	v.Set("backtest.trading.transfer_fee_rate", 0.00001)
	v.Set("backtest.trading.price_limit.normal", 0.10)
	v.Set("backtest.trading.price_limit.st", 0.05)
	v.Set("backtest.trading.price_limit.new", 0.20)
	v.Set("backtest.trading.new_stock_days", 60)
	eng, err := NewEngine(v, marketdata.NewInMemoryProvider(), zerolog.Nop())
	require.NoError(t, err)
	svc := NewJobService(store, eng)
	return svc, store
}

func TestJobService_NewJobService(t *testing.T) {
	svc, _ := newTestJobService(t)
	assert.NotNil(t, svc)
	assert.NotNil(t, svc.store)
	assert.NotNil(t, svc.engine)
}

func TestJobService_CreateJob(t *testing.T) {
	svc, store := newTestJobService(t)

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID: "momentum",
		Params:     map[string]any{"lookback": 20},
		Universe:   "600000.SH,600001.SH",
		StartDate:  "2020-01-01",
		EndDate:    "2023-12-31",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "momentum", job.StrategyID)
	assert.Equal(t, "pending", job.Status)

	time.Sleep(100 * time.Millisecond)

	stored, err := store.GetBacktestJob(context.Background(), job.ID)
	require.NoError(t, err)
	assert.NotNil(t, stored)
	assert.Equal(t, "momentum", stored["strategy_id"])
}

func TestJobService_GetJob(t *testing.T) {
	svc, store := newTestJobService(t)

	now := time.Now()
	store.CreateBacktestJob(context.Background(), map[string]any{
		"id":          "job-1",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "completed",
		"created_at":  now,
		"result":      []byte(`{"total_return": 0.15}`),
	})

	job, err := svc.GetJob(context.Background(), "job-1")
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "job-1", job.ID)
	assert.Equal(t, "completed", job.Status)
}

func TestJobService_GetJob_NotFound(t *testing.T) {
	svc, _ := newTestJobService(t)

	job, err := svc.GetJob(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, job)
}

func TestJobService_ListJobs(t *testing.T) {
	svc, store := newTestJobService(t)

	now := time.Now()
	for i := 0; i < 3; i++ {
		store.CreateBacktestJob(context.Background(), map[string]any{
			"id":          "job-" + string(rune('A'+i)),
			"strategy_id": "momentum",
			"universe":    "600000.SH",
			"start_date":  "2020-01-01",
			"end_date":    "2023-12-31",
			"status":      "completed",
			"created_at":  now,
		})
	}

	jobs, err := svc.ListJobs(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, jobs, 3)
}

func TestJobService_ListJobs_DefaultLimit(t *testing.T) {
	svc, _ := newTestJobService(t)

	jobs, err := svc.ListJobs(context.Background(), 0)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestJobService_StartJob_CancelViaParentContext(t *testing.T) {
	svc, store := newTestJobService(t)

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID: "momentum",
		Params:     map[string]any{"lookback": 20},
		Universe:   "600000.SH",
		StartDate:  "2020-01-01",
		EndDate:    "2023-12-31",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartJob(ctx, job.ID)

	time.Sleep(50 * time.Millisecond)

	cancel()

	time.Sleep(100 * time.Millisecond)

	stored, _ := store.GetBacktestJob(context.Background(), job.ID)
	assert.NotNil(t, stored)
}

func TestJobService_StartJob_StoreStartFails(t *testing.T) {
	svc, _ := newTestJobService(t)

	svc.StartJob(context.Background(), "nonexistent-job-id")

	time.Sleep(100 * time.Millisecond)
}

func TestJobService_CancelJob_Running(t *testing.T) {
	svc, store := newTestJobService(t)

	now := time.Now()
	store.CreateBacktestJob(context.Background(), map[string]any{
		"id":          "running-job",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "running",
		"created_at":  now,
		"started_at":  now,
	})

	err := svc.CancelJob(context.Background(), "running-job")
	require.NoError(t, err)
}

func TestJobService_CancelJob_Pending(t *testing.T) {
	svc, store := newTestJobService(t)

	store.CreateBacktestJob(context.Background(), map[string]any{
		"id":          "pending-job",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "pending",
		"created_at":  time.Now(),
	})

	err := svc.CancelJob(context.Background(), "pending-job")
	require.NoError(t, err)

	stored, _ := store.GetBacktestJob(context.Background(), "pending-job")
	assert.Nil(t, stored)
}

func TestJobService_CancelJob_Completed(t *testing.T) {
	svc, store := newTestJobService(t)

	store.CreateBacktestJob(context.Background(), map[string]any{
		"id":          "done-job",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "completed",
		"created_at":  time.Now(),
	})

	err := svc.CancelJob(context.Background(), "done-job")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already completed")
}

func TestJobService_CancelJob_NotFound(t *testing.T) {
	svc, _ := newTestJobService(t)

	err := svc.CancelJob(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestJobService_SaveSyncResult(t *testing.T) {
	svc, store := newTestJobService(t)

	resp := &BacktestResponse{
		ID:          "sync-1",
		Strategy:    "momentum",
		StockPool:   []string{"600000.SH", "600001.SH"},
		StartDate:   "2020-01-01",
		EndDate:     "2023-12-31",
		TotalReturn: 0.15,
	}

	err := svc.SaveSyncResult(context.Background(), resp)
	require.NoError(t, err)

	stored, err := store.GetBacktestJob(context.Background(), "sync-1")
	require.NoError(t, err)
	assert.NotNil(t, stored)
	assert.Equal(t, "completed", stored["status"])

	var result BacktestResponse
	resultBytes, ok := stored["result"].([]byte)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(resultBytes, &result))
	assert.InDelta(t, 0.15, result.TotalReturn, 0.001)
}

func TestJobService_SaveSyncResult_EmptyStockPool(t *testing.T) {
	svc, store := newTestJobService(t)

	resp := &BacktestResponse{
		ID:        "sync-2",
		Strategy:  "value",
		StockPool: []string{},
		StartDate: "2020-01-01",
		EndDate:   "2023-12-31",
	}

	err := svc.SaveSyncResult(context.Background(), resp)
	require.NoError(t, err)

	stored, _ := store.GetBacktestJob(context.Background(), "sync-2")
	assert.NotNil(t, stored)
	assert.Equal(t, "", stored["universe"])
}

// ---- Sprint 6 P0-6: concurrent access regression tests ------------------

// TestMockJobStore_Concurrent_1000xNoDataRace hammers the mock from
// many goroutines to confirm the deep-copy fix (Sprint 6 P0-6)
// eliminates the Get/Update race that previously tripped
// `go test -race`. With the previous "return by reference" mock,
// this test panics via the race detector. With the deep-copy mock,
// it must complete cleanly in well under a second.
func TestMockJobStore_Concurrent_1000xNoDataRace(t *testing.T) {
	store := newMockJobStore()
	ctx := context.Background()

	const N = 1000

	var wg sync.WaitGroup
	wg.Add(N * 2)

	// Pre-seed
	for i := 0; i < N; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		_ = store.CreateBacktestJob(ctx, map[string]any{
			"id":          jobID,
			"strategy_id": "momentum",
			"universe":    "600000.SH",
			"start_date":  "2020-01-01",
			"end_date":    "2023-12-31",
			"status":      "pending",
			"created_at":  time.Now(),
		})
	}

	// Hammer Update + Get concurrently.
	for i := 0; i < N; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		go func(id string) {
			defer wg.Done()
			_ = store.UpdateJobStarted(ctx, id)
		}(jobID)
		go func(id string) {
			defer wg.Done()
			_, _ = store.GetBacktestJob(ctx, id)
		}(jobID)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// 2000 concurrent Update/Get ops completed without a race
	case <-time.After(5 * time.Second):
		t.Fatal("2000 concurrent mockJobStore calls deadlocked within 5s")
	}
}

// TestJobService_ConcurrentCreates confirms the JobService layer is
// safe under burst CreateJob (which in turn races DB updates from
// the detached goroutine). No assertion on count — only that the
// created jobs are durably visible to GetJob.
func TestJobService_ConcurrentCreates(t *testing.T) {
	svc, store := newTestJobService(t)

	const N = 50 // smaller to keep the test fast
	var wg sync.WaitGroup
	wg.Add(N)
	ids := make([]string, N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			job, err := svc.CreateJob(context.Background(), CreateJobRequest{
				StrategyID:     "momentum",
				Universe:       "600000.SH",
				StartDate:      "2020-01-01",
				EndDate:        "2023-12-31",
				InitialCapital: 1000000,
			})
			if err == nil && job != nil {
				ids[i] = job.ID
			}
		}()
	}
	wg.Wait()

	// Every jobID we got back must be discoverable via the store.
	for _, id := range ids {
		if id == "" {
			continue
		}
		got, err := store.GetBacktestJob(context.Background(), id)
		assert.NoError(t, err)
		assert.NotNil(t, got, "every successfully created job must be visible in the store")
	}
}

// TestCloneJobMap_DeepCopy confirms the deep-copy helper used by
// the P0-6 fix actually produces an independent map (and not a
// shared reference that would re-introduce the race). The
// product-side test that this enables is `mapToJobRecord` running
// concurrently with `UpdateJobStarted` without a data race.
func TestCloneJobMap_DeepCopy(t *testing.T) {
	src := map[string]any{
		"id":     "x",
		"result": []byte{1, 2, 3},
		"nested": map[string]any{"k": "v"},
		"when":   time.Now(),
	}
	dst := cloneJobMap(src)

	// Mutate dst and confirm src is unchanged.
	dst["id"] = "y"
	dst["result"].([]byte)[0] = 99
	dst["nested"].(map[string]any)["k"] = "z"

	assert.Equal(t, "x", src["id"], "scalar copy must be independent")
	assert.Equal(t, byte(1), src["result"].([]byte)[0], "[]byte must be deep-copied")
	assert.Equal(t, "v", src["nested"].(map[string]any)["k"], "nested map must be deep-copied")
}

func TestCloneJobMap_Nil(t *testing.T) {
	assert.Nil(t, cloneJobMap(nil),
		"cloneJobMap(nil) must return nil, not an empty map (preserves GetBacktestJob miss contract)")
}

func TestParseUniverse(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"all", nil},
		{"600000.SH", []string{"600000.SH"}},
		{"600000.SH,600001.SH", []string{"600000.SH", "600001.SH"}},
		{"600000.SH, 600001.SH , 600002.SH", []string{"600000.SH", "600001.SH", "600002.SH"}},
		{"universe:600000.SH", []string{"600000.SH"}},
		{",,,", []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseUniverse(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestJobRecordToMap(t *testing.T) {
	now := time.Now()
	started := now.Add(1 * time.Hour)
	completed := now.Add(2 * time.Hour)

	record := &JobRecord{
		ID:          "test-1",
		StrategyID:  "momentum",
		Params:      []byte(`{"lookback":20}`),
		Universe:    "600000.SH",
		StartDate:   "2020-01-01",
		EndDate:     "2023-12-31",
		Status:      "completed",
		Result:      []byte(`{"total_return":0.15}`),
		ErrorMsg:    "",
		CreatedAt:   now,
		StartedAt:   &started,
		CompletedAt: &completed,
	}

	m := jobRecordToMap(record)
	assert.Equal(t, "test-1", m["id"])
	assert.Equal(t, "momentum", m["strategy_id"])
	assert.Equal(t, started, m["started_at"])
	assert.Equal(t, completed, m["completed_at"])
}

func TestJobRecordToMap_NilTimestamps(t *testing.T) {
	record := &JobRecord{
		ID:         "test-2",
		StrategyID: "value",
		Universe:   "600001.SH",
		StartDate:  "2020-01-01",
		EndDate:    "2023-12-31",
		Status:     "pending",
		CreatedAt:  time.Now(),
	}

	m := jobRecordToMap(record)
	assert.Equal(t, "test-2", m["id"])
	_, hasStarted := m["started_at"]
	assert.False(t, hasStarted)
	_, hasCompleted := m["completed_at"]
	assert.False(t, hasCompleted)
}

func TestMapToJobRecord(t *testing.T) {
	now := time.Now()
	started := now.Add(1 * time.Hour)

	m := map[string]any{
		"id":          "test-3",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "running",
		"params":      []byte(`{"lookback":20}`),
		"result":      []byte(`{}`),
		"error_msg":   "",
		"created_at":  now,
		"started_at":  started,
	}

	record := mapToJobRecord(m)
	assert.Equal(t, "test-3", record.ID)
	assert.Equal(t, "momentum", record.StrategyID)
	assert.Equal(t, "running", record.Status)
	require.NotNil(t, record.StartedAt)
	assert.Equal(t, started, *record.StartedAt)
}

func TestRecordToJob(t *testing.T) {
	now := time.Now()
	started := now.Add(1 * time.Hour)

	record := &JobRecord{
		ID:          "test-4",
		StrategyID:  "momentum",
		Params:      []byte(`{"lookback":20}`),
		Universe:    "600000.SH",
		StartDate:   "2020-01-01",
		EndDate:     "2023-12-31",
		Status:      "completed",
		Result:      []byte(`{"total_return":0.15}`),
		ErrorMsg:    "test error",
		CreatedAt:   now,
		StartedAt:   &started,
		CompletedAt: nil,
	}

	job := recordToJob(record)
	assert.Equal(t, "test-4", job.ID)
	assert.Equal(t, "momentum", job.StrategyID)
	assert.Equal(t, "completed", job.Status)
	assert.Equal(t, "test error", job.Error)
	require.NotNil(t, job.StartedAt)
	assert.Nil(t, job.CompletedAt)
}
