package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// 因子登记账本（D4）的库侧测试。与同包 D3 测试同款：本机 PG 直连、
// 测试数据自清理、无库则 Skip。

func newFactorRegistrationTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := NewPostgresStoreConnected(ctx, factorDefinitionsTestDSN)
	if err != nil {
		t.Skipf("local Postgres unavailable: %v", err)
	}
	clean := func(c context.Context) {
		_, _ = store.pool.Exec(c, `DELETE FROM factor_registration_audit`)
		_, _ = store.pool.Exec(c, `DELETE FROM factor_registrations`)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		clean(cleanupCtx)
	})
	if _, err := store.pool.Exec(ctx, `DELETE FROM factor_registrations`); err != nil {
		t.Skipf("factor_registrations table unavailable (run migrations first): %v", err)
	}
	return store
}

// govReg 造一条合规登记：momentum 公式型定义第 lookback 版。
func govReg(lookback int, registrar string, at time.Time) domain.FactorRegistration {
	def := domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelFormula,
		Formula:       "close / ts_delay(close, 20) - 1",
		Validity:      "close > 0 and ts_delay(close, 20) > 0",
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      lookback,
	}
	return domain.FactorRegistration{
		Definition:   def,
		VersionHash:  def.VersionHash(),
		Registrar:    registrar,
		Status:       domain.RegistrationActive,
		RegisteredAt: at,
	}
}

func regAt(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }

// ─── 落库往返 + 审计留痕（谁/何时/改了什么/为什么）───

func TestFactorRegistrations_SaveLoadRoundTripWithAudit(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()

	reg := govReg(30, "alice", regAt(14))
	if err := store.SaveFactorRegistration(ctx, reg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.LoadFactorRegistrations(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d registrations, want 1", len(loaded))
	}
	got := loaded[0]
	if got.Definition.Name != reg.Definition.Name || got.VersionHash != reg.VersionHash {
		t.Fatalf("round trip drifted: got %+v want %+v", got, reg)
	}
	if got.Registrar != "alice" || got.Status != domain.RegistrationActive {
		t.Fatalf("registrar/status drifted: %+v", got)
	}
	// 定义快照自包含（回滚后老版本可复现的载体）。
	if got.Definition.Formula != reg.Definition.Formula || got.Definition.VersionHash() != reg.VersionHash {
		t.Fatalf("definition snapshot drifted: %+v", got.Definition)
	}

	audit, err := store.LoadFactorRegistrationAudit(ctx)
	if err != nil {
		t.Fatalf("load audit: %v", err)
	}
	if len(audit) != 1 || audit[0].Action != domain.ActionRegister || audit[0].Actor != "alice" {
		t.Fatalf("register audit missing/incorrect: %+v", audit)
	}
	if audit[0].At.IsZero() {
		t.Fatalf("audit missing 'when': %+v", audit[0])
	}
}

// ─── 幂等：同一 (名, 哈希) 重放 = no-op，不新增行、不重复留痕 ───

func TestFactorRegistrations_SaveIdempotent(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()

	reg := govReg(30, "alice", regAt(14))
	if err := store.SaveFactorRegistration(ctx, reg); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := store.SaveFactorRegistration(ctx, reg); err != nil {
		t.Fatalf("second save (idempotent replay): %v", err)
	}
	loaded, err := store.LoadFactorRegistrations(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("after double save: %d rows, want 1", len(loaded))
	}
	audit, _ := store.LoadFactorRegistrationAudit(ctx)
	if len(audit) != 1 {
		t.Fatalf("idempotent replay must not duplicate audit entries: got %d", len(audit))
	}
}

// ─── 回滚：状态翻转 + 历史引用仍可命中（版本行不删）───

func TestFactorRegistrations_RollbackLeavesVersionRow(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()

	reg := govReg(30, "alice", regAt(14))
	if err := store.SaveFactorRegistration(ctx, reg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.RollbackFactorRegistration(ctx, domain.FactorMomentum, reg.VersionHash, "alice", "window too short", regAt(16)); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	loaded, err := store.LoadFactorRegistrations(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("rollback must not delete the version row (historical reproducibility): got %d rows", len(loaded))
	}
	got := loaded[0]
	if got.Status != domain.RegistrationRolledBack {
		t.Fatalf("status should be rolled_back, got %q", got.Status)
	}
	if got.RolledBackAt == nil || got.RolledBackBy != "alice" || got.RollbackReason != "window too short" {
		t.Fatalf("rollback trail incomplete: %+v", got)
	}
	// 历史引用：按 (名, 哈希) 精确命中，行还在 → 可复现。
	byName, err := store.LoadFactorRegistrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if byName[0].Definition.Formula != reg.Definition.Formula {
		t.Fatalf("historical definition must remain intact after rollback: %+v", byName[0].Definition)
	}

	audit, _ := store.LoadFactorRegistrationAudit(ctx)
	if len(audit) != 2 || audit[1].Action != domain.ActionRollback {
		t.Fatalf("rollback audit missing/incorrect: %+v", audit)
	}
}

// ─── 回滚 fail-closed：未登记 / 已回滚 / 缺 actor 都拒收 ───

func TestFactorRegistrations_RollbackFailClosed(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()
	reg := govReg(30, "alice", regAt(14))
	if err := store.SaveFactorRegistration(ctx, reg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 未登记版本。
	if err := store.RollbackFactorRegistration(ctx, domain.FactorMomentum, "deadbeef", "alice", "", regAt(16)); err == nil {
		t.Fatal("rollback of an unregistered version must fail closed")
	}
	// 缺 actor（审计「谁」不可缺）。
	if err := store.RollbackFactorRegistration(ctx, domain.FactorMomentum, reg.VersionHash, "  ", "", regAt(16)); err == nil {
		t.Fatal("rollback without actor must fail closed")
	}
	// 正常回滚 → 再次回滚应拒收。
	if err := store.RollbackFactorRegistration(ctx, domain.FactorMomentum, reg.VersionHash, "alice", "", regAt(16)); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := store.RollbackFactorRegistration(ctx, domain.FactorMomentum, reg.VersionHash, "alice", "", regAt(17)); err == nil {
		t.Fatal("rollback of an already-rolled-back version must fail closed")
	}
}

// ─── 写入路径自身校验：缺样登记不进库 ───

func TestFactorRegistrations_SaveRejectsMissingFour(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()

	missingRegistrar := govReg(30, "", regAt(14))
	if err := store.SaveFactorRegistration(ctx, missingRegistrar); err == nil {
		t.Fatal("registration without registrar must be rejected on write")
	}

	staleHash := govReg(30, "alice", regAt(14))
	staleHash.VersionHash = strings.Repeat("0", 64)
	if err := store.SaveFactorRegistration(ctx, staleHash); err == nil {
		t.Fatal("registration with a hash not matching its content must be rejected on write")
	}

	// 缺公式（pending 无内核体）。
	pending := domain.FactorRegistration{
		Definition: domain.FactorDefinition{
			Name: domain.FactorSize, Kernel: domain.KernelPending,
			Caliber: domain.CaliberZScore, EffectiveFrom: "2026-10-14", Lookahead: "trade_date",
		},
		Registrar: "alice", Status: domain.RegistrationActive, RegisteredAt: regAt(14),
	}
	pending.VersionHash = pending.Definition.VersionHash()
	if err := store.SaveFactorRegistration(ctx, pending); err == nil {
		t.Fatal("pending-kernel registration (no formula) must be rejected on write")
	}
}

// ─── D4 ⑤3 破坏验证：绕过写入路径直写库 → DB 护栏红；走正路 → 绿 ───

func TestFactorRegistrations_BypassWritePathBlockedByDBGuardrail(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()

	leafDef := `{"name":"momentum","kernel":"leaf","formula":"close","caliber":"z_score",` +
		`"input_fields":["close"],"effective_from":"2026-10-14","lookahead":"trade_date"}`

	// 破坏腿①：绕过程序直接 INSERT 一条**没登记人**的登记。
	_, err := store.pool.Exec(ctx, `
		INSERT INTO factor_registrations (factor_name, version_hash, definition, registrar, status, registered_at)
		VALUES ('momentum', $1, $2::jsonb, '', 'active', NOW())`,
		strings.Repeat("a", 64), leafDef,
	)
	if err == nil {
		t.Fatal("raw INSERT with empty registrar must be rejected by DB CHECK")
	}
	if !strings.Contains(err.Error(), "factor_registrations_registrar_present") {
		t.Fatalf("expected registrar CHECK to trip, got: %v", err)
	}

	// 破坏腿②：绕过写入路径直接 INSERT 一条**无内核体**的登记。
	_, err = store.pool.Exec(ctx, `
		INSERT INTO factor_registrations (factor_name, version_hash, definition, registrar, status, registered_at)
		VALUES ('size', $1, '{"name":"size","kernel":"pending"}'::jsonb, 'alice', 'active', NOW())`,
		strings.Repeat("b", 64),
	)
	if err == nil {
		t.Fatal("raw INSERT without a kernel body must be rejected by DB CHECK")
	}
	if !strings.Contains(err.Error(), "factor_registrations_kernel_body_present") {
		t.Fatalf("expected kernel-body CHECK to trip, got: %v", err)
	}

	// 破坏腿③：状态枚举外的值被拒。
	_, err = store.pool.Exec(ctx, `
		INSERT INTO factor_registrations (factor_name, version_hash, definition, registrar, status, registered_at)
		VALUES ('momentum', $1, $2::jsonb, 'alice', 'limbo', NOW())`,
		strings.Repeat("c", 64), leafDef,
	)
	if err == nil {
		t.Fatal("raw INSERT with unknown status must be rejected by DB CHECK")
	}

	// 还原腿：走正常写入路径 → 绿（护栏只在绕过时红，不误伤正路）。
	ok := govReg(30, "alice", regAt(14))
	if err := store.SaveFactorRegistration(ctx, ok); err != nil {
		t.Fatalf("proper write path must stay green after guardrail legs: %v", err)
	}
	loaded, err := store.LoadFactorRegistrations(ctx)
	if err != nil {
		t.Fatalf("load after restore: %v", err)
	}
	if len(loaded) != 1 || loaded[0].VersionHash != ok.VersionHash {
		t.Fatalf("restore failed: %+v", loaded)
	}
}

// ─── fail-closed：库行被绕过写入路径改坏 → 加载时哈希校验拦住 ───

func TestFactorRegistrations_TamperedRowFailsClosed(t *testing.T) {
	store := newFactorRegistrationTestStore(t)
	ctx := context.Background()

	reg := govReg(30, "alice", regAt(14))
	if err := store.SaveFactorRegistration(ctx, reg); err != nil {
		t.Fatalf("save: %v", err)
	}
	// 绕过写入路径改 definition 内容，但不动 version_hash。
	tampered := `{"name":"momentum","kernel":"formula","formula":"close / ts_delay(close, 5) - 1",` +
		`"caliber":"z_score","input_fields":["close"],"effective_from":"2026-10-14","lookahead":"trade_date","lookback":30}`
	if _, err := store.pool.Exec(ctx,
		`UPDATE factor_registrations SET definition = $1::jsonb WHERE factor_name = 'momentum'`, tampered,
	); err != nil {
		t.Fatalf("tamper setup: %v", err)
	}
	_, err := store.LoadFactorRegistrations(ctx)
	if err == nil {
		t.Fatal("tampered library row must fail closed on load")
	}
	if !strings.Contains(err.Error(), "does not match derived") {
		t.Fatalf("error should name the hash mismatch, got: %v", err)
	}
}
