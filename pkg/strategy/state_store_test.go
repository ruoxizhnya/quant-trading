// K2 切片 3：pkg/strategy 的 PGStateStore（quant.strategy_state）行为测试。
//
// ─── 真库测试纪律（照 pkg/eventstore/pg_eventstore_test.go，红线区） ─
//  1. **自己的数据自己造、自己验、自己删**：每个用例独享一对
//     (strategy_id, run_id)（uuid），清理只
//     `DELETE FROM quant.strategy_state WHERE strategy_id = $1 AND run_id = $2`。
//     **绝不**无 WHERE 删表——本机 5432 的 quant_trading 是真库（AUD-62 教训：
//     曾在这个库上被测试删掉 4 行真实数据）。
//  2. **库不在就红，不 skip**：用例自灌自证，不依赖预置数据。连不上库就
//     Skip 会让这套测试在没有库的机器上「恒绿」——比没有测试更糟
//     （docs/TEST.md §2.6）。本机起库见 docs/TEST.md §2.0.1。
//  3. 时间戳取 2200 基准区，避开真实业务时间（同 eventstore 先例）。
package strategy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// stTestDSN 连接串：与 docs/TEST.md §2.0.1、pkg/storage/postgres_test.go 同源。
// 可用 QUANT_TEST_DSN 覆盖（CI 的 postgres service 就是这同一个地址）。
func stTestDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
}

// stTestTS 返回 2200 基准区里的第 i 个时间戳。
func stTestTS(i int) time.Time {
	return time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
}

// stNewStore 建一个独占 (strategy_id, run_id) 的 store + 连接池，并把清理挂到
// t.Cleanup。返回 pool 是为了让用例能直接执行 SQL（查行数、造垃圾字节）。
func stNewStore(t *testing.T) (*PGStateStore, *pgxpool.Pool, string, string) {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, stTestDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败（DSN=%s；本机起库见 docs/TEST.md §2.0.1）: %v", stTestDSN(t), err)
	}
	store, err := NewPGStateStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPGStateStore 失败: %v", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		pool.Close()
		t.Fatalf("EnsureSchema 失败（表没建起来，后续用例无从谈起）: %v", err)
	}

	strategyID := "itest-" + uuid.NewString()
	runID := "itest-" + uuid.NewString()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// 清理口径：只删本 (strategy_id, run_id) 的行。无 WHERE 删表会在真库上
		// 删掉别人的状态——不可逆。
		if _, err := pool.Exec(ctx,
			`DELETE FROM quant.strategy_state WHERE strategy_id = $1 AND run_id = $2`,
			strategyID, runID); err != nil {
			t.Errorf("清理测试数据失败（strategy_id=%s run_id=%s，需手工检查残留）: %v", strategyID, runID, err)
		}
		pool.Close()
	})
	return store, pool, strategyID, runID
}

// ─── 建表 ──────────────────────────────────────────────────────────

// TestPGStateStoreEnsureSchemaIsIdempotent EnsureSchema 会被反复调用（每次
// 装配、每个用例的 setup），不等价于 no-op 的语句迟早炸在一次不该炸的启动上。
func TestPGStateStoreEnsureSchemaIsIdempotent(t *testing.T) {
	store, _, _, _ := stNewStore(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		require.NoErrorf(t, store.EnsureSchema(ctx), "第 %d 次 EnsureSchema 失败（幂等性被破坏）", i)
	}
}

// TestPGStateStoreConstructorRejectsNilPool 构造期就把话说清楚，别等到某次
// Save/Load 才在无关现场炸。
func TestPGStateStoreConstructorRejectsNilPool(t *testing.T) {
	if _, err := NewPGStateStore(nil); err == nil {
		t.Error("NewPGStateStore(pool=nil) 返回 nil error, want error")
	}
}

// ─── Roundtrip / Upsert / 找不到 ───────────────────────────────────

// TestPGStateStoreRoundtrip Save → Load 同字节 + 同 ts。
func TestPGStateStoreRoundtrip(t *testing.T) {
	store, _, sid, rid := stNewStore(t)
	ctx := context.Background()

	ts := stTestTS(7)
	state := []byte(`{"version":1,"sum":42.5,"n":3}`)
	require.NoError(t, store.Save(ctx, sid, rid, ts, state))

	got, gotTS, err := store.Load(ctx, sid, rid)
	require.NoError(t, err)
	require.Equal(t, state, got, "Load 必须原样返回 Save 的字节（不解释内容）")
	require.True(t, gotTS.Equal(ts), "ts = %s, want %s", gotTS, ts)
}

// TestPGStateStoreUpsertKeepsLatest 同 (strategyID, runID) Save 两次 → 单行，
// 内容为后者（PK + ON CONFLICT 只保留最新，见 state_store.go 文件头裁决）。
func TestPGStateStoreUpsertKeepsLatest(t *testing.T) {
	store, pool, sid, rid := stNewStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, sid, rid, stTestTS(1), []byte("first")))
	require.NoError(t, store.Save(ctx, sid, rid, stTestTS(2), []byte("second")))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM quant.strategy_state WHERE strategy_id = $1 AND run_id = $2`,
		sid, rid).Scan(&count))
	require.Equal(t, 1, count, "PK/upsert 语义下同一 (策略, run) 只能有一行")

	got, gotTS, err := store.Load(ctx, sid, rid)
	require.NoError(t, err)
	require.Equal(t, []byte("second"), got, "upsert 后应保留最新一份")
	require.True(t, gotTS.Equal(stTestTS(2)), "ts = %s, want %s", gotTS, stTestTS(2))
}

// TestPGStateStoreLoadNotFound 无行 → ErrStateNotFound（fail-loud 哨兵，不静默
// 返回空）。
func TestPGStateStoreLoadNotFound(t *testing.T) {
	store, _, sid, rid := stNewStore(t)
	ctx := context.Background()

	_, _, err := store.Load(ctx, sid, rid)
	require.Error(t, err, "无检查点必须返回 error，不得静默返回空状态")
	require.ErrorIs(t, err, ErrStateNotFound, "Load = %v, want errors.Is(err, ErrStateNotFound)", err)
}

// ─── Save 的 fail-loud 拒绝 ────────────────────────────────────────

// TestPGStateStoreSaveRejectsBadInput 三类「写下去就是脏行/会让续跑失效」的
// 输入必须被挡住，而不是落库后在下游静默出错。
func TestPGStateStoreSaveRejectsBadInput(t *testing.T) {
	store, _, sid, rid := stNewStore(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		ts    time.Time
		state []byte
	}{
		{"nil state", stTestTS(0), nil},
		{"empty state", stTestTS(0), []byte{}},
		{"zero ts", time.Time{}, []byte("x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, store.Save(ctx, sid, rid, tc.ts, tc.state), "非法输入必须被拒（fail-loud）")
		})
	}

	// 三次都被拒 => 一行都没落（无痕迹）。
	_, _, err := store.Load(ctx, sid, rid)
	require.ErrorIs(t, err, ErrStateNotFound, "被拒的 Save 竟然留下了痕迹: %v", err)
}
