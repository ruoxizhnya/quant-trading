// K13 · NautilusRunner unit tests — the Go half of the RunSpec link.
// K13-R1 adds the transport-hardening cases: wire null vs zero, empty 2xx body,
// 503 "kernel busy" + Retry-After, and deadline phase attribution.
//
// These run against a stub sidecar (httptest) so they need neither
// NautilusTrader nor Python. The "no silent fallback" property is asserted in
// every failure case: a failed call must return (nil, err) — never a
// synthesised success.
//
//破坏验证（开发期实测）：把 RunBacktest 里 resp.StatusCode 的检查改成
// 「忽略非 2xx，返回一个 completed 的空 response」，TestRunner_Non2xxSurfacesUnavailable
// 与 TestRunner_StatusFailedSurfacesUnavailable 立刻红（已还原）。
//   - K13-R1：把 runResponse 的 *float64 改回 float64 →
//     TestRunResponse_DistinguishesNullFromZero 红（null 重新被吞成 0）。

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
	srv, cap := newStubSidecar(t, http.StatusOK, `{"id":"x","status":"completed","total_return":0}`)
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
	assert.Contains(t, err.Error(), KernelName, "the error must name the kernel that did not run")
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
	assert.Contains(t, err.Error(), KernelName)
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

// K16 ④：生产路由开关。默认关（go-only）；显式值解析；打错字 fail-loud。
func TestKernelEnabledFromEnv(t *testing.T) {
	// 默认关：缺省/空 = 不注册 nautilus（生产 go-only）。
	for _, unset := range []string{"", "  ", "0", "false", "no", "off", "OFF", "False"} {
		t.Setenv(EnableEnvVar, unset)
		got, err := KernelEnabledFromEnv()
		assert.NoError(t, err, "value %q", unset)
		assert.False(t, got, "value %q 应解析为关（默认 go-only）", unset)
	}
	// 显式开启。
	for _, on := range []string{"1", "true", "yes", "on", "TRUE", " On "} {
		t.Setenv(EnableEnvVar, on)
		got, err := KernelEnabledFromEnv()
		assert.NoError(t, err, "value %q", on)
		assert.True(t, got, "value %q 应解析为开", on)
	}
	// 打错字必须报错（装配期炸），不许静默当关闭。
	for _, bad := range []string{"2", "ture", "enable", "y", "on-please"} {
		t.Setenv(EnableEnvVar, bad)
		_, err := KernelEnabledFromEnv()
		assert.Error(t, err, "value %q 必须被拒绝而非静默当关闭", bad)
	}
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

// ── K13-R1 · transport hardening ─────────────────────────────────────────────

// newStubSidecarHeaders is newStubSidecar with extra response headers (e.g.
// Retry-After on a 503).
func newStubSidecarHeaders(t *testing.T, status int, body string, headers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// D · the wire layer must keep "not reported" (null) distinct from a real zero.
func TestRunResponse_DistinguishesNullFromZero(t *testing.T) {
	var out runResponse
	require.NoError(t, json.Unmarshal(
		[]byte(`{"status":"completed","total_return":null,"sharpe_ratio":0}`), &out))

	assert.Nil(t, out.TotalReturn, "a wire null must stay absent, not become 0")
	require.NotNil(t, out.SharpeRatio, "a real 0 must stay present")
	assert.InDelta(t, 0.0, *out.SharpeRatio, 1e-12)
}

// A · a "completed" run that omits total_return is a contract violation: it must
// error (naming the kernel), never be folded into a successful TotalReturn=0.
// (This replaces the K13-R1 test that pinned the fold — that behaviour was the
// silent-wrong-value bug K13-R1-R1 A closes.)
func TestRunner_CompletedWithoutTotalReturnSurfacesError(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusOK,
		`{"id":"nt-run-1","status":"completed","total_return":null,"sharpe_ratio":null}`)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp, "a completed run with no total_return must not become a zeroed success")
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeInternal, appErrorCode(t, err))
	assert.Contains(t, err.Error(), KernelName, "the error must name the kernel")
	assert.Contains(t, err.Error(), "metric not reported")
}

// Observable metrics other than total_return may still legitimately be absent —
// only total_return is required for a completed run.
func TestRunner_CompletedWithTotalReturnAllowsAbsentSecondaryMetrics(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusOK,
		`{"id":"nt-run-1","status":"completed","total_return":0.0125,"sharpe_ratio":null}`)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.InDelta(t, 0.0125, resp.TotalReturn, 1e-9)
	assert.InDelta(t, 0.0, resp.SharpeRatio, 1e-12)
}

// C · an oversized response body must be refused, not buffered without bound.
func TestRunner_OversizedResponseBodySurfacesError(t *testing.T) {
	old := maxResponseBody
	maxResponseBody = 128
	defer func() { maxResponseBody = old }()

	big := `{"id":"x","status":"completed","total_return":0,"padding":"` +
		strings.Repeat("x", 512) + `"}`
	srv, _ := newStubSidecar(t, http.StatusOK, big)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp, "an oversized body must not be buffered into a response")
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), "exceeds")
}

// A 2xx with an empty body carries no result — refuse it, name the kernel.
func TestRunner_Empty2xxBodySurfacesUnavailableNamingKernel(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusNoContent, "")
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp, "an empty 2xx body must not be mapped to a zeroed success")
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), KernelName)
	assert.Contains(t, err.Error(), "empty body")
}

// 503 is explicitly classified as "kernel busy" with the Retry-After hint.
func TestRunner_KernelBusy503ParsesRetryAfter(t *testing.T) {
	srv := newStubSidecarHeaders(t, http.StatusServiceUnavailable,
		`{"status":"failed","error":"run lock held"}`,
		map[string]string{"Retry-After": "7"})
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())

	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeUnavailable, appErrorCode(t, err))
	assert.Contains(t, err.Error(), "busy")
	assert.Contains(t, err.Error(), "Retry-After=7s")
	assert.Contains(t, err.Error(), KernelName)
	assert.Contains(t, err.Error(), "run lock held")
}

func TestRunner_KernelBusy503KeepsHTTPDateRetryAfter(t *testing.T) {
	srv := newStubSidecarHeaders(t, http.StatusServiceUnavailable, "",
		map[string]string{"Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT"})
	runner := NewRunner(srv.URL, srv.Client())

	_, err := runner.RunBacktest(context.Background(), sampleRequest())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Retry-After=Wed, 21 Oct 2026 07:28:00 GMT")
}

// The deadline error must attribute the timeout to a phase and name the kernel.
func TestRunner_DeadlineNamesPhaseAndKernel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	runner := NewRunner(srv.URL, srv.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := runner.RunBacktest(ctx, sampleRequest())
	require.Error(t, err)
	assert.Equal(t, apperrors.ErrCodeTimeout, appErrorCode(t, err))
	assert.Contains(t, err.Error(), KernelName)
	assert.Contains(t, err.Error(), "Call+Queue")
}

// phaseTimings renders the five phases grouped into the three observable buckets.
func TestPhaseTimingsString(t *testing.T) {
	missing := phaseTimings{callQueueMS: 12.5, transferProcessMS: 0.4}
	assert.Equal(t, "Call+Queue=12.5ms | Exec=n/a | Transfer+Process=0.4ms", missing.String())

	withExec := phaseTimings{callQueueMS: 1, execMS: 8.25, haveExec: true, transferProcessMS: 2}
	assert.Equal(t, "Call+Queue=1.0ms | Exec=8.2ms | Transfer+Process=2.0ms", withExec.String())
}

// The success path parses the sidecar-reported timing (Exec phase).
func TestRunner_ParsesSidecarTiming(t *testing.T) {
	srv, _ := newStubSidecar(t, http.StatusOK,
		`{"id":"nt-run-1","status":"completed","total_return":0,"timing":{"queue_ms":1.5,"exec_ms":42.0,"total_ms":44.0}}`)
	runner := NewRunner(srv.URL, srv.Client())

	resp, err := runner.RunBacktest(context.Background(), sampleRequest())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "nt-run-1", resp.ID)
}
