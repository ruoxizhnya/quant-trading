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
// K0 切片 1：本文件只冻结接口契约。stub 方法体固定
// panic("contract stub: not implemented")，不含任何实现逻辑。
package eventstore

import (
	"context"
	"time"

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

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// PGEventStore 是 K1 PostgreSQL 实现（audit.message_log）的契约 stub。
type PGEventStore struct{}

// Append 先落库后分发地记录一条消息。
func (s *PGEventStore) Append(msg msgbus.Message) error {
	panic("contract stub: not implemented")
}

// Replay 回放 [from, to] 闭区间的落库消息。
func (s *PGEventStore) Replay(ctx context.Context, from, to time.Time) ([]msgbus.Message, error) {
	panic("contract stub: not implemented")
}

// Verify 校验落库完整性。
func (s *PGEventStore) Verify() error { panic("contract stub: not implemented") }

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
