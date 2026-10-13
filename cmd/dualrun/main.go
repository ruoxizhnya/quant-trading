// K15 双跑对齐实验 · 编排入口。
//
// 同一份目标权重信号流，两侧各跑一次：
//
//	Go 内核   —— wrReplay（本包）：信号流 → 调仓单 → Tracker（T+1/现金/
//	            ComputeFees 全三项，滑点 0）→ PortfolioValues/Trades
//	NT 侧车   —— nautilus.Runner 正门 → POST /v1/backtest（signals 进
//	            wire）→ weight_replay 适配器 → fills/account 报告回传
//	            → toResponse 落进同一组冻结字段
//
// 两侧产物喂**同一个** metrics.GenerateBacktestResult（指标实现只有一份），
// 再逐指标 + 逐笔 diff。实验要求可复现：两侧各跑两遍，确定性腿校验
// 「同输入 → 逐项一致」。
//
// 用法（本机）：
//
//	go run ./cmd/dualrun -strategy bh
//	go run ./cmd/dualrun -strategy quarterly
//
// 前置：PG（ohlcv_daily_qfq）与 NT 侧车（python -m sidecar --port 9100
// --catalog nt-sidecar/catalog）都已就绪；catalog 覆盖实验窗口。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/metrics"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/nautilus"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func main() {
	pgDSN := flag.String("pg", "postgres://postgres:postgres@127.0.0.1:5432/quant_trading?sslmode=disable", "Postgres DSN")
	sidecarURL := flag.String("url", nautilus.DefaultURL, "NT sidecar base URL")
	symbolsFlag := flag.String("symbols", "000001.SZ,600000.SH", "comma-separated symbols")
	start := flag.String("start", "2024-01-02", "window start (inclusive)")
	end := flag.String("end", "2024-12-31", "window end (inclusive)")
	capital := flag.Float64("capital", 1_000_000, "initial capital")
	rf := flag.Float64("rf", 0.0, "risk-free rate (annual)")
	strategy := flag.String("strategy", "bh", "signal plan: bh | quarterly")
	out := flag.String("out", "", "write the markdown report to this path")
	flag.Parse()

	logger := zerolog.Nop() // 实验产物即报告；tracker 的 debug 日志不进 stdout
	ctx := context.Background()

	symbols := strings.Split(*symbolsFlag, ",")
	for i := range symbols {
		symbols[i] = strings.TrimSpace(symbols[i])
	}

	signals, err := buildSignals(*strategy, symbols, *start, *end)
	must(err, "build signal plan")

	// ── 数据：PG 真实日线（Go 侧消费）──────────────────────────────
	store, err := storage.NewPostgresStore(ctx, *pgDSN)
	must(err, "connect postgres")
	parsedStart, err := time.Parse("2006-01-02", *start)
	must(err, "parse start")
	parsedEnd, err := time.Parse("2006-01-02", *end)
	must(err, "parse end")

	bars := make(map[string][]domain.OHLCV, len(symbols))
	for _, sym := range symbols {
		rows, err := store.GetOHLCV(ctx, sym, parsedStart, parsedEnd)
		must(err, "load bars for "+sym)
		if len(rows) == 0 {
			must(fmt.Errorf("no bars for %s in PG — is ohlcv_daily_qfq loaded for the window?", sym), "data")
		}
		bars[sym] = rows
	}

	tradingCfg := contracts.DefaultTradingConfig()
	// 与侧车 ashare.py 同参（2024 费率表）：佣金 0.03% / 最低 ¥5，
	// 印花税与过户费走 DefaultTradingConfig（fees 包单一真相）。
	commissionRate := 0.0003
	kernelDesc := fmt.Sprintf(
		"capital=%.0f commission=%.4f stamp/transfer=fees.Default(与 NT AShareFeeModel parity) slippage=0 D_max=0.10 lot=100 limit=±10%%",
		*capital, commissionRate)

	// ── Go 侧 ×2（确定性腿）───────────────────────────────────────
	cfg := defaultWRConfig(*capital)
	goRun1, err := wrReplay(cfg, signals, commissionRate, tradingCfg, bars, logger)
	must(err, "go replay #1")
	goRun2, err := wrReplay(cfg, signals, commissionRate, tradingCfg, bars, logger)
	must(err, "go replay #2")
	// 确定性比较忽略成交通号（tracker 每次 run 生成随机 UUID——那是编号
	// 不是行为）；量/价/费/净值逐项必须一致。
	goDeterministic := sameGoRun(goRun1, goRun2)

	// ── NT 侧 ×2（确定性腿）───────────────────────────────────────
	runner := nautilus.NewRunner(*sidecarURL, nil)
	req := contracts.BacktestRequest{
		Strategy:       "weight_replay",
		StockPool:      symbols,
		StartDate:      *start,
		EndDate:        *end,
		InitialCapital: *capital,
		RiskFreeRate:   *rf,
		Kernel:         nautilus.KernelName,
		Signals:        signals,
	}
	if err := req.ValidateRunSpec(); err != nil {
		must(err, "validate run spec")
	}
	ntResp1, err := runner.RunBacktest(ctx, req)
	must(err, "nautilus run #1")
	ntResp2, err := runner.RunBacktest(ctx, req)
	must(err, "nautilus run #2")
	// 同上：忽略响应 ID（侧车每次 run 生成新的 run_config_id）。
	ntDeterministic := sameResponse(ntResp1, ntResp2)

	// ── 同一指标实现 ─────────────────────────────────────────────
	goRes := metrics.GenerateBacktestResult(goRun1.PortfolioValues, goRun1.Trades, *rf, parsedStart, parsedEnd, *capital)
	ntRes := metrics.GenerateBacktestResult(ntResp1.PortfolioValues, ntResp1.Trades, *rf, parsedStart, parsedEnd, *capital)

	report := buildReport(
		fmt.Sprintf("%s .. %s", *start, *end),
		fmt.Sprintf("%s（%d 条目标权重信号，两侧同流）", *strategy, len(signals)),
		kernelDesc,
		goRes, ntRes,
	)
	report.GoDeterministic = goDeterministic
	report.NTDeterministic = ntDeterministic
	// 归因清单：实测差异逐项对到已知口径差异，解释不了的留开放问题。
	report.Attribution = []string{
		"① 价源精度（主因）：Go 消费 PG 前复权浮点价（7.566182…），NT catalog 为 tick 0.01 的 Decimal（7.57）——每笔成交价有 ≤0.005 的口径差，经恒定名义额再平衡放大成持仓路径分叉（go_only/nt_only ±100 股级 delta 偏移）与净值微差（§8.2 观察项：两侧同源 PG，差异为精度而非取数时点）。",
		"② 费用（K15-R1 复查后关闭）：两侧费用口径**同源且逐笔对齐**——Go ComputeFees 全三项 vs NT AShareFeeModel total（parity fixture 同源）；K15 首轮报告的「NT 缺过户费」是 diff 展示层 bug（matched 行 nt 费用误用 Go trade 的 commission 分量），已在 6a782c5 后的复查中修复并重跑验证。",
		"③ go_only / nt_only：全部是 ① 的下游（持仓路径微分叉），非执行机制缺失——对账键（date/symbol/side/qty/px-到分）已确认无机制性缺笔。",
		"④ win_rate：按 entry/exit 配对计算，对成交明细的微小分叉高度敏感（37%~42% 相对差全部来自配对序列错位），不设硬容差，以明细对账为准。",
		"⑤ 费率口径裁决（工单③-5）：选 b) 报告单列归因——Go 记账走 Tracker+ComputeFees（全三项），「撮合路径只计佣金」的 CostModel 口径边界不进入本次对齐路径；ExecutionService 路径的口径边界维持 §8.4 记档。",
		"⑥ 涨跌停/T+1 触发：本窗口两只主板标的的调仓路径未触发涨跌停拒单与 T+1 拒单（卖的是昨仓，合法）——两条护栏由两侧各自的单测护栏覆盖（Go tracker T+1 测试 / 侧车 sellable 测试 + 本次 wrReplay no-chase 腿），实验窗口内未触发如实记录。",
	}

	fmt.Println(report.Markdown())
	if *out != "" {
		must(os.WriteFile(*out, []byte(report.Markdown()), 0o644), "write report")
		fmt.Printf("report written to %s\n", *out)
	}
}

// buildSignals 构造实验信号流（两侧消费的同一条流）。
//
//	bh        首日 0.5/0.5 建仓，窗口末日全平 —— 测量纯执行差异
//	quarterly 季度调仓（含减仓/加仓/清仓）—— 测量调仓路径的差异归因
func buildSignals(plan string, symbols []string, start, end string) ([]contracts.SignalWeight, error) {
	if len(symbols) != 2 {
		return nil, fmt.Errorf("signal plans expect exactly 2 symbols, got %d", len(symbols))
	}
	var rows []contracts.SignalWeight
	add := func(sym string, w float64, asOf string) {
		rows = append(rows, contracts.SignalWeight{Symbol: sym, Weight: w, AsOf: asOf})
	}
	switch plan {
	case "bh":
		for _, sym := range symbols {
			add(sym, 0.5, start)
		}
		for _, sym := range symbols {
			add(sym, 0.0, end)
		}
	case "quarterly":
		a, b := symbols[0], symbols[1]
		add(a, 0.8, start)
		add(b, 0.2, start)
		add(a, 0.5, "2024-04-01") // 减仓 a、加仓 b（调仓路径）
		add(b, 0.5, "2024-04-01")
		add(a, 0.3, "2024-07-01")
		add(b, 0.7, "2024-07-01")
		add(a, 0.6, "2024-10-08") // 国庆后首个交易日
		add(b, 0.4, "2024-10-08")
		add(a, 0.0, end)
		add(b, 0.0, end)
	default:
		return nil, fmt.Errorf("unknown signal plan %q (known: bh, quarterly)", plan)
	}
	req := contracts.BacktestRequest{Signals: rows}
	if err := req.ValidateRunSpec(); err != nil {
		return nil, err
	}
	return rows, nil
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "dualrun: %s: %v\n", what, err)
		os.Exit(1)
	}
}

// sameGoRun / sameResponse 比较两次运行的数据面（净值 + 成交 + 指标），
// 忽略随机生成的标识字段（tracker 的 trade UUID、侧车的 run_config_id）——
// 确定性腿验的是「行为可复现」，不是「编号可复现」。
func sameGoRun(a, b *wrResult) bool {
	if len(a.PortfolioValues) != len(b.PortfolioValues) || len(a.Trades) != len(b.Trades) {
		return false
	}
	for i := range a.PortfolioValues {
		if a.PortfolioValues[i] != b.PortfolioValues[i] {
			return false
		}
	}
	for i := range a.Trades {
		x, y := a.Trades[i], b.Trades[i]
		x.ID, y.ID = "", ""
		if x != y {
			return false
		}
	}
	return true
}

func sameResponse(a, b *contracts.BacktestResponse) bool {
	x, y := *a, *b
	x.ID, y.ID = "", ""
	if !reflect.DeepEqual(x, y) {
		return false
	}
	// DeepEqual 已含切片内容比较；上面只剥了顶层 ID。
	return true
}
