package validation

import (
	"fmt"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// 验证器链的聚合入口：一次提案喂进去，六维的质疑清单吐出来。
//
// 前五片各自独立（统计 / 经济 / 稳健 / 偏差 / 冗余），这一层把它们拼成
// 人能看的一页。**它不替人下结论** —— 输出的是概率估计 + 质疑清单，
// 判生死的仍然是人（PRODUCT §边界：决策权在人）。
//
// 为什么必须有这一层：五片写完之后全仓零调用，整包是孤儿。能力造出来
// 不等于有人用（P2-13 的教训），聚合入口就是那个「有人用」的把手。

// Proposal 是一次提案：一条策略 + 它的回测结果 + 上下文。
//
// 只有 Result 是必需的，其余都是「有就更准」 —— 但缺了它们，
// 对应那一维会退化成未评估，而不是假装没事。
type Proposal struct {
	Name string `json:"name,omitempty"`

	// Result 是这次尝试的回测结果。五维里有三维直接吃它。
	Result *domain.BacktestResult `json:"-"`

	// NumTrials / FailedTrials 是这一轮探索总共试了多少次（**含失败**）。
	// 统计维靠它收紧门槛：只报成功次数，等于把最有力的那部分选择偏差藏起来。
	NumTrials    int `json:"num_trials"`
	FailedTrials int `json:"failed_trials"`

	// Neighbors 是同一轮探索里试过的邻近参数 —— 稳健维靠它分辨高原还是尖峰。
	Neighbors []NeighborPoint `json:"-"`

	// Cost 是成本模型，零值 = 用 A 股默认费率（见 economic.go）。
	Cost CostModel `json:"-"`

	// Bias 是偏差维的输入（前视 / 幸存者 / 复权口径）。
	Bias BiasInput `json:"-"`

	// Existing 是组合里已有的策略，冗余维拿它比相关性。
	Existing map[string]*domain.BacktestResult `json:"-"`
}

// Verdict 是聚合后的结论。
type Verdict struct {
	// Dimensions 是各维的概率。缺的那一维不出现在 map 里 ——
	// 不在 = 没评估，而不是 0 分。
	Dimensions map[string]float64 `json:"dimensions"`
	// Unassessed 是没评估的维度名，让「没查」在输出里看得见。
	Unassessed []string `json:"unassessed,omitempty"`

	// Probability 是综合概率，取**已评估维度的最小值**。
	//
	// 为什么是 min 而不是平均：这几维是合取关系 —— 一条策略必须在每一维
	// 都站得住，才谈得上下注。任何一个塌了，其它维再漂亮也救不回来。
	// 算术平均会让「四维优秀」掩盖「一维致命」，几何平均稀释得太狠
	//（0.02 配四个 0.9，几何平均还有 0.42，看起来居然还行）。
	// min 的解释性也最强：结论受限于最弱的那一环，一眼看得出该修哪。
	Probability float64 `json:"probability"`
	// Weakest 是最弱的那一维 —— 想提高结论可信度，就该从它下手。
	Weakest string `json:"weakest,omitempty"`

	// GeometricMean 是各维的几何平均，作为参考一起给出。
	// 它和 min 差得越多，说明各维越不均衡（有一维在拖后腿）。
	GeometricMean float64 `json:"geometric_mean"`

	// Blocking 是 blocking 级质疑的条数。>0 时这个提案基本可以否掉了。
	Blocking   int         `json:"blocking"`
	Challenges []Challenge `json:"challenges"`

	// 各维的完整结果，未计算的为 nil。
	Statistical *StatisticalResult `json:"statistical,omitempty"`
	Economic    *EconomicResult    `json:"economic,omitempty"`
	Robustness  *RobustnessResult  `json:"robustness,omitempty"`
	BiasResult  *BiasResult        `json:"bias,omitempty"`
	Redundancy  *RedundancyResult  `json:"redundancy,omitempty"`
}

// ValidateProposal 跑一遍验证器链，输出质疑清单 + 概率估计。
//
// 各维能算就算，算不了就记进 Unassessed —— 缺哪一维都说得清清楚楚，
// 不拿剩下的几维凑一个「看起来完整」的结论。
func ValidateProposal(p Proposal) Verdict {
	v := Verdict{Dimensions: map[string]float64{}}

	if p.Result == nil {
		v.Unassessed = []string{
			DimensionStatistical, DimensionEconomic,
			DimensionRobustness, DimensionRedundancy,
		}
		v.Challenges = []Challenge{{
			Dimension: DimensionBias,
			Severity:  SeverityBlocking,
			Message: "没有回测结果，五维里四维无法评估。给不出结论就是给不出 —— " +
				"不会拿剩下的一维凑一个看起来完整的数字。",
		}}
		v.Blocking = 1
		v.Probability = 0.5
		return v
	}

	periods := len(p.Result.PortfolioValues)

	// --- 统计 ---
	if p.NumTrials > 0 && periods > 0 {
		sr := ValidateStatistical(StatisticalInput{
			Sharpe:       p.Result.SharpeRatio,
			NumPeriods:   periods,
			NumTrials:    p.NumTrials,
			FailedTrials: p.FailedTrials,
		})
		v.Statistical = &sr
		v.Dimensions[DimensionStatistical] = sr.Probability
		v.Challenges = append(v.Challenges, sr.Challenges...)
	} else {
		v.Unassessed = append(v.Unassessed, DimensionStatistical)
		v.Challenges = append(v.Challenges, Challenge{
			Dimension: DimensionStatistical,
			Severity:  SeverityNote,
			Message: fmt.Sprintf(
				"统计维未评估：需要尝试次数（当前 %d）和观测期数（当前 %d）。"+
					"不知道试了多少次，就无法判断这个 Sharpe 是不是被挑出来的。",
				p.NumTrials, periods),
		})
	}

	// --- 经济 ---
	if er, ok := ValidateEconomicFromBacktest(p.Result, p.Cost); ok {
		v.Economic = &er
		v.Dimensions[DimensionEconomic] = er.Probability
		v.Challenges = append(v.Challenges, er.Challenges...)
	} else {
		v.Unassessed = append(v.Unassessed, DimensionEconomic)
		v.Challenges = append(v.Challenges, Challenge{
			Dimension: DimensionEconomic,
			Severity:  SeverityNote,
			Message: "经济维未评估：回测没有成交，算不出换手率，也就算不出成本。" +
				"（零成交不是零成本 —— 它是什么都没证明。）",
		})
	}

	// --- 稳健 ---
	rr := ValidateRobustness(RobustnessInput{
		Result:    p.Result,
		Neighbors: p.Neighbors,
	})
	v.Robustness = &rr
	v.Dimensions[DimensionRobustness] = rr.Probability
	v.Challenges = append(v.Challenges, rr.Challenges...)

	// --- 偏差 ---
	br := ValidateBias(p.Bias)
	v.BiasResult = &br
	// 偏差维里 0 表示「未评估」，不能塞进 Dimensions ——
	// 那样会被当成 0 分，而它是"不知道"。
	if br.Assessed > 0 {
		v.Dimensions[DimensionBias] = br.Probability
	} else {
		v.Unassessed = append(v.Unassessed, DimensionBias)
	}
	v.Challenges = append(v.Challenges, br.Challenges...)

	// --- 冗余 ---
	if len(p.Existing) > 0 {
		in := RedundancyFromBacktests(p.Result, p.Existing)
		dr := ValidateRedundancy(in)
		v.Redundancy = &dr
		if dr.Compared > 0 {
			v.Dimensions[DimensionRedundancy] = dr.Probability
		} else {
			v.Unassessed = append(v.Unassessed, DimensionRedundancy)
		}
		v.Challenges = append(v.Challenges, dr.Challenges...)
	} else {
		v.Unassessed = append(v.Unassessed, DimensionRedundancy)
		v.Challenges = append(v.Challenges, Challenge{
			Dimension: DimensionRedundancy,
			Severity:  SeverityNote,
			Message:   "冗余维未评估：没有传入已有策略，无从判断这是不是伪分散。",
		})
	}

	finalizeVerdict(&v)
	return v
}

// finalizeVerdict 算综合概率与统计信息。
func finalizeVerdict(v *Verdict) {
	if len(v.Dimensions) == 0 {
		v.Probability = 0.5
		v.Challenges = append(v.Challenges, Challenge{
			Dimension: DimensionBias,
			Severity:  SeverityNote,
			Message:   "六维一个都没评估，给出的 0.5 是中性值，不是结论。",
		})
		return
	}

	// 顺序固定 —— 同一个提案两次跑出来的 weakest 必须一样。
	order := []string{
		DimensionStatistical, DimensionEconomic, DimensionRobustness,
		DimensionBias, DimensionRedundancy,
	}

	minProb, weakest, prod := 2.0, "", 1.0
	n := 0
	for _, d := range order {
		p, ok := v.Dimensions[d]
		if !ok {
			continue
		}
		n++
		prod *= p
		if p < minProb {
			minProb, weakest = p, d
		}
	}
	v.Probability = minProb
	v.Weakest = weakest
	v.GeometricMean = math.Pow(prod, 1.0/float64(n))

	for _, c := range v.Challenges {
		if c.Severity == SeverityBlocking {
			v.Blocking++
		}
	}
}
