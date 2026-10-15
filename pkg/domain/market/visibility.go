package market

import (
	"fmt"
	"time"
)

// 规范层时间语义（可见时间点，point-in-time）——ADR-031 蓝图 §4.2 红线：
// 每条数据必须自带「什么时候才可见」，否则即数据泄漏，且极难事后发现
// （回测曲线照样漂亮）。本文件是 Go 侧口径的唯一真相。

// AsiaShanghai 是 A 股交易时区（UTC+8 固定偏移）。
//
// 用 FixedZone 而不是 time.LoadLocation：中国 1991 年后无夏令时，固定
// 偏移语义等价，且不依赖运行时 zoneinfo——Windows 开发机与精简 CI 容器
// 默认没有 tzdata，LoadLocation("Asia/Shanghai") 会直接失败。
var AsiaShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

// DailyVisibleAt 返回日线 bar 的可见时间点：交易日 15:00（收盘，
// Asia/Shanghai）。
//
// 入参按 UTC 零点解释的交易日（PG DATE 经 pgx 扫进 time.Time 的形态）——
// 只取它的日历日期分量，时段与位置另建。实际 Tushare 日更数据落库更晚，
// 15:00 是「收盘后可见」的规范口径（保守：不早于真实可见时刻）。
func DailyVisibleAt(tradeDate time.Time) time.Time {
	y, m, d := tradeDate.Date()
	return time.Date(y, m, d, 15, 0, 0, 0, AsiaShanghai)
}

// IntradayVisibleAt 返回日内 bar 的可见时间点 = **bar 周期结束时刻**。
//
// 1 分钟 bar（周期 1 分钟） stamped 10:00 → 10:01 可见；15m bucket
// stamped 09:30（周期 15 分钟）→ 09:45 可见。公式与蓝图 §4.3 的
// 「10:00 的 1 分钟线 10:01 才可见」逐字同构：可见 = 周期结束，不提前。
func IntradayVisibleAt(barStart time.Time, minutes int) time.Time {
	return barStart.Add(time.Duration(minutes) * time.Minute)
}

// VisibleBy 是分钟粒度 PIT 闸门的判定原语（D6）：bar 在 asOf 时刻是否
// **已可见**。口径 = 记录自带的 VisibleAt 不晚于 asOf —— 10:00 的 1m bar
// （VisibleAt = 10:01）在 10:00:30 不可见、10:01 可见。
//
// 为什么是方法不是工具函数：闸门判据必须与 VisibleAt 的填充口径
// （DailyVisibleAt / IntradayVisibleAt）同文件同语义，拆到别的文件，
// 「谁算可见、谁判可见」就会漂移。D6 的日内特征 provider 与闸门测试
// 都经本方法裁决，改错即红（D6 验收①）。
//
// 零值 VisibleAt（未声明）恒判不可见 —— 未声明的可见性是泄漏，不是
// 「永远可见」。
func (b OHLCV) VisibleBy(asOf time.Time) bool {
	return !b.VisibleAt.IsZero() && !b.VisibleAt.After(asOf)
}

// TimeSemantics 是 connector 注册契约（D2，蓝图 §4.2）的「时间语义」枚举：
// 该数据源的数据「什么时候才可见」。本文件上面的两个函数回答「怎么算」，
// 本枚举回答「connector 必须显式声明哪一类」——把防前视从注释提升为
// 可校验的注册契约字段。
//
// 字面量契约（跨语言）：nt-sidecar 翻译层按同名字面量对齐，防漂移测试钉住
// 词表（与 Frequency 词表同款纪律：扩展词表 = 常量 + 校验 + SPEC 三件套同步）。
type TimeSemantics string

const (
	// VisibleOnAnnouncementDay 按公告日可见：披露日早于等于回测日期才可用
	// （财报 / 公告 / 宏观发布等披露驱动数据）。
	VisibleOnAnnouncementDay TimeSemantics = "announcement_day"
	// VisibleAfterClose 收盘后可见：交易日 15:00 后（日线 bar、资金流、
	// 龙虎榜等日更数据）。保守口径，不早于真实可见时刻。
	VisibleAfterClose TimeSemantics = "after_close"
	// VisibleNextOpen 次日开盘可见：下一个交易日开盘（保守口径，用于
	// 收盘后仍无法及时取得、最早要等次日开盘的数据）。
	VisibleNextOpen TimeSemantics = "next_open"
	// VisibleCustom 自定义时点：必须随声明给出判定表达（VisibleExpr），
	// 例如实时快照 =「观察时刻即可见」、分钟 bar =「周期结束即可见」。
	VisibleCustom TimeSemantics = "custom"
)

// knownTimeSemantics 是当前支持的时间语义词表。扩展必须同步校验与 SPEC。
var knownTimeSemantics = []TimeSemantics{
	VisibleOnAnnouncementDay,
	VisibleAfterClose,
	VisibleNextOpen,
	VisibleCustom,
}

// ParseTimeSemantics 解析时间语义字面量；未知值返回错误（fail-loud，不静默
// 回退到某个"默认"语义——时间语义的"默认"就是数据泄漏）。
func ParseTimeSemantics(s string) (TimeSemantics, error) {
	t := TimeSemantics(s)
	if !t.Valid() {
		return "", fmt.Errorf("market: unknown time semantics %q (known: announcement_day/after_close/next_open/custom)", s)
	}
	return t, nil
}

// Valid 报告语义是否在词表内。零值 "" 是**未声明**，不算合法语义。
func (t TimeSemantics) Valid() bool {
	for _, k := range knownTimeSemantics {
		if t == k {
			return true
		}
	}
	return false
}

// RequiresExpression 报告该语义是否必须附判定表达（仅自定义时点需要）。
func (t TimeSemantics) RequiresExpression() bool { return t == VisibleCustom }

// String 让 TimeSemantics 可直接进日志/错误信息。
func (t TimeSemantics) String() string { return string(t) }

// KnownTimeSemantics 返回时间语义词表的拷贝（防调用方改动内部状态）。
func KnownTimeSemantics() []TimeSemantics {
	out := make([]TimeSemantics, len(knownTimeSemantics))
	copy(out, knownTimeSemantics)
	return out
}
