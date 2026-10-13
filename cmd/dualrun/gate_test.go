package main

// K16 ① 门禁腿：
//   1. K15 实测值（报告 §2 两实验的实测容差余量）在门禁下必须绿；
//   2. 破坏腿——把某指标推到超阈 / 破坏确定性 / 注入成交 mismatch，
//      门禁必须红并点名规则。红不了的门禁等于没有门禁。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/alert"
)

// okReport 构造一份 K15 bh 实测值量级的报告（全部在阈值内）。
// 成交对账计数取 K15 bh 实测值（matched=363 go_only=68 nt_only=61）：
// fills 腿比的是原始条数（go=431 vs nt=424，相对差 1.6% < 5%）。
func okReport() *Report {
	return &Report{
		GoDeterministic: true,
		NTDeterministic: true,
		MetricRows: []metricRow{
			{Name: "total_return", Go: 0.012, NT: 0.012 - 0.00086*0.012, Diff: -0.00086 * 0.012, RelDiff: -0.00086},
			{Name: "annual_return", Go: 0.012, NT: 0.012, Diff: 0, RelDiff: 0},
			{Name: "sharpe", Go: 1.42, NT: 1.42 - 0.00109*1.42, Diff: -0.00109 * 1.42, RelDiff: -0.00109},
			{Name: "sortino", Go: 2.0, NT: 2.0 - 0.00276*2.0, Diff: -0.00276 * 2.0, RelDiff: -0.00276},
			{Name: "max_drawdown", Go: -0.05, NT: -0.05, Diff: 0, RelDiff: 0},
			{Name: "win_rate", Go: 0.55, NT: 0.76, Diff: 0.21, RelDiff: 0.377}, // 不设阈：即使相对差 37.7% 也不得红
			{Name: "calmar", Go: 0.24, NT: 0.24 + 0.00261*0.24, Diff: 0.00261 * 0.24, RelDiff: 0.00261},
			// total_trades 是配对计数（K15 归因④ + K16 ① 复核），不设阈——
			// 即使相对差 82% 也不得红（见 TestGate_TotalTradesNeverFails）。
			{Name: "total_trades", Go: 216, NT: 220, Diff: 4, RelDiff: 0.0185},
		},
		MatchedTrades: 363,
		GoOnlyTrades:  68,
		NTOnlyTrades:  61,
	}
}

func violationsOf(t *testing.T, r *Report) []GateViolation {
	t.Helper()
	return EvaluateGate(r)
}

func TestGate_K15MeasuredValuesPass(t *testing.T) {
	// K15 两实验的实测容差都留有余量（-0.086% vs 0.2% 等），门禁必须绿。
	if vs := violationsOf(t, okReport()); len(vs) != 0 {
		t.Fatalf("K15 实测值量级必须通过门禁，got violations:\n%s", FormatViolations(vs))
	}
}

func TestGate_MetricOverThresholdIsRed(t *testing.T) {
	// 破坏腿①：把 total_return 相对差推到 0.5%（超 0.2% 阈）→ 红并点名。
	r := okReport()
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "total_return" {
			r.MetricRows[i].Diff = 0.005 * r.MetricRows[i].Go
			r.MetricRows[i].RelDiff = 0.005
			r.MetricRows[i].NT = r.MetricRows[i].Go + r.MetricRows[i].Diff
		}
	}
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "total_return") && strings.Contains(v.Message, "超阈") {
			found = true
		}
	}
	if !found {
		t.Fatalf("超阈的 total_return 必须被点名，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_AbsThresholdOnSharpe(t *testing.T) {
	// 破坏腿②：sharpe 绝对差 0.08（超 0.05 绝对阈）→ 红；注意这不是相对差口径。
	r := okReport()
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "sharpe" {
			r.MetricRows[i].Diff = 0.08
			r.MetricRows[i].NT = r.MetricRows[i].Go + 0.08
			r.MetricRows[i].RelDiff = 0.08 / r.MetricRows[i].Go
		}
	}
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "sharpe") && strings.Contains(v.Message, "超绝对阈") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sharpe 绝对差超阈必须按绝对口径点名，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_WinRateNeverFails(t *testing.T) {
	// 破坏腿③（反向）：win_rate 相对差拉到 100% 也不得红（K15 归因④裁决）。
	r := okReport()
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "win_rate" {
			r.MetricRows[i].Diff = r.MetricRows[i].Go // 100% 相对差
			r.MetricRows[i].RelDiff = 1.0
			r.MetricRows[i].NT = r.MetricRows[i].Go * 2
		}
	}
	if vs := violationsOf(t, r); len(vs) != 0 {
		t.Fatalf("win_rate 不设硬阈，即使相对差 100%% 也不得红，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_TotalTradesNeverFails(t *testing.T) {
	// 破坏腿③b（反向）：total_trades 是配对计数（与 win_rate 同一算法），
	// K16 ① fixture 实测 128 vs 233（+82%）证明它对 Direction 词汇表敏感
	// ——不设阈，即使相对差 82% 也不得红；笔数守恒由 fills 腿执法。
	r := okReport()
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "total_trades" {
			r.MetricRows[i].Go = 128
			r.MetricRows[i].NT = 233
			r.MetricRows[i].Diff = 105
			r.MetricRows[i].RelDiff = 105.0 / 128.0
		}
	}
	if vs := violationsOf(t, r); len(vs) != 0 {
		t.Fatalf("total_trades 不设硬阈（配对计数），即使相对差 82%% 也不得红，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_DeterminismFailureIsRed(t *testing.T) {
	// 破坏腿④：任一侧两遍重跑不一致 → 红（确定性是归因前提）。
	for _, mutate := range []func(*Report){
		func(r *Report) { r.GoDeterministic = false },
		func(r *Report) { r.NTDeterministic = false },
	} {
		r := okReport()
		mutate(r)
		vs := violationsOf(t, r)
		found := false
		for _, v := range vs {
			if strings.Contains(v.Rule, "determinism") {
				found = true
			}
		}
		if !found {
			t.Fatalf("确定性腿失败必须红，got:\n%s", FormatViolations(vs))
		}
	}
}

func TestGate_MismatchIsRed(t *testing.T) {
	// 破坏腿⑤：成交对账出现 mismatch（同键量价费矛盾）→ 红；
	// go_only/nt_only 是价源精度的合法分叉，不得红。
	r := okReport()
	r.TradeRows = []tradeReconRow{
		{Kind: "go_only", Day: "2024-01-03", Symbol: "000001.SZ", Side: "BUY"},
		{Kind: "nt_only", Day: "2024-01-03", Symbol: "600000.SH", Side: "BUY"},
		{Kind: "mismatch", Day: "2024-02-01", Symbol: "000001.SZ", Side: "SELL", Detail: "qty=100 px=10.00 (fees: go=5.00 nt=7.74)"},
	}
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "mismatch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("mismatch 必须红，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_MissingMetricIsRed(t *testing.T) {
	// 破坏腿⑥：报告缺指标 → 红。「缺指标 ≠ 通过」——空报告不能白拿绿灯。
	// 删 calmar（total_trades 已不设阈，删它不会红，见 TestGate_TotalTradesNeverFails）。
	r := okReport()
	keep := r.MetricRows[:0]
	for _, m := range r.MetricRows {
		if m.Name != "calmar" {
			keep = append(keep, m)
		}
	}
	r.MetricRows = keep
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "calmar") && strings.Contains(v.Message, "缺指标") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺指标必须红并点名，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_FillsCountOverToleranceIsRed(t *testing.T) {
	// 破坏腿⑦：两侧成交**笔数**相对差超 5% → 红（total_trades 的词汇无关
	// 替身）。K15 真数据量级（431 vs 424）不得红，此构造 128 vs 233 必须红。
	r := okReport()
	r.MatchedTrades = 386
	r.GoOnlyTrades = 61
	r.NTOnlyTrades = 59 // go=447 nt=445 —— 健康
	if vs := violationsOf(t, r); len(vs) != 0 {
		t.Fatalf("笔数相近（447 vs 445）应通过，got:\n%s", FormatViolations(vs))
	}
	r.MatchedTrades = 100
	r.GoOnlyTrades = 28
	r.NTOnlyTrades = 133 // go=128 nt=233，相对差 82% → 红
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "trades/fills") && strings.Contains(v.Message, "超阈") {
			found = true
		}
	}
	if !found {
		t.Fatalf("笔数相对差 82%% 必须红并点名，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_EmptyFillsIsRed(t *testing.T) {
	// 破坏腿⑧：两侧零成交 → 红（空成交 ≠ 通过，信号流/数据没灌进来时
	// 门禁不能白拿绿灯）。
	r := okReport()
	r.MatchedTrades, r.GoOnlyTrades, r.NTOnlyTrades = 0, 0, 0
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "trades/fills") && strings.Contains(v.Message, "空成交") {
			found = true
		}
	}
	if !found {
		t.Fatalf("零成交必须红，got:\n%s", FormatViolations(vs))
	}
}

func TestGate_GoZeroRelativeDegradesToAbsolute(t *testing.T) {
	// 口径腿：Go=0 时相对差不可算，退化为绝对差判定，不静默放过。
	r := okReport()
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "total_return" {
			r.MetricRows[i].Go = 0
			r.MetricRows[i].NT = 0.001 // 绝对差 0.001 < 0.002 退化阈 → 过
			r.MetricRows[i].Diff = 0.001
		}
	}
	if vs := violationsOf(t, r); len(vs) != 0 {
		t.Fatalf("Go=0 且绝对差在退化阈内应通过，got:\n%s", FormatViolations(vs))
	}
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "total_return" {
			r.MetricRows[i].NT = 0.01 // 绝对差 0.01 > 0.002 → 红
			r.MetricRows[i].Diff = 0.01
		}
	}
	vs := violationsOf(t, r)
	found := false
	for _, v := range vs {
		if strings.Contains(v.Rule, "total_return") && strings.Contains(v.Message, "Go=0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Go=0 超退化阈必须红并注明口径，got:\n%s", FormatViolations(vs))
	}
}

func TestFormatViolations(t *testing.T) {
	if got := FormatViolations(nil); !strings.Contains(got, "通过") {
		t.Errorf("零违例应渲染通过, got %q", got)
	}
	msg := FormatViolations([]GateViolation{{Rule: "metric/sharpe", Message: "boom"}})
	if !strings.Contains(msg, "1 项违例") || !strings.Contains(msg, "boom") {
		t.Errorf("违例清单渲染不完整: %q", msg)
	}
}

// ── K16 ⑤ 告警接线腿：门禁违例必经告警通道 ──────────────────────────
// 破坏验证：dispatchGateAlerts 不转发 → TestGateAlerts_* 全红。

func TestGateAlerts_DispatchForEachViolation(t *testing.T) {
	// 转达面：一条违例一条 breach，rule/message 原样带上，runID 带来源前缀。
	r := okReport()
	r.GoDeterministic = false
	for i := range r.MetricRows {
		if r.MetricRows[i].Name == "total_return" {
			r.MetricRows[i].Diff = 0.005 * r.MetricRows[i].Go
			r.MetricRows[i].RelDiff = 0.005
			r.MetricRows[i].NT = r.MetricRows[i].Go + r.MetricRows[i].Diff
		}
	}
	vs := violationsOf(t, r)
	if len(vs) != 2 {
		t.Fatalf("构造应产生 2 条违例（确定性 + total_return 超阈），got %d:\n%s", len(vs), FormatViolations(vs))
	}

	var gotRunID string
	var got []alert.DualRunBreach
	dispatchGateAlerts(vs, func(_ context.Context, runID string, breaches []alert.DualRunBreach) {
		gotRunID = runID
		got = breaches
	})

	if !strings.HasPrefix(gotRunID, "dualrun-") {
		t.Errorf("runID 应带 dualrun- 来源前缀，got %q", gotRunID)
	}
	if len(got) != len(vs) {
		t.Fatalf("一条违例一条告警，want %d got %d", len(vs), len(got))
	}
	for i, b := range got {
		if b.Rule != vs[i].Rule || b.Message != vs[i].Message {
			t.Errorf("breach[%d] 应原样转达门禁违例，want {%s %q} got {%s %q}",
				i, vs[i].Rule, vs[i].Message, b.Rule, b.Message)
		}
	}
}

func TestGateAlerts_ProductionFireDeliversAlerts(t *testing.T) {
	// 生产腿：productionBreachFire 经真实告警通道（webhook）送达，
	// rule=dualrun_gate、severity=critical、消息点名违例规则。
	received := make(chan alert.Alert, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a alert.Alert
		_ = json.NewDecoder(r.Body).Decode(&a)
		received <- a
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("DUALRUN_ALERT_WEBHOOK", srv.URL)

	vs := []GateViolation{{Rule: "metric/sharpe", Message: "sharpe: go=1.2 nt=1.377 |Δ|=0.177 超绝对阈 0.05"}}
	dispatchGateAlerts(vs, productionBreachFire())

	// productionBreachFire 的 mgr.Close() 排空 webhook 队列后送达已确定；
	// 超时护栏防将来通道语义变化时测试挂死。
	var a alert.Alert
	select {
	case a = <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("webhook 送达超时：门禁违例未进告警通道")
	}
	if a.Rule != alert.RuleDualRunGate {
		t.Errorf("rule 应为 %q（外部告警带来源前缀约定），got %q", alert.RuleDualRunGate, a.Rule)
	}
	if a.Severity != alert.SeverityCritical {
		t.Errorf("门禁违例全 critical（对齐一致是硬门禁），got %q", a.Severity)
	}
	for _, want := range []string{"metric/sharpe", "超绝对阈", vs[0].Message} {
		if !strings.Contains(a.Message, want) {
			t.Errorf("告警消息应点名 %q，got %q", want, a.Message)
		}
	}
	if a.Attributes["breach_rule"] != "metric/sharpe" {
		t.Errorf("breach_rule 属性应记录违例规则，got %v", a.Attributes["breach_rule"])
	}
	if runID, _ := a.Attributes["run_id"].(string); !strings.HasPrefix(runID, "dualrun-") {
		t.Errorf("run_id 属性应带 dualrun- 前缀，got %v", a.Attributes["run_id"])
	}
	select {
	case extra := <-received:
		t.Errorf("一条违例一条告警，不应多发，got extra: %+v", extra)
	default:
	}
}
