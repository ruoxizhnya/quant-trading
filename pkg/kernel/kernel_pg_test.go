// K1 切片 2：pkg/kernel 真库集成测试 —— 验证内核生命周期事件真正落库。
//
// 覆盖（对应任务书 3.3 的「真库集成测试」）：
//   - Boot 一个含**真 eventstore**（连本机 PG）的内核 → audit.message_log
//     里能 Replay 到 kernel.boot 行；
//   - Shutdown 后能 Replay 到 kernel.shutdown 行。
//
// ─── 清理纪律（沿用 pkg/eventstore 的红线）─────────────────────────
//  1. 每个用例独享一个 run_id（uuid），清理只 `DELETE ... WHERE run_id = $1`；
//     **绝不** `DELETE FROM audit.message_log`（无 WHERE）——本机 5432 是
//     真库（AUD-62 教训）。
//  2. Replay 只能按 ts 过滤、拿不到 publisher/run_id（Envelope 无这两字段），
//     故时间窗口用「现在 ± 2s」精确夹住本次写入。
//  3. **库不在就红，不 skip**（docs/TEST.md §2.6）：能连不上库就 Skip 会让
//     这套测试在没有库的机器上恒绿。
package kernel_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/kernel"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// kernelTestDSN 与 docs/TEST.md §2.0.1、pkg/eventstore 同源，可用
// QUANT_TEST_DSN 覆盖。
func kernelTestDSN() string {
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
}

// placeholderModules 是切片 2 尚未实现的 7 个模块名（BootOrder 里除
// eventstore/clock/msgbus 之外的 7 项）。
var placeholderModules = []string{
	"data-engine",
	"portfolio",
	"risk-engine",
	"exec-engine",
	"strategy-runtime",
	"indicators",
	"exec-algo",
}

// TestKernelLifecycleLandsInAuditLog 是最关键的一条：内核 Boot/Shutdown
// 必须在 audit.message_log 里留下 kernel.boot / kernel.shutdown 两行
// ——这是 D4 审计起点的可观测性证明。
func TestKernelLifecycleLandsInAuditLog(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, kernelTestDSN())
	if err != nil {
		t.Fatalf("连接测试库失败（DSN=%s；本机起库见 docs/TEST.md §2.0.1）: %v", kernelTestDSN(), err)
	}
	runID := "kernel-itest-" + uuid.NewString()
	store, err := eventstore.NewPGEventStore(pool, "kernel-itest", runID)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPGEventStore 失败: %v", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		pool.Close()
		t.Fatalf("EnsureSchema 失败: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(c, `DELETE FROM audit.message_log WHERE run_id = $1`, runID); err != nil {
			t.Errorf("清理测试数据失败（run_id=%s，需手工检查残留）: %v", runID, err)
		}
		pool.Close()
	})

	// 服务级内核：LiveClock（墙钟）。VirtualClock 是 per-回测 run 的，不属于这里。
	clk := clock.NewLiveClock()
	bus := msgbus.NewLiveBus(store) // tap = 真 eventstore
	k := kernel.NewKernel(clk, bus, store)

	register := func(m kernel.Module) {
		t.Helper()
		if err := k.Register(m); err != nil {
			t.Fatalf("Register(%s) 失败: %v", m.Name(), err)
		}
	}
	register(kernel.NewEventStoreModule(store))
	register(kernel.NewClockModule(clk))
	register(kernel.NewMsgBusModule(bus))
	for _, name := range placeholderModules {
		register(kernel.NewNoopModule(name))
	}

	// ── Boot：应把 kernel.boot 落库 ──────────────────────────────
	bootFrom := time.Now().UTC().Add(-2 * time.Second)
	if err := k.Boot(ctx); err != nil {
		t.Fatalf("Boot 失败: %v", err)
	}
	bootTo := time.Now().UTC().Add(2 * time.Second)
	assertReplayContainsTopic(t, ctx, store, bootFrom, bootTo, msgbus.TopicKernelBoot)

	// ── Shutdown：应把 kernel.shutdown 落库（先发消息再逆序停）─────
	shutFrom := time.Now().UTC().Add(-2 * time.Second)
	if err := k.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}
	shutTo := time.Now().UTC().Add(2 * time.Second)
	assertReplayContainsTopic(t, ctx, store, shutFrom, shutTo, msgbus.TopicKernelShutdown)

	// 收尾：Verify 对账（本实例写入账 == 落库行数）。
	if err := store.Verify(); err != nil {
		t.Errorf("Verify 失败（写入账对不上，可能有他处双写 audit.message_log）: %v", err)
	}
}

// assertReplayContainsTopic 用 store.Replay 在 [from,to] 窗口内回放，
// 断言至少有一条 topic == want 的消息。
func assertReplayContainsTopic(
	t *testing.T,
	ctx context.Context,
	store *eventstore.PGEventStore,
	from, to time.Time,
	want string,
) {
	t.Helper()
	msgs, err := store.Replay(ctx, from, to)
	if err != nil {
		t.Fatalf("Replay(%s, %s) 失败: %v", from, to, err)
	}
	var topics []string
	for _, m := range msgs {
		topics = append(topics, m.Topic())
		if m.Topic() == want {
			return
		}
	}
	t.Errorf("Replay 窗口 [%s, %s] 内未找到 topic=%q；实际回放到的 topic: %v",
		from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano), want, topics)
}
