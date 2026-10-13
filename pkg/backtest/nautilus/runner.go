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
// # K13-R1 hardening
//
//   - Every error message names the kernel (“KernelName“) so an operator can
//     tell which kernel did *not* run — the "counter-evidence" the brief asks
//     for (e.g. an empty 2xx body is reported as "no result", never a zeroed
//     success).
//   - A 503 from the sidecar (or an nginx/haproxy in front of it) is classified
//     explicitly as "kernel busy" and a “Retry-After“ hint is parsed out.
//   - The round trip is decomposed into the five phases the brief names, grouped
//     as (Call+Queue) / (Exec, server-reported) / (Transfer+Process), and the
//     breakdown rides along on failure so a timeout can be attributed.
//   - Absent metrics arrive as JSON “null“ and stay “nil“ at the wire layer
//     (“*float64“) instead of being silently collapsed to “0“ by
//     encoding/json.
//
// Security posture (ADR-031 D5)
//
// The sidecar is a separate OS process reached over loopback only. This client
// holds no database handle and sends nothing but the RunSpec.
//
// # K16 ② · round-trip observation
//
// RunBacktest reports every sidecar round trip (success or failure) to an
// optional RunObserver — a consumer-defined narrow port, so this package does
// not depend on pkg/observability (observability.Metrics implements the
// interface implicitly; the compile-time assertion lives at the assembly site,
// cmd/analysis/setup.go). Observation is uniformly attached via a defer on a
// named return, so every return path — transport error, 503, non-2xx, empty
// body, decode failure, status="failed", missing metric, success — is counted
// exactly once. The outcome vocabulary mirrors pkg/errors codes
// (ok / unavailable / timeout / internal) so alert rules can key off it
// directly and label cardinality stays bounded.
//
// 「回退次数=0」不是一条指标而是结构属性：本 Runner 没有任何回退分支——
// 全部失败路径返回 (nil, err)（runner_test 的 no-silent-fallback 断言），
// 因此"回退计数"恒等于 0，由测试执法而非监控抓数（详见
// docs/design/kernel/kernel-pluggable-nt-ready.md §8 表第 6 项）。
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
	"strconv"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
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

	// EnableEnvVar is the production routing switch for the nautilus kernel
	// (K16 ④). Default is **off**: the production registry exposes only the
	// go kernel, and nautilus must be explicitly opted in — a run request
	// naming kernel="nautilus" against an unregistered kernel fails closed
	// with ErrKernelNotRegistered (naming the kernel), never falls back.
	EnableEnvVar = "NT_SIDECAR_ENABLED"

	// RunPath is the sidecar's RunSpec execution endpoint.
	RunPath = "/v1/backtest"

	defaultTimeout = 10 * time.Minute

	// maxErrorBody bounds how much of a failure body is read into the error message.
	maxErrorBody = 4 << 10
)

// maxResponseBody bounds how much of a (successful) sidecar response body is
// buffered (K13-R1-R1 C). Generous enough for K15's equity curves / trade
// detail, but finite so a hostile or faulty sidecar can never balloon the Go
// process's memory. A var, not a const, only so a test can lower it without
// materialising a 64 MiB body.
var maxResponseBody int64 = 64 << 20

// URLFromEnv returns the sidecar base URL: $NT_SIDECAR_URL when set and
// non-blank, otherwise DefaultURL.
func URLFromEnv() string {
	if v := strings.TrimSpace(os.Getenv(URLEnvVar)); v != "" {
		return v
	}
	return DefaultURL
}

// KernelEnabledFromEnv reports whether the nautilus kernel should be
// registered in the production registry (K16 ④ routing switch).
//
// Semantics are deliberately fail-closed on typos, not on absence:
//
//	unset / empty → false （默认 go-only，nautilus 不注册）
//	"1" / "true" / "yes" / "on" → true（显式开启）
//	"0" / "false" / "no" / "off" → false（显式关闭）
//	anything else → error（打错字必须在装配期炸，不能静默当关闭）
//
// A disabled kernel is a *routing* decision: a request naming
// kernel="nautilus" then fails closed at Resolve (ErrKernelNotRegistered),
// which is the correct production behaviour — the run is refused with an
// explicit message rather than silently executed by another kernel.
func KernelEnabledFromEnv() (bool, error) {
	v := strings.TrimSpace(os.Getenv(EnableEnvVar))
	switch strings.ToLower(v) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf(
			"nautilus: %s=%q is not a recognised boolean (want 1/true/yes/on or 0/false/no/off) — refusing to guess, fix the configuration",
			EnableEnvVar, v)
	}
}

// Round-trip outcome vocabulary reported to RunObserver (K16 ②). The values
// mirror pkg/errors codes so alert rules keying off AppError.Code stay
// consistent with the metrics label; "ok" is the one non-error outcome.
const (
	OutcomeOK          = "ok"
	OutcomeUnavailable = "unavailable"
	OutcomeTimeout     = "timeout"
	OutcomeInternal    = "internal"
)

// RunObserver receives one observation per sidecar round trip (K16 ②).
// A nil observer is a no-op — observation must never fail a run.
//
// 消费方定义窄接口（consumer-defined port）：本包不认识 pkg/observability，
// observability.Metrics 隐式实现本接口（编译期断言在装配侧 cmd/analysis/setup.go）。
type RunObserver interface {
	// ObserveNTCall records one round trip: d is the wall-clock duration of
	// RunBacktest; outcome is one of the Outcome* constants.
	ObserveNTCall(outcome string, d time.Duration)
}

// Runner executes backtests through the NT sidecar over HTTP.
type Runner struct {
	baseURL  string
	client   *http.Client
	observer RunObserver // optional; nil means no observation
}

// compile-time assertion: Runner is what the K10 registry accepts.
var _ contracts.EngineRunner = (*Runner)(nil)

// NewRunner builds a Runner for baseURL. A nil client selects one with
// defaultTimeout — pass a client only to override transport or timeout
// behaviour (tests inject httptest's client). No round-trip observation
// (see NewObservedRunner).
func NewRunner(baseURL string, client *http.Client) *Runner {
	return NewObservedRunner(baseURL, client, nil)
}

// NewObservedRunner is NewRunner plus a round-trip observer (K16 ②).
// A nil observer is a no-op — observation must never affect behaviour.
func NewObservedRunner(baseURL string, client *http.Client, observer RunObserver) *Runner {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Runner{
		baseURL:  strings.TrimRight(baseURL, "/"),
		client:   client,
		observer: observer,
	}
}

// runRequest mirrors the sidecar's RunSpec DTO. Field names intentionally match
// contracts.BacktestRequest so the mapping is a straight copy.
type runRequest struct {
	Strategy       string                   `json:"strategy"`
	StockPool      []string                 `json:"stock_pool,omitempty"`
	IndexCode      string                   `json:"index_code,omitempty"`
	StartDate      string                   `json:"start_date"`
	EndDate        string                   `json:"end_date"`
	InitialCapital float64                  `json:"initial_capital,omitempty"`
	RiskFreeRate   float64                  `json:"risk_free_rate,omitempty"`
	Kernel         string                   `json:"kernel"`
	EngineParams   map[string]any           `json:"engine_params,omitempty"`
	Signals        []contracts.SignalWeight `json:"signals,omitempty"`
}

// equityPoint is one day of the sidecar's synthesised mark-to-market series
// (K15): the same cash + positions decomposition the Go tracker records.
type equityPoint struct {
	Date      string  `json:"date"`
	Equity    float64 `json:"equity"`
	Cash      float64 `json:"cash"`
	Positions float64 `json:"positions"`
}

// tradeWire is one fill from the sidecar's fills report (K15). NT's fee model
// returns the A-share fee as one total, so it rides in Commission with the
// components at zero; the diff tool compares the summed Go fees against it.
type tradeWire struct {
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Qty         float64 `json:"qty"`
	Price       float64 `json:"price"`
	Commission  float64 `json:"commission"`
	TransferFee float64 `json:"transfer_fee"`
	StampTax    float64 `json:"stamp_tax"`
	Ts          string  `json:"ts"`
}

// runResponse is the minimal K13 field set the sidecar returns. K15 replaces
// this with the full ResultSpec alignment (drawdown, trade-level reconciliation).
//
// Metric fields are “*float64“ (K13-R1 D): the sidecar emits strict JSON, so a
// metric it cannot compute arrives as “null“. Decoding into “*float64“ keeps
// "not reported" distinct from "genuinely zero" at the wire layer instead of
// letting encoding/json coerce “null“ into “0“. “toResponse“ is the single,
// explicit place where "not reported" is folded into the frozen DTO (which has
// no null representation).
type runResponse struct {
	ID             string   `json:"id"`
	Status         string   `json:"status"`
	Strategy       string   `json:"strategy"`
	StartDate      string   `json:"start_date"`
	EndDate        string   `json:"end_date"`
	InitialCapital *float64 `json:"initial_capital"`
	TotalReturn    *float64 `json:"total_return"`
	SharpeRatio    *float64 `json:"sharpe_ratio"`
	SortinoRatio   *float64 `json:"sortino_ratio"`
	TotalOrders    int      `json:"total_orders"`
	TotalPositions int      `json:"total_positions"`
	Iterations     int      `json:"iterations"`
	Timing         *timing  `json:"timing"`
	// K15 · the full data plane: daily mark-to-market series + per-fill list.
	EquityCurve []equityPoint `json:"equity_curve"`
	Trades      []tradeWire   `json:"trades"`
	Error       string        `json:"error"`
}

// timing is the sidecar-reported execution breakdown (K13-R1, deadline 五段拆).
// “queue_ms“ is time spent waiting on the sidecar's run lock (serialised runs),
// “exec_ms“ is the engine run itself; both are measured inside the sidecar,
// where the client cannot see them.
type timing struct {
	QueueMS float64 `json:"queue_ms"`
	ExecMS  float64 `json:"exec_ms"`
	TotalMS float64 `json:"total_ms"`
}

// phaseTimings decomposes one sidecar round trip into the five phases the brief
// names, grouped into the three buckets a client can actually observe:
//
//	Call+Queue          client: t0 → response headers received
//	                    (connect + send + sidecar queue + engine start-to-first-byte)
//	Exec                server-reported engine execution (timing.exec_ms)
//	Transfer+Process    client: headers → body read + JSON decode
type phaseTimings struct {
	callQueueMS       float64
	execMS            float64
	haveExec          bool
	transferProcessMS float64
}

func (p phaseTimings) String() string {
	exec := "n/a"
	if p.haveExec {
		exec = fmt.Sprintf("%.1fms", p.execMS)
	}
	return fmt.Sprintf("Call+Queue=%.1fms | Exec=%s | Transfer+Process=%.1fms",
		p.callQueueMS, exec, p.transferProcessMS)
}

// RunBacktest sends the RunSpec to the sidecar and maps the reply.
//
// Error taxonomy (surfaced through pkg/httpserver.StatusForAppError):
//   - sidecar unreachable / non-2xx → UNAVAILABLE (503)
//   - sidecar 503 → UNAVAILABLE, message explicitly marks the kernel "busy"
//   - context deadline exceeded     → TIMEOUT (504), attributed to Call+Queue
//   - undecodable or empty 2xx body → INTERNAL (500) / UNAVAILABLE
//
// K16 ② · 一次 round trip 一条观测（named-return defer 统一覆盖全部 return
// 路径，成功/失败各计一次；outcome 取自 pkg/errors 的错误码族）。
func (r *Runner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (outResp *contracts.BacktestResponse, outErr error) {
	callStart := time.Now()
	defer func() {
		if r.observer != nil {
			r.observer.ObserveNTCall(outcomeFor(outErr), time.Since(callStart))
		}
	}()

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
		Signals:        req.Signals,
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

	// Phase 1 · Call+Queue — everything up to the response headers arriving.
	t0 := time.Now()
	resp, err := r.client.Do(httpReq)
	headersAt := time.Now()
	pt := phaseTimings{callQueueMS: msSince(t0, headersAt)}
	if err != nil {
		return nil, r.transportError(ctx, err, pt)
	}
	defer func() { _ = resp.Body.Close() }()

	// K13-R1-R1 C · bounded read: read one byte past the limit so an oversized
	// body is detected rather than silently truncated. A hostile/faulty sidecar
	// must never make the Go process buffer an unbounded response.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		pt.transferProcessMS = msSince(headersAt, time.Now())
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q: read sidecar response body (Transfer+Process phase) [%s]: %w",
			KernelName, pt, err))
	}
	if int64(len(payload)) > maxResponseBody {
		pt.transferProcessMS = msSince(headersAt, time.Now())
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q: sidecar response body exceeds the %d-byte limit — refusing to buffer an unbounded response (Transfer+Process phase) [%s]",
			KernelName, maxResponseBody, pt))
	}

	// A 503 is the canonical "kernel busy" signal (sidecar overloaded, or an
	// nginx/HAProxy in front of it). Classify it explicitly and surface the
	// Retry-After hint; without this a busy kernel is indistinguishable from a
	// dead one.
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q busy (内核忙): sidecar returned HTTP 503%s [%s]: %s",
			KernelName, retryAfterHint(resp), pt, sidecarError(payload)))
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q failed: sidecar returned HTTP %d [%s]: %s",
			KernelName, resp.StatusCode, pt, sidecarError(payload)))
	}

	// A 2xx with an empty body (e.g. a stray 204) carries no result. Refuse it
	// and name the kernel — never synthesise a zeroed "success" (FR-1.3).
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q: sidecar returned HTTP %d with an empty body — no result to map (refusing to synthesise one)",
			KernelName, resp.StatusCode))
	}

	var out runResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, apperrors.Internal(
			fmt.Errorf("kernel %q: decode sidecar response: %w", KernelName, err), "nautilus.RunBacktest")
	}

	// Phase 3 · Transfer+Process — headers → body read → JSON decode.
	pt.transferProcessMS = msSince(headersAt, time.Now())
	if out.Timing != nil {
		pt.execMS = out.Timing.ExecMS
		pt.haveExec = true
	}

	// A 2xx body may still carry status="failed" (OBS-01 semantics: failed is an
	// outcome, not a transport state). Surface it — never return it as success.
	if out.Status == contracts.StatusFailed {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = "sidecar reported failed with no message"
		}
		return nil, apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q failed [%s]: %s", KernelName, pt, msg))
	}

	// K13-R1-R1 A (a) · 纵深防御：一个 completed 的 run 却缺 total_return 是契约
	// 违背。绝不能把它折叠成 0 当成成功——那与 K13-R1 A 要治的「像真的错值」
	// 同类。这里点名 kernel 报错，不静默折叠。（侧车在源头已把这种 run 标
	// failed；这一道防的是声称 completed 却漏报字段的侧车实现。）
	if out.Status == "" || out.Status == contracts.StatusCompleted {
		if out.TotalReturn == nil {
			return nil, apperrors.Internal(fmt.Errorf(
				"kernel %q: sidecar reported status=%q but no total_return (metric not reported) — refusing to fold a missing metric into 0 [%s]",
				KernelName, contracts.StatusCompleted, pt), "nautilus.RunBacktest")
		}
	}

	return r.toResponse(req, out), nil
}

// outcomeFor maps a RunBacktest error onto the RunObserver outcome
// vocabulary (K16 ②). All RunBacktest failures are *apperrors.AppError;
// anything outside the known code set is folded to "internal" so the label
// cardinality stays bounded (fail-closed on unknown codes — never a new
// unbounded label value).
func outcomeFor(err error) string {
	if err == nil {
		return OutcomeOK
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case apperrors.ErrCodeUnavailable:
			return OutcomeUnavailable
		case apperrors.ErrCodeTimeout:
			return OutcomeTimeout
		case apperrors.ErrCodeInternal:
			return OutcomeInternal
		}
	}
	return OutcomeInternal
}

// transportError classifies a failed round trip. context.DeadlineExceeded is the
// caller's own deadline firing (the handler sets 10 minutes); everything else is
// the sidecar being unreachable.
func (r *Runner) transportError(ctx context.Context, err error, pt phaseTimings) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return apperrors.Timeout("nautilus.RunBacktest", fmt.Errorf(
			"kernel %q: deadline exceeded in Call+Queue phase — no response headers from %s within the deadline, "+
				"so Exec/Transfer+Process are not observable [%s]: %w",
			KernelName, r.baseURL, pt, err))
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
			"kernel %q: call cancelled in Call+Queue phase [%s]: %w", KernelName, pt, err))
	}
	return apperrors.Unavailable("nautilus sidecar", fmt.Errorf(
		"kernel %q unreachable at %s [%s]: %w", KernelName, r.baseURL, pt, err))
}

// retryAfterHint renders the Retry-After header for a "kernel busy" error.
// Retry-After is either delta-seconds or an HTTP-date (RFC 9110 §10.2.3).
func retryAfterHint(resp *http.Response) string {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return ""
	}
	// Retry-After is either delta-seconds or an HTTP-date (RFC 9110 §10.2.3).
	if secs, err := strconv.Atoi(raw); err == nil {
		return fmt.Sprintf("; Retry-After=%ds", secs)
	}
	return fmt.Sprintf("; Retry-After=%s", raw)
}

// toResponse maps the sidecar payload onto the frozen response DTO. Fields the
// sidecar does not report (wire null → nil pointer) are left at their zero value;
// this is the single explicit place where "not reported" is folded, since
// BacktestResponse has no null representation.
//
// K15 · the data plane (equity curve / per-fill trades) lands in the existing
// frozen fields PortfolioValues / Trades — no schema addition. The wire shapes
// (equityPoint / tradeWire) mirror domain.PortfolioValue / domain.Trade field
// for field so the dual-run diff can feed both kernels' outputs through the
// same metrics package.
func (r *Runner) toResponse(req contracts.BacktestRequest, out runResponse) *contracts.BacktestResponse {
	status := out.Status
	if status == "" {
		status = contracts.StatusCompleted
	}
	resp := &contracts.BacktestResponse{
		ID:             out.ID,
		Status:         status,
		Strategy:       firstNonEmpty(out.Strategy, req.Strategy),
		StartDate:      firstNonEmpty(out.StartDate, req.StartDate),
		EndDate:        firstNonEmpty(out.EndDate, req.EndDate),
		InitialCapital: firstNonNil(out.InitialCapital, req.InitialCapital),
		TotalReturn:    deref(out.TotalReturn),
		SharpeRatio:    deref(out.SharpeRatio),
		SortinoRatio:   deref(out.SortinoRatio),
		StockPool:      req.StockPool,
	}
	for _, p := range out.EquityCurve {
		day, err := time.Parse(time.RFC3339, p.Date)
		if err != nil {
			// The synthesised dates are bare ISO dates; parse them as such.
			day, err = time.Parse("2006-01-02", p.Date)
			if err != nil {
				continue // a malformed date row is skipped, never fabricated
			}
		}
		resp.PortfolioValues = append(resp.PortfolioValues, domain.PortfolioValue{
			Date:       day.UTC(),
			TotalValue: p.Equity,
			Cash:       p.Cash,
			Positions:  p.Positions,
		})
	}
	for i, t := range out.Trades {
		ts, err := time.Parse(time.RFC3339, t.Ts)
		if err != nil {
			continue
		}
		// ID 含序号：同日同 symbol 同 side 同 qty 的两笔 fill（理论上不应
		// 出现，但 fills 报告不保证）不能共用一个 ID——编号必须唯一。
		resp.Trades = append(resp.Trades, domain.Trade{
			ID:          fmt.Sprintf("nt-%04d-%s-%s-%g", i, t.Symbol, t.Side, t.Qty),
			Symbol:      t.Symbol,
			Direction:   tradeDirection(t.Side),
			Quantity:    t.Qty,
			Price:       t.Price,
			Commission:  t.Commission,
			TransferFee: t.TransferFee,
			StampTax:    t.StampTax,
			Timestamp:   ts.UTC(),
		})
	}
	return resp
}

// tradeDirection maps the sidecar's BUY/SELL fill side onto the domain
// direction the Go tracker records for the same action: a buy opens/extends a
// long (DirectionLong), a sell reduces or closes it (DirectionClose).
func tradeDirection(side string) domain.Direction {
	if strings.EqualFold(side, "BUY") {
		return domain.DirectionLong
	}
	return domain.DirectionClose
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

func msSince(from, to time.Time) float64 {
	return float64(to.Sub(from).Microseconds()) / 1000.0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// deref maps a wire metric to the frozen DTO's float64. A nil (not reported)
// metric becomes 0 — the contract has no null; the distinction is preserved at
// the runResponse layer where it matters for decoding.
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func firstNonNil(v *float64, fallback float64) float64 {
	if v != nil {
		return *v
	}
	return fallback
}
