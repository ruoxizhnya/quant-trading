package storage

// DATA-02: 票池状态拆分的只读聚合。
//
// 这份读数会是 SPA 幸存者偏差块的唯一真相，所以钉三条：
//  1. 分组值必须逐值等于「按 status 过滤再数」的另一条路径 —— 工单 ③ 的口径是
//     `SELECT status, count(*) FROM stocks GROUP BY status`，这里用 WHERE 做交叉
//     核对，避免拿同一条 GROUP BY 自证。
//  2. status 为 NULL 的行（列可空：DDL 只有 DEFAULT 'active'，没有 NOT NULL）既不能
//     把读数打成 500，也不能被静默丢掉 —— 丢一行等于凭空少一只票。
//  3. 改一行状态，读数随之移动 —— 这也是工单 ⑤ 破坏验证在 CI 里的那条腿：
//     读数若是硬编码，这条直接红。
//
// 断言一律用**增量**而不是绝对值：这是开发库，5908 行随时可能被并发同步改动，
// 绝对值断言会把它变成偶发红。缺档读零值即可（Go 的 map 缺键返回 0）。

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// splitProbeSeq 取纳秒后 8 位：stocks.symbol 是 VARCHAR(20)，探针名必须短，
// 而 8 位 + 进程内不同标签足够避开并发运行（其它 agent 同时在跑本包测试）的撞键。
var splitProbeSeq = time.Now().UnixNano() % 100000000

// splitProbe 给每只探针股票一个本次运行独有的 symbol：既撞不上真实股票代码
// （D2P + 8 位数字与 A 股编码形状不同），也让清理精确只删自己插的那些。
// tag 单字母：N=null status，E=empty status，M=moved status。
func splitProbe(tag string) string {
	return fmt.Sprintf("D2P%08d%s.SH", splitProbeSeq, tag[:1])
}

// seedProbeStock 直接写 status 列插一行探针股票并登记删除。
// 不走 SaveStock —— 结构体那条路径表达不出「status 为 NULL」这个形状。
//
// 删除走 t.Cleanup 而不是测试体末尾的 inline DELETE：断言失败时 inline 那行会被
// 跳过，探针行就永久留在开发库里（下次读数凭空多一只票）。调用方因此必须
// **先**登记 store.Close —— cleanup 是 LIFO，后登记的删除才会跑在关池之前。
func seedProbeStock(t *testing.T, store *PostgresStore, ctx context.Context, symbol string, status *string) {
	t.Helper()
	_, err := store.DB().Exec(ctx,
		`INSERT INTO stocks (symbol, name, exchange, status) VALUES ($1, $2, $3, $4)`,
		symbol, "DATA-02 探针", "SSE", status)
	require.NoError(t, err, "insert probe stock %s", symbol)
	t.Cleanup(func() {
		if _, derr := store.DB().Exec(context.Background(),
			`DELETE FROM stocks WHERE symbol=$1`, symbol); derr != nil {
			t.Errorf("cleanup probe %s failed: %v", symbol, derr)
		}
	})
}

// TestStockStatusCounts_MatchesRawGroupByFoldedInGo 是工单 ③ 口径的机械化断言。
//
// oracle 取的是工单原话那条查询 `SELECT status, COUNT(*) FROM stocks GROUP BY status`
// —— **逐字不折**，折叠规则由测试侧在 Go 里再实现一遍：生产码在 SQL 里折、这里在 Go
// 里折，两条独立实现对不上就是缺陷。
//
// ⚠️ 分工要写清，别把这条当成什么都能抓：**GROUP BY 表达式陷阱不由这条守**。
// 2026-10-15 实测把生产分组改成原始列 `GROUP BY status`，本条照样绿 —— 库里当前
// 没有 NULL 或空串行时，折与不折结果相同。那条陷阱（NULL 与空串出成两行同名 unknown、
// map 互相覆盖少一只票）由 TestStockStatusCounts_FoldsNullAndEmptyIntoOneUnknownBucket
// 守：它自己造那两行，改坏时红在 `expected: 2, actual: 1`。
//
// 本条只读两趟（读数 + oracle），不是 N+1 条 WHERE —— 开发库里并发同步与并行测试
// 会话都在写 stocks，每多一趟读就多一个「两次读数之间被人改过」的假红窗口。
func TestStockStatusCounts_MatchesRawGroupByFoldedInGo(t *testing.T) {
	store := testStore(t)
	// 用 t.Cleanup 而不是 defer：见 seedProbeStock 的 LIFO 说明 —— 探针删除必须
	// 跑在关池之前，defer 会让删除拿到 closed pool。
	t.Cleanup(store.Close)
	ctx := context.Background()

	got, err := store.StockStatusCounts(ctx)
	require.NoError(t, err)

	rows, err := store.DB().Query(ctx, `SELECT status, COUNT(*) FROM stocks GROUP BY status`)
	require.NoError(t, err)
	want := map[string]int64{}
	for rows.Next() {
		var status sql.NullString // 原始列可空，这里如实接
		var n int64
		require.NoError(t, rows.Scan(&status, &n))
		key := "unknown"
		if status.Valid && status.String != "" {
			key = status.String
		}
		want[key] += n
	}
	rows.Close()
	require.NoError(t, rows.Err())

	// 空票池 = 没有可比对象，不是缺陷（AUD-51 口径：前置读的必须就是断言读的东西
	// —— 这里两边都读 stocks）。不 skip 的话这条会变成**零次循环空过**，
	// 一个谁都没断言的绿比红更糟。
	if len(want) == 0 {
		t.Skip("stocks 表为空，逐值核对无从谈起（开发库需先灌票池）")
	}

	assert.Equal(t, want, got, "读数应与库里 group by status 的折叠结果逐键逐值一致")
}

// TestStockStatusCounts_FoldsNullAndEmptyIntoOneUnknownBucket：
// 同时插一行 status IS NULL 与一行 status 为空串的探针股票，两行必须落到
// **同一个** unknown 档（+2），其它档不受影响，且调用方不报错。
//
// 这条同时钉住 SQL 的写法：`GROUP BY status`（走原始列）会出来两行都叫 unknown
// 的分组，map 赋值后只剩最后一行的数 —— 读数会少一只票且不报任何错。
func TestStockStatusCounts_FoldsNullAndEmptyIntoOneUnknownBucket(t *testing.T) {
	store := testStore(t)
	// 用 t.Cleanup 而不是 defer：见 seedProbeStock 的 LIFO 说明 —— 探针删除必须
	// 跑在关池之前，defer 会让删除拿到 closed pool。
	t.Cleanup(store.Close)
	ctx := context.Background()

	before, err := store.StockStatusCounts(ctx)
	require.NoError(t, err)

	empty := ""
	seedProbeStock(t, store, ctx, splitProbe("NULLSTATUS"), nil)
	seedProbeStock(t, store, ctx, splitProbe("EMPTYSTATUS"), &empty)

	after, err := store.StockStatusCounts(ctx)
	require.NoError(t, err, "status 为 NULL / 空串的行不该让读数失败")

	assert.Equal(t, before["unknown"]+2, after["unknown"],
		"NULL 与空串应折进同一个 unknown 档，合计 +2")
	assert.NotContains(t, after, "", "空串不该原样成为一个分组键")
	assert.Equal(t, before["active"], after["active"], "这两行不该被算进 active")
	assert.Equal(t, before["delisted"], after["delisted"], "这两行不该被算进 delisted")
}

// TestStockStatusCounts_StatusMoveMovesTheBucket：同一行从 active 改判 delisted，
// 两档各移动 1、总数不变。读数若硬编码，这条会红。
func TestStockStatusCounts_StatusMoveMovesTheBucket(t *testing.T) {
	store := testStore(t)
	// 用 t.Cleanup 而不是 defer：见 seedProbeStock 的 LIFO 说明 —— 探针删除必须
	// 跑在关池之前，defer 会让删除拿到 closed pool。
	t.Cleanup(store.Close)
	ctx := context.Background()

	before, err := store.StockStatusCounts(ctx)
	require.NoError(t, err)

	probe := splitProbe("TOMOVE")
	status := "active"
	seedProbeStock(t, store, ctx, probe, &status)

	mid, err := store.StockStatusCounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, before["active"]+1, mid["active"], "插入一只 active 探针票，active 档应 +1")

	_, err = store.DB().Exec(ctx, `UPDATE stocks SET status='delisted' WHERE symbol=$1`, probe)
	require.NoError(t, err)

	after, err := store.StockStatusCounts(ctx)
	require.NoError(t, err)
	assert.Equal(t, mid["active"]-1, after["active"], "改判后 active 档应回落 1")
	assert.Equal(t, mid["delisted"]+1, after["delisted"], "改判后 delisted 档应 +1")
}
