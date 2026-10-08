// K1 切片 1：BusTap「先记录后分发」的端到端证据（**真 PostgreSQL**）。
//
// bus_test.go 用的是内存 Tap，能精确观测分发顺序，但证明不了「真的写进了
// audit.message_log 并原样回得来」。本文件补上那一半：真 pool → 真 INSERT →
// 真 SELECT 回放。
//
// 清理纪律同 pkg/eventstore/pg_eventstore_test.go 的文件头：
//   - 每个用例独享 run_id，清理只按 run_id 删，**绝不**无 WHERE 的 DELETE；
//   - ts 落在 2200 年隔离区，保证 Replay 出来的就是我们写的那几条。
package msgbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// busTestDSN 与 eventstore 侧同源定义，便于单独改动。
func busTestDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
}

// newBusTestStore 同 eventstore 侧的 newTestStore：独占 run_id + EnsureSchema
// + t.Cleanup 里按 run_id 精确清理，最后关池。
func newBusTestStore(t *testing.T) *eventstore.PGEventStore {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, busTestDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败（DSN=%s；本机起库见 docs/TEST.md §2.0.1）: %v", busTestDSN(t), err)
	}
	runID := "busitest-" + uuid.NewString()
	store, err := eventstore.NewPGEventStore(pool, "msgbus-itest", runID)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPGEventStore 失败: %v", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		pool.Close()
		t.Fatalf("EnsureSchema 失败: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `DELETE FROM audit.message_log WHERE run_id = $1`, runID); err != nil {
			t.Errorf("清理测试数据失败（run_id=%s）: %v", runID, err)
		}
		pool.Close()
	})
	return store
}

// barDTO 一条 payload 的结构，验证「写进去的 JSON 回得来」。
type barDTO struct {
	Symbol string  `json:"symbol"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

// TestBusTapEndToEndAgainstPostgres —— 验收第 3 条正证据的真库版本。
//
// 断言链：Publish → audit.message_log 真落一行 → Replay 按 ts 升序读回，
// 且**与订阅者收到的逐条一致、顺序一致**。
// 这就是 BusTap 的全部价值：订阅者看到的 == 事后回放出来的。
func TestBusTapEndToEndAgainstPostgres(t *testing.T) {
	store := newBusTestStore(t)
	ctx := context.Background()

	clk := clock.NewVirtualClock(time.Date(2200, 2, 1, 0, 0, 0, 0, time.UTC))
	bus := msgbus.NewSyncBusWithClock(store, clk)

	var received []msgbus.Message
	if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) {
		received = append(received, m)
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	published := []barDTO{
		{Symbol: "600519.SH", Close: 1688.5, Volume: 3200},
		{Symbol: "000858.SZ", Close: 155.2, Volume: 12800},
		{Symbol: "601318.SH", Close: 46.8, Volume: 90500},
	}
	for i, dto := range published {
		ts := time.Date(2200, 2, 1, 9, 30, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		if err := clk.Advance(ts); err != nil {
			t.Fatalf("推进虚拟时钟失败: %v", err)
		}
		if err := bus.Publish(msgbus.TopicDataBar, dto); err != nil {
			t.Fatalf("Publish 第 %d 条失败: %v", i, err)
		}
	}

	if len(received) != len(published) {
		t.Fatalf("订阅者收到 %d 条, want %d", len(received), len(published))
	}

	replayed, err := store.Replay(ctx,
		time.Date(2200, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2200, 2, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(replayed) != len(received) {
		t.Fatalf("Replay 返回 %d 条 vs 订阅者收到 %d 条——BusTap 要求两者一一对应",
			len(replayed), len(received))
	}
	for i := range received {
		if replayed[i].Topic() != received[i].Topic() {
			t.Errorf("第 %d 条 topic: 回放 %q vs 收到 %q", i, replayed[i].Topic(), received[i].Topic())
		}
		if !replayed[i].Ts().Equal(received[i].Ts()) {
			t.Errorf("第 %d 条 ts: 回放 %s vs 收到 %s", i, replayed[i].Ts(), received[i].Ts())
		}

		var gotDTO barDTO
		raw, ok := replayed[i].Payload().(json.RawMessage)
		if !ok {
			t.Fatalf("第 %d 条 Payload() 类型 = %T, want json.RawMessage", i, replayed[i].Payload())
		}
		if err := json.Unmarshal(raw, &gotDTO); err != nil {
			t.Fatalf("第 %d 条 payload 反序列化失败: %v", i, err)
		}
		if gotDTO != published[i] {
			t.Errorf("第 %d 条 payload 往返失真: got %+v, want %+v", i, gotDTO, published[i])
		}
	}

	// 写下这些行之后，写入账必须对得上（eventstore.Verify 的最小语义）。
	if err := store.Verify(); err != nil {
		t.Errorf("Verify 失败（本轮的 3 条要么丢了要么多写了）: %v", err)
	}
}

// TestBusAgainstBrokenPostgresStore 用真 store 再走一遍反证腿：
// 库里的约束把 Append 挡回来时，handler 依然一次都不许跑。
//
// 这里不用「注入的假 store」而是让真的 PGEventStore 真失败——方法是给一个
// topic 里塞不可序列化的 payload（chan 无法 json.Marshal）。这样反证腿覆盖的
// 是真实实现路径，而不是替身。
func TestBusAgainstBrokenPostgresStore(t *testing.T) {
	store := newBusTestStore(t)
	bus := msgbus.NewSyncBus(store)

	handlerCalls := 0
	if err := bus.Subscribe(msgbus.TopicExecOrderIntent, func(context.Context, msgbus.Message) {
		handlerCalls++
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	err := bus.Publish(msgbus.TopicExecOrderIntent, map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("payload 不可序列化时 Publish 返回 nil——未记录的消息被放行")
	}
	if !errors.Is(err, msgbus.ErrAppendFailed) {
		t.Errorf("error = %v, want errors.Is(err, msgbus.ErrAppendFailed)", err)
	}
	if handlerCalls != 0 {
		t.Errorf("handler 被调用 %d 次, want 0（真库落不下去的消息不许派发）", handlerCalls)
	}
	if err := store.Verify(); err != nil {
		t.Errorf("失败的 Append 之后写入账应仍为 0: %v", err)
	}
}

// TestBusTopicIsolationAgainstPostgres 两个 topic 各自隔离：发给 A 的消息不会被
// B 的订阅者看到，库里也不会串 topic。
func TestBusTopicIsolationAgainstPostgres(t *testing.T) {
	store := newBusTestStore(t)
	ctx := context.Background()

	clk := clock.NewVirtualClock(time.Date(2200, 4, 1, 0, 0, 0, 0, time.UTC))
	bus := msgbus.NewSyncBusWithClock(store, clk)

	var barCount, fillCount int
	if err := bus.Subscribe(msgbus.TopicDataBar, func(context.Context, msgbus.Message) { barCount++ }); err != nil {
		t.Fatalf("Subscribe(bar) 失败: %v", err)
	}
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) { fillCount++ }); err != nil {
		t.Fatalf("Subscribe(fill) 失败: %v", err)
	}

	if err := bus.Publish(msgbus.TopicDataBar, barDTO{Symbol: "600000.SH", Close: 7.1, Volume: 100}); err != nil {
		t.Fatalf("Publish(bar) 失败: %v", err)
	}
	if barCount != 1 || fillCount != 0 {
		t.Fatalf("订阅计数 bar=%d fill=%d, want 1/0", barCount, fillCount)
	}

	replayed, err := store.Replay(ctx,
		time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2201, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(replayed) != 1 {
		t.Fatalf("Replay 返回 %d 条, want 1", len(replayed))
	}
	if replayed[0].Topic() != msgbus.TopicDataBar {
		t.Errorf("落库 topic = %q, want %q", replayed[0].Topic(), msgbus.TopicDataBar)
	}
}
