package mining

import (
	"fmt"
	"math"
)

// ─── 治理层三件之一、之二：过拟合检测 + 样本外验证（D8 ③2） ──────────────
//
// 过拟合检测：多重检验校正。核心事实（先例 pkg/ai/validation 的
// StatisticalInput.NumTrials）——试了 N 次才撞出来的结果，显著性要按 N
// 收紧；NumTrials 必须**含失败**，只统计成功次数等于把最有力的那部分
// 选择偏差藏起来。本文件把那条先例公式在因子场景落地（IC 的 t 值），
// 并显式把「无法评估」判为过拟合（fail-closed：说不清 = 不放行）。

// DefaultAlpha 是名义显著性水平（与 pkg/ai/validation 同值）。
const DefaultAlpha = 0.05

// OverfitInput 是过拟合检测的输入。
type OverfitInput struct {
	// IC 是候选因子的样本内信息系数（IC）。
	IC float64
	// NumPeriods 是观测期数（交易日）。
	NumPeriods int
	// NumTrials 是本轮共试了多少候选（**含失败**）。
	NumTrials int
	// FailedTrials 是其中失败多少次（失败率本身是信号）。
	FailedTrials int
}

// OverfitResult 是过拟合检测的输出。
type OverfitResult struct {
	RawPValue      float64 `json:"raw_p_value"`
	AdjustedPValue float64 `json:"adjusted_p_value"`
	Significant    bool    `json:"significant"`
	// Overfit = 校正后仍不显著（或无法评估）。true 即「疑似过拟合，拒收」。
	Overfit bool     `json:"overfit"`
	Reasons []string `json:"reasons,omitempty"`
}

// DetectOverfit 对候选做多重检验校正。
//
//	z   = |IC| × sqrt(期数)            —— IC 的 t 近似（H0: 真实 IC = 0）
//	raw = 2·(1 − Φ(z))                 —— 单次检验的 p 值
//	adj = 1 − (1 − raw)^NumTrials      —— 族系误差（试 N 次至少中一次的概率）
//
// 校正后 p ≥ alpha ⇒ Overfit=true。输入不合法（期数/尝试次数非正、失败次数
// 越界、IC NaN）同样判 Overfit=true 并附理由——**说不清就不放行**，与验证器链
// 「缺任何一个就给不出诚实的概率」同款 fail-closed 立场。
func DetectOverfit(in OverfitInput) OverfitResult {
	if in.NumPeriods <= 0 || in.NumTrials <= 0 ||
		in.FailedTrials < 0 || in.FailedTrials > in.NumTrials ||
		math.IsNaN(in.IC) || math.IsInf(in.IC, 0) {
		return OverfitResult{
			Overfit: true,
			Reasons: []string{fmt.Sprintf(
				"过拟合检测无法进行：观测期数=%d、尝试次数=%d、失败次数=%d、IC=%.4f 不构成可评估输入——缺任何一项都不放行",
				in.NumPeriods, in.NumTrials, in.FailedTrials, in.IC)},
		}
	}

	z := math.Abs(in.IC) * math.Sqrt(float64(in.NumPeriods))
	rawP := 2 * (1 - normalCDF(z))
	if rawP < 0 {
		rawP = 0
	}
	adjP := 1 - math.Pow(1-rawP, float64(in.NumTrials))
	if adjP > 1 {
		adjP = 1
	}

	res := OverfitResult{
		RawPValue:      rawP,
		AdjustedPValue: adjP,
		Significant:    adjP < DefaultAlpha,
		Overfit:        adjP >= DefaultAlpha,
	}
	if res.Overfit {
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"试了 %d 次之后 IC=%.4f 已不显著：校正后 p=%.4f ≥ %.2f（单次 p=%.4f）——这个 IC 的大头可能来自选择偏差，不是因子本身",
			in.NumTrials, in.IC, adjP, DefaultAlpha, rawP))
	}
	if in.FailedTrials > 0 && float64(in.FailedTrials)/float64(in.NumTrials) > 0.5 {
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"%d/%d 次尝试失败——成功样本只占少数，在这上面挑最优，选择偏差比看上去更大",
			in.FailedTrials, in.NumTrials))
	}
	return res
}

// DefaultMinRetention 是样本外验证的最小保留比（Out/In 幅度）。
const DefaultMinRetention = 0.5

// OOSInput 是样本外验证的输入。
type OOSInput struct {
	InSampleIC  float64
	OutSampleIC float64
	// MinRetention 为 0 时用 DefaultMinRetention。
	MinRetention float64
}

// OOSResult 是样本外验证的输出。
type OOSResult struct {
	Pass      bool     `json:"pass"`
	Retention float64  `json:"retention"`
	Reasons   []string `json:"reasons,omitempty"`
}

// ValidateOutOfSample 校验候选「换了时间段还站得住」。
//
// 判据（两条都要）：样本内外 IC **同号**，且幅度保留比 |Out|/|In| ≥ 阈值
// （默认 0.5）。fail-closed：样本内 IC 为 0 / NaN / 样本外缺失（NaN）
// 一律判不通过——「样本外没测」不等于「样本外通过」。
func ValidateOutOfSample(in OOSInput) OOSResult {
	retention := in.MinRetention
	if retention <= 0 {
		retention = DefaultMinRetention
	}
	if in.InSampleIC == 0 || math.IsNaN(in.InSampleIC) || math.IsInf(in.InSampleIC, 0) {
		return OOSResult{Pass: false, Reasons: []string{"样本内 IC 为 0 或非有限值——样本外验证无从谈起，不放行"}}
	}
	if math.IsNaN(in.OutSampleIC) || math.IsInf(in.OutSampleIC, 0) {
		return OOSResult{Pass: false, Reasons: []string{"样本外 IC 缺失——「没测」不等于「通过」，不放行"}}
	}

	ratio := math.Abs(in.OutSampleIC) / math.Abs(in.InSampleIC)
	res := OOSResult{Retention: ratio}
	if math.Signbit(in.InSampleIC) != math.Signbit(in.OutSampleIC) {
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"样本外 IC %.4f 与样本内 %.4f 符号相反——换个时间段就反号，判过拟合", in.OutSampleIC, in.InSampleIC))
		return res
	}
	if ratio < retention {
		res.Reasons = append(res.Reasons, fmt.Sprintf(
			"样本外 IC 保留比 %.2f < %.2f（样本内 %.4f → 样本外 %.4f）——失效太快", ratio, retention, in.InSampleIC, in.OutSampleIC))
		return res
	}
	res.Pass = true
	return res
}

// normalCDF 是标准正态分布的累积分布函数：Φ(x) = ½(1 + erf(x/√2))。
// 与 pkg/ai/validation 的算法一致（同一把尺子）。
func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}
