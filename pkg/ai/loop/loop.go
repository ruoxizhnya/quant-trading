// Package loop 提供 P1-2 的循环控制器：让搜索算法（或 LLM）能连续调用
// 底座，并随时响应中断。
//
// 职责边界很清楚：**控制器不搜索参数，也不评价结果**。参数由 Proposer 想，
// 好坏由验证器（P2-9）判。控制器只管节奏 —— 分配 seq、把每一步接到链路上、
// 该停就停。把这三者混在一起，就会得到那种既不能换算法、又讲不清为什么停
// 的东西。
package loop

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// TryRunner 跑一次尝试。*pipeline.Pipeline 天然满足它。
//
// 用接口而不是具体类型，是为了让控制器能被独立测试：真 pipeline 要解析意图、
// 生成 YAML、编译校验，慢且与本层职责无关。
type TryRunner interface {
	Execute(ctx context.Context, description string, runner pipeline.BacktestRunner) (*pipeline.Result, error)
}

// Proposer 提议下一组参数。它看得到已经试过什么（**含失败**），据此决定
// 下一步往哪走。
//
// 随机搜索、TPE、遗传、乃至 LLM 都可以是 Proposer —— 控制器不关心参数是怎么
// 想出来的。
type Proposer interface {
	Suggest(observed []Observation) Suggestion
}

// Observation 是一次已完成的尝试，供 Proposer 学习。
type Observation struct {
	Seq    int
	Params map[string]any
	Value  float64 // 目标值，越小越好
	OK     bool    // false = 这次尝试失败了，Value 无意义
}

// Suggestion 是 Proposer 给出的下一步。
type Suggestion struct {
	Params map[string]any
	// ParentSeq 指这次尝试由哪一次衍生而来（nil = 无父代，例如首轮随机探索）。
	//
	// 之所以用 seq 而不是库里的 experiment ID：Proposer 只看得见 seq，
	// 翻译成 ID 是控制器的活（它才知道哪一行落在哪个 ID 上）。
	ParentSeq *int
	// Hypothesis 说清「为什么试这个」。只有参数没有理由，回放时就讲不成
	// 故事 —— 而「为什么转到下一步」正是 P1-1c 要的东西。
	Hypothesis string
}

// StopReason 说明一轮探索为什么停下。
type StopReason string

const (
	// StopExhausted 试满了配置次数。
	StopExhausted StopReason = "exhausted"
	// StopCancelled 被叫停（ctx 取消或主动 Stop）。
	StopCancelled StopReason = "cancelled"
)

// Attempt 是一次尝试的结果。
type Attempt struct {
	Seq          int
	Params       map[string]any
	Hypothesis   string
	ExperimentID int64            // 实验日志的行 ID；0 = 这一路没记日志
	Result       *pipeline.Result // 即使失败也非 nil（pipeline 会带上失败信息）
	Err          error
	Value        float64
	OK           bool
}

// RunResult 是一轮探索的结果。
type RunResult struct {
	RunID string
	Tries []Attempt
	// Stopped 说明怎么停的。叫停和跑满是两回事 —— 一轮被叫停的探索，
	// 其「最优」的可信度完全不同。
	Stopped StopReason
	Best    *Attempt // 没有一次成功时为 nil
}

// Config 是一轮探索的配置。
type Config struct {
	RunID string // 空则自动生成
	// Description 交给 pipeline 的意图描述（本轮探索要试的那个方向）。
	Description string
	// MaxTries 最多试多少次。必须为正 —— 0 会被默默读成「跑完了一轮 0 次」，
	// 那比报错更糟。
	MaxTries int
	// DatasetSplit 这批尝试用的数据划分。空则按 train 记 —— 默认必须显式
	// 偏向训练期，而不是留空让人分不清这行用的哪份数据。
	DatasetSplit string
}

// Controller 驱动一轮探索。
type Controller struct {
	runner   TryRunner
	backtest pipeline.BacktestRunner
	proposer Proposer

	mu      sync.Mutex
	stopped bool
}

// NewController 构造控制器。backtest 可以为 nil，此时 pipeline 会跳过回测
// 阶段（用于只想看配置生成、不想真跑的场景）。
func NewController(runner TryRunner, proposer Proposer, backtest pipeline.BacktestRunner) *Controller {
	return &Controller{runner: runner, proposer: proposer, backtest: backtest}
}

// Stop 请求停止。不等待正在跑的那次尝试结束 —— 它会在下一次尝试开始前生效。
//
// 之所以不是「立刻杀掉当前尝试」：跑到一半被掐断的实验，其状态既不是成功
// 也不是失败，回放时最难解释。让它跑完、把结果老老实实记下来，反而更干净。
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
}

func (c *Controller) isStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped
}

// Run 连续跑最多 MaxTries 次尝试，直到跑满或被叫停。
//
// 返回 error 只在**配置错误**时（例如 MaxTries 非正）。单次尝试失败不返回
// 错误 —— 它记在对应 Attempt 的 Err 里，并继续往下试。把「某次失败了」和
// 「这一轮失败了」混为一谈，就会因为一次翻车丢掉整轮探索的证据。
func (c *Controller) Run(ctx context.Context, cfg Config) (*RunResult, error) {
	if cfg.MaxTries <= 0 {
		return nil, fmt.Errorf("MaxTries 必须为正，实际 %d", cfg.MaxTries)
	}
	if cfg.RunID == "" {
		cfg.RunID = uuid.New().String()
	}
	if cfg.DatasetSplit == "" {
		cfg.DatasetSplit = storage.DatasetSplitTrain
	}

	out := &RunResult{RunID: cfg.RunID, Stopped: StopExhausted}
	observed := make([]Observation, 0, cfg.MaxTries)
	// seq → 实验日志行 ID。Proposer 只给 seq，翻译成 ID 要靠这张表。
	idBySeq := make(map[int]int64, cfg.MaxTries)

	for seq := 0; seq < cfg.MaxTries; seq++ {
		// 中断检查放在开头而不是结尾：放结尾会多跑一次，而那一次正是用户
		// 明确叫停之后才启动的。
		if ctx.Err() != nil || c.isStopped() {
			out.Stopped = StopCancelled
			break
		}

		sug := c.proposer.Suggest(observed)

		execCtx := pipeline.WithExperimentContext(ctx, pipeline.ExperimentContext{
			RunID:        cfg.RunID,
			Seq:          seq,
			ParentID:     resolveParent(idBySeq, sug.ParentSeq),
			Hypothesis:   sug.Hypothesis,
			DatasetSplit: cfg.DatasetSplit,
		})
		// P1-2b：把这次的参数真的交给底座。少了这一步，控制器搜它的、
		// 底座跑自己的 —— 一轮下来是同一个策略重复 N 遍，搜索等于没搜。
		if len(sug.Params) > 0 {
			execCtx = pipeline.WithParameterOverrides(execCtx, sug.Params)
		}

		res, err := c.runner.Execute(execCtx, cfg.Description, c.backtest)

		a := Attempt{
			Seq:        seq,
			Params:     sug.Params,
			Hypothesis: sug.Hypothesis,
			Result:     res,
			Err:        err,
		}
		if res != nil {
			a.ExperimentID = res.ExperimentID
			idBySeq[seq] = res.ExperimentID
		}
		// 只有「跑完了且有回测结果」才算成功。失败时 Value 保持 0，
		// 但不能让它混进最优评选 —— 一个没跑出来的 0 会被误读成好结果。
		if err == nil && res != nil && res.BacktestResult != nil {
			a.OK = true
			a.Value = objective(res.BacktestResult)
		}
		out.Tries = append(out.Tries, a)

		observed = append(observed, Observation{
			Seq: seq, Params: sug.Params, Value: a.Value, OK: a.OK,
		})

		if a.OK && (out.Best == nil || a.Value < out.Best.Value) {
			best := a
			out.Best = &best
		}
	}

	return out, nil
}

// resolveParent 把 Proposer 给的 seq 翻译成库里的行 ID。
// 查不到就当没有父代 —— 父子关系是元数据，不是关键路径，不该拖垮整轮。
func resolveParent(idBySeq map[int]int64, seq *int) *int64 {
	if seq == nil {
		return nil
	}
	id, ok := idBySeq[*seq]
	if !ok || id == 0 {
		return nil
	}
	return &id
}

// objective 把回测结果折成一个「越小越好」的标量，给搜索算法当梯度用。
//
// 用负 Sharpe 是因为搜索算法统一做最小化，而我们想要 Sharpe 最大。
//
// 注意这是**搜索时的局部目标**，不是最终评价标准。真正的排序按
// 「高原面积 × 因果强度」（PRODUCT §决策摘要），那是验证器的活，
// 控制器不该越权替它决定什么叫好。
func objective(r *domain.BacktestResult) float64 {
	return -r.SharpeRatio
}
