// K13 · NautilusRunner unit tests — the Go half of the RunSpec link.
//
// These run against a stub sidecar (httptest) so they need neither
// NautilusTrader nor Python. The "no silent fallback" property is asserted in
// every failure case: a failed call must return (nil, err) — never a
// synthesised success.
//
//破坏验证（开发期实测）：把 RunBacktest 里 resp.StatusCode 的检查改成
// 「忽略非 2xx，返回一个 completed 的空 response」，TestRunner_Non2xxSurfacesUnavailable
// 与 TestRunner_StatusFailedSurfacesUnavailable 立刻红（已还原）。

package nautilus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

// capturedRequest records what the stub sidecar received.
type capturedRequest struct {
	mu     sync.Mutex
	method string
	path   string
	ctype  string
	body   map[string]any
}

func (c *capturedRequest) record(r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.method = r.Method
	c.path = r.URL.Path
	c.ctype = r.Header.Get("Content-Type")
	c.body = decoded
}

func (c *capturedRequest) snapshot() (string, string, string, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.path, c.ctype, c.body
}

// newStubSidecar starts a stub sidecar that always answers with the given
// status code and raw body, capturing the request it received.
func newStubSidecar(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func appErrorCode(t *testing.T, err error) apperrors.ErrorCode {
	t.Helper()
	var appErr *apperrors.AppError
	require.Error(t, err)
	require.True(t, errors.As(err, &appErr), "error must be an AppError, got %T: %v", err, err)
	return appErr.Code
}

func sampleRequest() contracts.BacktestRequest {
	return contracts.BacktestRequest{
		Strategy:       "buy_and_hold",
		StockPool:      []string{"600000.SH", "000001.SZ"},
		StartDate:      "2024-01-02",
		EndDate:        "2024-06-28",
		InitialCapital: 1_000_000,
		Kernel:         KernelName,
	}
}

// ── positive path ────────────────────────────────────────────────────────────

func TestRunner_MapsCompletedResult(t *testing.T) {
	const completed = `{
		"id": "nt-run-1",
		"status": "completed",
		"strategy": "buy_and_hold",
		"start_date": "2024-01-02",
		"end_date": "2024-06-28",
		"initial_capital": 1000000,
		"total_return": 0.0125,
		"sharpe_ratio": 1.75,
		"sortino_ratio": 2.5,
		"total_orders": 2,
		"total_positions": 2,
		"iterations": 240
	}`
	srv, cap := newStubSidecar(t, http.StatusOK, completed)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)

	// The result carries NT's own numbers — proof the value came through the
	// sidecar rather than from a zero-valued struct.
	assert.Equal(t, "nt-run-1", resp.ID)
	assert.Equal(t, contracts.StatusCompleted, resp.Status)
	assert.Equal(t, "buy_and_hold", resp.Strategy)
	assert.InDelta(t, 0.0125, resp.TotalReturn, 1e-9)
	assert.InDelta(t, 1.75, resp.SharpeRatio, 1e-9)
	assert.InDelta(t, 2.5, resp.SortinoRatio, 1e-9)
	assert.Equal(t, []string{"600000.SH", "000001.SZ"}, resp.StockPool)

	method, path, ctype, body := cap.snapshot()
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, RunPath, path)
	assert.Equal(t, "application/json", ctype)
	assert.Equal(t, "buy_and_hold", body["strategy"])
	assert.Equal(t, KernelName, body["kernel"])
	assert.Equal(t, "2024-01-02", body["start_date"])
	assert.Equal(t, "2024-06-28", body["end_date"])
	assert.Equal(t, []any{"600000.SH", "000001.SZ"}, body["stock_pool"])
}

func TestRunner_DefaultsKernelToGoWhenUnset(t *testing.T) {
	// EffectiveKernel resolves an empty Kernel to "go" before the wire payload is
	// built, so a caller reaching the registry with no kernel still sends an
	// explicit value on the wire.
	srv, cap := newStubSidecar(t, http.StatusOK, `{"id":"x","status":"completed"}`)
	runner := NewRunner(srv.URL, srv.Client())

	req := sampleRequest()
	req.Kernel = ""
	_, err := runner.RunBacktest(context.Background(), req)
	require.NoError(t, err)

	_, _, _, body := cap.snapshot()
	assert.Equal(t, contracts.DefaultKernel, body["kernel"])
}

// ── failure paths: every one must surface an error and a nil response ─────────

func TestRunner_Non2xxSurfacesUnavailable(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusInternalServerError,
		`{"status":"failed","error":"nautilus run failed: feed exhausted"}`)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp, "a failed run must not return a response (no silent fallback)")
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), "feed exhausted", "the sidecar's reason must be carried through")
}

func TestRunner_StatusFailedSurfacesUnavailable(t *testing.T) {
	// A 200 body can still be a failed outcome (OBS-01: failed is an outcome,
	// not a transport state). It must not be returned as success.
	srv, _ := newStubSidecar(t, http.StatusOK,
		`{"id":"nt-run-1","status":"failed","error":"catalog has no instrument for [999999.SZ]"}`)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp, "status=failed must not be reported as success")
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), "999999.SZ")
}

func TestRunner_UnreachableSidecarSurfacesUnavailable(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusOK, `{}`)
	url := srv.URL
	srv.Close() // sidecar is down

	runner := NewRunner(url, &http.Client{Timeout: 5 * time.Second})
	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp, "an unreachable kernel must not produce a response")
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), KernelName, "the error must name the kernel that did not run")
}

func TestRunner_DeadlineSurfacesTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // outlives the context deadline below
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	runner := NewRunner(srv.URL, srv.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	resp, err := runner.RunBacktest(ctx, sampleRequest())

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeTimeout, appErrorCode(t, err))
}

func TestRunner_MalformedResponseSurfacesInternal(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusOK, "not json at all")
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeInternal, appErrorCode(t, err))
}

func TestRunner_NonJSONFailureBodyStillYieldsReason(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusBadGateway, "<html>nginx</html>")
	runner := NewRunner(srv.URL, srv.Client())

	_, err := runner.RunBacktest(context.Background(), sampleRequest())
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), "502")
	assert.Contains(t, err.Error(), "nginx")
}

// ── assembly helpers ─────────────────────────────────────────────────────────

func TestURLFromEnv(t *testing.T) {
	t.Setenv(URLEnvVar, "")
	assert.Equal(t, DefaultURL, URLFromEnv())

	t.Setenv(URLEnvVar, "  http://127.0.0.1:9999  ")
	assert.Equal(t, "http://127.0.0.1:9999", URLFromEnv())
}

func TestNewRunnerTrimsTrailingSlash(t *testing.T) {
	runner := NewRunner("http://127.0.0.1:9100/", nil)
	assert.Equal(t, "http://127.0.0.1:9100", runner.baseURL)
	assert.Equal(t, DefaultURL, "http://127.0.0.1:9100")
}

func TestSidecarErrorPrefersJSONErrorField(t *testing.T) {
	assert.Equal(t, "boom", sidecarError([]byte(`{"error":"boom"}`)))
	assert.Equal(t, "(empty response body)", sidecarError(nil))
	assert.True(t, strings.Contains(sidecarError([]byte("raw text")), "raw text"))
}
