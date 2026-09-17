package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// P1-1 实验日志。
//
// PRODUCT §架构给它的定位：落 L1，由 AI 产生、经能力层写入，被验证器与审阅台
// 共同读取 —— 是**过拟合检测、路径回放、复盘**的共同数据源，不是附属功能。
//
// 这一层只做存取，不做判断：不评最优、不排序、不打分。「按高原面积 × 因果强度
// 排序」是摘要层的职责（PRODUCT §决策摘要），且依赖验证器输出，存储层决定不了。

// 实验状态。running 既是初始态，也是被叫停时的终态 —— 中途叫停的实验永远
// 停在 running，那正是回路断在哪一步的证据，不能被当成脏数据清掉。
const (
	ExperimentStatusRunning   = "running"
	ExperimentStatusCompleted = "completed"
	ExperimentStatusFailed    = "failed"
)

// 数据集划分。一次尝试必须说清自己用的是哪份数据，否则保留期验证无从谈起
// （验证器看的是「训练期调出来的参数在保留期还成不成立」）。
const (
	DatasetSplitTrain = "train"
	DatasetSplitHold  = "hold"
	DatasetSplitTest  = "test"
)

// Experiment 是一次尝试的完整记录。
type Experiment struct {
	ID           int64
	RunID        string
	Seq          int
	ParentID     *int64 // 由哪次尝试衍生而来（首轮为 nil）
	Hypothesis   string
	Params       map[string]any
	DatasetSplit string
	StrategyName string
	Expression   string
	Status       string
	Metrics      *ExperimentMetrics // 失败时为 nil
	Error        string
	CreatedAt    time.Time
	FinishedAt   *time.Time // 未结束（含被叫停）时为 nil
}

// ExperimentMetrics 是一次尝试的产出指标。
//
// 字段按验证器的需要逐步加，现在只放回测必出的三项。注意「试了几次才撞出来」
// 不在这个结构里 —— 它体现在同一 run_id 下的行数与 seq 上，是路径的属性，
// 不是单次结果的属性。
type ExperimentMetrics struct {
	SharpeRatio float64 `json:"sharpe_ratio"`
	TotalReturn float64 `json:"total_return"`
	TotalTrades int     `json:"total_trades"`
}

// experimentColumns 是 SELECT 的固定列序，scanExperiment 依赖它。
const experimentColumns = `id, run_id, seq, parent_id, hypothesis, params, dataset_split,
		       strategy_name, expression, status, metrics, error_message,
		       created_at, finished_at`

// InsertExperiment 开一次尝试，返回自增 ID。
//
// 初始状态恒为 running：结果要等回测跑完才知道，而「开始了但没结果」这个事实
// 必须先落库 —— 否则进程一崩，这次尝试就从未存在过，路径回放会缺一环。
func (s *PostgresStore) InsertExperiment(ctx context.Context, e *Experiment) (int64, error) {
	params, err := marshalJSONBObject(e.Params)
	if err != nil {
		return 0, fmt.Errorf("failed to encode experiment params: %w", err)
	}
	split := e.DatasetSplit
	if split == "" {
		split = DatasetSplitTrain
	}

	query := `
		INSERT INTO experiments
			(run_id, seq, parent_id, hypothesis, params, dataset_split,
			 strategy_name, expression, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		RETURNING id
	`
	var id int64
	err = s.pool.QueryRow(ctx, query,
		e.RunID, e.Seq, e.ParentID, e.Hypothesis, params, split,
		e.StrategyName, e.Expression, ExperimentStatusRunning,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert experiment: %w", err)
	}
	return id, nil
}

// GetExperiment 取单条记录；不存在时返回 (nil, nil) —— 与 GetBacktestJob 一致，
// 「没找到」不是错误，是查询结果。
func (s *PostgresStore) GetExperiment(ctx context.Context, id int64) (*Experiment, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+experimentColumns+` FROM experiments WHERE id = $1`, id)
	e, err := scanExperiment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return e, nil
}

// CompleteExperiment 收尾一次尝试。
//
// errMsg 非空即以失败收尾，且**丢弃 metrics**：失败的实验不该留下指标，
// 否则回放时会看到「失败了但 Sharpe 是 1.2」这种自相矛盾的记录。
// errMsg 为空则记为完成，m 为 nil 时指标留空（跑完了但没产出指标）。
func (s *PostgresStore) CompleteExperiment(ctx context.Context, id int64, m *ExperimentMetrics, errMsg string) error {
	status := ExperimentStatusCompleted
	if errMsg != "" {
		status = ExperimentStatusFailed
		m = nil
	}

	var metrics []byte
	if m != nil {
		b, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("failed to encode experiment metrics: %w", err)
		}
		metrics = b
	}

	var errVal *string
	if errMsg != "" {
		errVal = &errMsg
	}

	query := `
		UPDATE experiments
		SET status = $2, metrics = $3, error_message = $4, finished_at = NOW()
		WHERE id = $1
	`
	if _, err := s.pool.Exec(ctx, query, id, status, metrics, errVal); err != nil {
		return fmt.Errorf("failed to complete experiment: %w", err)
	}
	return nil
}

// ListExperiments 按尝试顺序（seq）回放一轮 run 的完整路径。
//
// 刻意不按插入时间排：并发跑的时候插入顺序是调度顺序，不是探索顺序，
// 而回放要看的是「第 n 次试了什么」，那只有 seq 说得清。
func (s *PostgresStore) ListExperiments(ctx context.Context, runID string) ([]*Experiment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+experimentColumns+` FROM experiments WHERE run_id = $1 ORDER BY seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to list experiments: %w", err)
	}
	defer rows.Close()

	var out []*Experiment
	for rows.Next() {
		e, err := scanExperiment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanExperiment 把一行读成 Experiment。pgx.Row 是 *pgxpool.Row 与 pgx.Rows
// 共有的最小接口，两条查询路径因此可以共用一份列序。
func scanExperiment(row pgx.Row) (*Experiment, error) {
	var e Experiment
	var params, metrics []byte
	var hypothesis, strategyName, expression, errMsg *string

	err := row.Scan(
		&e.ID, &e.RunID, &e.Seq, &e.ParentID, &hypothesis, &params, &e.DatasetSplit,
		&strategyName, &expression, &e.Status, &metrics, &errMsg,
		&e.CreatedAt, &e.FinishedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan experiment row: %w", err)
	}

	if len(params) > 0 {
		if err := json.Unmarshal(params, &e.Params); err != nil {
			return nil, fmt.Errorf("failed to decode experiment params: %w", err)
		}
	}
	if len(metrics) > 0 {
		var m ExperimentMetrics
		if err := json.Unmarshal(metrics, &m); err != nil {
			return nil, fmt.Errorf("failed to decode experiment metrics: %w", err)
		}
		e.Metrics = &m
	}

	// TEXT 列允许 NULL（手工修数据的行会是 NULL），但 Go 侧用 string 而非 *string：
	// 对实验日志来说「没写假设」和「空假设」没有区别，不值得让每个读的人处理 nil。
	e.Hypothesis = nullString(hypothesis)
	e.StrategyName = nullString(strategyName)
	e.Expression = nullString(expression)
	e.Error = nullString(errMsg)
	return &e, nil
}

// ExperimentUpdate 补写「试了什么」这部分字段。
//
// 为什么需要两步写：尝试必须**先落行**再填参数 —— 进程崩在中途时，那行
// running 是「跑到第几步断的」的唯一证据。但落行的那一刻参数还没定：
// 表达式要等 YAML 生成并注册之后才存在。所以先落占位，参数定了再补。
//
// 零值字段表示「不改」（SQL 侧走 COALESCE，传 NULL 即保留原值）。
type ExperimentUpdate struct {
	Params       map[string]any
	StrategyName string
	Expression   string
}

// UpdateExperiment 把「试了什么」补进已存在的那一行。id 为 0 表示这一轮
// 没有日志落点（例如 sink 写入失败），静默跳过。
func (s *PostgresStore) UpdateExperiment(ctx context.Context, id int64, u ExperimentUpdate) error {
	if id == 0 {
		return nil
	}
	var params []byte
	if u.Params != nil {
		b, err := marshalJSONBObject(u.Params)
		if err != nil {
			return fmt.Errorf("failed to encode experiment params: %w", err)
		}
		params = b
	}
	query := `
		UPDATE experiments SET
			params        = COALESCE($2, params),
			strategy_name = COALESCE($3, strategy_name),
			expression    = COALESCE($4, expression)
		WHERE id = $1
	`
	_, err := s.pool.Exec(ctx, query, id, params,
		nullableString(u.StrategyName), nullableString(u.Expression))
	if err != nil {
		return fmt.Errorf("failed to update experiment: %w", err)
	}
	return nil
}

func nullString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// nullableString 把空串转成 SQL NULL —— COALESCE 靠它区分
// 「这个字段没传（保留原值）」和「这个字段设为空」。
func nullableString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// marshalJSONBObject 把 map 编成 JSONB；nil 编成 '{}' 而不是 'null' ——
// 列上有 NOT NULL DEFAULT '{}'，写 null 进去会让「没参数」和「参数是 null」
// 变成两种状态，读的时候要分支处理，没这个必要。
func marshalJSONBObject(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}
