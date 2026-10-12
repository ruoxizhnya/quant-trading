// Package nautilus adapts the NautilusTrader sidecar (K13) to the frozen
// EngineRunner contract, so "nautilus" can be registered as a second kernel
// alongside the default Go engine.
//
// # Relationship to the frozen contracts
//
// The K10 registry (pkg/backtest/contracts/registry.go) holds
// contracts.EngineRunner — RunBacktest(ctx, BacktestRequest) → *BacktestResponse.
// That is the interface this Runner implements. (The task brief phrased it as
// "contracts.BacktestRunner"; that identifier is the five-argument AI-pipeline
// port in contracts/runner.go, which is deliberately unrelated to kernel
// routing — see the registry.go comment "接口选型说明". Implementing
// EngineRunner is what makes registration work.)
//
// # Fail-closed, no silent fallback
//
// Every failure — the sidecar is not running, the HTTP call times out, the
// sidecar answers non-2xx, or it answers status="failed" — is surfaced as a Go
// error. The Runner never manufactures a successful *BacktestResponse out of a
// failure, so a caller that asked for kernel="nautilus" either gets NT's result
// or an explicit error; it never silently receives a Go-kernel result wearing a
// nautilus label (FR-1.3).
//
// Security posture (ADR-031 D5)
//
// The sidecar is a separate OS process reached over loopback only. This client
// holds no database handle and sends nothing but the RunSpec.
package nautilus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

const (
	// KernelName is the registry key for the NautilusTrader sidecar kernel (K13).
	KernelName = "nautilus"

	// DefaultURL is the sidecar's loopback address. ADR-031 D5 requires the
	// sidecar to be reachable only from the host, so this is never a remote host.
	DefaultURL = "http://127.0.0.1:9100"

	// URLEnvVar overrides DefaultURL at assembly time.
	URLEnvVar = "NT_SIDECAR_URL"

	// RunPath is the sidecar's RunSpec execution endpoint.
	RunPath = "/v1/backtest"

	defaultTimeout = 10 * time.Minute

	// maxErrorBody bounds how much of a failure body is read into the error message.
	maxErrorBody = 4 << 10
)

// URLFromEnv returns the sidecar base URL: $NT_SIDECAR_URL when set and
// non-blank, otherwise DefaultURL.
func URLFromEnv() string {
	if v := strings.TrimSpace(os.Getenv(URLEnvVar)); v != "" {
		return v
	}
	return DefaultURL
}

// Runner executes backtests through the NT sidecar over HTTP.
type Runner struct {
	baseURL string
	client  *http.Client
}

// compile-time assertion: Runner is what the K10 registry accepts.
var _ contracts.EngineRunner = (*Runner)(nil)

// NewRunner builds a Runner for baseURL. A nil client selects one with
// defaultTimeout — pass a client only to override transport or timeout
// behaviour (tests inject httptest's client).
func NewRunner(baseURL string, client *http.Client) *Runner {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Runner{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

// runRequest mirrors the sidecar's RunSpec DTO. Field names intentionally match
// contracts.BacktestRequest so the mapping is a straight copy.
type runRequest struct {
	Strategy       string         `json:"strategy"`
	StockPool      []string       `json:"stock_pool,omitempty"`
	IndexCode      string         `json:"index_code,omitempty"`
	StartDate      string         `json:"start_date"`
	EndDate        string         `json:"end_date"`
	InitialCapital float64        `json:"initial_capital,omitempty"`
	RiskFreeRate   float64        `json:"risk_free_rate,omitempty"`
	Kernel         string         `json:"kernel"`
	EngineParams   map[string]any `json:"engine_params,omitempty"`
}

// runResponse is the minimal K13 field set the sidecar returns. K15 replaces
// this with the full ResultSpec alignment (drawdown, trade-level reconciliation).
type runResponse struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	Strategy       string  `json:"strategy"`
	StartDate      string  `json:"start_date"`
	EndDate        string  `json:"end_date"`
	InitialCapital float64 `json:"initial_capital"`
	TotalReturn    float64 `json:"total_return"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	SortinoRatio   float64 `json:"sortino_ratio"`
	TotalOrders    int     `json:"total_orders"`
	TotalPositions int     `json:"total_positions"`
	Iterations     int     `json:"iterations"`
	Error          string  `json:"error"`
}

// RunBacktest sends the RunSpec to the sidecar and maps the reply.
//
// Error taxonomy (surfaced through pkg/httpserver.StatusForAppError):
//   - sidecar unreachable / non-2xx → UNAVAILABLE (503)
//   - context deadline exceeded     → TIMEOUT (504)
//   - undecodable 2xx body          → INTERNAL (500)
func (r *Runner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	body, err := json.Marshal(runRequest{
		Strategy:       req.Strategy,
		StockPool:      req.StockPool,
		IndexCode:      req.IndexCode,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		InitialCapital: req.InitialCapital,
		RiskFreeRate:   req.RiskFreeRate,
		Kernel:         req.EffectiveKernel(),
		EngineParams:   req.EngineParams,
	})
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"encode run spec for the nautilus sidecar", "nautilus.RunBacktest")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+RunPath, bytes.NewReader(body))
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"build sidecar request", "nautilus.RunBacktest")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, r.transportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, apperrors.Unavailable("nautilus sidecar",
			fmt.Errorf("read sidecar response: %w", err))
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q failed: sidecar returned HTTP %d: %s",
			KernelName, resp.StatusCode, sidecarError(payload)))
	}

	var out runResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, apperrors.Internal(
			fmt.Errorf("decode sidecar response: %w", err), "nautilus.RunBacktest")
	}

	// A 2xx body may still carry status="failed" (OBS-01 semantics: failed is an
	// outcome, not a transport state). Surface it — never return it as success.
	if out.Status == contracts.StatusFailed {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = "sidecar reported failed with no message"
		}
		return nil, apperrors.Unavailable("nautilus sidecar", errors.New(msg))
	}

	return r.toResponse(req, out), nil
}

// transportError classifies a failed round trip. context.DeadlineExceeded is the
// caller's own deadline firing (the handler sets 10 minutes); everything else is
// the sidecar being unreachable.
func (r *Runner) transportError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return apperrors.Timeout("nautilus.RunBacktest", err)
	}
	return apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
		"kernel %q unreachable at %s: %w", KernelName, r.baseURL, err))
}

// toResponse maps the sidecar payload onto the frozen response DTO. Fields the
// sidecar does not report are left at their zero value rather than approximated;
// aligning them with the Go kernel's metrics is K15's scope.
func (r *Runner) toResponse(req contracts.BacktestRequest, out runResponse) *contracts.BacktestResponse {
	status := out.Status
	if status == "" {
		status = contracts.StatusCompleted
	}
	return &contracts.BacktestResponse{
		ID:             out.ID,
		Status:         status,
		Strategy:       firstNonEmpty(out.Strategy, req.Strategy),
		StartDate:      firstNonEmpty(out.StartDate, req.StartDate),
		EndDate:        firstNonEmpty(out.EndDate, req.EndDate),
		InitialCapital: firstNonZero(out.InitialCapital, req.InitialCapital),
		TotalReturn:    out.TotalReturn,
		SharpeRatio:    out.SharpeRatio,
		SortinoRatio:   out.SortinoRatio,
		StockPool:      req.StockPool,
	}
}

// sidecarError extracts a human-readable reason from a failure body, falling
// back to a bounded raw excerpt when the body is not the expected JSON shape.
func sidecarError(payload []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err == nil && strings.TrimSpace(body.Error) != "" {
		return body.Error
	}
	excerpt := strings.TrimSpace(string(payload))
	if len(excerpt) > maxErrorBody {
		excerpt = excerpt[:maxErrorBody] + "…"
	}
	if excerpt == "" {
		return "(empty response body)"
	}
	return excerpt
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(values ...float64) float64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
