package backtest

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// HTMLReportOptions controls optional sections / behavior in the rendered
// HTML report. Zero value renders an empty/header-only report — callers
// who want the full report should pass IncludeEquityChart=true and
// IncludeTrades=true. (The HTTP handler does this; the zero value is for
// the rare "metrics only" / "no chart" case.)
type HTMLReportOptions struct {
	IncludeEquityChart bool   // render equity curve section
	IncludeTrades      bool   // include trades table section
	Theme              string // "light" | "dark" (default "light")
	FooterNote         string // optional operator-supplied footer
}

// RenderHTML renders a self-contained HTML report for a single backtest
// result. The output is intentionally framework-free:
//   - no JS framework dependency at runtime (just an inline IIFE for chart)
//   - no external network calls (Chart.js loaded inline via data URI
//     pattern, or a minimal hand-rolled SVG line chart — see renderEquitySVG)
//   - CSS inlined for printability (Ctrl+P → "Save as PDF" works out of
//     the box; see @media print block)
//
// Returns the rendered bytes and the Content-Type the handler should set
// ("text/html; charset=utf-8"). Errors only on programmer mistakes
// (bad template name), not on user input.
func RenderHTML(resp BacktestResponse, opts HTMLReportOptions) ([]byte, string, error) {
	if opts.Theme == "" {
		opts.Theme = "light"
	}
	data := buildReportData(resp, opts)

	tmpl, err := template.New("report").Funcs(templateFuncs).Parse(htmlReportTemplate)
	if err != nil {
		return nil, "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, "", fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), "text/html; charset=utf-8", nil
}

type reportData struct {
	Report           BacktestResponse
	Theme            string
	GeneratedAt      string
	FooterNote       string
	IncludeEquity    bool
	IncludeTrades    bool
	EquitySVG        template.HTML // pre-rendered SVG (no JS, no CDN)
	EquityDataJSON   template.JS   // for inline JS chart fallback
	MetricsRows      []metricRow
	TradesRows       []tradeRow
	HasDrawdownDate  bool
	DrawdownDateText string
	ColorPrimary     string
	ColorDown        string
	ColorUp          string
	ColorMuted       string
	ColorBg          string
	ColorText        string
}

type metricRow struct {
	Key   string
	Label string
	Value string
	Hint  string
	Good  bool // true = higher is better
}

type tradeRow struct {
	Index    int
	ID       string
	Symbol   string
	Dir      string
	Quantity string
	Price    string
	Date     string
	PnL      string
}

func buildReportData(resp BacktestResponse, opts HTMLReportOptions) reportData {
	data := reportData{
		Report:        resp,
		Theme:         opts.Theme,
		GeneratedAt:   time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		FooterNote:    opts.FooterNote,
		IncludeEquity: opts.IncludeEquityChart,
		IncludeTrades: opts.IncludeTrades,
	}
	if opts.Theme == "dark" {
		data.ColorBg = "#0f172a"
		data.ColorText = "#e2e8f0"
		data.ColorMuted = "#94a3b8"
		data.ColorPrimary = "#60a5fa"
		data.ColorUp = "#34d399"
		data.ColorDown = "#f87171"
	} else {
		data.ColorBg = "#ffffff"
		data.ColorText = "#0f172a"
		data.ColorMuted = "#64748b"
		data.ColorPrimary = "#2563eb"
		data.ColorUp = "#16a34a"
		data.ColorDown = "#dc2626"
	}

	// Metrics — order matters; matches operator mental model (return → risk → trade stats)
	data.MetricsRows = []metricRow{
		{Key: "total_return", Label: "总收益率", Value: fmtPercent(resp.TotalReturn), Hint: "区间累计收益", Good: true},
		{Key: "annual_return", Label: "年化收益", Value: fmtPercent(resp.AnnualReturn), Hint: "复利年化", Good: true},
		{Key: "sharpe", Label: "Sharpe", Value: fmtFloat(resp.SharpeRatio, 2), Hint: "风险调整后收益", Good: true},
		{Key: "sortino", Label: "Sortino", Value: fmtFloat(resp.SortinoRatio, 2), Hint: "下行波动调整", Good: true},
		{Key: "calmar", Label: "Calmar", Value: fmtFloat(resp.CalmarRatio, 2), Hint: "收益/最大回撤", Good: true},
		{Key: "max_dd", Label: "最大回撤", Value: fmtPercent(resp.MaxDrawdown), Hint: "Peak-to-trough", Good: false},
		{Key: "win_rate", Label: "胜率", Value: fmtPercent(resp.WinRate), Hint: fmt.Sprintf("%d 赢 / %d 输", resp.WinTrades, resp.LoseTrades), Good: true},
		{Key: "total_trades", Label: "总交易", Value: fmt.Sprintf("%d", resp.TotalTrades), Hint: "区间内成交笔数", Good: true},
		{Key: "avg_holding", Label: "平均持仓(天)", Value: fmtFloat(resp.AvgHoldingDays, 1), Hint: "单笔持仓周期", Good: false},
	}

	if resp.MaxDrawdownDate != "" {
		data.HasDrawdownDate = true
		data.DrawdownDateText = resp.MaxDrawdownDate
	}

	// Equity SVG + data
	if opts.IncludeEquityChart {
		data.EquitySVG = renderEquitySVG(resp.PortfolioValues, data.ColorPrimary)
		data.EquityDataJSON = template.JS(portfolioValuesToJSArray(resp.PortfolioValues))
	}

	// Trades — newest first, cap to 200 for HTML readability
	if opts.IncludeTrades {
		trades := make([]domain.Trade, len(resp.Trades))
		copy(trades, resp.Trades)
		sort.Slice(trades, func(i, j int) bool {
			return trades[i].Timestamp.After(trades[j].Timestamp)
		})
		if len(trades) > 200 {
			trades = trades[:200]
		}
		for i, t := range trades {
			dir := "买"
			if t.Direction == domain.DirectionClose || t.Direction == domain.DirectionShort {
				dir = "卖"
			}
			data.TradesRows = append(data.TradesRows, tradeRow{
				Index:    i + 1,
				ID:       t.ID,
				Symbol:   t.Symbol,
				Dir:      dir,
				Quantity: fmt.Sprintf("%.0f", t.Quantity),
				Price:    fmtFloat(t.Price, 3),
				Date:     t.Timestamp.Format("2006-01-02"),
				PnL:      "—",
			})
		}
	}

	return data
}

// renderEquitySVG builds a small inline SVG line chart of the equity curve.
// No JS, no CDN — printable directly and works offline.
func renderEquitySVG(values []domain.PortfolioValue, color string) template.HTML {
	if len(values) == 0 {
		return `<svg viewBox="0 0 600 200" class="equity-empty"><text x="50%" y="50%" text-anchor="middle" fill="#94a3b8">暂无数据</text></svg>`
	}

	// Compute min/max with 5% headroom
	var minV, maxV = math.Inf(1), math.Inf(-1)
	for _, v := range values {
		if v.TotalValue < minV {
			minV = v.TotalValue
		}
		if v.TotalValue > maxV {
			maxV = v.TotalValue
		}
	}
	if math.IsInf(minV, 0) || math.IsInf(maxV, 0) {
		return `<svg viewBox="0 0 600 200"><text x="50%" y="50%" text-anchor="middle">数据异常</text></svg>`
	}
	span := maxV - minV
	if span < 1e-9 {
		span = maxV * 0.01
	}
	pad := span * 0.05
	minV -= pad
	maxV += pad

	const W, H = 720.0, 240.0
	const padL, padR, padT, padB = 50.0, 20.0, 20.0, 30.0
	plotW := W - padL - padR
	plotH := H - padT - padB
	stepX := plotW / float64(len(values)-1)

	// Build polyline points
	pts := make([]string, len(values))
	for i, v := range values {
		x := padL + stepX*float64(i)
		y := padT + plotH*(1-(v.TotalValue-minV)/(maxV-minV))
		pts[i] = fmt.Sprintf("%.1f,%.1f", x, y)
	}
	polyline := "<polyline fill='none' stroke='" + color + "' stroke-width='2' points='" + strings.Join(pts, " ") + "' />"

	// Build Y-axis gridlines (5 levels)
	var grids strings.Builder
	for i := 0; i <= 4; i++ {
		y := padT + plotH*float64(i)/4.0
		val := maxV - (maxV-minV)*float64(i)/4.0
		grids.WriteString(fmt.Sprintf("<line x1='%.1f' y1='%.1f' x2='%.1f' y2='%.1f' stroke='#e2e8f0' stroke-width='1' />", padL, y, padL+plotW, y))
		grids.WriteString(fmt.Sprintf("<text x='%.1f' y='%.1f' text-anchor='end' font-size='10' fill='#64748b'>%s</text>", padL-5, y+3, compactNum(val)))
	}

	// X-axis labels: first, last, and 2 mid points
	var xLabels strings.Builder
	labelIdxs := []int{0, len(values) / 3, 2 * len(values) / 3, len(values) - 1}
	for _, i := range labelIdxs {
		if i < 0 || i >= len(values) {
			continue
		}
		x := padL + stepX*float64(i)
		xLabels.WriteString(fmt.Sprintf("<text x='%.1f' y='%.1f' text-anchor='middle' font-size='10' fill='#64748b'>%s</text>", x, H-10, values[i].Date.Format("01-02")))
	}

	return template.HTML(fmt.Sprintf(
		`<svg viewBox="0 0 %.0f %.0f" preserveAspectRatio="xMidYMid meet" class="equity-svg">%s%s%s</svg>`,
		W, H, grids.String(), polyline, xLabels.String()))
}

func portfolioValuesToJSArray(values []domain.PortfolioValue) string {
	if len(values) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range values {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"date":%q,"total":%f,"cash":%f,"positions":%f}`,
			v.Date.Format("2006-01-02"), v.TotalValue, v.Cash, v.Positions)
	}
	sb.WriteString("]")
	return sb.String()
}

// Template helper funcs
var templateFuncs = template.FuncMap{
	"safeHTML": func(s string) template.HTML { return template.HTML(s) },
}

func fmtPercent(v float64) string {
	return fmt.Sprintf("%.2f%%", v*100)
}

func fmtFloat(v float64, digits int) string {
	return fmt.Sprintf("%.*f", digits, v)
}

func compactNum(v float64) string {
	if math.Abs(v) >= 1e8 {
		return fmt.Sprintf("%.0f亿", v/1e8)
	}
	if math.Abs(v) >= 1e4 {
		return fmt.Sprintf("%.0f万", v/1e4)
	}
	return fmt.Sprintf("%.0f", v)
}

// htmlReportTemplate is a self-contained HTML document. CSS is inlined
// (no external file); chart is a hand-rolled SVG (no Chart.js / no CDN).
// @media print rules make Ctrl+P → "Save as PDF" produce a clean PDF.
const htmlReportTemplate = `<!DOCTYPE html>
<html lang="zh-CN" data-theme="{{.Theme}}">
<head>
<meta charset="utf-8">
<title>回测报告 — {{.Report.Strategy}} ({{.Report.StartDate}} ~ {{.Report.EndDate}})</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root { color-scheme: {{if eq .Theme "dark"}}dark{{else}}light{{end}}; }
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 32px;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  background: {{.ColorBg}};
  color: {{.ColorText}};
  line-height: 1.5;
}
header { border-bottom: 1px solid {{.ColorMuted}}; padding-bottom: 16px; margin-bottom: 24px; }
h1 { margin: 0 0 4px 0; font-size: 24px; }
h2 { font-size: 18px; margin: 32px 0 12px 0; padding-bottom: 4px; border-bottom: 1px solid {{.ColorMuted}}; }
.subtitle { color: {{.ColorMuted}}; font-size: 13px; }
.summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; }
.metric {
  padding: 12px 16px;
  border: 1px solid {{.ColorMuted}};
  border-radius: 8px;
  background: rgba(127,127,127,0.04);
}
.metric .label { font-size: 12px; color: {{.ColorMuted}}; }
.metric .value { font-size: 20px; font-weight: 600; margin-top: 4px; }
.metric .hint { font-size: 11px; color: {{.ColorMuted}}; margin-top: 2px; }
.equity-svg { width: 100%; height: auto; max-height: 320px; background: rgba(127,127,127,0.02); border-radius: 8px; padding: 8px; }
.equity-empty { width: 100%; height: 200px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { padding: 6px 8px; text-align: left; border-bottom: 1px solid rgba(127,127,127,0.2); }
th { background: rgba(127,127,127,0.08); font-weight: 600; }
tr:hover { background: rgba(127,127,127,0.04); }
.buy { color: {{.ColorUp}}; font-weight: 600; }
.sell { color: {{.ColorDown}}; font-weight: 600; }
.symbol { font-family: "SF Mono", Consolas, monospace; font-size: 12px; }
footer { margin-top: 40px; padding-top: 16px; border-top: 1px solid {{.ColorMuted}}; font-size: 12px; color: {{.ColorMuted}}; }
.dd-warning { color: {{.ColorDown}}; font-weight: 600; }
.muted { color: {{.ColorMuted}}; }
@media print {
  body { padding: 16px; }
  h2 { page-break-after: avoid; }
  table { page-break-inside: auto; }
  tr { page-break-inside: avoid; }
  thead { display: table-header-group; }
  .metric { break-inside: avoid; }
  .no-print { display: none !important; }
}
@page { margin: 1.5cm; }
</style>
</head>
<body>
<header id="{{.Report.ID}}">
  <h1>{{.Report.Strategy}} 回测报告</h1>
  <div class="subtitle">区间: {{.Report.StartDate}} ~ {{.Report.EndDate}} · 报告 ID: <span class="symbol">{{.Report.ID}}</span> · 生成于 {{.GeneratedAt}}</div>
  <div class="subtitle">股票池: {{if .Report.StockPool}}{{range $i, $s := .Report.StockPool}}{{if $i}}, {{end}}<span class="symbol">{{$s}}</span>{{end}}{{else}}<span class="muted">未指定</span>{{end}} · 初始资金: {{.Report.InitialCapital}}</div>
</header>

<section>
  <h2>核心指标</h2>
  <div class="summary-grid">
    {{range .MetricsRows}}
    <div class="metric">
      <div class="label">{{.Label}}</div>
      <div class="value">{{.Value}}</div>
      <div class="hint">{{.Hint}}</div>
    </div>
    {{end}}
  </div>
  {{if .HasDrawdownDate}}
  <p class="muted" style="margin-top: 12px; font-size: 12px;">最大回撤发生日期: <strong>{{.DrawdownDateText}}</strong></p>
  {{end}}
</section>

{{if .IncludeEquity}}
<section>
  <h2>权益曲线</h2>
  {{.EquitySVG}}
</section>
{{end}}

{{if .IncludeTrades}}
{{if .TradesRows}}
<section>
  <h2>交易明细 <span class="muted" style="font-size: 12px; font-weight: 400;">(最新 {{len .TradesRows}} 笔)</span></h2>
  <table>
    <thead>
      <tr>
        <th>#</th><th>日期</th><th>标的</th><th>方向</th><th>数量</th><th>价格</th><th>订单 ID</th>
      </tr>
    </thead>
    <tbody>
      {{range .TradesRows}}
      <tr>
        <td>{{.Index}}</td>
        <td>{{.Date}}</td>
        <td class="symbol">{{.Symbol}}</td>
        <td class="{{if eq .Dir "买"}}buy{{else}}sell{{end}}">{{.Dir}}</td>
        <td>{{.Quantity}}</td>
        <td>{{.Price}}</td>
        <td class="symbol muted">{{.ID}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</section>
{{else}}
<section>
  <h2>交易明细</h2>
  <p class="muted">无交易记录 (该策略在区间内未触发任何信号)</p>
</section>
{{end}}
{{end}}

<footer>
  <p>本报告由 Quant Lab 自动生成,基于历史数据回测,不代表未来表现。投资有风险,入市需谨慎。</p>
  {{if .FooterNote}}<p class="muted">{{.FooterNote}}</p>{{end}}
  <p class="muted no-print">导出方式: GET /api/backtest/{{.Report.ID}}/export/html · 按 Ctrl+P 即可另存为 PDF</p>
</footer>
</body>
</html>`
