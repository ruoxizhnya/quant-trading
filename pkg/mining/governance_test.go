package mining

import (
	"math"
	"testing"
)

// ─── 治理层三件之一：过拟合检测（D8 ⑤2 机械判据） ────────────────────────
//
// 正例：已知「试了很多次才撞出来」的弱 IC 必须被标记过拟合。
// 反例：真实强信号（且只试了一次）不得误伤。

// TestDetectOverfit_FlagsKnownOverfit 是正例：IC=0.05 在 500 次尝试里撞出来
// ——单看很弱，校正后必然不显著。
func TestDetectOverfit_FlagsKnownOverfit(t *testing.T) {
	got := DetectOverfit(OverfitInput{IC: 0.05, NumPeriods: 100, NumTrials: 500, FailedTrials: 0})
	if !got.Overfit {
		t.Fatalf("weak IC found across 500 trials must be flagged as overfit, got %+v", got)
	}
	if !(got.AdjustedPValue >= DefaultAlpha) {
		t.Fatalf("adjusted p must be >= alpha for an overfit result, got %.6f", got.AdjustedPValue)
	}
	if got.AdjustedPValue < got.RawPValue {
		t.Fatalf("multiplicity correction must not lower the p value: raw=%.6f adj=%.6f", got.RawPValue, got.AdjustedPValue)
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("an overfit verdict must carry a reason, got none")
	}
}

// TestDetectOverfit_DoesNotFlagStrongSignal 是反例（不误伤）：真实强信号
// （IC=0.30，252 个交易日，只试了一次）必须放行。
func TestDetectOverfit_DoesNotFlagStrongSignal(t *testing.T) {
	got := DetectOverfit(OverfitInput{IC: 0.30, NumPeriods: 252, NumTrials: 1, FailedTrials: 0})
	if got.Overfit {
		t.Fatalf("a genuinely strong signal tried once must not be flagged, got %+v", got)
	}
	if !got.Significant {
		t.Fatalf("strong signal must be significant, got %+v", got)
	}
	if got.AdjustedPValue >= DefaultAlpha {
		t.Fatalf("adjusted p must be < alpha, got %.6f", got.AdjustedPValue)
	}
}

// TestDetectOverfit_NumTrialsIsLoadBearing 钉住「尝试次数必须计入校正」这条
// 先例教训：同一 IC / 期数，只改尝试次数，判定必须翻转。
//
// 这正是 D8 ③2 点名的「adjP 的 NumTrials 语义」：试 1 次与试 20000 次得到
// 的因子不是一回事，校正必须把选择偏差算进去。
func TestDetectOverfit_NumTrialsIsLoadBearing(t *testing.T) {
	one := DetectOverfit(OverfitInput{IC: 0.30, NumPeriods: 100, NumTrials: 1})
	many := DetectOverfit(OverfitInput{IC: 0.30, NumPeriods: 100, NumTrials: 20000})
	if one.Overfit {
		t.Fatalf("same IC found once should pass, got %+v", one)
	}
	if !many.Overfit {
		t.Fatalf("same IC found across 20000 trials must be flagged, got %+v", many)
	}
	if many.AdjustedPValue <= one.AdjustedPValue {
		t.Fatalf("more trials must raise the adjusted p: one=%.8f many=%.8f", one.AdjustedPValue, many.AdjustedPValue)
	}
}

// TestDetectOverfit_FailClosedOnBadInput 钉 fail-closed：说不清就不放行。
func TestDetectOverfit_FailClosedOnBadInput(t *testing.T) {
	cases := []struct {
		name string
		in   OverfitInput
	}{
		{"zero periods", OverfitInput{IC: 0.3, NumPeriods: 0, NumTrials: 1}},
		{"zero trials", OverfitInput{IC: 0.3, NumPeriods: 100, NumTrials: 0}},
		{"NaN IC", OverfitInput{IC: math.NaN(), NumPeriods: 100, NumTrials: 1}},
		{"Inf IC", OverfitInput{IC: math.Inf(1), NumPeriods: 100, NumTrials: 1}},
		{"negative failed trials", OverfitInput{IC: 0.3, NumPeriods: 100, NumTrials: 10, FailedTrials: -1}},
		{"failed trials exceed total", OverfitInput{IC: 0.3, NumPeriods: 100, NumTrials: 10, FailedTrials: 11}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectOverfit(c.in)
			if !got.Overfit {
				t.Fatalf("invalid input must fail closed (Overfit=true), got %+v", got)
			}
			if len(got.Reasons) == 0 {
				t.Fatalf("fail-closed verdict must explain why")
			}
		})
	}
}

// TestDetectOverfit_HighFailureRateAddsReason：失败占多数时额外提示选择偏差
// 更大——这是「NumTrials 必须含失败」的可观测面。
func TestDetectOverfit_HighFailureRateAddsReason(t *testing.T) {
	got := DetectOverfit(OverfitInput{IC: 0.05, NumPeriods: 100, NumTrials: 100, FailedTrials: 80})
	if len(got.Reasons) < 2 {
		t.Fatalf("majority-failed trials should add a selection-bias reason, got %v", got.Reasons)
	}
}

// ─── 治理层三件之二：样本外验证（D8 ⑤2） ────────────────────────────────

func TestValidateOutOfSample_PassesWhenSignalPersists(t *testing.T) {
	got := ValidateOutOfSample(OOSInput{InSampleIC: 0.30, OutSampleIC: 0.20})
	if !got.Pass {
		t.Fatalf("same-sign, 0.67 retention must pass, got %+v", got)
	}
	if math.Abs(got.Retention-0.6666) > 0.01 {
		t.Fatalf("retention must be |out|/|in| = 0.667, got %.4f", got.Retention)
	}
}

func TestValidateOutOfSample_Fails(t *testing.T) {
	cases := []struct {
		name string
		in   OOSInput
	}{
		{"sign flip", OOSInput{InSampleIC: 0.30, OutSampleIC: -0.20}},
		{"retention too low", OOSInput{InSampleIC: 0.30, OutSampleIC: 0.05}},
		{"out missing (NaN)", OOSInput{InSampleIC: 0.30, OutSampleIC: math.NaN()}},
		{"in is zero", OOSInput{InSampleIC: 0, OutSampleIC: 0.10}},
		{"in not finite", OOSInput{InSampleIC: math.Inf(1), OutSampleIC: 0.10}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ValidateOutOfSample(c.in)
			if got.Pass {
				t.Fatalf("case %q must fail out-of-sample, got %+v", c.name, got)
			}
			if len(got.Reasons) == 0 {
				t.Fatalf("a failed OOS verdict must explain why")
			}
		})
	}
}
