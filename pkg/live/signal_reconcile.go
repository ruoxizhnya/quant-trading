package live

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SignalReconcileConfig 决定信号对账的容差。全部是「绝对容差」，
// 只用于吸收浮点噪声（1e-9 量级），不用于宽容真正的语义漂移——
// 「信号为核心」的对账把任何超出噪声的差异都判 critical。
type SignalReconcileConfig struct {
	// StrengthTolerance 是两侧 strength 差的绝对容忍度。默认 1e-9。
	// strength 直接决定下单量（signalQuantity 用它），差一点点就是
	// 下单量不同，所以这里不能松。
	StrengthTolerance float64
	// LimitPriceTolerance 是限价单价格差的绝对容忍度。默认 1e-9。
	LimitPriceTolerance float64
}

// DefaultSignalReconcileConfig 返回默认容差（仅吸收浮点噪声）。
func DefaultSignalReconcileConfig() SignalReconcileConfig {
	return SignalReconcileConfig{
		StrengthTolerance:   1e-9,
		LimitPriceTolerance: 1e-9,
	}
}

// SignalDiscrepancyKind 分类信号差异的类型。
type SignalDiscrepancyKind string

const (
	// KindSignalMissingPaper 回测有、paper 无 —— 纸面回放漏掉了这根信号。
	KindSignalMissingPaper SignalDiscrepancyKind = "missing_paper"
	// KindSignalMissingBacktest paper 有、回测无 —— 纸面回放多产出一根信号。
	KindSignalMissingBacktest SignalDiscrepancyKind = "missing_backtest"
	// KindSignalDirection 方向不一致（一买一卖）。
	KindSignalDirection SignalDiscrepancyKind = "direction"
	// KindSignalStrength 强度不一致（超出容差）。
	KindSignalStrength SignalDiscrepancyKind = "strength"
	// KindSignalOrderType 订单类型不一致（市价 vs 限价）。
	KindSignalOrderType SignalDiscrepancyKind = "order_type"
	// KindSignalLimitPrice 限价价格不一致（超出容差）。
	KindSignalLimitPrice SignalDiscrepancyKind = "limit_price"
)

// SignalDiscrepancy 描述一条信号对账差异。
type SignalDiscrepancy struct {
	Date        time.Time             `json:"date"`
	Symbol      string                `json:"symbol"`
	Kind        SignalDiscrepancyKind `json:"kind"`
	PaperVal    string                `json:"paper_val"`    // paper 侧的值（可读表示）
	BacktestVal string                `json:"backtest_val"` // 回测侧的值
	Note        string                `json:"note,omitempty"`
}

// HasDifference 报告是否真的存在差异。空差异列表是「一致」的正面证据。
func (d SignalDiscrepancy) String() string {
	return fmt.Sprintf("%s %s: %s (paper=%s backtest=%s)",
		d.Date.Format("2006-01-02"), d.Symbol, d.Kind, d.PaperVal, d.BacktestVal)
}

// signalKey 是对账的归一化键：同一交易日、同一 symbol 的信号应当一一对应。
//
// date 存的是**归一化后的 UTC 自然日**（字符串），而不是裸 time.Time——
// time.Time 内部有 wall clock + monotonic clock + location 三个分量，两个
// 「同一时刻」只要 monotonic 或 location 不同就 `!=`，拿它当 map 键会让
// 对账器静默把同一天读成两个键、误报 missing。归一化到 UTC 自然日字符串后，
// 键的比较只取决于「是哪一天」，与时刻/时区表达无关。
type signalKey struct {
	date   string // UTC 自然日，格式 "2006-01-02"
	symbol string
}

// signalKeyFor 从一条信号构造归一化键。
func signalKeyFor(s domain.Signal) signalKey {
	return signalKey{date: dayKey(s.Date), symbol: s.Symbol}
}

// ReconcileSignals 比对 paper 与回测两侧产出的信号序列，返回差异列表。
//
// ─── 读法 A 的落点（为什么对信号、怎么对） ─────────────────────────
//
// 信号在「执行机制之前」：只要同一策略 + 同一批 bar + 同一窗口语义
// （切片 1 已保证），两侧信号就应当**逐条一致**。因此信号对账是
// 「同构验证」里最干净、最可证伪的一层——它直接抓「signalQuantity /
// toDomainSignals 这类复制而非共享的代码」在两侧漂移（切片 1 注释里
// 明写这是 import cycle 所迫的复制）。
//
// 比对口径（刻意收窄，只比「影响成交」的字段）：
//   - 键 = (date, symbol)；同键内按 direction → strength → order_type
//     排序后逐条对齐（排序使两侧内部顺序差异不误报）。
//   - 值 = direction（严格）/ strength（容差）/ order_type（严格）/
//     limit_price（容差）。
//   - **不比** CompositeScore（两侧都 = strength，冗余）、Factors/Metadata
//     （诊断字段，且切片 1 已知 nil vs 空 map 的表象差异不影响成交）。
//
// 差异分类与 severity 由 SignalDiscrepancyKind 表达；「信号为核心」的
// 判据下，任何超出浮点噪声的差异都是 critical（信号一致是硬门禁）。
func ReconcileSignals(paper, backtest []domain.Signal, cfg SignalReconcileConfig) []SignalDiscrepancy {
	if cfg.StrengthTolerance <= 0 {
		cfg.StrengthTolerance = DefaultSignalReconcileConfig().StrengthTolerance
	}
	if cfg.LimitPriceTolerance <= 0 {
		cfg.LimitPriceTolerance = DefaultSignalReconcileConfig().LimitPriceTolerance
	}

	paperByKey := groupSignals(paper)
	backtestByKey := groupSignals(backtest)

	// 键的并集，按 (date, symbol) 定序保证输出确定性。
	keys := make([]signalKey, 0, len(paperByKey)+len(backtestByKey))
	seen := map[signalKey]bool{}
	for k := range paperByKey {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range backtestByKey {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].date != keys[j].date {
			return keys[i].date < keys[j].date
		}
		return keys[i].symbol < keys[j].symbol
	})

	var out []SignalDiscrepancy
	for _, k := range keys {
		p := paperByKey[k]
		b := backtestByKey[k]

		switch {
		case len(p) == 0:
			// 回测有、paper 无。
			for _, bs := range b {
				out = append(out, SignalDiscrepancy{
					Date:        bs.Date,
					Symbol:      k.symbol,
					Kind:        KindSignalMissingPaper,
					BacktestVal: signalDesc(bs),
					Note:        "回测产出了这根信号，paper 回放没有",
				})
			}
		case len(b) == 0:
			// paper 有、回测无。
			for _, ps := range p {
				out = append(out, SignalDiscrepancy{
					Date:     ps.Date,
					Symbol:   k.symbol,
					Kind:     KindSignalMissingBacktest,
					PaperVal: signalDesc(ps),
					Note:     "paper 多产出了这根信号，回测没有",
				})
			}
		default:
			out = append(out, compareSignalLists(k, p, b, cfg)...)
		}
	}
	return out
}

// groupSignals 把信号按归一化键 (UTC 自然日, symbol) 归组。同键内的顺序由
// compareSignalLists 里的排序决定，这里不排（避免二次排序）。
func groupSignals(signals []domain.Signal) map[signalKey][]domain.Signal {
	m := map[signalKey][]domain.Signal{}
	for _, s := range signals {
		k := signalKeyFor(s)
		m[k] = append(m[k], s)
	}
	return m
}

// compareSignalLists 比对同键两侧的信号列表。两侧各自按
// (direction, strength, order_type, limit_price) 排序后逐条对齐，
// 长度不等时按缺失处理。
func compareSignalLists(k signalKey, paper, backtest []domain.Signal, cfg SignalReconcileConfig) []SignalDiscrepancy {
	sortSignals(paper)
	sortSignals(backtest)

	var out []SignalDiscrepancy
	n := len(paper)
	if len(backtest) > n {
		n = len(backtest)
	}
	for i := 0; i < n; i++ {
		switch {
		case i >= len(paper):
			out = append(out, SignalDiscrepancy{
				Date:        backtest[i].Date,
				Symbol:      k.symbol,
				Kind:        KindSignalMissingPaper,
				BacktestVal: signalDesc(backtest[i]),
				Note:        "同键第 N 条：回测有、paper 无",
			})
		case i >= len(backtest):
			out = append(out, SignalDiscrepancy{
				Date:     paper[i].Date,
				Symbol:   k.symbol,
				Kind:     KindSignalMissingBacktest,
				PaperVal: signalDesc(paper[i]),
				Note:     "同键第 N 条：paper 有、回测无",
			})
		default:
			out = append(out, compareSignals(k, paper[i], backtest[i], cfg)...)
		}
	}
	return out
}

// compareSignals 比对两条「已对齐」的信号，逐字段产出差异。
func compareSignals(k signalKey, p, b domain.Signal, cfg SignalReconcileConfig) []SignalDiscrepancy {
	var out []SignalDiscrepancy

	if p.Direction != b.Direction {
		out = append(out, SignalDiscrepancy{
			Date:        p.Date,
			Symbol:      k.symbol,
			Kind:        KindSignalDirection,
			PaperVal:    string(p.Direction),
			BacktestVal: string(b.Direction),
		})
	}

	if math.Abs(p.Strength-b.Strength) > cfg.StrengthTolerance {
		out = append(out, SignalDiscrepancy{
			Date:        p.Date,
			Symbol:      k.symbol,
			Kind:        KindSignalStrength,
			PaperVal:    fmt.Sprintf("%.10f", p.Strength),
			BacktestVal: fmt.Sprintf("%.10f", b.Strength),
		})
	}

	if p.OrderType != b.OrderType {
		out = append(out, SignalDiscrepancy{
			Date:        p.Date,
			Symbol:      k.symbol,
			Kind:        KindSignalOrderType,
			PaperVal:    string(p.OrderType),
			BacktestVal: string(b.OrderType),
		})
	}

	if math.Abs(p.LimitPrice-b.LimitPrice) > cfg.LimitPriceTolerance {
		out = append(out, SignalDiscrepancy{
			Date:        p.Date,
			Symbol:      k.symbol,
			Kind:        KindSignalLimitPrice,
			PaperVal:    fmt.Sprintf("%.6f", p.LimitPrice),
			BacktestVal: fmt.Sprintf("%.6f", b.LimitPrice),
		})
	}

	return out
}

// sortSignals 按 (direction, strength, order_type, limit_price) 定序，
// 使两侧同键内部顺序可复现、可对齐。
func sortSignals(s []domain.Signal) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Direction != s[j].Direction {
			return s[i].Direction < s[j].Direction
		}
		if s[i].Strength != s[j].Strength {
			return s[i].Strength < s[j].Strength
		}
		if s[i].OrderType != s[j].OrderType {
			return s[i].OrderType < s[j].OrderType
		}
		return s[i].LimitPrice < s[j].LimitPrice
	})
}

// signalDesc 是一条信号的可读表示，用于差异文案。
func signalDesc(s domain.Signal) string {
	return fmt.Sprintf("%s qty_factor=%.4f %s", s.Direction, s.Strength, s.OrderType)
}
