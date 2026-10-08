package eventstore

// ─── audit.message_log 的幂等建表能力 ─────────────────────────────
//
// 与 pkg/storage/postgres.go 内联 DDL 的关系（本切片不做合并）：
//
// 项目加表的唯一执行路径是 pkg/storage/postgres.go 的 migrate() 内联数组
// （见该文件中长达 40 行的取舍说明：golang-migrate 从未被调用，migrations/
// 只是历史记录）。按规矩这张表**最终**应该并进那个数组（Migration 048）。
//
// 本切片的做法是：把 DDL 放在本包自己的 EnsureSchema() 里，由 kernel Boot
// 的第一步调用（蓝图 §4.2：EventStore.Init 必须先于一切，否则启动期消息无
// 审计丢失）。两条理由：
//  1. **边界**：本切片的验收范围是 eventstore 这一个包。改 pkg/storage 的
//     migrate() 数组会牵动全服务的启动路径，属于「切面外的扩散」。
//  2. **自证**：测试要能独立把表建起来（CI 的 postgres 服务是空库），不依赖
//     「服务先跑过一遍 migrate」这种隐式前提。
//
// **合并时机 = 后续切片**（届时把 SchemaDDL 的每条逐字追加进 migrate() 数组，
// 并保留 EnsureSchema 作为「独立可安装」的入口；本文件 DDL 与
// contracts/kernel_modules.schema.sql 的 audit 段逐字一致，以契约文件为准）。
//
// DDL 来源：contracts/kernel_modules.schema.sql 第 43-55 行（K0 冻结）。

import (
	"context"
	"fmt"
	"time"
)

// SchemaDDL 是 audit schema + message_log 表 + 2 个索引的建表语句，逐字取自
// contracts/kernel_modules.schema.sql。每条都必须幂等（IF NOT EXISTS）——
// EnsureSchema 会被反复调用（每个测试的 setup、每次 Boot），不等价于
// no-op 的语句迟早会炸在一次不该炸的启动上。
//
// 顺序有意义：schema 先于表，表先于索引。
var SchemaDDL = []string{
	// pkg/storage/postgres.go 现在只建 ingest / research 两个 schema，audit
	// 得自己补。
	`CREATE SCHEMA IF NOT EXISTS audit`,

	// BusTap「先记录后分发」的落点。列清单是 K0 冻结契约，改列 = 改契约。
	//   ts        —— **业务时间**（回测=VirtualClock 当前值，实盘=墙钟），
	//                不是落库时间；Replay 按它检索。
	//   payload   —— jsonb，序列化方式见 PGEventStore.Append 注释。
	//   publisher —— 写入方标识（本 store 实例持有，Message 接口里没有）。
	//   run_id    —— 一次 run 的归因锚点，可空（run 建立前的早期消息）。
	`CREATE TABLE IF NOT EXISTS audit.message_log (
		id         BIGSERIAL PRIMARY KEY,
		ts         TIMESTAMPTZ NOT NULL,
		topic      TEXT NOT NULL,
		payload    JSONB NOT NULL,
		publisher  TEXT NOT NULL,
		run_id     TEXT
	)`,

	// 按 ts 范围回放（Replay）与「按 topic + 时间窗」审计都走这两条索引。
	`CREATE INDEX IF NOT EXISTS idx_message_log_ts ON audit.message_log (ts)`,
	`CREATE INDEX IF NOT EXISTS idx_message_log_topic_ts ON audit.message_log (topic, ts)`,
}

// ensureTimeout 建表是一次性动作，给它一个独立于单次 Append 的上限。
const ensureTimeout = 30 * time.Second

// EnsureSchema 幂等建立 audit schema 与 message_log 表。**对应 kernel Boot
// 的第 1 步**（EventStore.Init）：先开记录，后面 9 步发出的任何消息才有地方
// 落，否则启动期消息静默丢失。
//
// 调用时机由调用方决定，NewPGEventStore **不**隐式建表：构造函数做 I/O 会让
// 「连不上库」伪装成「构造失败」，而它本该是一条可被 Boot 顺序捕获的、有明确
// 语义的步骤；显式调用也让测试能复用一次安装。
func (s *PGEventStore) EnsureSchema(ctx context.Context) error {
	ctx, cancel := mergeTimeout(ctx, ensureTimeout)
	defer cancel()

	for i, ddl := range SchemaDDL {
		if _, err := s.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("eventstore: EnsureSchema 第 %d 条 DDL 失败: %w", i+1, err)
		}
	}
	return nil
}
