// K15 双跑对齐 · diff 报告。
//
// 指标层：两侧产物喂**同一个** metrics.GenerateBacktestResult——指标实现
// 只有一份，diff 反映的是数据差异（净值曲线 / 成交），不是公式漂移。
// 明细层：成交逐笔对账（键 = 日期 + symbol + 方向 + 量价费），差异逐条
// 点名。任何解释不了的差异如实列入开放问题，不用容差掩盖。
package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// metricRow 一行指标对比。RelDiff 以 Go 侧为分母（|Diff/Go|，Go=0 时以绝对差呈现）。
type metricRow struct {
	Name    string
	Go      float64
	NT      float64
	Diff    float64
	RelDiff float64
}

// tradeReconRow 一条成交对账结果。
type tradeReconRow struct {
	Kind   string // "matched" | "go_only" | "nt_only" | "mismatch"
	Day    string
	Symbol string
	Side   string
	Detail string // mismatch 时的量价费对比
}

// Report 是一次双跑对齐的完整结果。
type Report struct {
	Window        string
	Signals       string
	KernelConfig  string
	MetricRows    []metricRow
	TradeRows     []tradeReconRow
	MatchedTrades int
	GoOnlyTrades  int
	NTOnlyTrades  int
	// Deterministic 各侧两遍重跑逐项一致（确定性腿）。
	GoDeterministic bool
	NTDeterministic bool
	// Attribution 归因清单：实测差异逐项对到已知口径差异（实验结论的一部分）。
	Attribution   []string
	OpenQuestions []string
	GoResult      domain.BacktestResult
	NTResult      domain.BacktestResult
}

func fmtDay(t time.Time) string { return t.Format("2006-01-02") }

func tradeSide(d domain.Direction) string {
	switch d {
	case domain.DirectionLong:
		return "BUY"
	case domain.DirectionClose, domain.DirectionShort:
		return "SELL"
	default:
		return strings.ToUpper(string(d))
	}
}

// buildReport 生成指标 + 明细对账。goPV/ntPV 是两侧**原始**净值序列
// （同样的 metrics 实现已把指标算进 goRes/ntRes）。
func buildReport(
	window, signalsDesc, kernelDesc string,
	goRes, ntRes domain.BacktestResult,
) *Report {
	r := &Report{Window: window, Signals: signalsDesc, KernelConfig: kernelDesc, GoResult: goRes, NTResult: ntRes}
	r.MetricRows = []metricRow{
		num("total_return", goRes.TotalReturn, ntRes.TotalReturn),
		num("annual_return", goRes.AnnualReturn, ntRes.AnnualReturn),
		num("sharpe", goRes.SharpeRatio, ntRes.SharpeRatio),
		num("sortino", goRes.SortinoRatio, ntRes.SortinoRatio),
		num("max_drawdown", goRes.MaxDrawdown, ntRes.MaxDrawdown),
		num("win_rate", goRes.WinRate, ntRes.WinRate),
		num("calmar", goRes.CalmarRatio, ntRes.CalmarRatio),
		cnt("total_trades", float64(goRes.TotalTrades), float64(ntRes.TotalTrades)),
	}
	r.reconcileTrades(goRes.Trades, ntRes.Trades)
	return r
}

func num(name string, goV, ntV float64) metricRow {
	rel := 0.0
	if goV != 0 {
		rel = (ntV - goV) / goV
	}
	return metricRow{Name: name, Go: goV, NT: ntV, Diff: ntV - goV, RelDiff: rel}
}

func cnt(name string, goV, ntV float64) metricRow {
	return num(name, goV, ntV)
}

// reconcileTrades 逐笔对账：键 = (date, symbol, side, qty, price-到分)。
// price 舍入到分是**口径决定**：Go 侧消费 PG 的前复权浮点价（7.566182…），
// NT 侧 catalog 的 tick 精度是 0.01（7.57）——两侧数据链路的精度不同是
// 记档的口径差异，不该把每一笔都打成 mismatch；量到 0.01 股精度精确比。
// 同键两侧都有 → matched；单侧存在 → go_only / nt_only。
//
// K15-R1 复查修正：matched 行的费用对比必须取 **NT trade 自己的**
// Commission（NT fee model 返回全项 total）——此前误用循环变量 t（Go
// trade）的 Commission 分量，制造出「NT 缺过户费」的假象。
func (r *Report) reconcileTrades(goTrades, ntTrades []domain.Trade) {
	type key struct {
		day    string
		symbol string
		side   string
		qty    float64
		price  float64
	}
	mk := func(t domain.Trade) key {
		return key{fmtDay(t.Timestamp), t.Symbol, tradeSide(t.Direction),
			math.Round(t.Quantity*100) / 100, math.Round(t.Price*100) / 100}
	}
	goKeys := make(map[key]int, len(goTrades))
	for _, t := range goTrades {
		goKeys[mk(t)]++
	}
	ntBy := make(map[key]domain.Trade, len(ntTrades))
	ntKeys := make(map[key]int, len(ntTrades))
	for _, t := range ntTrades {
		k := mk(t)
		ntKeys[k]++
		if _, seen := ntBy[k]; !seen {
			ntBy[k] = t
		}
	}

	seen := make(map[key]bool)
	for _, t := range goTrades {
		k := mk(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		switch {
		case ntKeys[k] > 0:
			r.MatchedTrades += goKeys[k]
			nt := ntBy[k]
			r.TradeRows = append(r.TradeRows, tradeReconRow{Kind: "matched", Day: k.day, Symbol: k.symbol, Side: k.side,
				Detail: fmt.Sprintf("qty=%.0f px=%.2f (fees: go=%.2f nt=%.2f)", k.qty, k.price,
					t.Commission+t.TransferFee+t.StampTax, nt.Commission+nt.TransferFee+nt.StampTax)})
		case ntKeys[k] == 0:
			r.GoOnlyTrades += goKeys[k]
			r.TradeRows = append(r.TradeRows, tradeReconRow{Kind: "go_only", Day: k.day, Symbol: k.symbol, Side: k.side,
				Detail: fmt.Sprintf("qty=%.0f px=%.2f", k.qty, k.price)})
		}
	}
	for _, t := range ntTrades {
		k := mk(t)
		if seen[k] || goKeys[k] > 0 {
			continue
		}
		seen[k] = true
		r.NTOnlyTrades += ntKeys[k]
		r.TradeRows = append(r.TradeRows, tradeReconRow{Kind: "nt_only", Day: k.day, Symbol: k.symbol, Side: k.side,
			Detail: fmt.Sprintf("qty=%.0f px=%.2f", k.qty, k.price)})
	}
	sort.Slice(r.TradeRows, func(i, j int) bool {
		if r.TradeRows[i].Day != r.TradeRows[j].Day {
			return r.TradeRows[i].Day < r.TradeRows[j].Day
		}
		return r.TradeRows[i].Symbol < r.TradeRows[j].Symbol
	})
}

// Markdown 渲染成落盘报告（docs/design/kernel/）。
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# K15 双跑对齐报告（Go 内核 vs NautilusTrader 侧车）\n\n")
	fmt.Fprintf(&b, "- 窗口：%s\n", r.Window)
	fmt.Fprintf(&b, "- 信号流：%s\n", r.Signals)
	fmt.Fprintf(&b, "- 口径：%s\n", r.KernelConfig)
	fmt.Fprintf(&b, "- 确定性腿：Go %s / NT %s\n\n",
		yesNo(r.GoDeterministic), yesNo(r.NTDeterministic))

	b.WriteString("## 指标对齐（同一 metrics 实现计算）\n\n")
	b.WriteString("| 指标 | Go | NT | 差 | 相对差 |\n|---|---:|---:|---:|---:|\n")
	for _, m := range r.MetricRows {
		fmt.Fprintf(&b, "| %s | %.6f | %.6f | %+.6g | %+.4g%% |\n", m.Name, m.Go, m.NT, m.Diff, m.RelDiff*100)
	}

	b.WriteString("\n## 成交明细对账\n\n")
	fmt.Fprintf(&b, "matched=%d go_only=%d nt_only=%d\n\n", r.MatchedTrades, r.GoOnlyTrades, r.NTOnlyTrades)
	for _, row := range r.TradeRows {
		fmt.Fprintf(&b, "- `%s` %s %s %s — %s\n", row.Kind, row.Day, row.Symbol, row.Side, row.Detail)
	}

	if len(r.Attribution) > 0 {
		b.WriteString("\n## 差异归因（实测 → 口径）\n\n")
		for _, a := range r.Attribution {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}

	if len(r.OpenQuestions) > 0 {
		b.WriteString("\n## 开放问题\n\n")
		for _, q := range r.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}
	return b.String()
}

func yesNo(v bool) string {
	if v {
		return "一致 ✅"
	}
	return "不一致 ❌"
}
