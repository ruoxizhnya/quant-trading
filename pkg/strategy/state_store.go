// strategy-runtime 状态持久化（K2 切片 3）—— quant.strategy_state 的落库接线。
//
// 本文件把流式策略的检查点（BarHandler.SaveState 的原始字节）落到真库，供
// 断点续跑与回测-实盘迁移。落位理由：quant.strategy_state 的 DB 归属是
// **strategy-runtime 模块**（蓝图 §5 归属矩阵），它不走 pkg/storage 的应用层
// store——与 pkg/eventstore 是同一个先例：「内核模块自带 store」。
//
// ─── 与 pkg/storage/postgres.go migrate() 的关系（本切片不做合并） ──
//
// 项目加表的唯一执行路径是 pkg/storage/postgres.go 的 migrate() 内联数组
// （见该文件中长达 40 行的取舍说明：golang-migrate 从未被调用，migrations/
// 只是历史记录）。按规矩这张表**最终**应该并进那个数组。
//
// 本切片的做法照抄 pkg/eventstore/schema.go 的先例：把 DDL 放在本包自己的
// EnsureSchema() 里显式调用（kernel Boot 装配时调，测试 setup 时调），理由：
//  1. **边界**：本切片的验收范围是 pkg/strategy 一个包。改 pkg/storage 的
//     migrate() 数组会牵动全服务的启动路径，属于「切面外的扩散」。
//  2. **自证**：测试要能独立把表建起来（CI 的 postgres 服务是空库），不依赖
//     「服务先跑过一遍 migrate」这种隐式前提。
//
// **合并时机 = 后续切片**（届时把 SchemaDDL 的每条逐字追加进 migrate() 数组，
// 并保留 EnsureSchema 作为「独立可安装」的入口；本文件 DDL 与
// contracts/kernel_modules.schema.sql 第 184-197 行逐字一致，以契约文件为准）。
//
// ─── 裁决：contracts 里「历史版本」那句话与 PK/upsert 矛盾 ──────────
// contracts 第 184-197 行的注释写「同一个 (策略, run) 只保留最新一份（upsert），
// 历史版本留断点续跑时按 created_at 取」——后半句与 PRIMARY KEY
// (strategy_id, run_id) + upsert 语义**自相矛盾**：PK 决定了一个 (策略, run)
// 在表里只能有一行，upsert 只保留最新，历史版本根本不存在，无从「按
// created_at 取」。**以 PK/upsert 为准**（本次裁决，见 Save 注释）：只保留最新
// 一份；断点续跑读到的就是最新检查点，不需要也不存在历史版本检索。
package strategy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaDDL 是 quant schema + quant.strategy_state 表 + 1 个索引的建表语句，
// 逐字取自 contracts/kernel_modules.schema.sql 第 184-197 行（K0 冻结）。
// 每条都必须幂等（IF NOT EXISTS）——EnsureSchema 会被反复调用（每个测试的
// setup、每次 kernel 装配），不等价于 no-op 的语句迟早会炸在一次不该炸的
// 启动上。
//
// 顺序有意义：schema 先于表，表先于索引。
var SchemaDDL = []string{
	// pkg/storage/postgres.go 现在只建 ingest / research 两个 schema，
	// quant 得自己补（全仓库无第二处 CREATE SCHEMA quant）。
	`CREATE SCHEMA IF NOT EXISTS quant`,

	// 一个 run 可跑多个策略，状态按「策略 × run」定位；同一个 (策略, run)
	// 只保留最新一份（upsert）。列清单是 K0 冻结契约，改列 = 改契约。
	//   ts    —— **状态对应时刻**（回测 = VirtualClock 虚拟时间，
	//            实盘 = 墙钟），续跑按它跳过已处理的 bar 前缀。
	//   state —— SaveState() 的原始字节，不解释内容。
	`CREATE TABLE IF NOT EXISTS quant.strategy_state (
		strategy_id TEXT NOT NULL,
		run_id      TEXT NOT NULL,
		ts          TIMESTAMPTZ NOT NULL,
		state       BYTEA NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (strategy_id, run_id)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_strategy_state_run ON quant.strategy_state (run_id)`,
}

// ensureTimeout 建表是一次性动作，给它一个独立于单次 Save/Load 的上限。
const ensureTimeout = 30 * time.Second

// saveTimeout / loadTimeout 是单次 DB 操作的上限，沿用 eventstore 的
// mergeTimeout(ctx, timeout) 兜底模式（eventstore 的那个函数未导出，本包
// 自写等价小函数 mergeTimeout，见文件末）。
const (
	saveTimeout = 10 * time.Second
	loadTimeout = 10 * time.Second
)

// ErrStateNotFound：Load 在 quant.strategy_state 里查不到 (strategy_id, run_id)
// 对应的检查点。fail-loud 哨兵——调用方用 errors.Is 判定，**不要**把它当成
// 「空状态从头跑」的静默信号：断点续跑路径必须显式区分「首次运行（无行）」
// 与「数据丢失 / 键写错（也无行但语义完全不同）」。
var ErrStateNotFound = errors.New("strategy: 指定 (strategy_id, run_id) 无检查点")

// StateStore 持久化流式策略检查点（quant.strategy_state）。
//
// 落位理由：strategy-runtime 模块的 DB 归属（蓝图 §5 矩阵），不走 pkg/storage
// 应用层 store——与 pkg/eventstore 同为「内核模块自带 store」先例。
type StateStore interface {
	// Save 落库一份检查点：ts 是状态对应时刻，state 是 SaveState() 的原始字节。
	// 同一 (strategyID, runID) 重复 Save 是 UPSERT（只保留最新一份）。
	Save(ctx context.Context, strategyID, runID string, ts time.Time, state []byte) error

	// Load 读出 (strategyID, runID) 的最新检查点。无行时返回 ErrStateNotFound
	// （fail-loud，不静默返回空）。
	Load(ctx context.Context, strategyID, runID string) (state []byte, ts time.Time, err error)
}

// PGStateStore 是 quant.strategy_state 的 PostgreSQL 实现（pgxpool）。
//
// 无全局单例（ADR-027）：连接池由调用方注入，典型写法是把
// pkg/storage.PostgresStore.DB() 那个 pool 传进来。进程里可以有多个实例
// （不同 run / 不同策略），彼此互不影响。
type PGStateStore struct {
	pool *pgxpool.Pool
}

// NewPGStateStore 注入连接池构造。pool 为 nil 时返回 error 而不是留着到下一条
// Save/Load 才炸：构造期失败离错误最近，且 kernel 装配的第一步就能挡下来
// （同 eventstore.NewPGEventStore 对 nil pool 的处理）。
//
// 构造函数**不**隐式建表（同 eventstore 先例）：构造期做 I/O 会让「连不上库」
// 伪装成「构造失败」，而它本该是一条可被装配顺序捕获的、有明确语义的步骤。
// 建表走显式的 EnsureSchema，调用时机由调用方决定。
func NewPGStateStore(pool *pgxpool.Pool) (*PGStateStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("strategy: PGStateStore 连接池为 nil（注入 pkg/storage 的 pool）")
	}
	return &PGStateStore{pool: pool}, nil
}

// EnsureSchema 幂等建立 quant schema 与 quant.strategy_state 表（+索引）。
//
// 调用时机由调用方决定，构造函数不隐式建表（见 NewPGStateStore 注释）。
func (s *PGStateStore) EnsureSchema(ctx context.Context) error {
	ctx, cancel := mergeTimeout(ctx, ensureTimeout)
	defer cancel()

	for i, ddl := range SchemaDDL {
		if _, err := s.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("strategy: PGStateStore.EnsureSchema 第 %d 条 DDL 失败: %w", i+1, err)
		}
	}
	return nil
}

// saveSQL —— UPSERT 一份检查点。参数顺序：$1 strategyID / $2 runID / $3 ts / $4 state。
//
// ON CONFLICT (strategy_id, run_id) DO UPDATE：PK 决定一个 (策略, run) 只有一行，
// 重复 Save 覆盖 ts / state / created_at——**只保留最新**。contracts 注释里
// 「历史版本按 created_at 取」那句与 PK/upsert 矛盾，本实现以 PK/upsert 为准
// （见文件头裁决）。
const saveSQL = `INSERT INTO quant.strategy_state (strategy_id, run_id, ts, state)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (strategy_id, run_id)
	DO UPDATE SET ts = EXCLUDED.ts, state = EXCLUDED.state, created_at = now()`

// loadSQL —— 取 (strategy_id, run_id) 的最新检查点。
const loadSQL = `SELECT ts, state
	FROM quant.strategy_state
	WHERE strategy_id = $1 AND run_id = $2`

// Save 落库一份检查点（UPSERT，只保留最新）。
//
// Fail-loud 拒绝（宁 Fail 不脏写）：
//   - state 为 nil / 空：空状态读出来会让续跑**静默从头跑**（Load 拿到空字节
//     交给 LoadState，若实现不校验就会当成初始态），错误发生在很远的地方；
//   - ts 为零值时间：零值 ts 写库后，续跑按 `Date <= ts` 跳过前缀会一根都不跳
//     （或跳得莫名其妙），跳过逻辑失效。
//
// ts 归一到 UTC 后落库（TIMESTAMPTZ 存瞬时、与时区无关，归一只是让传参确定）。
func (s *PGStateStore) Save(ctx context.Context, strategyID, runID string, ts time.Time, state []byte) error {
	if len(state) == 0 {
		return fmt.Errorf("strategy: PGStateStore.Save 拒绝空 state (strategy=%s run=%s)——空状态会让续跑静默从头跑", strategyID, runID)
	}
	if ts.IsZero() {
		return fmt.Errorf("strategy: PGStateStore.Save 拒绝零值 ts (strategy=%s run=%s)——零值 ts 会让续跑跳过逻辑失效", strategyID, runID)
	}

	ctx, cancel := mergeTimeout(ctx, saveTimeout)
	defer cancel()

	if _, err := s.pool.Exec(ctx, saveSQL, strategyID, runID, ts.UTC(), state); err != nil {
		return fmt.Errorf("strategy: PGStateStore.Save 落库失败 (strategy=%s run=%s): %w", strategyID, runID, err)
	}
	return nil
}

// Load 读出 (strategy_id, run_id) 的最新检查点。无行 → ErrStateNotFound。
//
// 返回的 state 是 SaveState() 当初写进去的原始字节（深拷贝，pgx 的行缓冲在
// rows 之后会被复用，这里 QueryRow.Scan 已拷进我们自己的切片），ts 归一 UTC。
func (s *PGStateStore) Load(ctx context.Context, strategyID, runID string) ([]byte, time.Time, error) {
	ctx, cancel := mergeTimeout(ctx, loadTimeout)
	defer cancel()

	var (
		ts    time.Time
		state []byte
	)
	err := s.pool.QueryRow(ctx, loadSQL, strategyID, runID).Scan(&ts, &state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, time.Time{}, fmt.Errorf("%w (strategy=%s run=%s)", ErrStateNotFound, strategyID, runID)
		}
		return nil, time.Time{}, fmt.Errorf("strategy: PGStateStore.Load 查询失败 (strategy=%s run=%s): %w", strategyID, runID, err)
	}
	return state, ts.UTC(), nil
}

// mergeTimeout 给调用方传来的 ctx 套一个上限（模式来源：pkg/eventstore 的
// mergeTimeout，该函数在 eventstore 包内未导出，本包自写等价小函数）。
// 调用方完全可能传 context.Background()——不能因为没设截止就让它无限期挂着。
func mergeTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), d)
	}
	return context.WithTimeout(ctx, d)
}

// ─── 编译期合规检查 ────────────────────────────────────────────────
//
// PGStateStore 必须满足 StateStore，否则它就不是合法的检查点后端。
var _ StateStore = (*PGStateStore)(nil)
