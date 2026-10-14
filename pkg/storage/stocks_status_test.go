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

// TestStockStatusCounts_MatchesPerStatusWhereCount 是工单 ③ 口径的机械化断言：
// 分组读数逐值 == 按该 status 单条 WHERE 计数。
func TestStockStatusCounts_MatchesPerStatusWhereCount(t *testing.T) {
	store := testStore(t)
	// 用 t.Cleanup 而不是 defer：见 seedProbeStock 的 LIFO 说明 —— 探针删除必须
	// 跑在关池之前，defer 会让删除拿到 closed pool。
	t.Cleanup(store.Close)
	ctx := context.Background()

	got, err := store.StockStatusCounts(ctx)
	require.NoError(t, err)

	// distinct status（含 NULL/空串折成的 unknown）从库里现取，不写死枚举 ——
	// 库里将来多一种状态值，这条断言自动覆盖它，而不是先红在测试里。
	rows, err := store.DB().Query(ctx,
		`SELECT DISTINCT COALESCE(NULLIF(status, ''), 'unknown') AS status
		 FROM stocks ORDER BY 1`)
	require.NoError(t, err)
	var statuses []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		statuses = append(statuses, s)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	require.NotEmpty(t, statuses, "stocks 表没有任何 status 值，交叉核对无从谈起")

	assert.Len(t, got, len(statuses), "分组键集合应与库里 distinct status 一致，实测 %+v", got)

	for _, status := range statuses {
		var want int64
		if status == "unknown" {
			// unknown 是 NULL 与空串折出来的合成键，WHERE 得按折叠前的形状写。
			err = store.DB().QueryRow(ctx,
				`SELECT COUNT(*) FROM stocks WHERE status IS NULL OR status = ''`).Scan(&want)
		} else {
			err = store.DB().QueryRow(ctx,
				`SELECT COUNT(*) FROM stocks WHERE status = $1`, status).Scan(&want)
		}
		require.NoError(t, err, "count by WHERE for status=%s", status)
		assert.Equal(t, want, got[status], "status=%s 的分组读数与 WHERE 计数不一致", status)
	}
}

// TestStockStatusCounts_FoldsNullAndEmptyIntoOneUnknownBucket：
// 同时插一行 status IS NULL 与一行 status = ” 的探针股票，两行必须落到
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
