// Package compliance — Abnormal-trade detection (P2-5).
//
// 监管依据:
//   - 《证券市场操纵行为认定指引(试行)》(证监会 2007, 证券法 55 条)
//   - 《上海/深圳证券交易所交易规则》(2023 修订) 第六章 "交易监督"
//   - 《关于完善证券交易异常情况监测与报告机制的通知》(2022)
//
// 6 类异常行为 (沪深交易所自律监管关注):
//   1. 频繁撒单 (Frequent Cancellation) — 同一账户/标的在短窗口内
//      撤单率超过阈值 (例如 1 分钟内 ≥ 3 笔撤单且撤单率 > 50%)。
//   2. 自成交 (Self-Trade) — 同一账户对同一标的发出方向相反的
//      报单且成交 (买方 ≥ 卖方时)。应禁止。
//   3. 对倒 (Wash / Collusive Trade) — 不同账户在相近价位 +
//      相近时间 + 相同数量地反向成交, 制造虚假活跃度。
//   4. 洗售 (Matched Flipping) — 同一账户在短窗口内对同一标的
//      先买后卖再买 (或反向), 涉嫌拉抬/打压。
//   5. 虚假申报 (Spoofing) — 在最优买卖价上挂出大额报单但
//      在 < 500ms 内撤单, 制造虚假买卖深度。
//   6. 拉抬打压 (Manipulation) — 短时间内连续以高 (或低) 于市价
//      的价格成交, 显著偏离最近 N 笔 VWAP, 涉嫌操纵股价。
//
// 设计目标:
//   - 自包含: 输入 = 滑窗内的 OrderRecord + TradeRecord,
//     输出 = 命中的 Alert 列表 (按行为类型分组)。
//   - 阈值可配: 每个 Detector 都有独立的 Config, 测试可注入
//     紧阈值, 生产用宽阈值。
//   - 可审计: 每条 Alert 包含触发证据 (时间窗口、相关订单 ID、
//     价格/数量), 便于监管回溯。
package compliance

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ============================================================
// P2-5: 异常交易检测 — 数据类型
// ============================================================

// AbnormalCategory enumerates the 6 detected behavior types.
// String() returns the canonical regulatory name; UI / log code
// uses the name verbatim for traceability.
type AbnormalCategory string

const (
	CategoryFrequentCancel  AbnormalCategory = "frequent_cancel"   // 频繁撒单
	CategorySelfTrade       AbnormalCategory = "self_trade"        // 自成交
	CategoryWashTrade       AbnormalCategory = "wash_trade"        // 对倒
	CategoryMatchedFlipping AbnormalCategory = "matched_flipping"  // 洗售
	CategorySpoofing        AbnormalCategory = "spoofing"          // 虚假申报
	CategoryManipulation    AbnormalCategory = "manipulation"      // 拉抬打压
)

// String returns the canonical Chinese name of the category.
// Used in log lines and the JSON-serialised alert payload.
func (c AbnormalCategory) String() string {
	switch c {
	case CategoryFrequentCancel:
		return "频繁撒单"
	case CategorySelfTrade:
		return "自成交"
	case CategoryWashTrade:
		return "对倒"
	case CategoryMatchedFlipping:
		return "洗售"
	case CategorySpoofing:
		return "虚假申报"
	case CategoryManipulation:
		return "拉抬打压"
	default:
		return string(c)
	}
}

// AbnormalAlert is the structured output of a single detection.
// One alert = one (category, account/symbol, window) tuple. If the
// same behavior repeats in different windows, multiple alerts are
// emitted (each with its own evidence slice).
type AbnormalAlert struct {
	Category   AbnormalCategory // 异常类型
	AccountID  string           // 触发账户 (cross-account 场景可空)
	Symbol     string           // 触发标的
	DetectedAt time.Time        // 检测时间
	WindowFrom time.Time        // 滑窗起点
	WindowTo   time.Time        // 滑窗终点
	Severity   string           // "warning" / "critical"
	Summary    string           // 一句话中文摘要
	Evidence   []AlertEvidence  // 触发证据 (订单/成交流水)
}

// AlertEvidence points to a single OrderRecord or TradeRecord
// that contributed to the alert. The detector carries enough
// IDs and prices for the operator to reconstruct the behavior.
type AlertEvidence struct {
	OrderID    string    `json:"order_id,omitempty"`
	TradeID    string    `json:"trade_id,omitempty"`
	Symbol     string    `json:"symbol"`
	Direction  string    `json:"direction"`
	Quantity   float64   `json:"quantity"`
	Price      float64   `json:"price"`
	Timestamp  time.Time `json:"timestamp"`
	Cancelled  bool      `json:"cancelled,omitempty"` // 仅订单: 是否被撤
	Note       string    `json:"note,omitempty"`
}

// ============================================================
// P2-5: Detector 接口 + 6 个实现
// ============================================================

// Detector is the interface every abnormal-trade detector implements.
//
// The Detector returns zero or more alerts. An empty slice is
// "no anomaly detected in this window" — not an error. Detectors
// are read-only; they never mutate the input slices.
type Detector interface {
	Name() string
	Category() AbnormalCategory
	// Detect runs the detection over the supplied window. `orders`
	// and `trades` are pre-filtered to the window by the orchestrator
	// (see AbnormalDetector below); the detector works on what it
	// gets without re-filtering.
	Detect(accountID string, orders []OrderRecord, trades []TradeRecord, now time.Time) []AbnormalAlert
}

// OrderRecord is the input shape detectors operate on. It mirrors
// pkg/live.OrderRecord (which we re-declare as a value-only copy
// to keep the compliance package decoupled from the live package
// — see ODR-021 service-merge decision; the compliance module is
// deliberately a leaf package with no transitive dependencies on
// trader internals). The JSON tags use snake_case to match the
// HTTP wire format used by handlers_compliance.go.
type OrderRecord struct {
	OrderID      string    `json:"order_id"`
	Symbol       string    `json:"symbol"`
	Direction    string    `json:"direction"`
	Quantity     float64   `json:"quantity"`
	Price        float64   `json:"price"`
	FilledQty    float64   `json:"filled_qty"`
	AvgFillPrice float64   `json:"avg_fill_price"`
	Status       string    `json:"status"`
	SubmittedAt  time.Time `json:"submitted_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TradeRecord is the input shape for fill-level analysis. JSON
// tags are snake_case to match the HTTP wire format.
type TradeRecord struct {
	TradeID   string    `json:"trade_id"`
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Direction string    `json:"direction"`
	Quantity  float64   `json:"quantity"`
	Price     float64   `json:"price"`
	Fee       float64   `json:"fee"`
	TradeTime time.Time `json:"trade_time"`
}

// ============================================================
// 默认阈值 (可被 SetThresholds 整体覆盖)
// ============================================================

// Thresholds aggregates the per-detector thresholds. Defaults
// follow the 沪深交易所自律监管 关注线; production environments
// may tighten these via the orchestrator's `SetThresholds` API.
type Thresholds struct {
	FrequentCancel  FrequentCancelConfig
	SelfTrade       SelfTradeConfig
	WashTrade       WashTradeConfig
	MatchedFlipping MatchedFlippingConfig
	Spoofing        SpoofingConfig
	Manipulation    ManipulationConfig
}

// FrequentCancelConfig — 频繁撒单: 短窗口内撤单数 / 报单数 ≥ 阈值。
type FrequentCancelConfig struct {
	Window         time.Duration // 滑窗长度 (e.g. 1 minute)
	MinCancelCount int           // 触发下限 (e.g. 3)
	MinCancelRate  float64       // 撤单率 (e.g. 0.5 = 50%)
}

// SelfTradeConfig — 自成交: 在成交流中匹配 (account, symbol, time, qty)。
type SelfTradeConfig struct {
	Window        time.Duration // 滑窗长度
	MinQuantity   float64       // 触发下限 (e.g. 1 手 = 100 股)
}

// WashTradeConfig — 对倒: 不同账户在相近时间 + 相近价位 + 相同数量反向成交。
type WashTradeConfig struct {
	Window            time.Duration // 滑窗长度 (e.g. 5 minutes)
	PriceTolerancePct float64       // 价差容忍度 (e.g. 0.005 = 0.5%)
	MinQuantity       float64       // 触发下限
	MinAccountPair    int           // 最少涉事账户数 (2 = 一对)
}

// MatchedFlippingConfig — 洗售: 同一账户对同一标的在短窗口内
// 方向反复切换 (buy→sell→buy 或 sell→buy→sell)。
type MatchedFlippingConfig struct {
	Window         time.Duration // 滑窗长度
	MinFlips       int           // 最少方向切换次数 (3 = 2 次翻转)
	MinTotalVolume float64       // 累计成交数量下限
}

// SpoofingConfig — 虚假申报: 限价大单在 < CancelLatency 内撤单。
type SpoofingConfig struct {
	Window         time.Duration // 滑窗
	CancelLatency  time.Duration // 报单到撤单的最长时延 (e.g. 500ms)
	MinQuantity    float64       // 触发下限 (e.g. 1 万股)
	MinCancelCount int           // 短时内累计撤单数
}

// ManipulationConfig — 拉抬打压: 价格连续偏离 VWAP 超过阈值。
type ManipulationConfig struct {
	Window         time.Duration // 滑窗 (e.g. 5 minutes)
	MinTrades      int           // 最少成交笔数
	PriceDeviation float64       // 偏离 VWAP 的最小比例 (e.g. 0.02 = 2%)
	VWAPLookback   int           // 计算 VWAP 的样本数
}

// DefaultThresholds returns the regulatory-default thresholds.
// Documented values mirror the 沪深交易所 2023 自律监管关注线;
// callers can override per environment via SetThresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		FrequentCancel: FrequentCancelConfig{
			Window:         1 * time.Minute,
			MinCancelCount: 3,
			MinCancelRate:  0.5,
		},
		SelfTrade: SelfTradeConfig{
			Window:      5 * time.Minute,
			MinQuantity: 100, // 1 手
		},
		WashTrade: WashTradeConfig{
			Window:            5 * time.Minute,
			PriceTolerancePct: 0.005,
			MinQuantity:       1000, // 1 万股
			MinAccountPair:    2,
		},
		MatchedFlipping: MatchedFlippingConfig{
			Window:         30 * time.Minute,
			MinFlips:       3,
			MinTotalVolume: 1000,
		},
		Spoofing: SpoofingConfig{
			Window:         1 * time.Minute,
			CancelLatency:  500 * time.Millisecond,
			MinQuantity:    10_000, // 1 万股
			MinCancelCount: 2,
		},
		Manipulation: ManipulationConfig{
			Window:         5 * time.Minute,
			MinTrades:      3,
			PriceDeviation: 0.02,
			VWAPLookback:   20,
		},
	}
}

// ============================================================
// Orchestrator: AbnormalDetector
// ============================================================

// AbnormalDetector runs the 6-category detection over a sliding
// window. It is safe for concurrent use (the underlying detectors
// are stateless; only the threshold map is mutex-guarded).
type AbnormalDetector struct {
	mu         sync.RWMutex
	thresholds Thresholds
	detectors  []Detector
}

// NewAbnormalDetector constructs an orchestrator with the default
// thresholds. Use SetThresholds to override per environment.
func NewAbnormalDetector() *AbnormalDetector {
	t := DefaultThresholds()
	return &AbnormalDetector{
		thresholds: t,
		detectors: []Detector{
			&frequentCancelDetector{thresholds: t.FrequentCancel},
			&selfTradeDetector{thresholds: t.SelfTrade},
			&washTradeDetector{thresholds: t.WashTrade},
			&matchedFlippingDetector{thresholds: t.MatchedFlipping},
			&spoofingDetector{thresholds: t.Spoofing},
			&manipulationDetector{thresholds: t.Manipulation},
		},
	}
}

// SetThresholds atomically swaps in a new threshold set. Detectors
// are recreated from the new set on the next call to RunAll —
// this keeps the per-detector state immutable during a single run.
func (d *AbnormalDetector) SetThresholds(t Thresholds) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.thresholds = t
	d.detectors = []Detector{
		&frequentCancelDetector{thresholds: t.FrequentCancel},
		&selfTradeDetector{thresholds: t.SelfTrade},
		&washTradeDetector{thresholds: t.WashTrade},
		&matchedFlippingDetector{thresholds: t.MatchedFlipping},
		&spoofingDetector{thresholds: t.Spoofing},
		&manipulationDetector{thresholds: t.Manipulation},
	}
}

// Thresholds returns a copy of the current threshold set. The copy
// is taken under the read lock so callers can iterate safely.
func (d *AbnormalDetector) Thresholds() Thresholds {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.thresholds
}

// RunAll runs all 6 detectors over the supplied window and
// returns the merged alert list. Alerts from different detectors
// are not deduplicated — multiple categories can fire on the same
// incident (e.g. spoofing + manipulation), and operators need
// to see all of them.
func (d *AbnormalDetector) RunAll(accountID string, orders []OrderRecord, trades []TradeRecord, now time.Time) []AbnormalAlert {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []AbnormalAlert
	for _, det := range d.detectors {
		out = append(out, det.Detect(accountID, orders, trades, now)...)
	}
	return out
}

// ============================================================
// Detector 1: 频繁撒单 (FrequentCancel)
// ============================================================

type frequentCancelDetector struct {
	thresholds FrequentCancelConfig
}

func (d *frequentCancelDetector) Name() string              { return "frequent_cancel" }
func (d *frequentCancelDetector) Category() AbnormalCategory { return CategoryFrequentCancel }

func (d *frequentCancelDetector) Detect(_ string, orders []OrderRecord, _ []TradeRecord, now time.Time) []AbnormalAlert {
	// Bucket orders by symbol; per-symbol analysis is the regulator's
	// framing ("对同一标的的频繁撒单").
	bySym := make(map[string][]OrderRecord)
	for _, o := range orders {
		bySym[o.Symbol] = append(bySym[o.Symbol], o)
	}
	var out []AbnormalAlert
	for sym, list := range bySym {
		// Sort by submission time so the window scan is O(N).
		sort.Slice(list, func(i, j int) bool { return list[i].SubmittedAt.Before(list[j].SubmittedAt) })
		windowStart := now.Add(-d.thresholds.Window)
		submitted, cancelled := 0, 0
		var evidence []AlertEvidence
		var windowFrom, windowTo time.Time
		for _, o := range list {
			if o.SubmittedAt.Before(windowStart) {
				continue
			}
			submitted++
			if windowFrom.IsZero() || o.SubmittedAt.Before(windowFrom) {
				windowFrom = o.SubmittedAt
			}
			if o.SubmittedAt.After(windowTo) {
				windowTo = o.SubmittedAt
			}
			if o.Status == "cancelled" {
				cancelled++
				evidence = append(evidence, AlertEvidence{
					OrderID: o.OrderID, Symbol: o.Symbol, Direction: o.Direction,
					Quantity: o.Quantity, Price: o.Price, Timestamp: o.SubmittedAt, Cancelled: true,
				})
			}
		}
		if submitted == 0 {
			continue
		}
		cancelRate := float64(cancelled) / float64(submitted)
		if cancelled >= d.thresholds.MinCancelCount && cancelRate >= d.thresholds.MinCancelRate {
			out = append(out, AbnormalAlert{
				Category:   CategoryFrequentCancel,
				Symbol:     sym,
				DetectedAt: now,
				WindowFrom: windowFrom,
				WindowTo:   windowTo,
				Severity:   "warning",
				Summary: fmt.Sprintf("标的 %s 在 %s 窗口内撤单 %d 笔, 撤单率 %.1f%%",
					sym, d.thresholds.Window, cancelled, cancelRate*100),
				Evidence: evidence,
			})
		}
	}
	return out
}

// ============================================================
// Detector 2: 自成交 (SelfTrade)
// ============================================================

type selfTradeDetector struct {
	thresholds SelfTradeConfig
}

func (d *selfTradeDetector) Name() string              { return "self_trade" }
func (d *selfTradeDetector) Category() AbnormalCategory { return CategorySelfTrade }

func (d *selfTradeDetector) Detect(accountID string, _ []OrderRecord, trades []TradeRecord, now time.Time) []AbnormalAlert {
	if accountID == "" {
		return nil
	}
	windowStart := now.Add(-d.thresholds.Window)
	// Index trades by symbol for O(N) same-symbol pairing.
	bySym := make(map[string][]TradeRecord)
	for _, t := range trades {
		if t.TradeTime.Before(windowStart) {
			continue
		}
		bySym[t.Symbol] = append(bySym[t.Symbol], t)
	}
	var out []AbnormalAlert
	for sym, list := range bySym {
		// Sort by trade time.
		sort.Slice(list, func(i, j int) bool { return list[i].TradeTime.Before(list[j].TradeTime) })
		// For each buy, find a later sell (or vice versa) of the
		// same quantity on the same symbol within the window.
		for i := 0; i < len(list); i++ {
			a := list[i]
			if a.Quantity < d.thresholds.MinQuantity {
				continue
			}
			for j := i + 1; j < len(list); j++ {
				b := list[j]
				if a.Direction == b.Direction {
					continue
				}
				if a.Quantity != b.Quantity {
					continue
				}
				out = append(out, AbnormalAlert{
					Category:   CategorySelfTrade,
					AccountID:  accountID,
					Symbol:     sym,
					DetectedAt: now,
					WindowFrom: a.TradeTime,
					WindowTo:   b.TradeTime,
					Severity:   "critical",
					Summary: fmt.Sprintf("账户 %s 在标的 %s 上自成交, 数量 %.0f 价位 %.2f/%.2f",
						accountID, sym, a.Quantity, a.Price, b.Price),
					Evidence: []AlertEvidence{
						{TradeID: a.TradeID, Symbol: sym, Direction: a.Direction, Quantity: a.Quantity, Price: a.Price, Timestamp: a.TradeTime},
						{TradeID: b.TradeID, Symbol: sym, Direction: b.Direction, Quantity: b.Quantity, Price: b.Price, Timestamp: b.TradeTime},
					},
				})
				break
			}
		}
	}
	return out
}

// ============================================================
// Detector 3: 对倒 (WashTrade)
// ============================================================

type washTradeDetector struct {
	thresholds WashTradeConfig
}

func (d *washTradeDetector) Name() string              { return "wash_trade" }
func (d *washTradeDetector) Category() AbnormalCategory { return CategoryWashTrade }

// accountInTrade looks up an account id attached to a trade.
// In the simple cross-account model we get the account from the
// order_id (assumed encoded as "<account>:<order>"); when the
// account is unknown we fall back to the order_id itself.
//
// For compliance use, the orchestrator typically calls RunAll
// once per account, so the cross-account pairing is reconstructed
// by the operator / regulator in a post-hoc review; the detector
// focuses on "two opposite-direction fills in the same window
// at the same price with the same quantity" which is the
// canonical wash signature.
func (d *washTradeDetector) Detect(_ string, _ []OrderRecord, trades []TradeRecord, now time.Time) []AbnormalAlert {
	windowStart := now.Add(-d.thresholds.Window)
	var inWindow []TradeRecord
	for _, t := range trades {
		if t.TradeTime.Before(windowStart) {
			continue
		}
		if t.Quantity < d.thresholds.MinQuantity {
			continue
		}
		inWindow = append(inWindow, t)
	}
	if len(inWindow) < 2 {
		return nil
	}
	sort.Slice(inWindow, func(i, j int) bool { return inWindow[i].TradeTime.Before(inWindow[j].TradeTime) })
	// Group by (symbol, quantity, rounded-price).
	type key struct{ sym string; qty float64; priceKey int64 }
	groups := make(map[key][]TradeRecord)
	for _, t := range inWindow {
		priceKey := int64(t.Price * 1000) // 3-decimal rounding for grouping
		k := key{t.Symbol, t.Quantity, priceKey}
		groups[k] = append(groups[k], t)
	}
	var out []AbnormalAlert
	for k, list := range groups {
		if len(list) < d.thresholds.MinAccountPair {
			continue
		}
		// Need at least one buy + one sell in the bucket.
		hasBuy, hasSell := false, false
		for _, t := range list {
			if t.Direction == "buy" || t.Direction == "long" {
				hasBuy = true
			} else if t.Direction == "sell" || t.Direction == "short" || t.Direction == "close" {
				hasSell = true
			}
		}
		if !hasBuy || !hasSell {
			continue
		}
		var evidence []AlertEvidence
		for _, t := range list {
			evidence = append(evidence, AlertEvidence{
				TradeID: t.TradeID, Symbol: t.Symbol, Direction: t.Direction,
				Quantity: t.Quantity, Price: t.Price, Timestamp: t.TradeTime,
			})
		}
		out = append(out, AbnormalAlert{
			Category:   CategoryWashTrade,
			Symbol:     k.sym,
			DetectedAt: now,
			WindowFrom: list[0].TradeTime,
			WindowTo:   list[len(list)-1].TradeTime,
			Severity:   "critical",
			Summary: fmt.Sprintf("标的 %s 在 %s 窗口内出现 %d 笔同价位+同数量对倒, 单笔 %.0f 股 @ %.2f",
				k.sym, d.thresholds.Window, len(list), k.qty, float64(k.priceKey)/1000),
			Evidence:   evidence,
		})
	}
	return out
}

// ============================================================
// Detector 4: 洗售 (MatchedFlipping)
// ============================================================

type matchedFlippingDetector struct {
	thresholds MatchedFlippingConfig
}

func (d *matchedFlippingDetector) Name() string              { return "matched_flipping" }
func (d *matchedFlippingDetector) Category() AbnormalCategory { return CategoryMatchedFlipping }

func (d *matchedFlippingDetector) Detect(accountID string, _ []OrderRecord, trades []TradeRecord, now time.Time) []AbnormalAlert {
	if accountID == "" {
		return nil
	}
	windowStart := now.Add(-d.thresholds.Window)
	// Per-symbol direction sequence.
	bySym := make(map[string][]TradeRecord)
	for _, t := range trades {
		if t.TradeTime.Before(windowStart) {
			continue
		}
		bySym[t.Symbol] = append(bySym[t.Symbol], t)
	}
	var out []AbnormalAlert
	for sym, list := range bySym {
		if len(list) < d.thresholds.MinFlips {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].TradeTime.Before(list[j].TradeTime) })
		// Count direction flips (transitions buy↔sell).
		flips := 0
		var totalVolume float64
		for i := 1; i < len(list); i++ {
			if dirOf(list[i]) != dirOf(list[i-1]) {
				flips++
			}
			totalVolume += list[i].Quantity
		}
		totalVolume += list[0].Quantity
		if flips < d.thresholds.MinFlips {
			continue
		}
		if totalVolume < d.thresholds.MinTotalVolume {
			continue
		}
		var evidence []AlertEvidence
		for _, t := range list {
			evidence = append(evidence, AlertEvidence{
				TradeID: t.TradeID, Symbol: sym, Direction: t.Direction,
				Quantity: t.Quantity, Price: t.Price, Timestamp: t.TradeTime,
			})
		}
		out = append(out, AbnormalAlert{
			Category:   CategoryMatchedFlipping,
			AccountID:  accountID,
			Symbol:     sym,
			DetectedAt: now,
			WindowFrom: list[0].TradeTime,
			WindowTo:   list[len(list)-1].TradeTime,
			Severity:   "warning",
			Summary: fmt.Sprintf("账户 %s 在 %s 内对 %s 方向反复切换 %d 次, 累计成交量 %.0f",
				accountID, d.thresholds.Window, sym, flips, totalVolume),
			Evidence:   evidence,
		})
	}
	return out
}

func dirOf(t TradeRecord) string {
	switch t.Direction {
	case "buy", "long":
		return "buy"
	default:
		return "sell"
	}
}

// ============================================================
// Detector 5: 虚假申报 (Spoofing)
// ============================================================

type spoofingDetector struct {
	thresholds SpoofingConfig
}

func (d *spoofingDetector) Name() string              { return "spoofing" }
func (d *spoofingDetector) Category() AbnormalCategory { return CategorySpoofing }

func (d *spoofingDetector) Detect(_ string, orders []OrderRecord, _ []TradeRecord, now time.Time) []AbnormalAlert {
	windowStart := now.Add(-d.thresholds.Window)
	// Group cancellations by symbol. We treat all fast-cancels in
	// the window as one bucket because the regulator's framing is
	// "申报→撤单 ≤ Latency 的大单在窗口内累计 ≥ N 笔" — the per-order
	// latency is a YES/NO filter, not a grouping key.
	bySym := make(map[string][]OrderRecord)
	for _, o := range orders {
		if o.SubmittedAt.Before(windowStart) {
			continue
		}
		if o.Status != "cancelled" {
			continue
		}
		if o.Quantity < d.thresholds.MinQuantity {
			continue
		}
		latency := o.UpdatedAt.Sub(o.SubmittedAt)
		if latency <= 0 || latency > d.thresholds.CancelLatency {
			continue
		}
		bySym[o.Symbol] = append(bySym[o.Symbol], o)
	}
	var out []AbnormalAlert
	for sym, list := range bySym {
		if len(list) < d.thresholds.MinCancelCount {
			continue
		}
		var evidence []AlertEvidence
		for _, o := range list {
			evidence = append(evidence, AlertEvidence{
				OrderID: o.OrderID, Symbol: o.Symbol, Direction: o.Direction,
				Quantity: o.Quantity, Price: o.Price, Timestamp: o.SubmittedAt, Cancelled: true,
				Note: fmt.Sprintf("latency=%s", o.UpdatedAt.Sub(o.SubmittedAt)),
			})
		}
		sort.Slice(evidence, func(i, j int) bool { return evidence[i].Timestamp.Before(evidence[j].Timestamp) })
		out = append(out, AbnormalAlert{
			Category:   CategorySpoofing,
			Symbol:     sym,
			DetectedAt: now,
			WindowFrom: evidence[0].Timestamp,
			WindowTo:   evidence[len(evidence)-1].Timestamp,
			Severity:   "critical",
			Summary: fmt.Sprintf("标的 %s 在 %s 内出现 %d 笔大额快速撤单 (申报→撤单 ≤ %s)",
				sym, d.thresholds.Window, len(list), d.thresholds.CancelLatency),
			Evidence:   evidence,
		})
	}
	return out
}

// ============================================================
// Detector 6: 拉抬打压 (Manipulation)
// ============================================================

type manipulationDetector struct {
	thresholds ManipulationConfig
}

func (d *manipulationDetector) Name() string              { return "manipulation" }
func (d *manipulationDetector) Category() AbnormalCategory { return CategoryManipulation }

func (d *manipulationDetector) Detect(_ string, _ []OrderRecord, trades []TradeRecord, now time.Time) []AbnormalAlert {
	windowStart := now.Add(-d.thresholds.Window)
	bySym := make(map[string][]TradeRecord)
	for _, t := range trades {
		if t.TradeTime.Before(windowStart) {
			continue
		}
		bySym[t.Symbol] = append(bySym[t.Symbol], t)
	}
	var out []AbnormalAlert
	for sym, list := range bySym {
		if len(list) < d.thresholds.MinTrades {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].TradeTime.Before(list[j].TradeTime) })
		// Use the first N as the VWAP baseline; subsequent trades
		// must be > +threshold (拉抬) or < -threshold (打压) of
		// that VWAP.
		n := d.thresholds.VWAPLookback
		if n > len(list) {
			n = len(list)
		}
		baseline := list[:n]
		var vwapNum, vwapDen float64
		for _, t := range baseline {
			vwapNum += t.Price * t.Quantity
			vwapDen += t.Quantity
		}
		if vwapDen == 0 {
			continue
		}
		vwap := vwapNum / vwapDen
		// Scan the remaining trades for outliers.
		var pumpEvidence, dumpEvidence []AlertEvidence
		var pumpCount, dumpCount int
		for _, t := range list[n:] {
			if t.Price >= vwap*(1+d.thresholds.PriceDeviation) {
				pumpEvidence = append(pumpEvidence, AlertEvidence{
					TradeID: t.TradeID, Symbol: sym, Direction: t.Direction,
					Quantity: t.Quantity, Price: t.Price, Timestamp: t.TradeTime,
					Note: fmt.Sprintf("VWAP=%.4f 偏离=+%.2f%%", vwap, (t.Price-vwap)/vwap*100),
				})
				pumpCount++
			} else if t.Price <= vwap*(1-d.thresholds.PriceDeviation) {
				dumpEvidence = append(dumpEvidence, AlertEvidence{
					TradeID: t.TradeID, Symbol: sym, Direction: t.Direction,
					Quantity: t.Quantity, Price: t.Price, Timestamp: t.TradeTime,
					Note: fmt.Sprintf("VWAP=%.4f 偏离=%.2f%%", vwap, (t.Price-vwap)/vwap*100),
				})
				dumpCount++
			}
		}
		if pumpCount+pumpCount+dumpCount < d.thresholds.MinTrades {
			// Need enough outliers to constitute a "pattern" — at
			// least MinTrades total (consistent with the minimum
			// outlier count below).
		}
		// The regulator's framing is "N consecutive trades deviate
		// by ≥ X% from VWAP" — we require at least MinTrades
		// outliers on one side.
		if pumpCount >= d.thresholds.MinTrades {
			out = append(out, AbnormalAlert{
				Category:   CategoryManipulation,
				Symbol:     sym,
				DetectedAt: now,
				WindowFrom: list[0].TradeTime,
				WindowTo:   list[len(list)-1].TradeTime,
				Severity:   "critical",
				Summary: fmt.Sprintf("标的 %s 在 %s 内出现 %d 笔拉抬成交 (偏离 VWAP ≥ +%.1f%%)",
					sym, d.thresholds.Window, pumpCount, d.thresholds.PriceDeviation*100),
				Evidence: pumpEvidence,
			})
		}
		if dumpCount >= d.thresholds.MinTrades {
			out = append(out, AbnormalAlert{
				Category:   CategoryManipulation,
				Symbol:     sym,
				DetectedAt: now,
				WindowFrom: list[0].TradeTime,
				WindowTo:   list[len(list)-1].TradeTime,
				Severity:   "critical",
				Summary: fmt.Sprintf("标的 %s 在 %s 内出现 %d 笔打压成交 (偏离 VWAP ≥ %.1f%%)",
					sym, d.thresholds.Window, dumpCount, d.thresholds.PriceDeviation*100),
				Evidence: dumpEvidence,
			})
		}
	}
	return out
}
