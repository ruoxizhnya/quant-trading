package market

import "fmt"

// Frequency 是规范层 schema 的**记录自带频率字段**（ADR-031 蓝图 §4.1：
// 频率是数据自带字段，不靠来源表隐式区分——这是现状「domain.OHLCV 无
// 频率字段、靠表名区分」要补的第一刀）。
//
// 字面量契约（跨语言）：nt-sidecar `sidecar/mapping.py` 的 FREQ_1D 等常量
// 与本包常量逐字相同，翻译层测试钉住。
type Frequency string

const (
	// Freq1m 1 分钟 bar（ohlcv_minute，mootdx）。
	Freq1m Frequency = "1m"
	// Freq15m 15 分钟 bar（由 1m 聚合产出，不物化）。
	Freq15m Frequency = "15m"
	// Freq1d 日线 bar（ohlcv_daily_qfq，Tushare 前复权，唯一进回测的口径）。
	Freq1d Frequency = "1d"
)

// knownFrequencies 是当前规范层支持的频率词表。扩展频率 = 加常量 + 路由
// 分支 + 聚合口径（见 docs/SPEC.md 规范层 Schema 节），不是随手加字符串。
var knownFrequencies = []Frequency{Freq1m, Freq15m, Freq1d}

// ParseFrequency 解析频率字面量；未知频率返回错误（fail-loud，不静默
// 回退到某个"默认"频率——回测里"默认"就是泄漏）。
func ParseFrequency(s string) (Frequency, error) {
	f := Frequency(s)
	if !f.Valid() {
		return "", fmt.Errorf("market: unknown frequency %q (known: 1m/15m/1d)", s)
	}
	return f, nil
}

// Valid 报告频率是否在规范层词表内。零值 "" 是**未声明**（遗留取数路径
// 不填该字段），不算合法频率。
func (f Frequency) Valid() bool {
	for _, k := range knownFrequencies {
		if f == k {
			return true
		}
	}
	return false
}

// String 让 Frequency 可直接进日志/错误信息。
func (f Frequency) String() string { return string(f) }

// IsIntraday 区分日内频率（1m/15m）与日频（1d）。
func (f Frequency) IsIntraday() bool {
	return f == Freq1m || f == Freq15m
}

// IntradayMinutes 返回日内频率的分钟数；日频或未声明返回 (0, false)。
func (f Frequency) IntradayMinutes() (int, bool) {
	switch f {
	case Freq1m:
		return 1, true
	case Freq15m:
		return 15, true
	default:
		return 0, false
	}
}

// KnownFrequencies 返回规范层频率词表的拷贝（防调用方改动内部状态）。
//
// 存在理由：词表扩展必须「常量 + 路由分支 + 聚合口径 + SPEC 词表」四件套
// 同步（见文件顶注释），防漂移测试与消费方（频率枚举 / 校验）通过它比对
// SPEC——只读这里，不各自维护一份会漂移的清单。
func KnownFrequencies() []Frequency {
	out := make([]Frequency, len(knownFrequencies))
	copy(out, knownFrequencies)
	return out
}

// FrequencySpec 是 connector 注册契约（D2，蓝图 §4.2）的「频率口径」枚举：
// 该数据源以什么节奏产出数据。它与记录自带的 Frequency 是两个正交的轴——
// FrequencySpec 声明「这个源是什么口径」，Frequency 标注「这条记录是什么
// 频率」（例如 mootdx 源口径为 intraday，产出记录频率为 1m）。
//
// 日内数据如何聚合成日频引用 D1 聚合规则（见 docs/SPEC.md 规范层 Schema 节）。
type FrequencySpec string

const (
	// FreqSpecDaily 日频：每日一条（日线 bar、资金流、龙虎榜等）。
	FreqSpecDaily FrequencySpec = "daily"
	// FreqSpecIntraday 分钟频：日内多条 bar / 快照（1m bar、实时行情）。
	FreqSpecIntraday FrequencySpec = "intraday"
	// FreqSpecEvent 事件流：非等间隔的披露 / 发布驱动记录（财报、公告、
	// 宏观发布、新闻）。事件流的口径由记录自带的可见时间锚点承载。
	FreqSpecEvent FrequencySpec = "event"
)

// knownFrequencySpecs 是当前支持的连接器频率口径词表。
var knownFrequencySpecs = []FrequencySpec{FreqSpecDaily, FreqSpecIntraday, FreqSpecEvent}

// ParseFrequencySpec 解析频率口径字面量；未知值返回错误（fail-loud）。
func ParseFrequencySpec(s string) (FrequencySpec, error) {
	f := FrequencySpec(s)
	if !f.Valid() {
		return "", fmt.Errorf("market: unknown frequency spec %q (known: daily/intraday/event)", s)
	}
	return f, nil
}

// Valid 报告频率口径是否在词表内。零值 "" 是**未声明**，不算合法口径。
func (f FrequencySpec) Valid() bool {
	for _, k := range knownFrequencySpecs {
		if f == k {
			return true
		}
	}
	return false
}

// String 让 FrequencySpec 可直接进日志/错误信息。
func (f FrequencySpec) String() string { return string(f) }

// KnownFrequencySpecs 返回频率口径词表的拷贝（防调用方改动内部状态）。
func KnownFrequencySpecs() []FrequencySpec {
	out := make([]FrequencySpec, len(knownFrequencySpecs))
	copy(out, knownFrequencySpecs)
	return out
}
