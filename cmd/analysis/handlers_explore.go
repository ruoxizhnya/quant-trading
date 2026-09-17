package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/loop"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/search"
)

// ExploreHandler 是 P1-2 循环控制器的 HTTP 入口。
//
// 控制器本身只是个库，没有这一层它就只是躺在包里的能力 —— 跟 P1-1b 的 sink
// 同一个教训：**加能力不等于接上了线**。
//
// 它同时是 P1-3 观察页的数据源：进度靠 OnAttempt 回调累积，叫停走 Stop。
type ExploreHandler struct {
	// pipeline 用接口而非具体类型：控制器只要求它能 Execute 一次尝试，
	// 这样测试才能注入一个不解析意图、不跑编译校验的替身。
	tryRunner loop.TryRunner
	runner    pipeline.BacktestRunner

	mu   sync.Mutex
	runs map[string]*exploreRun
}

// exploreRun 是一轮进行中（或已结束）的探索。
type exploreRun struct {
	mu       sync.Mutex
	ctrl     *loop.Controller
	cancel   context.CancelFunc
	started  time.Time
	attempts []loop.Attempt
	result   *loop.RunResult
	err      error
	done     bool
}

// StartExploreRequest 启动一轮探索的请求。
type StartExploreRequest struct {
	Description string `json:"description" binding:"required" example:"做一个动量策略"`
	MaxTries    int    `json:"max_tries" example:"50"`
	// LookbackMin / LookbackMax 限定回看窗口的搜索范围；不传则用 5..250。
	LookbackMin int `json:"lookback_min"`
	LookbackMax int `json:"lookback_max"`
}

// AttemptView 是一次尝试的对外视图。
type AttemptView struct {
	Seq          int            `json:"seq"`
	Hypothesis   string         `json:"hypothesis"`
	Params       map[string]any `json:"params"`
	ExperimentID int64          `json:"experiment_id"`
	Sharpe       float64        `json:"sharpe"`
	OK           bool           `json:"ok"`
	Error        string         `json:"error,omitempty"`
}

// ExploreStatusResponse 是一轮探索的实时状态。
type ExploreStatusResponse struct {
	RunID    string        `json:"run_id"`
	Running  bool          `json:"running"`
	Stopped  string        `json:"stopped,omitempty"`
	Attempts []AttemptView `json:"attempts"`
	BestSeq  *int          `json:"best_seq,omitempty"`
}

// NewExploreHandler 构造探索处理器。
func NewExploreHandler(p loop.TryRunner, runner pipeline.BacktestRunner) *ExploreHandler {
	return &ExploreHandler{
		tryRunner: p,
		runner:    runner,
		runs:      make(map[string]*exploreRun),
	}
}

// RegisterExploreRoutes 注册探索路由。
func (h *ExploreHandler) RegisterExploreRoutes(router *gin.RouterGroup) {
	g := router.Group("/explore")
	{
		g.POST("/runs", h.Start)
		g.GET("/runs/:runID", h.Status)
		g.POST("/runs/:runID/stop", h.Stop)
	}
}

// registerExploreRoutes 注册探索路由。
//
// 探索用的 pipeline 实例**必须带上实验日志落点** —— 否则这一路的尝试一行
// 都不会记，而「试了多少次」正是过拟合检测唯一依赖的数字。
func registerExploreRoutes(router *gin.Engine, runner pipeline.BacktestRunner, sink pipeline.ExperimentSink) {
	var opts []pipeline.PipelineOption
	if sink != nil {
		opts = append(opts, pipeline.WithExperimentSink(sink))
	}
	handler := NewExploreHandler(pipeline.NewPipeline(opts...), runner)
	api := router.Group("/api")
	handler.RegisterExploreRoutes(api)
}

// Start 启动一轮探索，立即返回 run_id —— 一轮可能跑几十上百次尝试，
// 不能让 HTTP 请求挂在那儿等。
func (h *ExploreHandler) Start(c *gin.Context) {
	var req StartExploreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.MaxTries <= 0 {
		req.MaxTries = 20
	}
	lbMin, lbMax := req.LookbackMin, req.LookbackMax
	if lbMin <= 0 {
		lbMin = 5
	}
	if lbMax <= lbMin {
		lbMax = 250
	}

	// ⚠️ 刻意不用 c.Request.Context()：响应一返回 gin 就会取消它，异步跑的
	// 这一轮会在第一步就被掐断。这里要的是独立于请求的生命周期。
	ctx, cancel := context.WithCancel(context.Background())

	space := &search.SearchSpace{Params: []search.ParamDef{
		// 参数名必须与意图参数同名，否则覆盖静默失效（见 P1-2b）。
		{Name: "lookback_days", Type: "int", Min: float64(lbMin), Max: float64(lbMax)},
	}}

	runID := uuid.New().String()
	run := &exploreRun{
		ctrl:    loop.NewController(h.tryRunner, loop.NewTPEProposer(space, time.Now().UnixNano()), h.runner),
		cancel:  cancel,
		started: time.Now(),
	}
	h.mu.Lock()
	h.runs[runID] = run
	h.mu.Unlock()

	go func() {
		defer func() {
			run.mu.Lock()
			run.done = true
			run.mu.Unlock()
		}()
		out, err := run.ctrl.Run(ctx, loop.Config{
			RunID:       runID,
			Description: req.Description,
			MaxTries:    req.MaxTries,
			OnAttempt: func(a loop.Attempt) {
				run.mu.Lock()
				run.attempts = append(run.attempts, a)
				run.mu.Unlock()
			},
		})
		run.mu.Lock()
		run.result, run.err = out, err
		run.mu.Unlock()
	}()

	c.JSON(http.StatusOK, gin.H{"run_id": runID, "max_tries": req.MaxTries})
}

// Status 查一轮探索的实时进度。
func (h *ExploreHandler) Status(c *gin.Context) {
	h.mu.Lock()
	run, ok := h.runs[c.Param("runID")]
	h.mu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}

	run.mu.Lock()
	defer run.mu.Unlock()

	views := make([]AttemptView, 0, len(run.attempts))
	for _, a := range run.attempts {
		v := AttemptView{
			Seq:          a.Seq,
			Hypothesis:   a.Hypothesis,
			Params:       a.Params,
			ExperimentID: a.ExperimentID,
			OK:           a.OK,
		}
		if a.Result != nil && a.Result.BacktestResult != nil {
			v.Sharpe = a.Result.BacktestResult.SharpeRatio
		}
		if a.Err != nil {
			v.Error = a.Err.Error()
		}
		views = append(views, v)
	}

	resp := ExploreStatusResponse{
		RunID:    c.Param("runID"),
		Running:  !run.done,
		Attempts: views,
	}
	if run.result != nil {
		resp.Stopped = string(run.result.Stopped)
		if run.result.Best != nil {
			seq := run.result.Best.Seq
			resp.BestSeq = &seq
		}
	}
	c.JSON(http.StatusOK, resp)
}

// Stop 叫停一轮探索。已跑的不丢 —— 这正是「被叫停的探索」与「跑满的探索」
// 可信度不同的地方，日志里都留着。
func (h *ExploreHandler) Stop(c *gin.Context) {
	h.mu.Lock()
	run, ok := h.runs[c.Param("runID")]
	h.mu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}

	run.ctrl.Stop()
	if run.cancel != nil {
		run.cancel()
	}
	c.JSON(http.StatusOK, gin.H{"run_id": c.Param("runID"), "stopping": true})
}
