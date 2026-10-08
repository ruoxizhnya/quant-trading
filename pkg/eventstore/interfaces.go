// Package eventstore 是内核的消息事件存储：所有模块间消息
// 先落库后分发（BusTap 语义），供审计、回放与调试。
//
// DB 归属（冻结）：PostgreSQL 表 audit.message_log（DDL 冻结于
// contracts/kernel_modules.schema.sql，K0 切片 1）。该表的唯一写者
// 是本模块（经 msgbus 的派发前钩子），禁止任何他处双写——
// 遵循项目数据归属铁律（AGENTS.md §2/§7）。
//
// 冻结语义（蓝图 §5 eventstore 行 + D4 拍板）：
//   - Append 在消息派发给订阅者之前落库——订阅者看不到未落库的
//     消息。这是「先记录后分发」的 BusTap 契约，也是 EventStore
//     必须第一个 Boot 的原因（蓝图 §4.2：否则启动期消息无审计丢失）。
//   - 全量落库（D4）：所有 topic 一律记录，retention 机制留作将来。
//   - Replay 按 [from, to] 闭区间回放落库消息，用于审计/调试/断点续跑。
//   - Verify 校验落库完整性（如 id 连续性 / ts 单调 / 期望条数对账），
//     K1 实现。
//
// K0 切片 1 冻结了本包的接口契约（EventStore；stub 方法体固定
// panic("contract stub: not implemented")）。K1 切片 1 已把 stub 替换为
// 真实实现（PGEventStore），契约半个字未改。
//
// ─── 导入方向（写 SyncBus 之前先看这条） ────────────────────────────
// 本包 import pkg/msgbus（Message 接口与 Envelope 在那儿）。因此 pkg/msgbus
// **不得** import 本包——那会是导入环。后果：SyncBus 的构造参数类型不能写成
// eventstore.EventStore，而由消费方 msgbus 自己定义一个窄接口（msgbus.Tap，
// 只有一个 Append(Message) error）。eventstore.EventStore 天然满足它（Go
// 结构化类型），由 pkg/msgbus 测试里的静态断言钉住。
package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// EventStore 是消息事件存储的统一抽象。
type EventStore interface {
	// Append 落库一条消息。先记录后分发：本调用成功后，msgbus 才会
	// 把该消息派发给订阅者；本调用失败则消息不得派发（也不得静默
	// 丢弃——由调用方决定终止）。
	Append(msg msgbus.Message) error

	// Replay 回放 [from, to] 闭区间（含两端）内落库的消息，按落库
	// 顺序（id 升序）返回。用于审计、调试与断点续跑。
	Replay(ctx context.Context, from, to time.Time) ([]msgbus.Message, error)

	// Verify 校验落库完整性（id 连续性 / ts 单调 / 条数对账等）。
	// K1 实现；实现细节由 K1 的测试契约冻结。
	Verify() error
}

// ─── K1 实现（替换 K0 的 panic stub，契约不变） ─────────────────────

// 哨兵 error：调用方用 errors.Is 判定，不要比字符串。
var (
	// ErrNilMessage：Append(nil)。消息本身是空指针的话，落下去的是一行
	// 「topic 未知 / ts 未知」的垃圾行，回放时无法归因——宁 Fail 不脏写。
	ErrNilMessage = errors.New("eventstore: 消息为 nil")

	// ErrEmptyTopic：消息的 topic 是空串。audit.message_log.topic 是 NOT NULL
	// TEXT，空串能落库，但它代表「发布方没走注册表」，回放时检索不到。
	ErrEmptyTopic = errors.New("eventstore: 消息 topic 为空（必须取自 msgbus topics 注册表）")

	// ErrPayloadNotMarshalable：payload 序列化失败（例如带 channel / func 的
	// 结构体）。BusTap 的「先记录」要求记录必须成功才能派发，序列化失败就是
	// 记录失败，必须往上抛，不能退化为「跳过记录照样派发」。
	ErrPayloadNotMarshalable = errors.New("eventstore: payload 无法 JSON 序列化")

	// ErrIntegrity：Verify 发现落库与本次写入账不一致。
	ErrIntegrity = errors.New("eventstore: 落库与本次写入账不一致")
)

// 单次 DB 操作的上限。Append/Replay/Verify 三个方法 **契约里都没有 ctx 参数**，
// 因此内部一律自建带超时的 context：既不让调用方拿到取消权（契约没给），
// 也不让一次卡住的 SQL 把整个内核挂死（挂死 = 消息停止派发 = 静默停摆，
// 比报错糟得多）。
const (
	appendTimeout = 10 * time.Second
	replayTimeout = 30 * time.Second
	verifyTimeout = 30 * time.Second
)

// appendSQL —— INSERT 一条消息。参数顺序与 Audit 列一一对应：
// $1 ts / $2 topic / $3 payload / $4 publisher / $5 run_id。
//
// payload 传 []byte 而不是 string / pgtype.JSONB：pgx v5 的 JSONBCodec 对
// []byte 的处理是「原样塞进去」（pgtype/json.go 的
// encodePlanJSONCodecEitherFormatByteSlice），正是我们要的语义——序列化由
// 我们自己做完（见 marshalPayload），pgx 只当搬运工。
const appendSQL = `INSERT INTO audit.message_log (ts, topic, payload, publisher, run_id)
	VALUES ($1, $2, $3, $4, $5)`

// replaySQL —— 按 [from, to] 闭区间回放。
//
// ORDER BY ts ASC, id ASC：契约要求「按 ts 升序」；ts 相同时（同一虚拟时刻
// 的两笔 tick / 同一 ns 内连发两条）用落库顺序 id 兜底，**保证回放结果的
// 顺序唯一确定**。没有 id 这一维，同 ts 的行在 PG 里的返回顺序是实现细节
// （取决于计划类型与物理布局），两次回放可能给出两种顺序——那会直接污染
// 「断点续跑」与审计比对。
const replaySQL = `SELECT ts, topic, payload
	FROM audit.message_log
	WHERE ts >= $1 AND ts <= $2
	ORDER BY ts ASC, id ASC`

// verifyCountSQL —— Verify 的计数口径：**只数本实例写的那些行**
// （publisher + run_id 同时命中）。理由：Verify 的语义是「本次写入账能不
// 能对上」，而不是「全表有多少行」——别的 run / 别的发布者写进来的行不归
// 本实例管，把它们算进来会让 Verify 在共享库里恒失败，进而被人关掉。
// run_id 为 NULL 时走 $2 IS NULL 分支。
const verifyCountSQL = `SELECT count(*)
	FROM audit.message_log
	WHERE publisher = $1 AND run_id IS NOT DISTINCT FROM $2`

// verifyCorruptSQL —— 顺带查「结构性损坏」：topic 空 / payload 为 NULL 的
// 行。这两列都是 NOT NULL，正常路径写不出来；一旦出现说明有别的东西绕过
// 本模块写了这张表（数据归属铁律被破坏），必须 fail-loud。
const verifyCorruptSQL = `SELECT count(*)
	FROM audit.message_log
	WHERE publisher = $1 AND run_id IS NOT DISTINCT FROM $2
	  AND (btrim(topic) = '' OR payload IS NULL)`

// PGEventStore 是 audit.message_log 的 PostgreSQL 实现（K1）。
//
// 无全局单例（ADR-027）：连接池由调用方注入，典型写法是把
// pkg/storage.PostgresStore.DB() 那个 pool 传进来。进程里可以有多个实例
// （不同 run / 不同 publisher），彼此互不影响。
type PGEventStore struct {
	pool *pgxpool.Pool
	// publisher 是「谁在写」的落库标识，对应 audit.message_log.publisher
	//（NOT NULL）。契约里 msgbus.Message **没有** Publisher() 方法，所以
	// 发布者身份只能由 store 实例持有：通常一个进程一条内核管线 = 一个
	// publisher（如 "kernel" / "msgbus"）。将来若需要「每条消息不同发布者」，
	// 走变更评审给 Message 加 Publisher()，而不是在这里做隐式推断。
	publisher string
	// runID 是一次回测 / 实盘 run 的归因 id，可空（-> NULL）。
	runID string
	// appended 是本实例成功 Append 的条数。用途单一：给 Verify 做「写入账」
	// 的对账基准。用 atomic 而不是普通 int：契约只保证 Publish 的**分发**是
	// 同步的，没要求整个进程单线程，Append 完全可能被多个 goroutine 调用。
	appended atomic.Int64
}

// NewPGEventStore 注入连接池构造。publisher 不得为空（列是 NOT NULL，且空
// 发布者无法归因）。runID 传空串表示落 NULL（run 未建立时的早期消息）。
//
// pool 为 nil 时返回 error 而不是留着到下一条 Publish 才炸：构造期失败离错
// 误最近，且 Kernel Boot 的第一步（EventStore.Init）就能把它挡下来。
func NewPGEventStore(pool *pgxpool.Pool, publisher, runID string) (*PGEventStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("eventstore: 连接池为 nil（注入 pkg/storage 的 pool）")
	}
	if publisher == "" {
		return nil, fmt.Errorf("eventstore: publisher 不得为空（audit.message_log.publisher 是 NOT NULL）")
	}
	return &PGEventStore{pool: pool, publisher: publisher, runID: runID}, nil
}

// Append 落库一条消息。**BusTap 语义的「先记录」这一半** ：本调用成功返回
// 之后，调用方（msgbus.Publish）才允许把消息派发给订阅者；失败则调用方必须
// 中止派发——消息既不能丢，也不能「没记录就广播出去」（那样审计会缺一行，
// 而缺口只有在回放对账时才会暴露，现场早就没了）。
//
// payload 序列化方式（本处定死并写进注释，免得每个调用方各猜一套）：
//   - 一律 encoding/json.Marshal，落 jsonb；
//   - nil payload → JSON `null`（合法 jsonb，回放时 Payload() 返回 nil）；
//   - json.RawMessage 原样透传（Marshal 不做二次加工）；
//   - 序列化失败（chan / func / 循环引用）→ 包 ErrPayloadNotMarshalable 返回
//     error，**绝不**降级为「跳过记录」或「记个大括号」。
func (s *PGEventStore) Append(msg msgbus.Message) error {
	if msg == nil {
		return ErrNilMessage
	}
	topic := msg.Topic()
	if topic == "" {
		return ErrEmptyTopic
	}
	payload, err := marshalPayload(msg.Payload())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), appendTimeout)
	defer cancel()

	// ts 归一到 UTC：回测时间来自 VirtualClock（已是 UTC），实盘是墙钟（带
	// 时区）。TIMESTAMPTZ 本身存瞬时、与时区无关，归一只是为了让传参确定性。
	if _, err := s.pool.Exec(ctx, appendSQL,
		msg.Ts().UTC(), topic, payload, s.publisher, textOrNull(s.runID),
	); err != nil {
		return fmt.Errorf("eventstore: Append(%s) 落库失败（调用方不得派发该消息）: %w", topic, err)
	}
	s.appended.Add(1)
	return nil
}

// Replay 回放 [from, to] 闭区间（含两端）的落库消息，按 ts 升序（同 ts 按
// id 升序兜底，见 replaySQL 注释）。
//
// Payload() 返回 **json.RawMessage**（原始 JSON 字节），而不是反序列化后
// 的 map —— 理由：回放的职责是把当初那条消息原样交回来，调用方才知道该
// 反序列化成什么类型（map 会把 int64 精度、字段顺序、null vs 缺失抹掉）。
// 调用侧写法：json.Unmarshal(msg.Payload().(json.RawMessage), &myStruct)。
func (s *PGEventStore) Replay(ctx context.Context, from, to time.Time) ([]msgbus.Message, error) {
	ctx, cancel := mergeTimeout(ctx, replayTimeout)
	defer cancel()

	if to.Before(from) {
		return nil, fmt.Errorf("eventstore: Replay 区间非法 from=%s 晚于 to=%s",
			from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	}

	rows, err := s.pool.Query(ctx, replaySQL, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("eventstore: Replay(%s, %s) 查询失败: %w", from, to, err)
	}
	defer rows.Close()

	var out []msgbus.Message
	for rows.Next() {
		var (
			ts      time.Time
			topic   string
			payload []byte
		)
		if err := rows.Scan(&ts, &topic, &payload); err != nil {
			return nil, fmt.Errorf("eventstore: Replay 扫描行失败: %w", err)
		}
		// 深拷贝：pgx 的行缓冲在 rows.Next() 之后会被复用。
		out = append(out, msgbus.NewEnvelope(topic, ts.UTC(), cloneRaw(payload)))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventstore: Replay 读取结果集失败: %w", err)
	}
	return out, nil
}

// Verify 校验落库完整性。**K1 的最小可用边界**（不做什么写在下面）：
//
//	做：
//	  1. 本实例写入范围内的行数 == 本实例成功 Append 的次数（写入账对账）；
//	  2. 范围内没有结构性损坏行（topic 空 / payload 为 NULL）。
//
//	不做（明确留作将来，K1 不做以保证 Victim-less 的最小实现）：
//	  - 哈希链 / 逐条 checksum（那需要额外的列与「上一条 hash」的写入
//	    协议，改的是 contracts 里的 DDL，属于契约变更，不在本切片范围）；
//	  - id 连续性检查（行被运维删过是正常的 retention 行为，连续性与「完
//	    整性」不是一回事，误报会训练人忽略红灯）；
//	  - ts 单调性检查（消息本身就是按业务时间乱序到达的，ts 天然不单调）。
//
// 注意：本方法只数与本实例 (publisher, run_id) 匹配的行，所以若有人手工
// DELETE 了这些行，Verify 会失败——这是刻意的：那确实就是数据丢了，红灯是
// 对的。测试里清理数据必须在 Verify 断言之后做（或另起 store）。
func (s *PGEventStore) Verify() error {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()

	want := s.appended.Load()

	var got, corrupt int64
	if err := s.pool.QueryRow(ctx, verifyCountSQL, s.publisher, textOrNull(s.runID)).Scan(&got); err != nil {
		return fmt.Errorf("eventstore: Verify 计数失败: %w", err)
	}
	if got != want {
		return fmt.Errorf("eventstore: 本实例写入账不一致——落库 %d 行，成功 Append %d 次: %w",
			got, want, ErrIntegrity)
	}
	if err := s.pool.QueryRow(ctx, verifyCorruptSQL, s.publisher, textOrNull(s.runID)).Scan(&corrupt); err != nil {
		return fmt.Errorf("eventstore: Verify 损坏行检查失败: %w", err)
	}
	if corrupt != 0 {
		return fmt.Errorf("eventstore: %d 行结构损坏（topic 为空或 payload 为 NULL）——可能有他处绕过本模块写 audit.message_log: %w",
			corrupt, ErrIntegrity)
	}
	return nil
}

// marshalPayload 见 Append 注释的「序列化方式」一条。
// payload 为 nil 时返回 JSON null 而不是 Go 的 nil 切片——后者会让 pgx 写
// 入 SQL NULL，撞上 payload 列的 NOT NULL 约束。
func marshalPayload(payload any) ([]byte, error) {
	if payload == nil {
		return []byte("null"), nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPayloadNotMarshalable, err)
	}
	return b, nil
}

// textOrNull 把 runID 转成 PG 的 TEXT / NULL：空串视作「尚未建立 run」。
func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// cloneRaw 深拷贝一行 JSON，避免调用方拿到指向 pgx 行缓冲的切片。
func cloneRaw(b []byte) json.RawMessage {
	if b == nil {
		return nil
	}
	out := make(json.RawMessage, len(b))
	copy(out, b)
	return out
}

// mergeTimeout 给调用方传来的 ctx 套一个上限：Replay 契约上有 ctx，但调用方
// 完全可能传 context.Background()——不能因为调用方没设截止就让它无限期挂着。
func mergeTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), d)
	}
	return context.WithTimeout(ctx, d)
}

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 方法表达式守卫：从 EventStore 接口删除任一方法，本包
// go build 直接编译失败（防「删方法后测试仍绿」）。
var (
	_ EventStore = (*PGEventStore)(nil)

	_ func(EventStore, msgbus.Message) error                                            = EventStore.Append
	_ func(EventStore, context.Context, time.Time, time.Time) ([]msgbus.Message, error) = EventStore.Replay
	_ func(EventStore) error                                                            = EventStore.Verify
)
