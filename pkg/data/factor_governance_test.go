package data

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ─── D4 ⑤1 演练：登记 → 被公式引用 → 回滚 ─────────────────────────────────
//
// 本测试就是「演练记录」的可执行形态（验收④1：完整走一遍三步，每步留痕）。
// 一步步断言，等价于逐步截图：每一步之后账本的状态与审计留痕都被钉死。

// momentumV 返回同名的 momentum 公式因子定义第 v 版（改内容必然改哈希）。
// 两版都过注册表闸门（公式可解析、identifier ↔ 输入字段双向相等、字段在
// 规范层词表内），才可能登记成功——登记不是「写字符串」。
func momentumV(lookback int) domain.FactorDefinition {
	window := lookback
	return domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelFormula,
		Formula:       "close / ts_delay(close, 20) - 1",
		Validity:      "close > 0 and ts_delay(close, 20) > 0",
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      window,
	}
}

func at(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }

func TestRegistrationBook_Drill_RegisterReferenceRollback(t *testing.T) {
	book := NewRegistrationBook()
	v1 := momentumV(30)
	v2 := momentumV(10)
	hash1 := v1.VersionHash()
	hash2 := v2.VersionHash()
	if hash1 == hash2 {
		t.Fatal("两版定义内容不同，哈希必须不同——否则版本纪律失效")
	}

	// ── 第 1 步：登记 v1 ──
	reg1, err := book.Register(v1, "alice", "initial momentum definition", at(14))
	if err != nil {
		t.Fatalf("步骤1 登记 v1 失败: %v", err)
	}
	if reg1.VersionHash != hash1 {
		t.Fatalf("登记的版本哈希应等于内容派生值: got %s want %s", reg1.VersionHash, hash1)
	}
	if got, hash, ok := book.ResolveEffective(domain.FactorMomentum); !ok || hash != hash1 || got.Formula != v1.Formula {
		t.Fatalf("步骤1 后新引用应命中 v1: ok=%v hash=%s", ok, hash)
	}

	// ── 第 2 步：被公式引用（解析出的定义必须真的能算）──
	// 「被引用」= 新引用解析到一个在册版本，且该版本的公式过计算闸门
	// （注册表构造 = 计算线加载定义时走的同一道门）。
	def, pinnedHash, ok := book.ResolveEffective(domain.FactorMomentum)
	if !ok {
		t.Fatal("步骤2 新引用应命中在册版本")
	}
	if _, err := NewRegistry([]domain.FactorDefinition{def}); err != nil {
		t.Fatalf("步骤2 被引用的定义必须能过计算闸门: %v", err)
	}

	// 登记 v2（新内容 → 新哈希），新引用随之抬到 v2。
	if _, err := book.Register(v2, "bob", "tighten momentum window", at(15)); err != nil {
		t.Fatalf("步骤2 登记 v2 失败: %v", err)
	}
	if _, hash, ok := book.ResolveEffective(domain.FactorMomentum); !ok || hash != hash2 {
		t.Fatalf("登记 v2 后新引用应命中最新在册版本 v2: ok=%v hash=%s", ok, hash)
	}
	// 历史引用（钉 hash1）仍命中 v1。
	if _, ok := book.ResolvePinned(domain.FactorMomentum, pinnedHash); !ok {
		t.Fatal("历史引用（钉 v1 哈希）应命中")
	}

	// ── 第 3 步：回滚 v2 ──
	if err := book.Rollback(domain.FactorMomentum, hash2, "alice", "window too short", at(16)); err != nil {
		t.Fatalf("步骤3 回滚 v2 失败: %v", err)
	}
	// 新引用不再命中 v2 → 回落到在册的 v1。
	if _, hash, ok := book.ResolveEffective(domain.FactorMomentum); !ok || hash != hash1 {
		t.Fatalf("回滚 v2 后新引用应回落到 v1: ok=%v hash=%s", ok, hash)
	}
	// 历史引用（钉 v2 哈希）**仍然命中**——回滚不破坏历史复现。
	if got, ok := book.ResolvePinned(domain.FactorMomentum, hash2); !ok || got.Formula != v2.Formula {
		t.Fatalf("回滚后历史引用（钉 v2 哈希）仍须命中: ok=%v", ok)
	}

	// ── 审计留痕：三步各留一条，谁/何时/改了什么/为什么齐全 ──
	audit := book.Audit()
	if len(audit) != 3 {
		t.Fatalf("审计应恰好 3 条（register v1 / register v2 / rollback v2），got %d: %+v", len(audit), audit)
	}
	wantActions := []domain.RegistrationAction{domain.ActionRegister, domain.ActionRegister, domain.ActionRollback}
	wantHashes := []string{hash1, hash2, hash2}
	wantActors := []string{"alice", "bob", "alice"}
	for i, e := range audit {
		if e.Action != wantActions[i] || e.VersionHash != wantHashes[i] || e.Actor != wantActors[i] {
			t.Fatalf("审计第 %d 条不符: got %+v", i, e)
		}
		if e.At.IsZero() {
			t.Fatalf("审计第 %d 条缺「何时」: %+v", i, e)
		}
	}
	if audit[2].Reason == "" {
		t.Fatalf("回滚留痕缺「为什么」: %+v", audit[2])
	}
}

// TestRegistrationBook_RollbackAllNewReferenceMisses：全部版本回滚后，新引用
// 必须显式 miss（ok=false），不凑默认定义；历史引用仍逐版本命中。
func TestRegistrationBook_RollbackAllNewReferenceMisses(t *testing.T) {
	book := NewRegistrationBook()
	v1 := momentumV(30)
	v2 := momentumV(10)
	_, _ = book.Register(v1, "alice", "", at(14))
	_, _ = book.Register(v2, "alice", "", at(15))
	if err := book.Rollback(domain.FactorMomentum, v1.VersionHash(), "alice", "deprecated", at(16)); err != nil {
		t.Fatal(err)
	}
	if err := book.Rollback(domain.FactorMomentum, v2.VersionHash(), "alice", "deprecated", at(17)); err != nil {
		t.Fatal(err)
	}
	if _, hash, ok := book.ResolveEffective(domain.FactorMomentum); ok || hash != "" {
		t.Fatalf("全回滚后新引用必须 miss: ok=%v hash=%s", ok, hash)
	}
	if _, ok := book.ResolvePinned(domain.FactorMomentum, v1.VersionHash()); !ok {
		t.Fatal("全回滚后历史引用（钉 v1）仍须命中")
	}
	if _, ok := book.ResolvePinned(domain.FactorMomentum, v2.VersionHash()); !ok {
		t.Fatal("全回滚后历史引用（钉 v2）仍须命中")
	}
}

// TestRegistrationBook_FailClosed：登记/回滚的拒收面（D4 ③2、⑤2/3）。
func TestRegistrationBook_FailClosed(t *testing.T) {
	book := NewRegistrationBook()

	// ① 缺公式（pending 无内核体）→ 拒收。
	pending := domain.FactorDefinition{
		Name: domain.FactorSize, Kernel: domain.KernelPending,
		Caliber: domain.CaliberZScore, EffectiveFrom: "2026-10-14", Lookahead: "trade_date",
	}
	if _, err := book.Register(pending, "alice", "", at(14)); err == nil {
		t.Fatal("pending 定义不得登记为生效（缺公式）")
	}

	// ② 缺登记人 → 拒收。
	if _, err := book.Register(momentumV(30), "   ", "", at(14)); err == nil {
		t.Fatal("缺登记人必须拒收")
	}

	// ③ 注册表闸门：公式用了未声明的字段 → 拒收（登记 = 这一版真的能算）。
	hidden := momentumV(30)
	hidden.InputFields = []string{"roe"} // 公式用 close，却声明 roe → 双向不等
	if _, err := book.Register(hidden, "alice", "", at(14)); err == nil {
		t.Fatal("公式 input_fields 与 identifier 不等必须被注册表闸门拒收")
	}

	// ④ 重复登记（同一版原样重放）→ ErrRegistrationDuplicate（errors.Is）。
	if _, err := book.Register(momentumV(30), "alice", "", at(14)); err != nil {
		t.Fatal(err)
	}
	_, err := book.Register(momentumV(30), "bob", "", at(15))
	if !errors.Is(err, ErrRegistrationDuplicate) {
		t.Fatalf("重复登记同一版本应返回 ErrRegistrationDuplicate，got: %v", err)
	}

	// ⑤ 回滚未知版本 → 拒收。
	if err := book.Rollback(domain.FactorMomentum, "deadbeef", "alice", "", at(16)); err == nil {
		t.Fatal("回滚未登记的版本必须拒收")
	}
	// ⑥ 回滚缺 actor → 拒收。
	if err := book.Rollback(domain.FactorMomentum, momentumV(30).VersionHash(), "", "", at(16)); err == nil {
		t.Fatal("回滚缺 actor（审计「谁」）必须拒收")
	}
	// ⑦ 回滚已回滚 → 拒收。
	if err := book.Rollback(domain.FactorMomentum, momentumV(30).VersionHash(), "alice", "", at(16)); err != nil {
		t.Fatal(err)
	}
	err = book.Rollback(domain.FactorMomentum, momentumV(30).VersionHash(), "alice", "", at(17))
	if err == nil {
		t.Fatal("回滚已回滚的版本必须拒收（幂等重放不算成功）")
	}
}

// TestRegistrationBookFrom_RejectsBadRows：从库回读重建账本时逐条过四样校验，
// 坏行不得混进账本（fail-closed），重复版本同样拒收。
func TestRegistrationBookFrom_RejectsBadRows(t *testing.T) {
	good := domain.FactorRegistration{
		Definition: momentumV(30), VersionHash: momentumV(30).VersionHash(),
		Registrar: "alice", Status: domain.RegistrationActive, RegisteredAt: at(14),
	}
	if _, err := NewRegistrationBookFrom([]domain.FactorRegistration{good}); err != nil {
		t.Fatalf("合规行应重建成功: %v", err)
	}

	// 坏行：哈希与内容不符（库被绕过写入路径改过）→ 拒。
	bad := good
	bad.VersionHash = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := NewRegistrationBookFrom([]domain.FactorRegistration{bad}); err == nil {
		t.Fatal("哈希不符的坏行必须拒载")
	}

	// 重复 (name, hash) → 拒。
	if _, err := NewRegistrationBookFrom([]domain.FactorRegistration{good, good}); !errors.Is(err, ErrRegistrationDuplicate) {
		t.Fatalf("重复版本应返回 ErrRegistrationDuplicate，got: %v", err)
	}
}

// TestRegistrationBook_ConcurrentRegisterResolveRollback 是 REV-03 ⑤(a1) 的
// 并发语义钉桩：在 `-race` 下把写侧（Register/Rollback）与读侧（Resolve*/
// Registrations/Audit）并发交叠，锁必须让它们互不撕裂。缺写侧保护即报
// 「并发写 map / 并发读写 slice」。
//
// 两阶段各自把并发度拉满，且断言去重的封闭性（不是「大概率对」）：
//
//	阶段 1：同一版本被 N 个 goroutine 并发登记——恰好 1 次成功，其余
//	       ErrRegistrationDuplicate（去重判在锁内，未加锁即并发写 map）；
//	阶段 2：同一版本被 N 个 goroutine 并发回滚——恰好 1 次成功，其余
//	       「已回滚」报错（状态读改在锁内，未加锁即撕裂 status）。
func TestRegistrationBook_ConcurrentRegisterResolveRollback(t *testing.T) {
	book := NewRegistrationBook()
	v1 := momentumV(30)
	v2 := momentumV(10)
	hash1 := v1.VersionHash()
	hash2 := v2.VersionHash()

	const workers = 64

	// 读 goroutine：全程锤四条读路径，与写侧并发交叠（阶段 1/2 期间不停）。
	stopRead := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stopRead:
					return
				default:
				}
				_, _, _ = book.ResolveEffective(domain.FactorMomentum)
				_, _ = book.ResolvePinned(domain.FactorMomentum, hash1)
				_, _ = book.ResolvePinned(domain.FactorMomentum, hash2)
				_ = book.Registrations()
				_ = book.Audit()
			}
		}()
	}

	// ── 阶段 1：并发登记两版（各 workers 个 goroutine 抢登记同一版）──
	start := make(chan struct{})
	var writers sync.WaitGroup
	var ok1, dup1, ok2, dup2 int32
	for i := 0; i < workers; i++ {
		writers.Add(2)
		go func() {
			defer writers.Done()
			<-start
			if _, err := book.Register(v1, "racer-a", "", at(14)); err == nil {
				atomic.AddInt32(&ok1, 1)
			} else if errors.Is(err, ErrRegistrationDuplicate) {
				atomic.AddInt32(&dup1, 1)
			} else {
				t.Errorf("并发登记 v1 意外错误: %v", err)
			}
		}()
		go func() {
			defer writers.Done()
			<-start
			if _, err := book.Register(v2, "racer-b", "", at(15)); err == nil {
				atomic.AddInt32(&ok2, 1)
			} else if errors.Is(err, ErrRegistrationDuplicate) {
				atomic.AddInt32(&dup2, 1)
			} else {
				t.Errorf("并发登记 v2 意外错误: %v", err)
			}
		}()
	}
	close(start)
	writers.Wait()

	if ok1 != 1 || dup1 != workers-1 {
		t.Fatalf("v1 并发登记应恰好 1 成功 + %d 去重，got ok=%d dup=%d", workers-1, ok1, dup1)
	}
	if ok2 != 1 || dup2 != workers-1 {
		t.Fatalf("v2 并发登记应恰好 1 成功 + %d 去重，got ok=%d dup=%d", workers-1, ok2, dup2)
	}
	if got := len(book.Registrations()); got != 2 {
		t.Fatalf("并发去重后登记数应恰好 2（v1+v2），got %d", got)
	}
	if _, hash, ok := book.ResolveEffective(domain.FactorMomentum); !ok || hash != hash2 {
		t.Fatalf("两版在册时新引用应命中较新的 v2: ok=%v hash=%s", ok, hash)
	}

	// ── 阶段 2：并发回滚 v2（唯一成功者置 rolled_back，其余撞「已回滚」）──
	start2 := make(chan struct{})
	var roll sync.WaitGroup
	var okR, alreadyR int32
	for i := 0; i < workers; i++ {
		roll.Add(1)
		go func() {
			defer roll.Done()
			<-start2
			err := book.Rollback(domain.FactorMomentum, hash2, "racer-c", "concurrent rollback", at(16))
			switch {
			case err == nil:
				atomic.AddInt32(&okR, 1)
			case strings.Contains(err.Error(), "already rolled back"):
				atomic.AddInt32(&alreadyR, 1)
			default:
				t.Errorf("并发回滚 v2 意外错误: %v", err)
			}
		}()
	}
	close(start2)
	roll.Wait()
	close(stopRead)
	readers.Wait()

	if okR != 1 || alreadyR != workers-1 {
		t.Fatalf("v2 并发回滚应恰好 1 成功 + %d 已回滚，got ok=%d already=%d", workers-1, okR, alreadyR)
	}

	// ── 最终态（读路径一致性）：v2 回滚 → 新引用回落 v1，两版历史引用都命中 ──
	if _, hash, ok := book.ResolveEffective(domain.FactorMomentum); !ok || hash != hash1 {
		t.Fatalf("v2 回滚后新引用应回落 v1: ok=%v hash=%s", ok, hash)
	}
	if _, ok := book.ResolvePinned(domain.FactorMomentum, hash1); !ok {
		t.Fatal("v1 历史引用仍须命中")
	}
	if _, ok := book.ResolvePinned(domain.FactorMomentum, hash2); !ok {
		t.Fatal("v2 历史引用（虽已回滚）仍须命中——回滚不破坏复现")
	}
	audit := book.Audit()
	if len(audit) != 3 {
		t.Fatalf("审计应恰好 3 条（register v1 / register v2 / rollback v2），got %d: %+v", len(audit), audit)
	}
	var rollbacks int
	for _, e := range audit {
		if e.Action == domain.ActionRollback {
			rollbacks++
		}
	}
	if rollbacks != 1 {
		t.Fatalf("回滚审计留痕应恰好 1 条（并发去重只写一次），got %d", rollbacks)
	}
}
