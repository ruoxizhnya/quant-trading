// OBS-08 切片 2：字段「可用性」声明测试。
//
// 与能力层的分工（别混）：
//   - 能力层 = provider 代码认不认这个字段（切片 1 已做，被跨包护栏钉住）；
//   - 可用性层 = 那张表里当前有没有数据（本切片）。
//
// 「认」不等于「有」：实测 stock_fundamentals / stock_sector_map 均为 0 行，
// 于是 8 个字段语法合法、过闸门、provider 也认，但求值必然拿不到数据 ——
// AI 对着空数据静默产垃圾。
//
// 本文件两条腿：
//  1. 纯函数（fake probe）：可用性判定的语义与 fail-loud 行为；
//  2. 真库核对（真 probe）：判定结果必须与**真库行数**一致。
package expression

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// ─── 1. 纯函数（fake probe）─────────────────────────────────────────

// fakeProbe 按给定的「表 → 是否有数据」作答，并可按 failOn 注入探测失败。
func fakeProbe(data map[string]bool, failOn string) AvailabilityProbe {
	return func(_ context.Context, table string) (bool, error) {
		if table == failOn {
			return false, context.DeadlineExceeded
		}
		return data[table], nil
	}
}

func TestFieldAvailabilityReflectsProbe(t *testing.T) {
	avail, err := FieldAvailability(context.Background(), fakeProbe(map[string]bool{
		TableOHLCV: true, TableFundamentals: false, TableSectorMap: false,
	}, ""))
	require.NoError(t, err)

	// 行情有数据 ⇒ 6 个 market 字段可用。
	for _, f := range []string{"open", "high", "low", "close", "volume", "turnover"} {
		require.True(t, avail[f], "字段 %q 依赖 %s（有数据）应可用", f, TableOHLCV)
	}
	// 财报 0 行 ⇒ 7 个 fundamentals 字段不可用 —— 这正是 OBS-08 切片 2 要暴露的事实。
	for _, f := range []string{"pe", "pb", "ps", "roe", "roa", "revenue", "profit"} {
		require.False(t, avail[f], "字段 %q 依赖 %s（无数据）应不可用", f, TableFundamentals)
	}
	// sector 依赖 stock_sector_map（0 行）⇒ 不可用。
	require.False(t, avail["sector"], "sector 依赖 %s（无数据）应不可用", TableSectorMap)
}

func TestFieldAvailabilityAllTablesHaveData(t *testing.T) {
	avail, err := FieldAvailability(context.Background(), fakeProbe(map[string]bool{
		TableOHLCV: true, TableFundamentals: true, TableSectorMap: true,
	}, ""))
	require.NoError(t, err)
	for f, ok := range avail {
		require.True(t, ok, "全部表有数据时字段 %q 应可用", f)
	}
	require.Len(t, avail, len(fieldRegistry), "可用性地图应覆盖全部已登记字段")
}

// TestFieldAvailabilityProbeFailureIsFailLoud 探测失败≠「没数据」。库挂了若
// 被当成「字段不可用」，AI 会以为这几个字段本来就没数据 —— 静默降级比报错
// 危险得多（与 OBS-01「静默成功比失败危险」同源）。
func TestFieldAvailabilityProbeFailureIsFailLoud(t *testing.T) {
	_, err := FieldAvailability(context.Background(), fakeProbe(nil, TableFundamentals))
	require.Error(t, err, "探测失败必须 fail-loud，不得降级成「不可用」")
	require.Contains(t, err.Error(), TableFundamentals, "错误应点明是哪张表探测失败")
}

func TestFieldAvailabilityRejectsNilProbe(t *testing.T) {
	_, err := FieldAvailability(context.Background(), nil)
	require.Error(t, err, "probe 为 nil 必须报错（调用方漏注入）")
}

func TestAvailableDataFieldsWith(t *testing.T) {
	all := AvailableDataFields()

	// nil = 未做可用性过滤，语义是「能力层全集」，不是「全部可用」。
	require.Equal(t, all, AvailableDataFieldsWith(nil), "avail=nil 应等价于能力层全集")

	// 只留行情可用。
	avail, err := FieldAvailability(context.Background(), fakeProbe(map[string]bool{
		TableOHLCV: true, TableFundamentals: false, TableSectorMap: false,
	}, ""))
	require.NoError(t, err)
	got := AvailableDataFieldsWith(avail)
	require.Equal(t, []string{"close", "high", "low", "open", "turnover", "volume"}, got,
		"只应返回有数据的 6 个行情字段（升序）")
	for _, f := range got {
		require.NotContains(t, []string{"pe", "pb", "ps", "roe", "roa", "revenue", "profit"}, f,
			"零数据的基本面字段 %q 不应出现在可用清单里", f)
	}
}

func TestFieldsDependingOn(t *testing.T) {
	require.Equal(t, []string{"pb", "pe", "profit", "ps", "revenue", "roa", "roe"},
		FieldsDependingOn(TableFundamentals), "fundamentals 表应关联 7 个字段（升序）")
	require.Equal(t, []string{"sector"}, FieldsDependingOn(TableSectorMap))
	require.Empty(t, FieldsDependingOn("no_such_table"), "未知表应返回空")
}

// TestFieldDefsDependOnKnownTables 是注册表自洽护栏：DependsOn 必须是已声明
// 的表常量之一。拼错的表名会让探测永远返回「不可用」，且极难发现。
func TestFieldDefsDependOnKnownTables(t *testing.T) {
	known := map[string]bool{TableOHLCV: true, TableFundamentals: true, TableSectorMap: true}
	for f, def := range fieldRegistry {
		require.NotEmpty(t, def.DependsOn, "字段 %q 缺 DependsOn（无法判定可用性）", f)
		require.True(t, known[def.DependsOn], "字段 %q 的 DependsOn %q 不是已知表常量（拼写漂移）", f, def.DependsOn)
	}
}

// ─── 2. 真库核对（真 probe）─────────────────────────────────────────

// availTestDSN 与 pkg/strategy/state_store_test.go、docs/TEST.md §2.0.1 同源。
func availTestDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
}

// TestFieldAvailabilityMatchesRealDatabase 是真库核对：可用性判定必须与
// 真库行数一致（探测机制正确），并且把「当前哪些字段实际不可用」打进日志——
// 这正是 OBS-08 切片 2 要让 AI 看见的信息。
//
// 断言**不做**「某字段必须不可用」，只做「判定 ≡ 真库行数」：数据补上后
// 本测试仍绿（机制没坏），避免把「业务现状」钉成「不变式」造成假红。
func TestFieldAvailabilityMatchesRealDatabase(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, availTestDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败（本机起库见 docs/TEST.md §2.0.1）: %v", err)
	}
	defer pool.Close()

	// 真 probe：查表行数（只读）。
	probe := func(ctx context.Context, table string) (bool, error) {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		var n int64
		if err := pool.QueryRow(cctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return false, err
		}
		return n > 0, nil
	}

	avail, err := FieldAvailability(ctx, probe)
	require.NoError(t, err)

	// 逐表核对：判定结果 ≡ 真库行数 > 0。
	for _, table := range []string{TableOHLCV, TableFundamentals, TableSectorMap} {
		hasData, err := probe(ctx, table)
		require.NoError(t, err, "探测 %s 失败", table)
		for _, f := range FieldsDependingOn(table) {
			require.Equal(t, hasData, avail[f],
				"字段 %q 的可用性判定 (%v) 与表 %s 的真实行数 (%d 行 ⇒ %v) 不一致",
				f, avail[f], table, hasData, hasData)
		}
		t.Logf("表 %-20s 有数据=%v ⇒ 影响字段 %v", table, hasData, FieldsDependingOn(table))
	}
	t.Logf("当前真库可用字段（供 AI 生成表达式用）：%v", AvailableDataFieldsWith(avail))
}
