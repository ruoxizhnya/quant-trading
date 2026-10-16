package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// factorDefinitionsTestDSN 与仓内其他存储测试同款（pkg/storage 既有惯例：
// 本机 PG 直连，测试数据自清理）。
const factorDefinitionsTestDSN = "postgres://postgres:postgres@localhost:5432/quant_trading"

func newFactorDefTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := NewPostgresStoreConnected(ctx, factorDefinitionsTestDSN)
	if err != nil {
		t.Skipf("local Postgres unavailable: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		// 表为 D3 新建；测试后清空（本仓测试数据自清理纪律）。
		_, _ = store.pool.Exec(cleanupCtx, `DELETE FROM factor_definitions`)
	})
	// 前置清场：保证起点干净（上次运行可能留尾）。
	if _, err := store.pool.Exec(ctx, `DELETE FROM factor_definitions`); err != nil {
		t.Skipf("factor_definitions table unavailable (run migrations first): %v", err)
	}
	return store
}

// ─── D3 ①：定义骨架落库，回读与种子逐条一致 ───

func TestFactorDefinitions_SaveAndLoadRoundTrip(t *testing.T) {
	store := newFactorDefTestStore(t)
	ctx := context.Background()

	if err := store.SaveFactorDefinitions(ctx, domain.BuiltinFactorDefinitions); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.LoadFactorDefinitions(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != len(domain.BuiltinFactorDefinitions) {
		t.Fatalf("loaded %d definitions, want %d", len(loaded), len(domain.BuiltinFactorDefinitions))
	}
	byName := make(map[domain.FactorType]domain.FactorDefinition, len(loaded))
	for _, d := range loaded {
		byName[d.Name] = d
	}
	for _, seed := range domain.BuiltinFactorDefinitions {
		got, ok := byName[seed.Name]
		if !ok {
			t.Errorf("definition %q missing after round trip", seed.Name)
			continue
		}
		if got.VersionHash() != seed.VersionHash() {
			t.Errorf("definition %q: hash drifted after round trip: %s vs %s",
				seed.Name, got.VersionHash(), seed.VersionHash())
		}
		if got.Kernel != seed.Kernel || got.Formula != seed.Formula || got.Validity != seed.Validity ||
			got.CodeRef != seed.CodeRef || got.Caliber != seed.Caliber || got.EffectiveFrom != seed.EffectiveFrom ||
			got.Lookahead != seed.Lookahead || got.Lookback != seed.Lookback {
			t.Errorf("definition %q: skeleton fields drifted after round trip: %+v", seed.Name, got)
		}
		if strings.Join(got.InputFields, ",") != strings.Join(seed.InputFields, ",") {
			t.Errorf("definition %q: input_fields drifted: %v vs %v", seed.Name, got.InputFields, seed.InputFields)
		}
	}
}

// ─── 幂等：重复 upsert 同一组定义不报错、行数不变 ───

func TestFactorDefinitions_UpsertIdempotent(t *testing.T) {
	store := newFactorDefTestStore(t)
	ctx := context.Background()

	if err := store.SaveFactorDefinitions(ctx, domain.BuiltinFactorDefinitions); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := store.SaveFactorDefinitions(ctx, domain.BuiltinFactorDefinitions); err != nil {
		t.Fatalf("second save (idempotent upsert): %v", err)
	}
	loaded, err := store.LoadFactorDefinitions(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != len(domain.BuiltinFactorDefinitions) {
		t.Fatalf("after double upsert: %d rows, want %d", len(loaded), len(domain.BuiltinFactorDefinitions))
	}
}

// ─── fail-closed：库里公式被绕过写入路径改坏 → 哈希校验拦住 ───

func TestFactorDefinitions_TamperedRowFailsClosed(t *testing.T) {
	store := newFactorDefTestStore(t)
	ctx := context.Background()

	if err := store.SaveFactorDefinitions(ctx, domain.BuiltinFactorDefinitions); err != nil {
		t.Fatalf("save: %v", err)
	}
	// 模拟「绕过写入路径直接改库」：改 formula 但不动 version_hash。
	if _, err := store.pool.Exec(ctx,
		`UPDATE factor_definitions SET formula = 'close / ts_delay(close, 5) - 1' WHERE factor_name = 'momentum'`,
	); err != nil {
		t.Fatalf("tamper setup: %v", err)
	}
	_, err := store.LoadFactorDefinitions(ctx)
	if err == nil {
		t.Fatal("tampered library row must fail closed on load")
	}
	if !strings.Contains(err.Error(), "does not match derived") {
		t.Fatalf("error should name the hash mismatch, got: %v", err)
	}
}

// ─── 写入路径自身也校验：坏定义（结构非法）不进库 ───

func TestFactorDefinitions_InvalidDefinitionRejected(t *testing.T) {
	store := newFactorDefTestStore(t)
	ctx := context.Background()

	bad := domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelFormula,
		Formula:       "close / ts_delay(close, 20) - 1",
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "", // 缺前视纪律声明
	}
	if err := store.SaveFactorDefinitions(ctx, []domain.FactorDefinition{bad}); err == nil {
		t.Fatal("definition without lookahead must be rejected on write")
	}
}
