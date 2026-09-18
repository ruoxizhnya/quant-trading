package examples

import (
	"testing"
)

// P2-10 的回归测试：缺失值不再被当成 0。
//
// 这个 bug 的杀伤力在于它会**主动把人引向错误的交易**：PE 缺失折成 0 之后，
// 在「PE 越低越便宜」的排序里直接冲到第一名 —— 一只数据不全的股票会被当成
// 最便宜的票买进来。改之前存储层用 COALESCE(pe, 0) 兜底，因子层又靠
// "v != 0" 来近似"缺数据"，两头都在猜，中间没有人说真话。
//
// 现在的规矩：**没披露 = nil = 这一项不参与打分**（得 0 分 = 中性），
// 而不是 "0 = 极端便宜"。

func TestCalculateZScores_MissingPEIsNeutralNotCheapest(t *testing.T) {
	s := &valueMomentumStrategy{}

	cheap, mid, unknown := 10.0, 30.0, (*float64)(nil)
	data := map[string]*stockFactorData{
		"CHEAP":   {Symbol: "CHEAP", PE: &cheap, PB: &mid, Momentum: 0.1, ROE: &mid},
		"EXPENS":  {Symbol: "EXPENS", PE: &mid, PB: &mid, Momentum: 0.1, ROE: &mid},
		"UNKNOWN": {Symbol: "UNKNOWN", PE: unknown, PB: unknown, Momentum: 0.1, ROE: unknown},
	}

	zs := s.calculateZScores(data, s.calculatePercentiles(data))

	unknownZS, ok := zs["UNKNOWN"]
	if !ok {
		t.Fatal("缺数据的股票也该有 z-score 条目（各项为中性 0）")
	}
	if unknownZS.ZScorePE != 0 || unknownZS.ZScorePB != 0 || unknownZS.ZScoreQu != 0 {
		t.Fatalf("缺 PE/PB/ROE 的股票在这三项上应是中性 0，实际 PE=%.3f PB=%.3f Qu=%.3f",
			unknownZS.ZScorePE, unknownZS.ZScorePB, unknownZS.ZScoreQu)
	}

	// 真正便宜的那只必须在 PE 这一项上赢过"数据缺失"的那只 ——
	// 否则缺失值就还是在变相冒充便宜货。
	cheapZS := zs["CHEAP"]
	if cheapZS.ZScorePE <= unknownZS.ZScorePE {
		t.Fatalf("PE 最低的股票在 PE 项上应优于数据缺失的股票：%.3f vs %.3f",
			cheapZS.ZScorePE, unknownZS.ZScorePE)
	}
}

// 均值/标准差只统计有数据的样本 —— 把缺失值当 0 混进分布，
// 会把整个横截面的中心拉向 0，所有人的 z-score 跟着一起错。
func TestCalculateMeanStd_SkipsMissing(t *testing.T) {
	ten, twenty := 10.0, 20.0
	data := map[string]*stockFactorData{
		"A": {PE: &ten},
		"B": {PE: &twenty},
		"C": {PE: nil}, // 没披露
	}

	mean, std := calculateMeanStd(data, func(d *stockFactorData) *float64 { return d.PE })

	// 只该看 A、B：(10+20)/2 = 15。若把 C 当 0 混进来，均值会是 10。
	if mean < 14.9 || mean > 15.1 {
		t.Fatalf("均值应只统计有数据的样本（期望 15），实际 %.3f", mean)
	}
	if std < 4.9 || std > 5.1 {
		t.Fatalf("标准差应只统计有数据的样本（期望 5），实际 %.3f", std)
	}

	// 一条数据都没有时给 (0, 0)，让调用方自己决定怎么办 ——
	// 那不是"可以算"，是"算不了"。
	allMissing := map[string]*stockFactorData{"A": {PE: nil}}
	if m, sd := calculateMeanStd(allMissing, func(d *stockFactorData) *float64 { return d.PE }); m != 0 || sd != 0 {
		t.Fatalf("全部缺失时应返回 (0, 0)，实际 (%.3f, %.3f)", m, sd)
	}
}
