package main

// ToolsHandler exposes the Tools Registry over HTTP (S7-P3-3, ODR-043).
//
// Endpoints:
//   GET  /api/tools          — list all tools with their schemas
//   GET  /api/tools/:name    — get a single tool's schema
//   POST /api/tools/:name    — execute a tool with args
//
// Design follows the "pattern B" handler style used by RiskHandler /
// ExecutionHandler / ComplianceHandler / PipelineHandler: a struct
// with functional options and a RegisterRoutes method. This is the
// project's演化方向 for new handlers.

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
)

// ToolsHandler serves the /api/tools/* endpoints.
type ToolsHandler struct {
	registry *tools.Registry
	logger   zerolog.Logger
}

// ToolsHandlerOption configures a ToolsHandler.
type ToolsHandlerOption func(*ToolsHandler)

// WithToolsLogger overrides the default logger.
func WithToolsLogger(l zerolog.Logger) ToolsHandlerOption {
	return func(h *ToolsHandler) { h.logger = l }
}

// NewToolsHandler constructs a ToolsHandler backed by reg. Panics if
// reg is nil — wiring bug, fail loud at startup.
func NewToolsHandler(reg *tools.Registry, logger zerolog.Logger, opts ...ToolsHandlerOption) *ToolsHandler {
	if reg == nil {
		panic("analysis: NewToolsHandler called with nil Registry")
	}
	h := &ToolsHandler{
		registry: reg,
		logger:   logger.With().Str("component", "tools-handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes mounts the /api/tools/* endpoints on the router.
func (h *ToolsHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api")
	tools := api.Group("/tools")
	{
		tools.GET("", h.handleList)
		tools.GET("/", h.handleList) // trailing-slash variant
		tools.GET("/:name", h.handleGet)
		tools.POST("/:name", h.handleExecute)
	}
}

// handleList returns all registered tools' ToolInfo (name + description
// + parameters + output_schema), sorted by name.
func (h *ToolsHandler) handleList(c *gin.Context) {
	list := h.registry.List()
	c.JSON(http.StatusOK, gin.H{
		"tools": list,
		"count": len(list),
	})
}

// handleGet returns a single tool's schema. Returns 404 if the tool
// name is not registered.
func (h *ToolsHandler) handleGet(c *gin.Context) {
	name := c.Param("name")
	t, err := h.registry.Get(name)
	if err != nil {
		h.respondToolError(c, http.StatusNotFound, err)
		return
	}

	info := tools.ToolInfo{
		Name:        t.Name(),
		Description: t.Description(),
	}
	if sp := tools.AsSchemaProvider(t); sp != nil {
		info.Parameters = sp.Parameters()
		info.OutputSchema = sp.OutputSchema()
	}
	c.JSON(http.StatusOK, info)
}

// executeRequest is the JSON body for POST /api/tools/:name.
type executeRequest struct {
	Args map[string]interface{} `json:"args"`
}

// executeResponse is the JSON body for a successful execution.
type executeResponse struct {
	Result interface{} `json:"result"`
}

// handleExecute looks up the tool by name and invokes Execute with the
// request body's "args" map.
func (h *ToolsHandler) handleExecute(c *gin.Context) {
	name := c.Param("name")

	var req executeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
			"code":  "INVALID_BODY",
		})
		return
	}

	result, err := h.registry.Execute(c.Request.Context(), name, req.Args)
	if err != nil {
		h.respondToolError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, executeResponse{Result: result})
}

// respondToolError maps a tools error to an appropriate HTTP status
// code and a structured JSON error body. The error "code" field lets
// external agents distinguish failure modes programmatically.
func (h *ToolsHandler) respondToolError(c *gin.Context, status int, err error) {
	// Log the full error for diagnostics; the client gets a concise message.
	h.logger.Warn().Err(err).Str("tool", c.Param("name")).Msg("tool request failed")

	// Classify the error to pick the right status + code.
	switch {
	case errors.Is(err, tools.ErrToolNotRegistered):
		status = http.StatusNotFound
		c.JSON(status, gin.H{
			"error": err.Error(),
			"code":  "TOOL_NOT_REGISTERED",
		})
	case errors.Is(err, tools.ErrInvalidArgs):
		status = http.StatusBadRequest
		c.JSON(status, gin.H{
			"error": err.Error(),
			"code":  "INVALID_ARGS",
		})
	default:
		// Downstream / execution error — return 500 with the message
		// so the caller can see what went wrong (e.g. "strategy not
		// found: momentum"). We do NOT leak stack traces or internal
		// paths; the error string from the Tool is already
		// user-facing (it was constructed with fmt.Errorf in builtin/).
		c.JSON(status, gin.H{
			"error": err.Error(),
			"code":  "TOOL_EXECUTION_FAILED",
		})
	}
}
