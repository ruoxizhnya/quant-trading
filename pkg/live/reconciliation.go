// Package live — 券资金对账 (securities-fund reconciliation) Worker (P2-8).
//
// 监管依据:
//   - 《证券公司大额交易报告管理办法》(2024-04 修订) + 中国结算 《证券
//     资金对账指引》(2023): 券商内部系统应在每个交易日按 ≥ 15 分钟粒度
//     与登记结算公司 (中证登) 对账一次, 偏差超阈值必须在当日书面报告。
//   - 《证券公司内部控制指引》(2020) §4.3 "交易系统与登记结算系统的对账
//     必须留痕, 包括对账时间, 偏差项目, 处置结果"。
//   - 中国证券业协会 《证券公司交易系统运维管理自律规则》(2022) §5.2:
//     对账偏差处置 SLA = 30 分钟内发出预警, 60 分钟内启动人工核查。
//
// 设计目标:
//   - 与券商对账 (mock 模式下 self-check, 真实模式下通过 broker.QuerySettlement)
//   - 每 15 分钟自动对账一次 + 支持 ForceRun
//   - 偏差超阈值 → 立即通过 alert.AlertManager 派发 critical / warning 告警
//   - 落盘 on-disk JSON 报告, 文件名 rec-YYYYMMDD-HHMM.json
//   - 完全无状态 (除 history buffer): 重启不丢失历史, 历史只由调用方持有
package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ============================================================
// 配置 / 阈值
// ============================================================

// ReconciliationConfig 决定 Worker 的对账节奏与偏差容忍度。
//
//	Interval:        对账间隔, 默认 15 分钟 (监管下限)。< 1s 会被 clamp 到 1s。
//	CashTolerance:   资金余额绝对偏差容忍度, 默认 0.01 CNY (0.01 元 = 1 分)。
//	QuantityTolerance: 持仓数量绝对偏差容忍度, 默认 0 股 (整股; 实际账务
//	                   应严格相等, 出现 1 股偏差即疑似丢单 / 重复成交)。
//	MarketValueTol:  持仓市值相对偏差容忍度, 默认 0.5% (万分之 50),
//	                   主要覆盖行情刷新延迟造成的价差。
//	FeeTolerance:    手续费 / 过户费 / 印花税 累计偏差容忍度, 默认 0.01 CNY。
//	ReportPath:      on-disk JSON 报告落盘目录; 文件名 rec-YYYYMMDD-HHMM.json
//	HistoryLimit:    内存中保留的最近对账报告条数 (给 HTTP 层看)。
//	Enabled:         false → Start() 立即返回, 不创建 goroutine。
//	Now:             时钟注入 (测试用)。nil → time.Now。
type ReconciliationConfig struct {
	Interval          time.Duration
	CashTolerance     float64
	QuantityTolerance float64
	MarketValueTol    float64
	FeeTolerance      float64
	ReportPath        string
	HistoryLimit      int
	Enabled           bool
	Now               func() time.Time
}

// DefaultReconciliationConfig returns regulatory-recommended defaults.
//
// The defaults match the 中国结算 《证券资金对账指引》 (2023) §3.2 and
// the 中国证券业协会 《证券公司交易系统运维管理自律规则》 (2022) §5.2.
func DefaultReconciliationConfig() ReconciliationConfig {
	return ReconciliationConfig{
		Interval:          15 * time.Minute,
		CashTolerance:     0.01,  // 1 分
		QuantityTolerance: 0.0,   // 整股
		MarketValueTol:    0.005, // 0.5%
		FeeTolerance:      0.01,  // 1 分
		ReportPath:        "compliance/reconciliation",
		HistoryLimit:      100,
		Enabled:           true,
	}
}

// ============================================================
// Snapshot 数据结构
// ============================================================

// PositionSnap is the per-symbol balance used in reconciliation.
// Both sides (local + broker) populate the same fields; reconciliation
// compares the delta and fires a discrepancy when the delta exceeds
// the configured tolerance.
type PositionSnap struct {
	Symbol      string  `json:"symbol"`       // 股票代码
	Quantity    float64 `json:"quantity"`     // 持仓数量 (股)
	AvgCost     float64 `json:"avg_cost"`     // 持仓成本 (元/股)
	MarketValue float64 `json:"market_value"` // 持仓市值 (元)
}

// CashSnap is the per-account cash balance. Some brokers split cash
// into "available" + "frozen"; we only reconcile the total (available +
// frozen == 券商报表余额) to keep the contract minimal.
type CashSnap struct {
	TotalCNY  float64 `json:"total_cny"`
	Available float64 `json:"available,omitempty"`
	Frozen    float64 `json:"frozen,omitempty"`
}

// FeeSnap is the per-day fee breakdown. 印花税 + 佣金 + 过户费 + 其他。
// 任何一项累计偏差超 FeeTolerance 都触发 discrepancy。
type FeeSnap struct {
	Commission float64 `json:"commission"`
	StampTax   float64 `json:"stamp_tax"`
	Transfer   float64 `json:"transfer"`
	Other      float64 `json:"other"`
}

// Sum returns the total fee across all four buckets. Used to
// short-circuit fee comparisons when local and broker totals
// both fall under FeeTolerance.
func (f FeeSnap) Sum() float64 {
	return f.Commission + f.StampTax + f.Transfer + f.Other
}

// AccountSnapshot is the broker's authoritative view of an account at
// a specific moment. The reconciliation engine compares LocalSnapshot
// vs BrokerSnapshot field by field and emits Discrepancy entries for
// every delta above the configured tolerance.
//
// AccountID is the unique account identifier (券商资金账号); ReconciliationID
// is the broker-side reconciliation document id (券商对账单编号) when
// available.
type AccountSnapshot struct {
	AccountID        string                  `json:"account_id"`
	ReconciliationID string                  `json:"reconciliation_id,omitempty"`
	AsOf             time.Time               `json:"as_of"`
	Cash             CashSnap                `json:"cash"`
	Positions        map[string]PositionSnap `json:"positions"` // key = symbol
	Fees             FeeSnap                 `json:"fees"`
}

// LocalSnapshot is the in-process trading system's view of the same
// account. For the mock trader this is just `trader.GetAccount` +
// `trader.GetPositions`; for a real broker adapter it would be the
// local order store's view of the day.
//
// TodayPnL is optional — when the broker provides a realized P&L
// column, the discrepancy engine will compare it as a sanity check
// (caller-supplied; we don't compute it here).
type LocalSnapshot struct {
	AccountID string                  `json:"account_id"`
	AsOf      time.Time               `json:"as_of"`
	Cash      CashSnap                `json:"cash"`
	Positions map[string]PositionSnap `json:"positions"`
	Fees      FeeSnap                 `json:"fees"`
	TodayPnL  float64                 `json:"today_pnl"`
}

// ============================================================
// Discrepancy / Report
// ============================================================

// DiscrepancySeverity ranks how urgent a deviation is. Channel
// implementations route "critical" through PagerDuty / SMS and
// "warning" through Slack / email; "info" is dashboard-only.
type DiscrepancySeverity string

const (
	// SeverityCritical: 必须立即人工介入 (例: 现金偏差 > 100 元 / 持仓
	// 偏差 > 1000 股 / 任何平账差额)。监管 SLA 30 分钟。
	SeverityCritical DiscrepancySeverity = "critical"
	// SeverityWarning: 偏差在容忍区间, 但需要关注 (例: 现金偏差 1-100
	// 元, 持仓市值偏差 0.5-1%)。
	SeverityWarning DiscrepancySeverity = "warning"
	// SeverityInfo: 偏差在容差内但仍然记录, 用于追溯。
	SeverityInfo DiscrepancySeverity = "info"
)

// DiscrepancyKind classifies the type of mismatch.
type DiscrepancyKind string

const (
	// KindCash: 现金余额差异
	KindCash DiscrepancyKind = "cash"
	// KindQuantity: 持仓数量差异
	KindQuantity DiscrepancyKind = "quantity"
	// KindMarketValue: 持仓市值差异 (数量一致, 价格不一致 → 行情刷新)
	KindMarketValue DiscrepancyKind = "market_value"
	// KindFee: 手续费 / 印花税 / 过户费 差异
	KindFee DiscrepancyKind = "fee"
	// KindMissingLocal: 券商有, 本地无 (例: 券商已扣手续费, 本地未记)
	KindMissingLocal DiscrepancyKind = "missing_local"
	// KindMissingBroker: 本地有, 券商无 (例: 重复扣款, 鬼单)
	KindMissingBroker DiscrepancyKind = "missing_broker"
)

// Discrepancy is a single matched deviation between local and broker
// snapshots. Multiple discrepancies can fire per reconciliation cycle
// (e.g. cash off by 1.5 CNY AND 2 positions missing locally).
type Discrepancy struct {
	Kind       DiscrepancyKind     `json:"kind"`
	Severity   DiscrepancySeverity `json:"severity"`
	Symbol     string              `json:"symbol,omitempty"` // for position-level
	Field      string              `json:"field,omitempty"`  // for fee breakdown
	LocalVal   float64             `json:"local_val"`
	BrokerVal  float64             `json:"broker_val"`
	Delta      float64             `json:"delta"`
	Tolerance  float64             `json:"tolerance"`
	Note       string              `json:"note,omitempty"`
	DetectedAt time.Time           `json:"detected_at"`
}

// HasCritical reports whether any of the discrepancies is critical.
// Helper for AlertManager dispatching.
func (d Discrepancy) IsCritical() bool { return d.Severity == SeverityCritical }

// ReconciliationReport is the top-level JSON document written to
// `rec-YYYYMMDD-HHMM.json`. SchemaVersion is the contract identifier
// — consumers (audit tools, regulator interfaces) pin to it.
type ReconciliationReport struct {
	SchemaVersion  string          `json:"schema_version"`
	GeneratedAt    time.Time       `json:"generated_at"`
	AccountID      string          `json:"account_id"`
	LocalAsOf      time.Time       `json:"local_as_of"`
	BrokerAsOf     time.Time       `json:"broker_as_of"`
	BridgeReconcID string          `json:"bridge_reconc_id,omitempty"`
	Local          LocalSnapshot   `json:"local"`
	Broker         AccountSnapshot `json:"broker"`
	Discrepancies  []Discrepancy   `json:"discrepancies"`
	CriticalCount  int             `json:"critical_count"`
	WarningCount   int             `json:"warning_count"`
	InfoCount      int             `json:"info_count"`
	Healthy        bool            `json:"healthy"`
	Note           string          `json:"note,omitempty"`
}

// ============================================================
// BrokerQuerier — 由 wiring 层注入, 真实券商对接时实现 QuerySettlement
// ============================================================

// BrokerQuerier is the read-only interface the reconciliation engine
// uses to fetch the broker's authoritative view of the account.
//
// Implementations:
//   - mock: returns the same MockTrader's internal state (self-check)
//   - ctp / 恒生 / 金证: 走券商提供的 query_settlement / QryAccount
//     接口, 用 settlement_id 反查 (T+1 settlement 是 A 股标准做法)
type BrokerQuerier interface {
	// QuerySettlement returns the broker's authoritative AccountSnapshot
	// for the given account at the given as-of time. Implementations
	// are expected to honour ctx for cancellation / deadline.
	QuerySettlement(ctx context.Context, accountID string, asOf time.Time) (*AccountSnapshot, error)
}

// LocalSnapshotter is the local-side adapter. The mock trader
// implements it directly; in a real production system, the local
// adapter would wrap the order store + position manager + cash ledger.
type LocalSnapshotter interface {
	SnapshotLocal(ctx context.Context, accountID string, asOf time.Time) (*LocalSnapshot, error)
}

// ============================================================
// Reconcile 算法 (pure — stateless; safe for concurrent calls)
// ============================================================

// Reconcile compares local vs broker snapshots and returns the list of
// discrepancies. The function is pure: it does not touch I/O, network,
// or the clock. Tolerance values are applied independently per field.
//
// Algorithm:
//  1. Cash: |local.total - broker.total| > CashTolerance → discrepancy
//  2. Position quantity: union(local.symbols, broker.symbols); for each,
//     |local.qty - broker.qty| > QuantityTolerance → discrepancy
//  3. Position market value: when qty matches, |local.mv - broker.mv| / max(mv, 1)
//     > MarketValueTol → discrepancy
//  4. Fee: per bucket |local.x - broker.x| > FeeTolerance → discrepancy
//  5. AsOf: if |local.asof - broker.asof| > 60s → info (timestamp drift)
func Reconcile(local LocalSnapshot, broker AccountSnapshot, cfg ReconciliationConfig) []Discrepancy {
	now := time.Now()
	if cfg.Now != nil {
		now = cfg.Now()
	}
	out := make([]Discrepancy, 0, 8)

	// 0) AsOf drift — informational only. > 60s drift is suspicious;
	//    could indicate the broker is reporting stale state.
	asOfDrift := local.AsOf.Sub(broker.AsOf)
	if asOfDrift < 0 {
		asOfDrift = -asOfDrift
	}
	if asOfDrift > 60*time.Second {
		out = append(out, Discrepancy{
			Kind:       KindCash, // 借用 cash kind — 没有 asof 专用 kind
			Severity:   SeverityInfo,
			Field:      "as_of_drift_seconds",
			LocalVal:   float64(local.AsOf.Unix()),
			BrokerVal:  float64(broker.AsOf.Unix()),
			Delta:      asOfDrift.Seconds(),
			Tolerance:  60,
			Note:       "本地 / 券商对账时间戳漂移 > 60s",
			DetectedAt: now,
		})
	}

	// 1) Cash total
	cashDelta := local.Cash.TotalCNY - broker.Cash.TotalCNY
	if absFloat(cashDelta) > cfg.CashTolerance {
		out = append(out, Discrepancy{
			Kind:       KindCash,
			Severity:   severityForCash(absFloat(cashDelta), cfg.CashTolerance),
			Field:      "total_cny",
			LocalVal:   local.Cash.TotalCNY,
			BrokerVal:  broker.Cash.TotalCNY,
			Delta:      cashDelta,
			Tolerance:  cfg.CashTolerance,
			Note:       cashNote(cashDelta),
			DetectedAt: now,
		})
	}

	// 2 + 3) Positions: walk union of symbols
	symbols := unionSymbols(local.Positions, broker.Positions)
	for _, sym := range symbols {
		lp, lok := local.Positions[sym]
		bp, bok := broker.Positions[sym]
		if !lok {
			out = append(out, Discrepancy{
				Kind:       KindMissingLocal,
				Severity:   SeverityCritical,
				Symbol:     sym,
				LocalVal:   0,
				BrokerVal:  bp.Quantity,
				Delta:      -bp.Quantity,
				Tolerance:  cfg.QuantityTolerance,
				Note:       "券商有持仓, 本地缺失 (疑似漏单)",
				DetectedAt: now,
			})
			continue
		}
		if !bok {
			out = append(out, Discrepancy{
				Kind:       KindMissingBroker,
				Severity:   SeverityCritical,
				Symbol:     sym,
				LocalVal:   lp.Quantity,
				BrokerVal:  0,
				Delta:      lp.Quantity,
				Tolerance:  cfg.QuantityTolerance,
				Note:       "本地有持仓, 券商缺失 (疑似鬼单)",
				DetectedAt: now,
			})
			continue
		}
		// qty diff
		qtyDelta := lp.Quantity - bp.Quantity
		if absFloat(qtyDelta) > cfg.QuantityTolerance {
			out = append(out, Discrepancy{
				Kind:       KindQuantity,
				Severity:   severityForQty(absFloat(qtyDelta)),
				Symbol:     sym,
				Field:      "quantity",
				LocalVal:   lp.Quantity,
				BrokerVal:  bp.Quantity,
				Delta:      qtyDelta,
				Tolerance:  cfg.QuantityTolerance,
				Note:       "持仓数量偏差超容差",
				DetectedAt: now,
			})
		} else if absFloat(qtyDelta) > 0 {
			// qty matches within tolerance but isn't exactly equal
			// (only happens when QuantityTolerance > 0). Emit info
			// for traceability.
			out = append(out, Discrepancy{
				Kind:       KindQuantity,
				Severity:   SeverityInfo,
				Symbol:     sym,
				Field:      "quantity",
				LocalVal:   lp.Quantity,
				BrokerVal:  bp.Quantity,
				Delta:      qtyDelta,
				Tolerance:  cfg.QuantityTolerance,
				Note:       "持仓数量在容差内, 但不严格相等",
				DetectedAt: now,
			})
		}
		// market value: only if qty matches
		if absFloat(qtyDelta) <= cfg.QuantityTolerance {
			mvDelta := lp.MarketValue - bp.MarketValue
			base := lp.MarketValue
			if base < bp.MarketValue {
				base = bp.MarketValue
			}
			rel := 0.0
			if base > 0 {
				rel = absFloat(mvDelta) / base
			}
			if rel > cfg.MarketValueTol {
				out = append(out, Discrepancy{
					Kind:       KindMarketValue,
					Severity:   severityForMV(rel, cfg.MarketValueTol),
					Symbol:     sym,
					Field:      "market_value",
					LocalVal:   lp.MarketValue,
					BrokerVal:  bp.MarketValue,
					Delta:      mvDelta,
					Tolerance:  cfg.MarketValueTol,
					Note:       "持仓市值相对偏差超容差 (行情刷新延迟?)",
					DetectedAt: now,
				})
			}
		}
	}

	// 4) Fee buckets
	fees := []struct {
		field string
		l, b  float64
	}{
		{"commission", local.Fees.Commission, broker.Fees.Commission},
		{"stamp_tax", local.Fees.StampTax, broker.Fees.StampTax},
		{"transfer", local.Fees.Transfer, broker.Fees.Transfer},
		{"other", local.Fees.Other, broker.Fees.Other},
	}
	for _, f := range fees {
		d := f.l - f.b
		if absFloat(d) > cfg.FeeTolerance {
			out = append(out, Discrepancy{
				Kind:       KindFee,
				Severity:   severityForFee(absFloat(d), cfg.FeeTolerance),
				Field:      f.field,
				LocalVal:   f.l,
				BrokerVal:  f.b,
				Delta:      d,
				Tolerance:  cfg.FeeTolerance,
				Note:       "费用偏差超容差: " + f.field,
				DetectedAt: now,
			})
		}
	}

	// Sort: critical first, then warning, then info; within each
	// group, alphabetical by kind+symbol for stable display.
	sort.SliceStable(out, func(i, j int) bool {
		if rankSeverity(out[i].Severity) != rankSeverity(out[j].Severity) {
			return rankSeverity(out[i].Severity) > rankSeverity(out[j].Severity)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Symbol < out[j].Symbol
	})

	return out
}

// ============================================================
// Worker — 周期性对账 loop
// ============================================================

// AlertDispatcher is the minimal contract the worker needs from
// pkg/alert. We use an interface (instead of *alert.AlertManager) so
// the worker remains test-friendly and decoupled from the alert
// package's internals. The HTTP wiring layer passes the live manager.
type AlertDispatcher interface {
	DispatchReconciliationAlerts(ctx context.Context, accountID string, discrepancies []Discrepancy)
}

// ReportPersister is called once per successful reconciliation cycle.
// The wiring layer is expected to forward the report to:
//   - on-disk JSON file under cfg.ReportPath
//   - database (future)
//   - downstream consumers (regulator interfaces)
//
// The worker itself only keeps the most recent N reports in memory;
// persistence beyond that is the caller's responsibility.
type ReportPersister interface {
	PersistReport(ctx context.Context, report ReconciliationReport) error
}

// NullReportPersister is a no-op persister for tests and for callers
// that don't need on-disk reports.
type NullReportPersister struct{}

// PersistReport implements ReportPersister.
func (NullReportPersister) PersistReport(_ context.Context, _ ReconciliationReport) error {
	return nil
}

// FSReportPersister writes each ReconciliationReport to
// `<dir>/rec-YYYYMMDD-HHMM.json`. It is the production default;
// safe for concurrent use by one writer (the worker goroutine),
// but if multiple workers share the same directory, callers should
// add a mutex or partition by account id.
type FSReportPersister struct {
	dir string
}

// NewFSReportPersister returns a persister that writes JSON reports
// to dir. Creates the directory if it doesn't exist (parents included).
func NewFSReportPersister(dir string) (*FSReportPersister, error) {
	if dir == "" {
		return nil, fmt.Errorf("report dir must not be empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create report dir %q: %w", dir, err)
	}
	return &FSReportPersister{dir: dir}, nil
}

// PersistReport writes r as JSON to <dir>/rec-YYYYMMDD-HHMM.json.
// Overwrites any existing file for the same minute (rare in practice).
func (p *FSReportPersister) PersistReport(_ context.Context, r ReconciliationReport) error {
	filename := fmt.Sprintf("rec-%s.json", r.GeneratedAt.Format("20060102-150405"))
	path := filepath.Join(p.dir, filename)
	payload, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write report %q: %w", path, err)
	}
	return nil
}

// Dir returns the configured directory (read-only).
func (p *FSReportPersister) Dir() string { return p.dir }

// HistoryBuffer is a bounded ring buffer of recent reports. It is
// safe for concurrent reads (the HTTP layer) and a single writer
// (the worker). Capacity is fixed at construction.
type HistoryBuffer struct {
	mu      sync.RWMutex
	entries []ReconciliationReport
	idx     int
	full    bool
	cap     int
}

// NewHistoryBuffer returns a buffer with the given capacity. A
// non-positive capacity is replaced with 1.
func NewHistoryBuffer(c int) *HistoryBuffer {
	if c <= 0 {
		c = 1
	}
	return &HistoryBuffer{
		entries: make([]ReconciliationReport, c),
		cap:     c,
	}
}

// Append stores a single report, evicting the oldest if at capacity.
func (h *HistoryBuffer) Append(r ReconciliationReport) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[h.idx] = r
	h.idx = (h.idx + 1) % h.cap
	if h.idx == 0 {
		h.full = true
	}
}

// Snapshot returns a copy of stored reports in newest-first order.
// Empty slice when the buffer has never been written to.
func (h *HistoryBuffer) Snapshot() []ReconciliationReport {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.full && h.idx == 0 {
		return []ReconciliationReport{}
	}
	n := h.idx
	if h.full {
		n = h.cap
	}
	out := make([]ReconciliationReport, 0, n)
	for i := 0; i < n; i++ {
		pos := (h.idx - 1 - i + h.cap*2) % h.cap
		out = append(out, h.entries[pos])
	}
	return out
}

// Latest returns the most recent report. Returns nil when empty.
func (h *HistoryBuffer) Latest() *ReconciliationReport {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.full && h.idx == 0 {
		return nil
	}
	pos := h.idx - 1
	if pos < 0 {
		pos = h.cap - 1
	}
	r := h.entries[pos]
	return &r
}

// Len returns the number of stored reports.
func (h *HistoryBuffer) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.full {
		return h.cap
	}
	return h.idx
}

// ReconciliationWorker is the periodic reconciliation engine. It
// runs a single goroutine that ticks on cfg.Interval, fetches local
// and broker snapshots, runs the pure Reconcile function, persists
// the report, and dispatches alerts when discrepancies are found.
type ReconciliationWorker struct {
	cfg       ReconciliationConfig
	broker    BrokerQuerier
	local     LocalSnapshotter
	persister ReportPersister
	dispatch  AlertDispatcher
	history   *HistoryBuffer
	logger    zerolog.Logger
}

// NewReconciliationWorker wires the worker to its dependencies. The
// returned struct is ready to Start; the loop does not begin until
// Start is called.
//
// Pass nil for persister to disable persistence (useful in tests
// that don't need the side-effect). Pass nil for dispatcher to
// silently skip alert routing.
func NewReconciliationWorker(
	cfg ReconciliationConfig,
	broker BrokerQuerier,
	local LocalSnapshotter,
	persister ReportPersister,
	dispatcher AlertDispatcher,
	history *HistoryBuffer,
	logger zerolog.Logger,
) *ReconciliationWorker {
	if cfg.Interval <= 0 {
		// Production default: 15 minutes (regulatory minimum for
		// 证券资金对账 per 中国结算 《证券资金对账指引》 (2023) §3.2).
		cfg.Interval = 15 * time.Minute
	}
	// Note: we do NOT clamp to a minimum of 1s; the worker is also
	// useful for tests with sub-second intervals (e.g. 20ms for
	// fast-loop smoke tests). Production callers should set
	// Interval to ≥ 15 * time.Minute as a hard floor.
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = 100
	}
	if persister == nil {
		persister = NullReportPersister{}
	}
	if history == nil {
		history = NewHistoryBuffer(cfg.HistoryLimit)
	}
	return &ReconciliationWorker{
		cfg:       cfg,
		broker:    broker,
		local:     local,
		persister: persister,
		dispatch:  dispatcher,
		history:   history,
		logger:    logger.With().Str("component", "reconciliation_worker").Logger(),
	}
}

// History returns the in-memory report buffer for HTTP exposure.
func (w *ReconciliationWorker) History() *HistoryBuffer { return w.history }

// Config returns the worker's effective configuration (a copy).
func (w *ReconciliationWorker) Config() ReconciliationConfig {
	return w.cfg
}

// Start begins the periodic reconciliation loop. It blocks until ctx
// is cancelled. The first run happens after cfg.Interval (not
// immediately) so that startup-time configuration has a chance to
// take effect.
//
// To force an immediate reconciliation, call ReconcileOnce directly.
func (w *ReconciliationWorker) Start(ctx context.Context) {
	if !w.cfg.Enabled {
		w.logger.Info().Msg("ReconciliationWorker disabled by config; not starting")
		return
	}
	if w.broker == nil || w.local == nil {
		w.logger.Warn().Msg("ReconciliationWorker missing broker/local querier; not starting")
		return
	}
	w.logger.Info().
		Dur("interval", w.cfg.Interval).
		Str("report_path", w.cfg.ReportPath).
		Msg("ReconciliationWorker starting")

	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info().Msg("ReconciliationWorker stopping (context cancelled)")
			return
		case <-ticker.C:
			if _, err := w.ReconcileOnce(ctx, ""); err != nil {
				w.logger.Warn().Err(err).Msg("ReconciliationWorker cycle failed")
			}
		}
	}
}

// ReconcileOnce runs a single reconciliation cycle for the given
// account. When accountID is empty, the worker uses the local
// snapshot's AccountID (or, if both are empty, fails).
//
// Returns the report (also stored in History and dispatched to
// alerts/persister). Errors come from the broker / local fetch path;
// the pure Reconcile function never errors.
func (w *ReconciliationWorker) ReconcileOnce(ctx context.Context, accountID string) (*ReconciliationReport, error) {
	now := time.Now()
	if w.cfg.Now != nil {
		now = w.cfg.Now()
	}
	if accountID == "" && w.local != nil {
		// Best-effort accountID discovery: a single "default" fetch.
		probe, err := w.local.SnapshotLocal(ctx, "", now)
		if err == nil && probe != nil {
			accountID = probe.AccountID
		}
	}
	if accountID == "" {
		return nil, fmt.Errorf("account id is required (no default account)")
	}

	// Fetch both sides in parallel — they are independent and there
	// is no point serialising two network calls.
	var (
		localSnap  *LocalSnapshot
		brokerSnap *AccountSnapshot
		localErr   error
		brokerErr  error
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		localSnap, localErr = w.local.SnapshotLocal(ctx, accountID, now)
	}()
	go func() {
		defer wg.Done()
		brokerSnap, brokerErr = w.broker.QuerySettlement(ctx, accountID, now)
	}()
	wg.Wait()

	if localErr != nil {
		return nil, fmt.Errorf("local snapshot: %w", localErr)
	}
	if brokerErr != nil {
		return nil, fmt.Errorf("broker snapshot: %w", brokerErr)
	}
	if localSnap == nil || brokerSnap == nil {
		return nil, fmt.Errorf("nil snapshot (local=%v, broker=%v)", localSnap, brokerSnap)
	}

	// Reconcile (pure).
	discs := Reconcile(*localSnap, *brokerSnap, w.cfg)

	// Build report.
	rep := ReconciliationReport{
		SchemaVersion:  "rec-1.0",
		GeneratedAt:    now,
		AccountID:      accountID,
		LocalAsOf:      localSnap.AsOf,
		BrokerAsOf:     brokerSnap.AsOf,
		BridgeReconcID: brokerSnap.ReconciliationID,
		Local:          *localSnap,
		Broker:         *brokerSnap,
		Discrepancies:  discs,
		Healthy:        len(discs) == 0 || allInfoOnly(discs),
	}
	for _, d := range discs {
		switch d.Severity {
		case SeverityCritical:
			rep.CriticalCount++
		case SeverityWarning:
			rep.WarningCount++
		case SeverityInfo:
			rep.InfoCount++
		}
	}
	if !rep.Healthy {
		rep.Note = fmt.Sprintf("对账偏差: critical=%d warning=%d info=%d",
			rep.CriticalCount, rep.WarningCount, rep.InfoCount)
	}

	// Persist (best-effort: log on failure but don't fail the cycle).
	if err := w.persister.PersistReport(ctx, rep); err != nil {
		w.logger.Warn().Err(err).Str("account_id", accountID).
			Msg("ReconciliationWorker persist failed")
	}

	// Dispatch alerts.
	if w.dispatch != nil && len(discs) > 0 {
		w.dispatch.DispatchReconciliationAlerts(ctx, accountID, discs)
	}

	// Record in history.
	w.history.Append(rep)

	w.logger.Info().
		Str("account_id", accountID).
		Int("discrepancies", len(discs)).
		Int("critical", rep.CriticalCount).
		Int("warning", rep.WarningCount).
		Bool("healthy", rep.Healthy).
		Msg("ReconciliationWorker cycle complete")

	return &rep, nil
}

// ============================================================
// internal helpers
// ============================================================

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func unionSymbols(a, b map[string]PositionSnap) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for s := range a {
		seen[s] = struct{}{}
	}
	for s := range b {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func rankSeverity(s DiscrepancySeverity) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// severityForCash escalates by absolute CNY delta. 1-100 = warning,
// > 100 = critical. Sub-1-cent deltas are masked by tolerance.
func severityForCash(absDelta, tol float64) DiscrepancySeverity {
	switch {
	case absDelta > 100:
		return SeverityCritical
	case absDelta > tol:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// severityForQty escalates by absolute share count. 1-1000 shares
// = warning, > 1000 = critical (assumes a 10 CNY avg price ≈
// 10,000 CNY position, so > 1,000 shares ≈ 1,000,000 CNY 偏差).
func severityForQty(absDelta float64) DiscrepancySeverity {
	switch {
	case absDelta > 1000:
		return SeverityCritical
	case absDelta > 0:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// severityForMV: 2x tolerance = warning, 5x tolerance = critical.
func severityForMV(rel, tol float64) DiscrepancySeverity {
	switch {
	case rel > 5*tol:
		return SeverityCritical
	case rel > 2*tol:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// severityForFee: 10x tolerance = critical (fees are usually
// sub-100 CNY per trade; 10x deviation is a real miscalculation).
func severityForFee(absDelta, tol float64) DiscrepancySeverity {
	switch {
	case absDelta > 10*tol:
		return SeverityCritical
	case absDelta > tol:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

func cashNote(delta float64) string {
	if delta > 0 {
		return "本地资金 > 券商资金 (本地多记, 疑似重复扣款)"
	}
	return "本地资金 < 券商资金 (本地少记, 疑似漏扣手续费 / 印花税)"
}

// allInfoOnly returns true when every discrepancy is at info level.
// A reconciliation is "healthy" when there are no discrepancies at
// all OR every discrepancy is informational (within tolerance).
func allInfoOnly(discs []Discrepancy) bool {
	for _, d := range discs {
		if d.Severity != SeverityInfo {
			return false
		}
	}
	return true
}
