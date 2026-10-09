// K7 切片 1：pkg/strategy 的 PGSignalStore（quant.external_signals）行为测试。
//
// ─── 真库测试纪律（照 state_store_test.go，红线区） ─
//  1. **自己的数据自己造、自己验、自己删**：每个用例独享一个 model_id（uuid），
//     清理只 `DELETE FROM quant.external_signals WHERE model_id = $1`。
//     **绝不**无 WHERE 删表——本机 5432 的 quant_trading 是真库（AUD-62 教训）。
//  2. **库不在就红，不 skip**：用例自灌自证，不依赖预置数据（docs/TEST.md §2.6）。
//  3. 时间戳取 2200 基准区，避开真实业务时间（同 eventstore 先例）。
package strategy

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/require"
)

// sigNewStore 建一个独占 model_id 的 PGSignalStore + 连接池，并把清理挂到
// t.Cleanup。返回 pool 供用例直接执行 SQL（查行数）。
func sigNewStore(t *testing.T) (*PGSignalStore, *pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, stTestDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败（DSN=%s；本机起库见 docs/TEST.md §2.0.1）: %v", stTestDSN(t), err)
	}
	store, err := NewPGSignalStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPGSignalStore 失败: %v", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		pool.Close()
		t.Fatalf("EnsureSchema 失败（表没建起来，后续用例无从谈起）: %v", err)
	}

	modelID := "itest-" + uuid.NewString()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// 清理口径：只删本 model_id 的行。无 WHERE 删表会在真库上删掉别人的
		// 信号——不可逆。
		if _, err := pool.Exec(ctx,
			`DELETE FROM quant.external_signals WHERE model_id = $1`, modelID); err != nil {
			t.Errorf("清理测试数据失败（model_id=%s，需手工检查残留）: %v", modelID, err)
		}
		pool.Close()
	})
	return store, pool, modelID
}

// sigDay 返回 2200 基准区的第 i 个交易日时间戳（同日 00:00 UTC）。
func sigDay(i int) time.Time {
	return time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i)
}

func sigSig(modelID, symbol string, dir domain.Direction, strength float64, asOf time.Time) ExternalSignal {
	return ExternalSignal{
		ModelID:   modelID,
		Symbol:    symbol,
		Direction: dir,
		Strength:  strength,
		AsOf:      asOf,
	}
}

// ─── 建表 / 构造 ────────────────────────────────────────────────────

func TestPGSignalStoreEnsureSchemaIsIdempotent(t *testing.T) {
	store, _, _ := sigNewStore(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		require.NoErrorf(t, store.EnsureSchema(ctx), "第 %d 次 EnsureSchema 失败（幂等性被破坏）", i)
	}
}

func TestPGSignalStoreConstructorRejectsNilPool(t *testing.T) {
	if _, err := NewPGSignalStore(nil); err == nil {
		t.Error("NewPGSignalStore(pool=nil) 返回 nil error, want error")
	}
}

// ─── Roundtrip / 幂等 / 排序 ────────────────────────────────────────

// TestPGSignalStoreSaveAndListForRoundtrip Save → ListFor 读回完整信号（含
// factors 与 direction 还原）。
func TestPGSignalStoreSaveAndListForRoundtrip(t *testing.T) {
	store, _, modelID := sigNewStore(t)
	ctx := context.Background()

	sig := sigSig(modelID, "600000.SH", domain.DirectionLong, 0.75, sigDay(3))
	sig.Factors = map[string]float64{"ml_pred": 0.032}
	require.NoError(t, store.Save(ctx, sig))

	got, err := store.ListFor(ctx, modelID, "600000.SH", sigDay(5))
	require.NoError(t, err)
	require.Len(t, got, 1, "应读回刚写入的 1 条信号")
	require.Equal(t, modelID, got[0].ModelID)
	require.Equal(t, "600000.SH", got[0].Symbol)
	require.Equal(t, domain.DirectionLong, got[0].Direction)
	require.InDelta(t, 0.75, got[0].Strength, 1e-12)
	require.True(t, got[0].AsOf.Equal(sigDay(3)), "as_of 应原样读回 = %s", got[0].AsOf)
	require.Equal(t, map[string]float64{"ml_pred": 0.032}, got[0].Factors)
}

// TestPGSignalStoreSaveIdempotentByContentHash 同一可执行信号写两次 → 单行
// （content_hash 是幂等坐标，重复注入不重复落库）。
func TestPGSignalStoreSaveIdempotentByContentHash(t *testing.T) {
	store, pool, modelID := sigNewStore(t)
	ctx := context.Background()

	sig := sigSig(modelID, "600000.SH", domain.DirectionLong, 0.75, sigDay(3))
	require.NoError(t, store.Save(ctx, sig))
	require.NoError(t, store.Save(ctx, sig)) // 重复注入

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM quant.external_signals WHERE model_id = $1`, modelID).Scan(&count))
	require.Equal(t, 1, count, "content_hash 幂等：同一信号只能落一行")
}

// TestPGSignalStoreListForExcludesFuture 未来信号（as_of > upTo）不可见——这是
// 「按 as_of 拉取」防前视的主边界。破坏验证腿 a：把 listSignalsSQL 的
// `AND as_of <= $3` 去掉 → 本测试红（读到了未来信号）。
func TestPGSignalStoreListForExcludesFuture(t *testing.T) {
	store, _, modelID := sigNewStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.1, sigDay(1))))
	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.2, sigDay(3))))
	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.3, sigDay(5))))

	got, err := store.ListFor(ctx, modelID, "600000.SH", sigDay(3))
	require.NoError(t, err)
	require.Len(t, got, 2, "upTo=day3 应只读到 day1+day3，未来 day5 不可见")
	for _, s := range got {
		require.False(t, s.AsOf.After(sigDay(3)), "读到未来信号 %s（防前视被破坏）", s.AsOf)
	}
}

// TestPGSignalStoreListForOrdering 结果按 as_of 升序（消费侧按时间顺序递进
// 的信号游标依赖它）。
func TestPGSignalStoreListForOrdering(t *testing.T) {
	store, _, modelID := sigNewStore(t)
	ctx := context.Background()

	// 乱序写入，读回必须升序。
	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.3, sigDay(5))))
	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.1, sigDay(1))))
	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.2, sigDay(3))))

	got, err := store.ListFor(ctx, modelID, "600000.SH", sigDay(9))
	require.NoError(t, err)
	require.Len(t, got, 3)
	for i := 1; i < len(got); i++ {
		require.True(t, !got[i].AsOf.Before(got[i-1].AsOf),
			"ListFor 必须按 as_of 升序：第 %d 条 %s 早于第 %d 条 %s",
			i, got[i].AsOf, i-1, got[i-1].AsOf)
	}
}

// TestPGSignalStoreListForScopedBySymbolAndModel 只返回 (model_id, symbol) 命中
// 的信号，其它 symbol / model 不串。
func TestPGSignalStoreListForScopedBySymbolAndModel(t *testing.T) {
	store, _, modelID := sigNewStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, sigSig(modelID, "600000.SH", domain.DirectionLong, 0.1, sigDay(1))))
	require.NoError(t, store.Save(ctx, sigSig(modelID, "000001.SZ", domain.DirectionLong, 0.2, sigDay(1))))
	require.NoError(t, store.Save(ctx, sigSig("other-model", "600000.SH", domain.DirectionLong, 0.3, sigDay(1))))

	got, err := store.ListFor(ctx, modelID, "600000.SH", sigDay(9))
	require.NoError(t, err)
	require.Len(t, got, 1, "只应返回 (model_id, symbol) 命中的那一条")
	require.Equal(t, "600000.SH", got[0].Symbol)
}

// TestPGSignalStoreListForEmptyIsNotError 无匹配 → 空切片而非 error（空是合法
// 结果，不是「找不到」的哨兵）。
func TestPGSignalStoreListForEmptyIsNotError(t *testing.T) {
	store, _, modelID := sigNewStore(t)
	ctx := context.Background()

	got, err := store.ListFor(ctx, modelID, "600000.SH", sigDay(9))
	require.NoError(t, err)
	require.Empty(t, got, "无匹配信号应返回空切片而非 error")
}

// ─── Save 的 fail-loud 拒绝 ────────────────────────────────────────

// TestPGSignalStoreSaveRejectsBadInput 非法输入必须被挡在库外，而不是落一行
// 脏数据。校验口径与 ValidateExternalSignal 单测一致，这里验证 PG 实现真的
// 调用了它。
func TestPGSignalStoreSaveRejectsBadInput(t *testing.T) {
	store, pool, modelID := sigNewStore(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*ExternalSignal)
	}{
		{"empty model_id", func(s *ExternalSignal) { s.ModelID = "" }},
		{"empty symbol", func(s *ExternalSignal) { s.Symbol = "" }},
		{"garbage direction", func(s *ExternalSignal) { s.Direction = "buy" }},
		{"NaN strength", func(s *ExternalSignal) { s.Strength = math.NaN() }},
		{"zero as_of", func(s *ExternalSignal) { s.AsOf = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sigSig(modelID, "600000.SH", domain.DirectionLong, 0.1, sigDay(1))
			tc.mutate(&s)
			require.Error(t, store.Save(ctx, s), "非法输入必须被拒（fail-loud）")
		})
	}

	// 全部被拒 => 一行都没落（无痕迹）。
	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM quant.external_signals WHERE model_id = $1`, modelID).Scan(&count))
	require.Equal(t, 0, count, "被拒的 Save 竟然留下了痕迹")
}
