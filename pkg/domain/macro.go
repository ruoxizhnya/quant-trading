package domain

// 宏观数据层（P2-1，TASKS.md §五 遗留待裁决）—— quant.macro_series 的领域类型。
//
// 定位：国家层宏观指标观测（CPI / PPI / PMI / M2 / 社融 / LPR / 汇率），
// 与 industry_vitality（P2-2，行业景气）并列不重复——后者是行业维度，
// 本类型是国家维度。所有类型统一带 Source + AsOf 两个溯源列，与
// quant.macro_series 的列一一对应；唯一键即幂等键（同键重导不重复插行）。
//
// 边界：本层只做静态宏观数据的落库与只读查询。宏观因子消费方（策略 /
// 因子）另案，不在本任务范围。

import "time"

// MacroSeries 是一个宏观指标观测（quant.macro_series）。
//
// indicator 为指标代码（CPI_YOY / PPI_YOY / PMI / M2 / M2_YOY / TSF_INC /
// LPR_1Y / LPR_5Y / USDCNY_CLOSE，目录见 pkg/data/macro.go 的
// macroIndicatorSpecs）；freq 为频率（M=月 / D=日）；obs_date 为观察日
// （DATE，PIT——月频取当月首日，日频取自然日）；unit 为计量单位
// （% / 亿元 / 点 / 汇率）；唯一键 indicator+freq+obs_date+source
// （不同来源的同指标各行其行，同来源重导刷新取值）。
type MacroSeries struct {
	Indicator string    `json:"indicator"`
	Freq      string    `json:"freq"`
	ObsDate   time.Time `json:"obs_date"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Source    string    `json:"source"`
	AsOf      time.Time `json:"as_of"`
}
