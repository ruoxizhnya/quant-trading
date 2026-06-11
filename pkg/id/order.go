// Package id — Sprint 6 P1-23 (ODR-013, ADR-018)
//
// ID 生成统一包。0 此包前 9 处调用 `uuid.New().String()`
// 散落在 pkg/live / pkg/backtest / pkg/sync / pkg/strategy
// / pkg/ai/pipeline / cmd/execution 等模块，且全部使用
// UUID v4（随机）。问题：
//
//  1. 不可排序：DB 按 v4 随机主键插入，B-tree 页分裂严重，
//     订单量大时 insert tps 退化。
//  2. 不可推断顺序：运维查 "上午 10:00 下的单" 只能 SELECT
//     ORDER BY created_at，无法按主键。
//  3. 不一致：3 套订单号生成路径（live.MockTrader、
//     live.persistentMockTrader、cmd/execution）独立调用，
//     未来加 trace_id 时必须各自改。
//
// 解决
// ----
// `github.com/google/uuid` v1.6+ 支持 UUID v7 —
// 前 48 bit 是 unix_ms 时间戳，可按时间排序且 DB 插入有序。
// 本包提供 3 个 helper：
//
//	id.OrderID()   — 订单 ID（高频，DB 主键，必须 v7 排序）
//	id.JobID()     — 异步任务 ID（Copilot / sync / backtest）
//	id.SubscriptionID() — pub/sub 频道 ID（低频）
//
// 全部走 uuid.NewV7()；如未来要加 trace_id / region 等
// 前缀，可在此包集中加，不污染调用方。
package id

import (
	"github.com/google/uuid"
)

// OrderID returns a new UUID v7 suitable for use as the
// primary key of an `orders` / `order_results` table.
//
// Rationale for v7 over v4:
//   - The first 48 bits are unix_ms timestamp → DB inserts
//     append to the rightmost B-tree page, avoiding page
//     splits at high write throughput.
//   - Operators can `ORDER BY order_id` and get chronological
//     order without a separate `created_at` index.
//
// All call sites that previously used uuid.New().String() for
// order IDs should be migrated here. Backtest and SyncJob IDs
// use the same primitive (JobID) because they share the
// same DB storage shape and the same ordering benefit.
func OrderID() string {
	return newV7()
}

// JobID is the async-job analog of OrderID — backtest jobs,
// Copilot jobs, ETL sync jobs. Same v7 primitive because the
// DB column types are identical (VARCHAR(64) PRIMARY KEY) and
// the ordering benefit is identical.
func JobID() string {
	return newV7()
}

// SubscriptionID is used as a key in the in-memory pub/sub
// registry (e.g. advanced_mock_trader.Subscribe). It is
// never persisted, so v7 is just a defensive default —
// collisions are vanishingly rare regardless of version.
func SubscriptionID() string {
	return newV7()
}

// newV7 centralizes the uuid.NewV7() call so we can
// switch to a future uuid.NewV8() (or a Snowflake
// alternative) by editing one line.
//
// Returns the canonical 36-char string form
// (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func newV7() string {
	u, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 can only fail on a system time error
		// (clock went backward past 1970). This is a
		// process-fatal condition; falling back to v4 would
		// silently lose the ordering guarantee. Surface the
		// panic so a misconfigured NTP is loud, not silent.
		panic("id: UUID v7 generation failed (system clock?): " + err.Error())
	}
	return u.String()
}
