package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
)

// PipelineHandler handles pipeline-related HTTP requests
type PipelineHandler struct {
	pipeline *pipeline.Pipeline
}

// NewPipelineHandler creates a new pipeline handler
func NewPipelineHandler() *PipelineHandler {
	return &PipelineHandler{
		pipeline: pipeline.NewPipeline(),
	}
}

// registerPipelineRoutes registers pipeline routes with the router
func registerPipelineRoutes(router *gin.Engine) {
	handler := NewPipelineHandler()
	api := router.Group("/api")
	handler.RegisterPipelineRoutes(api)
}

// RunPipelineRequest represents a request to run the pipeline
// @Description Request body for running the AI strategy generation pipeline
// @Description 运行AI策略生成流水线的请求体
type RunPipelineRequest struct {
	Description string `json:"description" binding:"required" example:"20日动量策略，在沪深300中选出最强10只股票"`
}

// PipelineJobResponse represents a pipeline job result
// @Description Pipeline job execution result
// @Description 流水线作业执行结果
type PipelineJobResponse struct {
	ID             string      `json:"id"`
	Status         string      `json:"status"`
	Intent         interface{} `json:"intent,omitempty"`
	YAMLConfig     string      `json:"yaml_config,omitempty"`
	GeneratedCode  string      `json:"generated_code,omitempty"`
	BuildError     string      `json:"build_error,omitempty"`
	BacktestResult interface{} `json:"backtest_result,omitempty"`
	BacktestError  string      `json:"backtest_error,omitempty"`
	StartedAt      string      `json:"started_at"`
	CompletedAt    *string     `json:"completed_at,omitempty"`
	DurationMs     int64       `json:"duration_ms"`
	Logs           []string    `json:"logs,omitempty"`
}

// RegisterPipelineRoutes registers pipeline routes
func (h *PipelineHandler) RegisterPipelineRoutes(router *gin.RouterGroup) {
	pipeline := router.Group("/pipeline")
	{
		pipeline.POST("/run", h.RunPipeline)
		pipeline.GET("/jobs", h.GetPipelineJobs)
		pipeline.GET("/jobs/:id", h.GetPipelineJob)
	}
}

// RunPipeline runs the full AI strategy generation pipeline
// @Summary      Run AI Strategy Pipeline
// @Description  Execute the full pipeline: intent parsing -> YAML generation -> code generation -> compilation -> backtest
// @Description  执行完整的AI策略生成流水线：意图解析->YAML生成->代码生成->编译验证->回测
// @Tags         AI Pipeline
// @Accept       json
// @Produce      json
// @Param        request  body      RunPipelineRequest  true  "Pipeline request"
// @Success      200      {object}  PipelineJobResponse  "Pipeline execution result"
// @Failure      400      {object}  map[string]string    "Invalid request"
// @Failure      500      {object}  map[string]string    "Internal server error"
// @Router       /pipeline/run [post]
func (h *PipelineHandler) RunPipeline(c *gin.Context) {
	var req RunPipelineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.pipeline.Execute(c.Request.Context(), req.Description, nil)
	if err != nil {
		// Return the result even if there was an error, so the client can see partial results
		c.JSON(http.StatusOK, pipelineResultToResponse(result))
		return
	}

	c.JSON(http.StatusOK, pipelineResultToResponse(result))
}

// GetPipelineJobs returns all pipeline jobs
// @Summary      List Pipeline Jobs
// @Description  Get a list of all pipeline jobs and their status
// @Description  获取所有流水线作业及其状态
// @Tags         AI Pipeline
// @Produce      json
// @Success      200  {array}   PipelineJobResponse  "List of pipeline jobs"
// @Router       /pipeline/jobs [get]
func (h *PipelineHandler) GetPipelineJobs(c *gin.Context) {
	// For now, return an empty list
	// In a production system, this would iterate through all jobs in the pipeline
	c.JSON(http.StatusOK, []PipelineJobResponse{})
}

// GetPipelineJob returns a specific pipeline job by ID
// @Summary      Get Pipeline Job
// @Description  Get the status and result of a specific pipeline job
// @Description  获取特定流水线作业的状态和结果
// @Tags         AI Pipeline
// @Produce      json
// @Param        id   path      string               true  "Job ID"
// @Success      200  {object}  PipelineJobResponse  "Pipeline job result"
// @Failure      404  {object}  map[string]string    "Job not found"
// @Router       /pipeline/jobs/{id} [get]
func (h *PipelineHandler) GetPipelineJob(c *gin.Context) {
	jobID := c.Param("id")
	result := h.pipeline.GetJob(jobID)
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, pipelineResultToResponse(result))
}

// pipelineResultToResponse converts a pipeline.Result to a PipelineJobResponse
func pipelineResultToResponse(result *pipeline.Result) PipelineJobResponse {
	if result == nil {
		return PipelineJobResponse{}
	}

	resp := PipelineJobResponse{
		ID:            result.ID,
		Status:        string(result.Status),
		YAMLConfig:    result.YAMLConfig,
		GeneratedCode: result.GeneratedCode,
		BuildError:    result.BuildError,
		BacktestError: result.BacktestError,
		StartedAt:     result.StartedAt.Format("2006-01-02T15:04:05Z"),
		DurationMs:    result.DurationMs,
		Logs:          result.Logs,
	}

	if result.Intent != nil {
		resp.Intent = result.Intent
	}

	if result.BacktestResult != nil {
		resp.BacktestResult = result.BacktestResult
	}

	if result.CompletedAt != nil {
		completedAt := result.CompletedAt.Format("2006-01-02T15:04:05Z")
		resp.CompletedAt = &completedAt
	}

	return resp
}
