//go:build stress

// K13-R1 增项 · bare-link stress harness (mock NT).
//
// Local, one-off, **not in CI** (build tag “stress“). It hammers the Go
// Runner against a mock sidecar that reproduces the four "bare link" hazards the
// task brief names, and asserts the fail-closed invariant holds under load:
//
//	顺序成交    the mock issues strictly increasing fill ids per request
//	响应可丢    ~10% of responses are dropped by hijacking and closing the socket
//	不落库      the mock has no database (it is a mock; nothing is persisted)
//	可伴生      N workers hammer it concurrently
//
// Run (from the repo root):
//
//	go test -tags stress -run TestStressBareLink -timeout 700s ./pkg/backtest/nautilus/
//
// Budget defaults to 30s; set NT_STRESS_SECONDS to stretch it (hard-capped at
// 600s per the brief).
package nautilus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	stressDefaultSeconds = 30
	stressCapSeconds     = 600
	stressWorkers        = 8
	stressDropOneIn      = 10 // 1 in 10 responses dropped
)

func stressBudget(t *testing.T) time.Duration {
	t.Helper()
	secs := stressDefaultSeconds
	if v := strings.TrimSpace(os.Getenv("NT_STRESS_SECONDS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("NT_STRESS_SECONDS must be a positive integer, got %q", v)
		}
		secs = n
	}
	if secs > stressCapSeconds {
		t.Logf("NT_STRESS_SECONDS=%d exceeds the %ds cap; clamping", secs, stressCapSeconds)
		secs = stressCapSeconds
	}
	return time.Duration(secs) * time.Second
}

// mockNT is a minimal NautilusTrader stand-in for the bare transport link.
type mockNT struct {
	seq      atomic.Int64
	mu       sync.Mutex
	rnd      *rand.Rand
	received atomic.Int64
}

func (m *mockNT) handler(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body)
	m.received.Add(1)

	// 顺序成交: a strictly increasing fill id, assigned in arrival order.
	fill := m.seq.Add(1)

	m.mu.Lock()
	drop := m.rnd.Intn(stressDropOneIn) == 0
	m.mu.Unlock()
	if drop {
		// 响应可丢: close the socket without answering — the client must see a
		// transport error, never a synthesised success.
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":              fmt.Sprintf("stress-%d", fill),
		"status":          "completed",
		"strategy":        "buy_and_hold",
		"start_date":      "2024-01-02",
		"end_date":        "2024-06-28",
		"total_return":    0.01,
		"total_orders":    fill,
		"total_positions": fill,
		"iterations":      200,
		"timing":          map[string]float64{"queue_ms": 0.1, "exec_ms": 1.0, "total_ms": 1.1},
	})
}

func TestStressBareLink(t *testing.T) {
	if testing.Short() {
		t.Skip("stress harness is skipped in -short mode")
	}
	// Deterministic drop pattern so a failure is reproducible.
	mock := &mockNT{rnd: rand.New(rand.NewSource(20261012))}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	runner := NewRunner(srv.URL, &http.Client{Timeout: 5 * time.Second})
	budget := stressBudget(t)
	deadline := time.Now().Add(budget)

	var ok, failed, badInvariant atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < stressWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				resp, err := runner.RunBacktest(context.Background(), sampleRequest())
				switch {
				case err == nil && resp != nil:
					ok.Add(1)
				case err != nil && resp == nil:
					failed.Add(1)
				default:
					// The only forbidden outcome: a response alongside an error,
					// i.e. a silent fallback / synthesised success (FR-1.3).
					badInvariant.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	t.Logf("stress bare link: ok=%d failed(≈dropped)=%d bad=%d received=%d over %s",
		ok.Load(), failed.Load(), badInvariant.Load(), mock.received.Load(), budget)

	if badInvariant.Load() != 0 {
		t.Fatalf("fail-closed invariant broken: %d calls returned a response *and* an error",
			badInvariant.Load())
	}
	if ok.Load() == 0 {
		t.Fatalf("the bare link never completed a run in %s", budget)
	}
	if failed.Load() == 0 {
		t.Fatalf("no dropped-response path was exercised — the mock drop rate is not taking effect")
	}
	if mock.received.Load() == 0 {
		t.Fatalf("the mock received no requests")
	}
}
