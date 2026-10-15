package data

import (
	"math"
	"testing"

	"github.com/rs/zerolog"
)

// 前复权口径的回归测试。
//
// 为什么值得单独一个文件：复权价是回测里每一个数字的地基，而 qfq 的换算式
// （raw × 当日因子 / 最新因子）**很容易凭直觉写错** —— 写成除以首日因子、
// 或者忘记除、或者因子取反，都能产出「看起来像股价」的数字，单看结果发现不了。
// 所以这里钉两类断言：
//   1. 绝对值：直接拿 Tushare 官方文档给出的算例比对（防口径记错）；
//   2. 边界：缺失因子怎么办、没有因子怎么办（防「拿不复权价冒充复权价」）。

func qfqTestClient() *TushareClient {
	return &TushareClient{logger: zerolog.Nop()}
}

// dailyItem 按 FetchDailyOHLCV 请求的字段顺序构造一行：
// ts_code, trade_date, open, high, low, close, vol, amount
func dailyItem(tradeDate string, open, high, low, close, vol, amount float64) []any {
	return []any{"600519.SH", tradeDate, open, high, low, close, vol, amount}
}

func dailyResp(items ...[]any) *TushareResponse {
	return &TushareResponse{
		Data: TushareData{
			Fields: []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"},
			Items:  items,
		},
	}
}

func almost(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.005 {
		t.Errorf("%s = %.4f, want %.4f (官方算例)", what, got, want)
	}
}

// TestNormalizeDailyOHLCV_MatchesOfficialExample 用 Tushare 官方 `adj_factor`
// 接口文档「示例4：利用复权因子计算前复权和后复权价格」的原始数据做绝对值断言。
//
// 官方给出的那一行（贵州茅台 600519.SH，2020-01-02）：
//
//	raw:  open=1128.00 high=1145.06 low=1116.00 close=1130.00
//	factor(当日)=7.3186   factor(最新, 2026-07-13)=8.6463
//	官方算出: open_qfq=954.79 high_qfq=969.23 low_qfq=944.63 close_qfq=956.48
//
// 用官方数字当基准，是为了让「口径写错了」立刻变红，而不是产出一份自洽的错误。
func TestNormalizeDailyOHLCV_MatchesOfficialExample(t *testing.T) {
	resp := dailyResp(
		dailyItem("20200102", 1128.00, 1145.06, 1116.00, 1130.00, 25271.64, 2856061.0),
		dailyItem("20260713", 1197.12, 1215.00, 1190.19, 1210.99, 12000.0, 1400000.0),
	)
	factors := map[string]float64{
		"20200102": 7.3186,
		"20260713": 8.6463,
	}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}

	first := got[0]
	almost(t, "open_qfq", first.Open, 954.79)
	almost(t, "high_qfq", first.High, 969.23)
	almost(t, "low_qfq", first.Low, 944.63)
	almost(t, "close_qfq", first.Close, 956.48)

	// 基准日（区间内最新交易日）的 qfq 价格应当等于原始价 —— scale 恒为 1。
	// 这一条同时钉住「除以最新因子」而不是「除以首日因子」：若写反，这里会不等于原值。
	last := got[1]
	almost(t, "latest open_qfq", last.Open, 1197.12)
	almost(t, "latest close_qfq", last.Close, 1210.99)
}

// TestNormalizeDailyOHLCV_VolumeNotAdjusted 成交量不参与**复权**，但要做**单位换算**。
// tushare 的 vol/amount 本来就是原始成交量，前复权不改它 —— 如果顺手乘了 scale，
// 换手率、成交额类因子会整体偏小，而且偏得没有规律（越早越小）。
//
// 单位换算另说：tushare 的 vol 单位是「手」，规范层 schema 声明的 volume
// 单位是「股」（docs/SPEC.md「规范层 Schema」节），故 ×sharesPerLot。
func TestNormalizeDailyOHLCV_VolumeNotAdjusted(t *testing.T) {
	resp := dailyResp(dailyItem("20200102", 1128.00, 1145.06, 1116.00, 1130.00, 25271.64, 2856061.0))
	factors := map[string]float64{"20200102": 7.3186}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	// 25271.64 手 → 2527164 股：不复权（不乘 scale），但要换算单位。
	almost(t, "volume_shares", got[0].Volume, 2527164.0)
	// amount 单位是千元，规范层 turnover 单位是 CNY ⇒ ×1000（见
	// TestNormalizeDailyOHLCV_TurnoverThousandCNYToCNY）。同样不参与复权。
	almost(t, "turnover_cny", got[0].Turnover, 2856061.0*1000)
}

// TestNormalizeDailyOHLCV_TurnoverThousandCNYToCNY 成交额单位：源侧千元 → 规范层 CNY。
//
// 实测依据（2026-10-15，真库 ohlcv_daily_qfq 392.6 万行全量，非引用文档）：
//
//	turnover / (volume_股 × close) 的中位数 = 0.0010127（p01 0.000967、p99 0.001955）
//	⇒ 库里的 turnover 比「元」小三个数量级，即 tushare 的千元口径原样入库了。
//	单行核对：600519.SH 2026-09-24 close=1237、volume=3123935 股、
//	turnover=3867310.92（千元）→ ×1000 = 38.67 亿元 ≈ vwap × 股数。
//
// 而规范层声明的一直是 CNY（docs/SPEC.md「规范层 Schema」的 Turnover 行、
// pkg/domain/market/types.go），故这里补上 ×1000 —— 与 DATA-04 的 volume 同型：
// 契约早就写对了，是实现漏了一步。
//
// **不取整**（与 volume 相反）：真库实测 ×1000 后有 **199,240 行**偏离整数元超过
// 1e-3（其中 5,015 行正好 0.5 元）—— 那是源侧 amount 带 4 位小数、元以下的**真实
// 精度**，取整会把它抹掉；只有 61,699 行属于 <1e-6 的浮点噪声。而下游没有任何整数
// 纪律要求它 —— NT 导出的 SELECT 根本不带 turnover（nt-sidecar/tools/export_bars.py:63）。
// DATA-04 的 volume 必须取整是因为 as_quantity 精确判整数，这里没有这个约束。
func TestNormalizeDailyOHLCV_TurnoverThousandCNYToCNY(t *testing.T) {
	resp := dailyResp(dailyItem("20260924", 1240.0, 1255.0, 1231.0, 1237.0, 31239.35, 3867310.92))
	factors := map[string]float64{"20260924": 1.0}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0].Turnover != 3867310920.0 {
		t.Errorf("turnover = %.6f, want 3867310920（千元 3867310.92 × 1000 = 元）", got[0].Turnover)
	}
	// 反证腿：换算必须真的发生 —— 与源侧原值相等就是「千元直接当元存」的旧行为。
	if got[0].Turnover == 3867310.92 {
		t.Error("turnover 仍是源侧千元原值 ⇒ 单位换算没生效")
	}
	// 复权因子不参与成交额：两行时首行 scale≈0.846（≠1），若顺手乘了 scale，
	// 越早的成交额被压得越厉害 —— 与 volume 同一条纪律。
	respAdj := dailyResp(
		dailyItem("20200102", 1128.00, 1145.06, 1116.00, 1130.00, 25271.64, 2856061.0),
		dailyItem("20260713", 1197.12, 1215.00, 1190.19, 1210.99, 12000.0, 1400000.0),
	)
	adjusted := qfqTestClient().normalizeDailyOHLCV(respAdj, "600519.SH", map[string]float64{
		"20200102": 7.3186,
		"20260713": 8.6463,
	})
	if len(adjusted) != 2 {
		t.Fatalf("got %d records, want 2", len(adjusted))
	}
	if adjusted[0].Turnover != 2856061.0*1000 {
		t.Errorf("首行 turnover = %.6f, want %.6f（千元×1000，且不乘 scale=%.6f）",
			adjusted[0].Turnover, 2856061.0*1000, 7.3186/8.6463)
	}
}

// TestNormalizeDailyOHLCV_VolumeLotsToSharesRounds 手→股必须取整。
//
// 真库实测：392.6 万行里有 **37 万行（约 9.5%）**的 volume×100 在 float64 下
// **不是精确整数**（最大误差 4.77e-07）。不 round 的话，存进 double precision
// 的值形如 4152457645.0000005，NT 侧 as_quantity 的 is_integer() 是**精确**
// 判断，会把这批数据全判成「不是整数」→ 整批损坏 → 导出路径整条断。
//
// 下面两个用例取自真库误差榜前二（一个向上偏、一个向下偏）。
func TestNormalizeDailyOHLCV_VolumeLotsToSharesRounds(t *testing.T) {
	cases := []struct {
		name string
		lots float64
	}{
		{"rounds_up", 41524576.45},   // ×100 = 4152457645.0000005（向上偏）
		{"rounds_down", 40031647.16}, // ×100 = 4003164715.9999995（向下偏）
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 用例自证：这个值 ×100 必须**确实**不精确，否则本测试测不到取整，
			// 会悄悄退化成假护栏（改坏 round 也不红）。
			if tc.lots*sharesPerLot == math.Round(tc.lots*sharesPerLot) {
				t.Fatalf("用例失效：%v×100 恰好精确，检验不到取整 —— 换一个误差榜上的值", tc.lots)
			}

			resp := dailyResp(dailyItem("20200102", 100.0, 101.0, 99.0, 100.5, tc.lots, 1.0))
			factors := map[string]float64{"20200102": 1.0}

			got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
			if len(got) != 1 {
				t.Fatalf("got %d records, want 1", len(got))
			}
			want := math.Round(tc.lots * sharesPerLot)
			if got[0].Volume != want {
				t.Fatalf("volume = %v, want %v（未取整）", got[0].Volume, want)
			}
			if got[0].Volume != float64(int64(got[0].Volume)) {
				t.Fatalf("volume = %v 不是整数 —— NT 侧 as_quantity 会把整批数据判成损坏",
					got[0].Volume)
			}
		})
	}
}

// TestNormalizeDailyOHLCV_MissingFactorSkipsRow 某天取不到因子时**跳过**该行，
// 绝不补 1.0。补 1.0 等于宣称「这天没除权」，在除权日附近会造出假跳空，
// 而回测会把那个跳空当成真信号 —— 这正是 P2-10 那条坑的形状（缺失值折成默认值
// = 编数据，而且编出来的数据会主动把人引向错误的交易）。
func TestNormalizeDailyOHLCV_MissingFactorSkipsRow(t *testing.T) {
	resp := dailyResp(
		dailyItem("20260102", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
		dailyItem("20260103", 100.0, 101.0, 99.0, 100.5, 1000, 100000), // 这天没有因子
		dailyItem("20260104", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
	)
	factors := map[string]float64{
		"20260102": 10.0,
		"20260104": 12.0, // 基准（最新）
	}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (the factor-less day must be dropped)", len(got))
	}
	for _, r := range got {
		if r.Date.Format("20060102") == "20260103" {
			t.Fatal("20260103 没有复权因子却进了库 —— 那一行是不复权价冒充的 qfq")
		}
	}
	// 留下来的那行必须真的被换算过（不是原值）：100 × 10 / 12 = 83.33
	almost(t, "open_qfq", got[0].Open, 83.333)
}

// TestNormalizeDailyOHLCV_NoFactorsReturnsNothing 整个区间都没有因子时返回空，
// 而不是把不复权价格原样写进 ohlcv_daily_qfq。
//
// 这张表的名字里就写着 qfq —— 把不复权价存进去是**对真值的反面**，而且极难发现：
// 回测照跑、曲线照画，只是每个价格都是错的（除权日会有一堆假跳空）。
func TestNormalizeDailyOHLCV_NoFactorsReturnsNothing(t *testing.T) {
	resp := dailyResp(dailyItem("20260102", 100.0, 101.0, 99.0, 100.5, 1000, 100000))

	if got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", nil); len(got) != 0 {
		t.Fatalf("no factors: got %d records, want 0 (must not store unadjusted prices as qfq)", len(got))
	}
	if got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", map[string]float64{}); len(got) != 0 {
		t.Fatalf("empty factors: got %d records, want 0", len(got))
	}
}

// TestNormalizeDailyOHLCV_NonPositiveFactorSkipped 因子 <= 0 是脏数据，同样跳过
// （它不可能是合法的复权因子；放进去会把价格乘成 0 或负数）。
func TestNormalizeDailyOHLCV_NonPositiveFactorSkipped(t *testing.T) {
	resp := dailyResp(
		dailyItem("20260102", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
		dailyItem("20260104", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
	)
	factors := map[string]float64{
		"20260102": 0,     // 脏：非正
		"20260104": -12.0, // 脏：负数，且它会被当成“最新”
	}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 0 {
		t.Fatalf("got %d records, want 0 (non-positive factors are unusable)", len(got))
	}
}
