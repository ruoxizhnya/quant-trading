package domain

import (
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// FactorCaliber 是因子产出口径（ADR-031 蓝图 §5.2）——苹果橘子不可加权
// 混用：z-score 是截面标准化分，rank 是百分位，raw 是原始值，三者量纲
// 不同，混在一支组合里加权即造假。现有插件「默认全 z-score」的隐含假设
// 在此显式化。
type FactorCaliber string

const (
	// CaliberZScore 截面 z-score 标准化分。
	CaliberZScore FactorCaliber = "z-score"
	// CaliberRank 截面百分位（0~1 或 0~100，生产方声明）。
	CaliberRank FactorCaliber = "rank"
	// CaliberRaw 原始值（未标准化）。
	CaliberRaw FactorCaliber = "raw"
)

// FactorOutput 是规范层**因子产出 schema**（蓝图 §5.2「所有因子一个样」，
// 策略看到的那一半）。D1 冻结字段形状；因子定义的入库/版本指纹属 D3，
// 消费属 D6/D8——形状先冻结，后面不许每因子发明一种 schema。
//
// 与 market.OHLCV / market.Fundamental 并列，构成规范层三类字段
// （docs/SPEC.md 规范层 Schema 节，防漂移测试钉住）。
type FactorOutput struct {
	// Factor 因子名。命名空间与 domain.FactorType 冻结清单同域
	//（D3 起改库管理 + fail-closed，名册语义不放松）。
	Factor string `json:"factor"`
	// Date 分数对应日（按日分数序列）。
	Date time.Time `json:"date"`
	// Symbol 股票代码（如 "000001.SZ"）。
	Symbol string `json:"symbol"`
	// Value 分数。**nil = 缺失**（缺失≠0：0 是有效分数，「没有」是
	// 数据状态，两者不可互相顶包——指针 nil 即「没有」，用前显式判空）。
	Value *float64 `json:"value"`
	// Caliber 口径标签，随分数走。
	Caliber FactorCaliber `json:"caliber"`
	// Frequency 产出序列频率。因子 = 按日分数序列（蓝图 §5.1），当前
	// 恒为 market.Freq1d；D6 引入日内特征时扩展。
	Frequency market.Frequency `json:"frequency"`
	// VisibleAt 可见时间点（PIT 语义）：该分数最早可被回测看见的时刻，
	// 由其输入数据（引用 D1 规范层字段）的可见性推导——输入未齐 =
	// 分数不可见，缺输入那天的 Value 为 nil。
	VisibleAt time.Time `json:"visible_at"`
}
