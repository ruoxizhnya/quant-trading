package main

// AUD-59 的护栏：交易日历同步的「请求区间」与「取回来的区间」必须对得上。
//
// 原缺陷的形状（实测，2026-09-25）：
//
//	POST /api/sync/jobs {"type":"calendar",
//	                     "params":{"start_date":"2022-01-01","end_date":"2026-09-25"}}
//	→ status: completed, count: 2922, **零错误零告警**
//	→ 库里却只有 2022-01-01 ~ 2025-12-31，还少 3 个交易日
//
// 根因是「校验器承诺的格式集合 ⊋ 消费者实际支持的格式集合」：handler 的
// validDateRange 与它给出的报错文案都明说 `must be YYYYMMDD or YYYY-MM-DD`，
// 而执行器按 8 位定长硬切 `[:4]`/`[4:6]`/`[6:8]` —— `2022-01-01` 变成 `2022--01-`，
// Tushare 视作非法日期、回落到它自己的默认区间。
//
// **本文件的存在意义是「让下一次再犯时有人喊」**，所以每条断言都配一条对照腿
// （容差内的正当偏差必须放行）—— 一个恒红的检查会被人直接删掉，
// 那和没有检查是一样的。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/sync"
)

// dailyCalendar 造出 from..to（含两端）的**逐日**日历条目 —— 与 `trade_cal`
// 的真实形状一致（它返回自然日，含休市日，不只是交易日）。
func dailyCalendar(t *testing.T, from, to string) []storage.TradingCalendarEntry {
	t.Helper()
	lo, err := time.Parse("2006-01-02", from)
	require.NoError(t, err)
	hi, err := time.Parse("2006-01-02", to)
	require.NoError(t, err)

	var out []storage.TradingCalendarEntry
	for d := lo; !d.After(hi); d = d.AddDate(0, 0, 1) {
		out = append(out, storage.TradingCalendarEntry{TradeDate: d, Exchange: "SSE", IsTradingDay: d.Weekday() != time.Saturday && d.Weekday() != time.Sunday})
	}
	return out
}

func TestParseSyncDate_AcceptsBothDocumentedFormats(t *testing.T) {
	want := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"YYYYMMDD", "20230101"},
		{"YYYY-MM-DD", "2023-01-01"},
		{"带首尾空格", " 2023-01-01 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSyncDate(tc.raw)
			require.NoError(t, err, "handler 的报错文案承诺收这两种格式，就必须真收")
			assert.True(t, want.Equal(got), "parsed %s, want %s", got, want)
		})
	}

	// 反证腿：真正畸形的串必须被拒绝。前两条是「按 8 位定长硬切」会造出来的
	// 形状 —— 如果哪天有人把那个写法塞回来，这两条会先替他喊。
	for _, bad := range []string{"", "   ", "2022--01-", "2023--01-", "2023/01/01", "20230101x", "202301011"} {
		t.Run("reject:"+bad, func(t *testing.T) {
			if _, err := parseSyncDate(bad); err == nil {
				t.Fatalf("parseSyncDate(%q) 应当报错，却解析成功了", bad)
			}
		})
	}
}

// TestCalendarFetchArgs_DerivedFromTheParsedTime 是 AUD-59 的**根因断言**。
//
// 取数参数必须从**解析后的时间**格式出来，不能在原始字符串上切片。
// 用「带首尾空格的合法日期」把这条性质钉死：原始串显然不是 `20230101`，
// 但只要参数由解析结果格式化，取数参数就必然被规范化为 `20230101`。
func TestCalendarFetchArgs_DerivedFromTheParsedTime(t *testing.T) {
	start, end, startArg, endArg, err := calendarFetchArgs(sync.CalendarSyncParams{
		StartDate: " 2023-01-01 ",
		EndDate:   "2026-09-25",
	})
	require.NoError(t, err)

	assert.Equal(t, "20230101", startArg)
	assert.Equal(t, "20260925", endArg)
	assert.True(t, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Equal(start))
	assert.True(t, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC).Equal(end))
}

func TestCalendarFetchArgs_BothFormatsGiveTheSameWindow(t *testing.T) {
	_, _, compactStart, compactEnd, err := calendarFetchArgs(sync.CalendarSyncParams{
		StartDate: "20230925", EndDate: "20260925",
	})
	require.NoError(t, err)

	_, _, dashedStart, dashedEnd, err := calendarFetchArgs(sync.CalendarSyncParams{
		StartDate: "2023-09-25", EndDate: "2026-09-25",
	})
	require.NoError(t, err)

	assert.Equal(t, compactStart, dashedStart,
		"两种格式必须解析到同一个窗口 —— 契约承诺了两种都收")
	assert.Equal(t, compactEnd, dashedEnd)
}

func TestCalendarFetchArgs_RejectsUnusableInput(t *testing.T) {
	for _, tc := range []struct {
		name          string
		start, end    string
		wantErrSubstr string
	}{
		{"start 是硬切产物", "2022--01-", "20260925", "start_date"},
		{"end 是硬切产物", "20220101", "2026--09-", "end_date"},
		{"end 早于 start", "2026-09-25", "2022-01-01", "before start_date"},
		{"两个都为空", "", "", "start_date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := calendarFetchArgs(sync.CalendarSyncParams{StartDate: tc.start, EndDate: tc.end})
			require.Error(t, err, "解析不出窗口就必须失败，绝不能带着垃圾日期去取数")
			assert.Contains(t, err.Error(), tc.wantErrSubstr)
		})
	}
}

// TestValidateCalendarCoverage_ReplaysTheAUD59Shape 用**实测的故障数字**回放。
func TestValidateCalendarCoverage_ReplaysTheAUD59Shape(t *testing.T) {
	reqStart := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	reqEnd := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

	t.Run("实测形状：请求到 2026-09-25，只回来到 2025-12-31", func(t *testing.T) {
		entries := dailyCalendar(t, "2022-01-01", "2025-12-31") // 1461 条，正好是当时落库的区间
		err := validateCalendarCoverage("SSE", reqStart, reqEnd, entries)
		require.Error(t, err, "这正是 AUD-59：作业报 completed、count 2922，库里却是错的区间")
		assert.Contains(t, err.Error(), "2025-12-31")
		assert.Contains(t, err.Error(), "2026-09-25")
	})

	t.Run("头部被截：请求从 2023-01-01 起，却从 2023-02-01 才开始", func(t *testing.T) {
		entries := dailyCalendar(t, "2023-02-01", "2026-09-25")
		require.Error(t, validateCalendarCoverage("SSE", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), reqEnd, entries))
	})

	t.Run("空结果也必须报错，而不是安静地写 0 行", func(t *testing.T) {
		err := validateCalendarCoverage("SZSE", reqStart, reqEnd, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no calendar entries")
	})

	// 对照腿：容差内的正当偏差必须放行。没有这条，上面那些断言可能只是
	// 「这个函数恒报错」—— 那就又成了假护栏。
	t.Run("对照腿：边界各差 5 天（容差 7 天内）必须放行", func(t *testing.T) {
		entries := dailyCalendar(t, "2023-01-06", "2026-09-20")
		require.NoError(t, validateCalendarCoverage("SSE",
			time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
			entries))
	})

	t.Run("对照腿：逐日对齐必须放行", func(t *testing.T) {
		require.NoError(t, validateCalendarCoverage("SSE", reqStart, reqEnd, dailyCalendar(t, "2022-01-01", "2026-09-25")))
	})
}
