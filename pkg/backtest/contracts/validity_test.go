package contracts

import (
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// OBS-01 的纯函数单测：CheckValidity 的每一条 Code 都要能被单独触发，
// 且正常结果一条都不触发（防「把正常回测打成无效」）。

func containsReasonCode(reasons []InvalidReason, code string) bool {
	for _, r := range reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

// validResult 是一份「正常回测」的结果：票池非空、有成交、日期正常、指标有限。
func validResult() domain.BacktestResult {
	return domain.BacktestResult{
		StartDate:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		EndDate:         time.Date(2024, 6, 28, 0, 0, 0, 0, time.UTC),
		TotalReturn:     0.21,
		AnnualReturn:    0.38,
		SharpeRatio:     1.4,
		SortinoRatio:    1.9,
		MaxDrawdown:     -0.12,
		WinRate:         0.55,
		TotalTrades:     37,
		AvgHoldingDays:  9.5,
		CalmarRatio:     3.1,
		UniverseMaxSize: intPtr(42),
	}
}

func TestCheckValidity_ValidRunHasNoReasons(t *testing.T) {
	res := validResult()
	if got := CheckValidity(&res); len(got) != 0 {
		t.Fatalf("正常回测不该被判无效，实际 %+v", got)
	}
}

func TestCheckValidity_EmptyUniverse(t *testing.T) {
	res := validResult()
	res.UniverseMaxSize = intPtr(0)
	// 真实现场里空票池必然伴随 0 成交 —— 两条都该报出来。
	res.TotalTrades = 0

	got := CheckValidity(&res)
	if !containsReasonCode(got, InvalidEmptyUniverse) {
		t.Fatalf("票池规模=0 必须命中 empty_universe，实际 %+v", got)
	}
	if !containsReasonCode(got, InvalidZeroTrades) {
		t.Fatalf("0 成交必须命中 zero_trades，实际 %+v", got)
	}
}

func TestCheckValidity_ZeroTrades(t *testing.T) {
	res := validResult()
	res.TotalTrades = 0
	got := CheckValidity(&res)
	if !containsReasonCode(got, InvalidZeroTrades) {
		t.Fatalf("0 成交必须命中 zero_trades，实际 %+v", got)
	}
	if containsReasonCode(got, InvalidEmptyUniverse) {
		t.Fatalf("票池非空时不该命中 empty_universe，实际 %+v", got)
	}
}

func TestCheckValidity_ZeroStartDate(t *testing.T) {
	res := validResult()
	res.StartDate = time.Time{} // 0001-01-01
	got := CheckValidity(&res)
	if !containsReasonCode(got, InvalidZeroStartDate) {
		t.Fatalf("零值起始日期必须命中 zero_start_date，实际 %+v", got)
	}
}

func TestCheckValidity_GarbageMetric(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*domain.BacktestResult)
	}{
		{"MaxFloat64-sortino", func(r *domain.BacktestResult) { r.SortinoRatio = math.MaxFloat64 }},
		{"NaN-sharpe", func(r *domain.BacktestResult) { r.SharpeRatio = math.NaN() }},
		{"plusInf-calmar", func(r *domain.BacktestResult) { r.CalmarRatio = math.Inf(1) }},
		{"minusInf-annual", func(r *domain.BacktestResult) { r.AnnualReturn = math.Inf(-1) }},
		{"beyond-physical-cap", func(r *domain.BacktestResult) { r.TotalReturn = 1e13 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := validResult()
			tc.apply(&res)
			got := CheckValidity(&res)
			if !containsReasonCode(got, InvalidGarbageMetric) {
				t.Fatalf("%s 必须命中 garbage_metric，实际 %+v", tc.name, got)
			}
		})
	}
}

func TestCheckValidity_NilResultIsNoReasons(t *testing.T) {
	if got := CheckValidity(nil); got != nil {
		t.Fatalf("nil 结果返回 nil，实际 %+v", got)
	}
}

func TestInvalidReasonCodes(t *testing.T) {
	reasons := []InvalidReason{
		{Code: InvalidEmptyUniverse, Detail: "x"},
		{Code: InvalidZeroTrades, Detail: "y"},
	}
	got := InvalidReasonCodes(reasons)
	if len(got) != 2 || got[0] != InvalidEmptyUniverse || got[1] != InvalidZeroTrades {
		t.Fatalf("Codes 抽取错误：%v", got)
	}
	if InvalidReasonCodes(nil) != nil {
		t.Fatal("空输入应返回 nil")
	}
}

// intPtr 返回 int 的指针（OBS-01 审查修复：UniverseMaxSize 改为 *int，
// nil = 未评估、0 = 明确为空 —— 未知不等于空）。
func intPtr(v int) *int { return &v }

// TestCheckValidity_UnassessedUniverseIsNotClaimedEmpty 是审查修复的护栏：
// nil = 未评估（其它 BacktestResult 生产路径没填该字段）**不能**被断成空票池 ——
// 那是 fail-loud 最不能容忍的假阳性（假红灯会训练人忽略红灯）。
func TestCheckValidity_UnassessedUniverseIsNotClaimedEmpty(t *testing.T) {
	res := validResult()
	res.UniverseMaxSize = nil

	if got := CheckValidity(&res); containsReasonCode(got, InvalidEmptyUniverse) {
		t.Fatalf("未评估（nil）不能被判成空票池，实际 %+v", got)
	}
}
