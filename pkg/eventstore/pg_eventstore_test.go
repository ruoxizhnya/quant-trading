// K1 切片 1：pkg/eventstore 的 PGEventStore 行为测试。
//
// ─── 自灌自证 / 清理纪律（红线区，改动前先读） ───────────────────────
//  1. **自己的数据自己造、自己验、自己删**：每个用例独享一个 run_id
//     （uuid），清理只 `DELETE ... WHERE run_id = $1`。**绝不用**
//     `DELETE FROM audit.message_log`（无 WHERE）——本机 5432 的
//     quant_trading 是真库（AUD-62 教训：曾在这个库上被测试删掉 4 行真实
//     交易日数据）。
//  2. **时间点选 2200 年**：Replay 只能按 ts 过滤、拿不到 publisher/run_id
//     （Envelope 没有这两个字段）。把写数据的 ts 放在一个真实业务绝不会用到的
//     区间里，才能让「Replay 回来的就是我写的那几条」这个断言成立——同样的
//     道理 AUD-62 在 trading_calendar 上踩过一次。
//  3. **库不在就红，不 skip**：本包的用例是自灌的，不依赖任何预置数据。若连
//     不上库就 Skip，这套测试会在没有库的机器上变成「恒绿」——那是比没有
//     测试更糟的状态（docs/TEST.md §2.6）。CI 的 go job 有 postgres service，
//     本机按 docs/TEST.md §2.0.1 手起原生库。
package eventstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// ─── 测试脚手架 ────────────────────────────────────────────────────

// testDSN 连接串：与 docs/TEST.md §2.0.1、pkg/storage/postgres_test.go 同源。
// 可用 QUANT_TEST_DSN 覆盖（CI 的服务就是这同一个地址，一般不用改）。
func testDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
}

// testPublisher 是本套测试统一的 publisher 标识。
const testPublisher = "eventstore-itest"

// newTestStore 建一个独占 run_id 的 store + 连接池，并把清理挂到 t.Cleanup。
// 返回 pool 是为了让个别用例能直接执行 SQL（删行造故障、查 publisher 列）。
func newTestStore(t *testing.T) (*eventstore.PGEventStore, *pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败（DSN=%s；本机起库见 docs/TEST.md §2.0.1）: %v", testDSN(t), err)
	}
	runID := "itest-" + uuid.NewString()
	store, err := eventstore.NewPGEventStore(pool, testPublisher, runID)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPGEventStore 失败: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("EnsureSchema 失败（表没建起来，后续用例无从谈起）: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// 清理口径：只删本 run_id 的行。写成 DELETE 无 WHERE 会在真库上删掉
		// 别人的审计记录——那是不可逆的。
		if _, err := pool.Exec(ctx, `DELETE FROM audit.message_log WHERE run_id = $1`, runID); err != nil {
			t.Errorf("清理测试数据失败（run_id=%s，需手工检查残留）: %v", runID, err)
		}
		pool.Close()
	})
	return store, pool, runID
}

// testTS 返回 2200 基准区里的第 i 个时间戳（见文件头第 2 条）。
func testTS(i int) time.Time {
	return time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
}

// msg 造一条信封。
func msg(topic string, ts time.Time, payload any) msgbus.Message {
	return msgbus.NewEnvelope(topic, ts, payload)
}

// ─── 建表 ──────────────────────────────────────────────────────────

// TestEnsureSchemaIsIdempotent EnsureSchema 会被反复调用（每次 Boot、每个
// 用例的 setup），不等价于 no-op 的语句迟早炸在一次不该炸的启动上。
func TestEnsureSchemaIsIdempotent(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("第 %d 次 EnsureSchema 失败（幂等性被破坏）: %v", i, err)
		}
	}
}

// TestMessageLogShapeMatchesContract 把表结构钉在契约上：
// contracts/kernel_modules.schema.sql 第 45-55 行的列清单与类型。
// 有人改了 DDL 忘了改契约（或反过来），这条会红。
func TestMessageLogShapeMatchesContract(t *testing.T) {
	_, pool, _ := newTestStore(t)
	ctx := context.Background()

	want := map[string]string{
		"id":        "bigint",
		"ts":        "timestamp with time zone",
		"topic":     "text",
		"payload":   "jsonb",
		"publisher": "text",
		"run_id":    "text",
	}

	rows, err := pool.Query(ctx, `SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'audit' AND table_name = 'message_log'`)
	if err != nil {
		t.Fatalf("查询 information_schema 失败: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("扫描列信息失败: %v", err)
		}
		got[name] = dataType
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("读取列信息失败: %v", err)
	}

	for name, dataType := range want {
		if got[name] != dataType {
			t.Errorf("audit.message_log.%s 类型 = %q, want %q（契约：contracts/kernel_modules.schema.sql）",
				name, got[name], dataType)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("audit.message_log 出现契约外列 %q（改表 = 改契约，须走变更评审）", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("列数 = %d, want %d（实际列: %v）", len(got), len(want), got)
	}
}

// ─── Append / Replay ──────────────────────────────────────────────

// barPayload 用来验证 JSON 往返：字段类型覆盖字符串 / 浮点 / 整型 / 布尔。
type barPayload struct {
	Symbol string  `json:"symbol"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
	IsST   bool    `json:"is_st"`
}

// TestAppendThenReplayRoundTrip 核心往返：写进去什么，回放出来还是什么，
// 一条不少、顺序不乱。
func TestAppendThenReplayRoundTrip(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	type probe struct {
		topic   string
		payload any
	}
	published := []probe{
		{msgbus.TopicDataBar, barPayload{Symbol: "600519.SH", Close: 1688.5, Volume: 12345, IsST: false}},
		{msgbus.TopicExecOrderIntent, barPayload{Symbol: "000001.SZ", Close: 11.02, Volume: 100, IsST: true}},
		{msgbus.TopicRiskVerdict, map[string]any{"verdict": "pass", "reason": nil}},
	}
	for i, p := range published {
		if err := store.Append(ctx, msg(p.topic, testTS(i), p.payload)); err != nil {
			t.Fatalf("Append 第 %d 条(%s) 失败: %v", i, p.topic, err)
		}
	}

	got, err := store.Replay(ctx, testTS(0), testTS(len(published)))
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(got) != len(published) {
		t.Fatalf("Replay 返回 %d 条, want %d（有消息丢了或多出来了）", len(got), len(published))
	}
	for i, want := range published {
		if got[i].Topic() != want.topic {
			t.Errorf("第 %d 条 topic = %q, want %q", i, got[i].Topic(), want.topic)
		}
		if !got[i].Ts().Equal(testTS(i)) {
			t.Errorf("第 %d 条 ts = %s, want %s（ts 是业务时间，必须原样回来）", i, got[i].Ts(), testTS(i))
		}
	}

	// JSON 往返：payload 必须能被反序列化回原类型且字段无损。
	var decoded barPayload
	raw, ok := got[0].Payload().(json.RawMessage)
	if !ok {
		t.Fatalf("Replay 的 Payload() 类型是 %T, want json.RawMessage（回放就该给原始 JSON，由调用方决定类型）",
			got[0].Payload())
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化首条 payload 失败: %v", err)
	}
	want0 := published[0].payload.(barPayload)
	if decoded != want0 {
		t.Errorf("payload 往返失真: got %+v, want %+v", decoded, want0)
	}
}

// TestReplayIsTsAscending 故意乱序写入，回放必须仍按 ts 升序。
// 回放必须「与时间流向一致」，否则断点续跑会把未来的消息先喂给订阅者。
func TestReplayIsTsAscending(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	// 乱序 Append：3, 1, 2 分钟。
	for _, i := range []int{3, 1, 2} {
		if err := store.Append(ctx, msg(msgbus.TopicDataBar, testTS(i), map[string]int{"i": i})); err != nil {
			t.Fatalf("Append(%d) 失败: %v", i, err)
		}
	}

	got, err := store.Replay(ctx, testTS(0), testTS(10))
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Replay 返回 %d 条, want 3", len(got))
	}
	want := []time.Time{testTS(1), testTS(2), testTS(3)}
	for i := range want {
		if !got[i].Ts().Equal(want[i]) {
			t.Errorf("第 %d 条 ts = %s, want %s（未按 ts 升序）", i, got[i].Ts(), want[i])
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i].Ts().Before(got[i-1].Ts()) {
			t.Errorf("回放乱序: 第 %d 条 %s 早于第 %d 条 %s", i, got[i].Ts(), i-1, got[i-1].Ts())
		}
	}
}

// TestReplayBoundaryIsClosed 契约写的是「[from, to] 闭区间（含两端）」——
// 端点上的消息必须回得来，这是 closed 与 half-open 的唯一区别。
func TestReplayBoundaryIsClosed(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	for i := 0; i <= 4; i++ {
		if err := store.Append(ctx, msg(msgbus.TopicDataBar, testTS(i), map[string]int{"i": i})); err != nil {
			t.Fatalf("Append(%d) 失败: %v", i, err)
		}
	}

	got, err := store.Replay(ctx, testTS(1), testTS(3))
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Replay([1,3]) 返回 %d 条, want 3（闭区间含 1 与 3）", len(got))
	}
	if got[0].Ts().Equal(testTS(1)) == false || got[2].Ts().Equal(testTS(3)) == false {
		t.Errorf("端点丢失: got [%s ... %s], want [%s ... %s]", got[0].Ts(), got[2].Ts(), testTS(1), testTS(3))
	}
}

// TestReplayEmptyWindow 空窗口返回空切片而不是 nil-error，也不是报错。
func TestReplayEmptyWindow(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	got, err := store.Replay(ctx, testTS(100), testTS(200))
	if err != nil {
		t.Fatalf("空窗口 Replay 失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("空窗口返回 %d 条, want 0", len(got))
	}
}

// TestReplayRejectsInvertedRange from 晚于 to 是调用方的错，Fail 比「回空」
// 好——回空会让「写反参数」看起来像「这段时间没消息」。
func TestReplayRejectsInvertedRange(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Replay(ctx, testTS(5), testTS(1)); err == nil {
		t.Error("Replay(from 晚于 to) 返回 nil error, want error")
	}
}

// TestAppendRejectsIllFormedMessage 三类「写下去就是脏行」的输入必须被挡住，
// 而不是退化成一行 topic 未知 / payload 未知的记录。
func TestAppendRejectsIllFormedMessage(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.Append(ctx, nil); !errors.Is(err, eventstore.ErrNilMessage) {
		t.Errorf("Append(nil) = %v, want errors.Is(err, ErrNilMessage)", err)
	}
	// 空 topic：未走注册表，落库后回放检索不到。
	if err := store.Append(ctx, msg("", testTS(0), "x")); !errors.Is(err, eventstore.ErrEmptyTopic) {
		t.Errorf("Append(topic=\"\") = %v, want errors.Is(err, ErrEmptyTopic)", err)
	}
	// 不可序列化的 payload：chan 没法 json.Marshal。
	if err := store.Append(ctx, msg(msgbus.TopicDataBar, testTS(0), map[string]any{"ch": make(chan int)})); !errors.Is(err, eventstore.ErrPayloadNotMarshalable) {
		t.Errorf("Append(payload=chan) = %v, want errors.Is(err, ErrPayloadNotMarshalable)", err)
	}

	// 三条都失败 => 一条都没落库（写入账应为 0）。
	if err := store.Verify(); err != nil {
		t.Errorf("被拒的输入竟然留下了痕迹: Verify = %v", err)
	}
}

// TestAppendNilPayloadBecomesJSONNull nil payload 落 `null` 而不是 SQL NULL——
// payload 列是 NOT NULL，写 NULL 会被约束挡下，届时错误发生在数据库层、
// 离业务语义很远。
func TestAppendNilPayloadBecomesJSONNull(t *testing.T) {
	store, pool, runID := newTestStore(t)
	ctx := context.Background()

	if err := store.Append(ctx, msg(msgbus.TopicRunStart, testTS(0), nil)); err != nil {
		t.Fatalf("Append(nil payload) 失败: %v", err)
	}

	var raw []byte
	if err := pool.QueryRow(ctx,
		`SELECT payload::text FROM audit.message_log WHERE run_id = $1`, runID).Scan(&raw); err != nil {
		t.Fatalf("读取 payload 原值失败: %v", err)
	}
	if string(raw) != "null" {
		t.Errorf("payload 原值 = %q, want \"null\"", string(raw))
	}
}

// TestAppendWritesPublisherAndRunID publisher / run_id 两列 Envelope 里看不到
// （接口没有这两个方法），只能直接查库——但它们是审计归因的全部依据，
// 必须确认真的写进去了。
func TestAppendWritesPublisherAndRunID(t *testing.T) {
	store, pool, runID := newTestStore(t)
	ctx := context.Background()

	if err := store.Append(ctx, msg(msgbus.TopicKernelBoot, testTS(0), map[string]string{"phase": "boot"})); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	var gotPublisher string
	var gotRunID pgtype.Text
	if err := pool.QueryRow(ctx,
		`SELECT publisher, run_id FROM audit.message_log WHERE run_id = $1`, runID,
	).Scan(&gotPublisher, &gotRunID); err != nil {
		t.Fatalf("读取 publisher/run_id 失败: %v", err)
	}
	if gotPublisher != testPublisher {
		t.Errorf("publisher = %q, want %q", gotPublisher, testPublisher)
	}
	if !gotRunID.Valid || gotRunID.String != runID {
		t.Errorf("run_id = %+v, want %q（每次 run 一个 id，审计靠它归因）", gotRunID, runID)
	}
}

// TestAppendWithEmptyRunIDWritesNULL 构造时给空 run_id => 落 NULL（run 建立
// 之前的早期消息）。it's 法定场景，不能出错也不能写成空串。
func TestAppendWithEmptyRunIDWritesNULL(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), testDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	t.Cleanup(pool.Close)

	runless, err := eventstore.NewPGEventStore(pool, testPublisher, "")
	if err != nil {
		t.Fatalf("NewPGEventStore 失败: %v", err)
	}
	if err := runless.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema 失败: %v", err)
	}

	// 同样用隔离区 ts；清理只能按 publisher + run_id IS NULL + ts 精确删，
	// 不能按 run_id IS NULL 扫（那会误删别人的早期消息）。
	ts := time.Date(2200, 6, 6, 6, 6, 6, 0, time.UTC)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx,
			`DELETE FROM audit.message_log WHERE publisher = $1 AND run_id IS NULL AND ts = $2`,
			testPublisher, ts); err != nil {
			t.Errorf("清理无 run_id 的测试数据失败: %v", err)
		}
	})

	ctx := context.Background()
	if err := runless.Append(ctx, msg(msgbus.TopicKernelBoot, ts, "boot")); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	var storedRunID pgtype.Text
	if err := pool.QueryRow(ctx,
		`SELECT run_id FROM audit.message_log WHERE publisher = $1 AND ts = $2`,
		testPublisher, ts).Scan(&storedRunID); err != nil {
		t.Fatalf("读取 run_id 失败: %v", err)
	}
	if storedRunID.Valid {
		t.Errorf("run_id = %q（Valid=%v）, want NULL", storedRunID.String, storedRunID.Valid)
	}
}

// ─── Append 的 ctx 语义（K0-P2-3）─────────────────────────────────

// TestAppendHonorsCallerContext —— Append 的 ctx 由调用方（msgbus.Publish）
// 逐消息传入，取消/超时必须真的传到落库这一步。
//
// 旧实现是 `context.WithTimeout(context.Background(), appendTimeout)`：调用方
// 完全没有取消权，「这一次发布要不要落库」无从表达。改完后：
//   - 已取消 / 已超时的 ctx ⇒ 不做任何 I/O，直接返回 error；
//   - 错误链上保留 ctx 的原始原因（errors.Is 到 context.Canceled /
//     DeadlineExceeded），总线一侧才能据此判定「零分发」；
//   - 一条都没写（写入账仍为 0，由 Verify 钉住）。
//
// 「调用方给的截止比 appendTimeout 更近时按更近的算」这一点由 deadline 子用例
// 覆盖：mergeTimeout 取父子中更早的截止，而不是拿 appendTimeout 覆盖调用方。
func TestAppendHonorsCallerContext(t *testing.T) {
	cases := []struct {
		name    string
		ctx     func() (context.Context, context.CancelFunc)
		wantErr error
	}{
		{
			name: "已取消",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr: context.Canceled,
		},
		{
			name: "截止已过",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _, _ := newTestStore(t)
			ctx, cancel := tc.ctx()
			defer cancel()

			err := store.Append(ctx, msg(msgbus.TopicDataBar, testTS(0), map[string]int{"i": 0}))
			if err == nil {
				t.Fatal("取消/超时的 ctx 上 Append 返回 nil——取消权没传到落库这一步")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Append = %v, want errors.Is(err, %v)", err, tc.wantErr)
			}
			// 一条都没写：写入账（本实例成功 Append 次数）必须仍为 0。
			if err := store.Verify(); err != nil {
				t.Errorf("Verify 失败——被取消的 Append 竟然留下了痕迹: %v", err)
			}
		})
	}
}

// ─── Verify ───────────────────────────────────────────────────────

// TestVerifyMatchesAppendCount Verify 的最小语义：本实例写入范围内可计数，
// 且与本实例成功 Append 的次数一致。
func TestVerifyMatchesAppendCount(t *testing.T) {
	store, _, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.Verify(); err != nil {
		t.Errorf("空账 Verify 失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := store.Append(ctx, msg(msgbus.TopicDataBar, testTS(i), map[string]int{"i": i})); err != nil {
			t.Fatalf("Append(%d) 失败: %v", i, err)
		}
	}
	if err := store.Verify(); err != nil {
		t.Errorf("写 5 条后 Verify 失败（写入账对不上）: %v", err)
	}
}

// TestVerifyDetectsMissingRow Verify 的防空转腿：手工删掉一行（模拟数据丢失 /
// 有人绕过本模块清理），Verify 必须红。一条永远绿的 Verify 等于没有 Verify。
func TestVerifyDetectsMissingRow(t *testing.T) {
	store, pool, runID := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := store.Append(ctx, msg(msgbus.TopicDataBar, testTS(i), map[string]int{"i": i})); err != nil {
			t.Fatalf("Append(%d) 失败: %v", i, err)
		}
	}
	if err := store.Verify(); err != nil {
		t.Fatalf("前置条件：删之前 Verify 就该通过，实际 = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tag, err := pool.Exec(ctx,
		`DELETE FROM audit.message_log WHERE ctid IN (
			SELECT ctid FROM audit.message_log WHERE run_id = $1 LIMIT 1)`, runID)
	if err != nil {
		t.Fatalf("造故障（删一行）失败: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("期望删掉 1 行，实际 %d 行（Delete 没生效的话这条测试是瞎的）", tag.RowsAffected())
	}

	err = store.Verify()
	if err == nil {
		t.Fatal("少了一行 Verify 仍然返回 nil——Verify 是瞎的")
	}
	if !errors.Is(err, eventstore.ErrIntegrity) {
		t.Errorf("Verify = %v, want errors.Is(err, ErrIntegrity)", err)
	}
}

// ─── 构造函数边界 ──────────────────────────────────────────────────

// TestNewPGEventStoreValidatesInputs 构造期就把话说清楚，别等到某次 Publish
// 才在无关现场炸。
func TestNewPGEventStoreValidatesInputs(t *testing.T) {
	if _, err := eventstore.NewPGEventStore(nil, testPublisher, "r"); err == nil {
		t.Error("NewPGEventStore(pool=nil) 返回 nil error, want error")
	}
	pool, err := pgxpool.New(context.Background(), testDSN(t))
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer pool.Close()

	if _, err := eventstore.NewPGEventStore(pool, "", "r"); err == nil {
		t.Error("NewPGEventStore(publisher=\"\") 返回 nil error, want error（列是 NOT NULL）")
	}
	if _, err := eventstore.NewPGEventStore(pool, testPublisher, ""); err != nil {
		t.Errorf("NewPGEventStore(runID=\"\") = %v, want nil（空 run_id 是合法场景）", err)
	}
}

// ─── 与 msgbus 的契约实现相符 ───────────────────────────────────────

// TestEnvelopeImplementsMessage Replay 返回的是 msgbus.Envelope，这里确认它
// 确实满足 msgbus.Message——PGEventStore 依赖这个成立。
func TestEnvelopeImplementsMessage(t *testing.T) {
	e := msgbus.NewEnvelope(msgbus.TopicDataBar, testTS(0), "payload")
	var m msgbus.Message = e
	if m.Topic() != msgbus.TopicDataBar {
		t.Errorf("Topic() = %q, want %q", m.Topic(), msgbus.TopicDataBar)
	}
	if !m.Ts().Equal(testTS(0)) {
		t.Errorf("Ts() = %s, want %s", m.Ts(), testTS(0))
	}
	if m.Payload() != "payload" {
		t.Errorf("Payload() = %v, want \"payload\"", m.Payload())
	}
}
