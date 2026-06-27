package systemalert

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nopLogger returns a no-op logger for tests.
func nopLogger() zerolog.Logger { return zerolog.New(nil) }

// recordingChannel captures alerts for test assertions. It is safe for
// concurrent use.
type recordingChannel struct {
	mu       sync.Mutex
	name     string
	received []Alert
	err      error
}

func (r *recordingChannel) Send(a Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.received = append(r.received, a)
	return r.err
}

func (r *recordingChannel) Name() string { return r.name }

func (r *recordingChannel) Snapshot() []Alert {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Alert, len(r.received))
	copy(out, r.received)
	return out
}

func TestAlertManager_Fire(t *testing.T) {
	am := NewAlertManager(nopLogger())
	rc := &recordingChannel{name: "recorder"}
	am.AddChannel(rc)

	a := Alert{
		ID:       "A1",
		Type:     AlertBacktestFailure,
		Severity: SeverityWarning,
		Title:    "backtest failed",
		Message:  "engine returned error",
		Source:   "backtest-engine",
	}
	require.NoError(t, am.Fire(a))

	got := rc.Snapshot()
	require.Len(t, got, 1)
	assert.Equal(t, "A1", got[0].ID)
	assert.Equal(t, AlertBacktestFailure, got[0].Type)
	// Timestamp auto-filled when zero.
	assert.False(t, got[0].Timestamp.IsZero())

	// History should reflect the fired alert.
	hist := am.GetHistory(10)
	require.Len(t, hist, 1)
	assert.Equal(t, "A1", hist[0].ID)
}

func TestAlertManager_Cooldown(t *testing.T) {
	am := NewAlertManager(nopLogger())
	rc := &recordingChannel{name: "recorder"}
	am.AddChannel(rc)
	am.AddRule(AlertRule{
		Name:     "bt-cooldown",
		Type:     AlertBacktestFailure,
		Cooldown: 200 * time.Millisecond,
	})

	a := Alert{ID: "A1", Type: AlertBacktestFailure, Severity: SeverityWarning, Title: "fail", Message: "m", Source: "s"}
	// First fire: delivered.
	require.NoError(t, am.Fire(a))
	require.Len(t, rc.Snapshot(), 1)

	// Second fire immediately: suppressed by cooldown.
	require.NoError(t, am.Fire(a))
	require.Len(t, rc.Snapshot(), 1, "second fire should be suppressed by cooldown")

	// Wait past cooldown.
	time.Sleep(250 * time.Millisecond)
	require.NoError(t, am.Fire(a))
	require.Len(t, rc.Snapshot(), 2, "third fire after cooldown should be delivered")
}

func TestAlertManager_AddChannel(t *testing.T) {
	am := NewAlertManager(nopLogger())
	rc1 := &recordingChannel{name: "r1"}
	rc2 := &recordingChannel{name: "r2"}
	am.AddChannel(rc1)
	am.AddChannel(rc2)

	require.NoError(t, am.Fire(Alert{
		ID: "A", Type: AlertSystemError, Severity: SeverityCritical,
		Title: "t", Message: "m", Source: "s",
	}))
	assert.Len(t, rc1.Snapshot(), 1)
	assert.Len(t, rc2.Snapshot(), 1)
}

func TestAlertManager_GetHistory(t *testing.T) {
	am := NewAlertManager(nopLogger())
	am.AddChannel(&recordingChannel{name: "r"})
	ids := []string{"A", "B", "C", "D", "E"}
	for _, id := range ids {
		require.NoError(t, am.Fire(Alert{
			ID: id, Type: AlertSystemError, Severity: SeverityInfo,
			Title: "t", Message: "m", Source: "s",
		}))
	}
	// limit < total returns most recent N.
	hist := am.GetHistory(3)
	require.Len(t, hist, 3)
	assert.Equal(t, "C", hist[0].ID)
	assert.Equal(t, "E", hist[2].ID)
	// limit <= 0 returns all.
	all := am.GetHistory(0)
	require.Len(t, all, 5)
}

func TestLogChannel_Send(t *testing.T) {
	// LogChannel never errors; just ensure it doesn't panic and returns nil.
	lc := NewLogChannel(nopLogger())
	a := Alert{
		ID: "L1", Type: AlertDataSyncStall, Severity: SeverityCritical,
		Title: "stall", Message: "sync stalled", Source: "data-service",
		Metadata: map[string]string{"lag": "120s"},
	}
	require.NoError(t, lc.Send(a))
	assert.Equal(t, "log", lc.Name())

	// Exercise warning and info severity branches too.
	require.NoError(t, lc.Send(Alert{ID: "L2", Type: AlertSystemError, Severity: SeverityWarning, Title: "w", Message: "m", Source: "s"}))
	require.NoError(t, lc.Send(Alert{ID: "L3", Type: AlertBacktestFailure, Severity: SeverityInfo, Title: "i", Message: "m", Source: "s"}))
}

func TestWebhookChannel_Send(t *testing.T) {
	var (
		mu       sync.Mutex
		gotBody  []byte
		gotType  string
		gotSev   string
		called   int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		called++
		gotBody, _ = io.ReadAll(r.Body)
		gotType = r.Header.Get("X-Alert-Type")
		gotSev = r.Header.Get("X-Alert-Severity")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wc := NewWebhookChannel(srv.URL, time.Second, nopLogger())
	require.Equal(t, "webhook", wc.Name())

	a := Alert{
		ID: "W1", Type: AlertStrategyDegradation, Severity: SeverityWarning,
		Title: "degraded", Message: "IC dropped", Source: "live-trader",
		Metadata: map[string]string{"ic": "0.02"},
	}
	require.NoError(t, wc.Send(a))

	mu.Lock()
	require.Equal(t, 1, called, "webhook should be called exactly once")
	assert.Equal(t, "strategy_degradation", gotType)
	assert.Equal(t, "warning", gotSev)
	var decoded Alert
	require.NoError(t, json.Unmarshal(gotBody, &decoded))
	assert.Equal(t, "W1", decoded.ID)
	assert.Equal(t, AlertStrategyDegradation, decoded.Type)
	assert.Equal(t, "0.02", decoded.Metadata["ic"])
	mu.Unlock()

	// Non-2xx response surfaces an error.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errSrv.Close()
	wcErr := NewWebhookChannel(errSrv.URL, time.Second, nopLogger())
	err := wcErr.Send(a)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-2xx")
}

func TestAlertManager_Concurrent(t *testing.T) {
	am := NewAlertManager(nopLogger())
	rc := &recordingChannel{name: "r"}
	am.AddChannel(rc)
	// No cooldown rule — every fire should be delivered.
	var wg sync.WaitGroup
	var sent int64
	const goroutines = 20
	const perG = 50
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				if err := am.Fire(Alert{
					ID: "c", Type: AlertSystemError, Severity: SeverityInfo,
					Title: "t", Message: "m", Source: "s",
				}); err == nil {
					atomic.AddInt64(&sent, 1)
				}
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(goroutines*perG), sent)
	assert.Len(t, rc.Snapshot(), goroutines*perG)
	assert.Len(t, am.GetHistory(0), goroutines*perG)
}

func TestAlertManager_ConditionSuppresses(t *testing.T) {
	// Bonus: a rule whose Condition returns false suppresses the alert.
	am := NewAlertManager(nopLogger())
	rc := &recordingChannel{name: "r"}
	am.AddChannel(rc)
	am.AddRule(AlertRule{
		Name:      "only-critical",
		Type:      AlertSystemError,
		Condition: func(v interface{}) bool { return v.(Alert).Severity == SeverityCritical },
	})

	// Warning severity: condition fails → suppressed.
	require.NoError(t, am.Fire(Alert{
		ID: "A", Type: AlertSystemError, Severity: SeverityWarning,
		Title: "t", Message: "m", Source: "s",
	}))
	assert.Empty(t, rc.Snapshot(), "warning should be suppressed by condition")
	assert.Empty(t, am.GetHistory(0), "suppressed alert should not be recorded")

	// Critical severity: condition passes → delivered.
	require.NoError(t, am.Fire(Alert{
		ID: "B", Type: AlertSystemError, Severity: SeverityCritical,
		Title: "t", Message: "m", Source: "s",
	}))
	require.Len(t, rc.Snapshot(), 1)
	require.Len(t, am.GetHistory(0), 1)
}
