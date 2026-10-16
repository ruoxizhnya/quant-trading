package domain

// 市场活跃度数据类（D5 新数据源样板，ADR-031 蓝图 §4.4）——
// market_activity 表的领域类型。
//
// 定位：**个股日频活跃度观测**（换手率 / 自由流通换手率 / 量比），
// 与 macro_series（国家维度宏观）、industry_vitality（行业维度景气）
// 并列不重复——本类型是个股日频的市场关注度截面。所有类型统一带
// Source + AsOf 两个溯源列，与 market_activity 的列一一对应；唯一键
// 即幂等键（同键重导刷新行而不插新行）。
//
// 时间语义（D2 注册契约，pkg/data/source/declarations.go）：
// **收盘后可见**（after_close）——换手率由当日全天的成交量除以股本
// 统计得出，收盘前不存在。蓝图 §4.2 点名的「高危 PIT 场景」第一个
// 真实考卷：盘中用当日全天活跃度即偷看。
//
// 缺失≠0：三个数值列全部为指针。nil = 上游未发布/未披露（停牌股
// 当日无换手），落库为 NULL、求值期为 NaN——绝不是有效分数 0。

import "time"

// MarketActivity 是一个个股日频活跃度观测（market_activity）。
//
// Symbol 为规范层代码（如 "600519.SH"）；TradeDate 为交易日（DATE，
// PIT 锚点——声明可见性即「该日收盘后」）；TurnoverRate 为换手率
// （%，总股本口径 = 成交量/总股本）；TurnoverRateF 为自由流通换手率
// （%，自由流通股本口径）；VolumeRatio 为量比（当日均量/过去 5 日均
// 量，无量纲）。三个数值列 nil = 缺失，不补 0。
type MarketActivity struct {
	Symbol        string    `json:"symbol"`
	TradeDate     time.Time `json:"trade_date"`
	TurnoverRate  *float64  `json:"turnover_rate"`
	TurnoverRateF *float64  `json:"turnover_rate_f"`
	VolumeRatio   *float64  `json:"volume_ratio"`
	Source        string    `json:"source"`
	AsOf          time.Time `json:"as_of"`
}
