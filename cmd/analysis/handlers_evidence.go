package main

// EvidenceHandler serves the L0 evidence service (ADR-022 §5, TASKS.md L0-3).
//
// Endpoints:
//   GET /api/evidence/:content_hash — return the unique archived source
//   response (the single `ingest.raw` row) for that hash, or 404 when the
//   hash was never ingested.
//
// Why this endpoint exists: a citation tuple
// {source, dataset, key, as_of, content_hash} replaces "file path + fuzzy
// string" as the platform's evidence coordinate. Any number produced by any
// work face must resolve back to exactly one archived external response, and
// work face 1 reads its data through this read-only door instead of keeping
// its own copy (ADR-022 §2 class A/B). 404 is a first-class answer — it means
// "not ingested", not "error".
//
// Design follows the "pattern B" handler style used by ToolsHandler /
// RiskHandler / ExecutionHandler: a struct with functional options and a
// RegisterRoutes method.

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// EvidenceHandler serves the /api/evidence/* endpoints.
type EvidenceHandler struct {
	store  *storage.PostgresStore
	logger zerolog.Logger
}

// EvidenceHandlerOption configures an EvidenceHandler.
type EvidenceHandlerOption func(*EvidenceHandler)

// WithEvidenceLogger overrides the default logger.
func WithEvidenceLogger(l zerolog.Logger) EvidenceHandlerOption {
	return func(h *EvidenceHandler) { h.logger = l }
}

// NewEvidenceHandler constructs an EvidenceHandler backed by store. Panics if
// store is nil — wiring bug, fail loud at startup.
func NewEvidenceHandler(store *storage.PostgresStore, logger zerolog.Logger, opts ...EvidenceHandlerOption) *EvidenceHandler {
	if store == nil {
		panic("analysis: NewEvidenceHandler called with nil PostgresStore")
	}
	h := &EvidenceHandler{
		store:  store,
		logger: logger.With().Str("component", "evidence-handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes mounts the /api/evidence/* endpoints on the router.
func (h *EvidenceHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api")
	api.GET("/evidence/:content_hash", h.handleGet)
}

// handleGet returns the archived source response for the requested hash.
// A hash that was never ingested yields 404 — the caller's signal that the
// citation it is holding cannot be resolved.
func (h *EvidenceHandler) handleGet(c *gin.Context) {
	contentHash := c.Param("content_hash")

	record, err := h.store.GetRawIngest(c.Request.Context(), contentHash)
	if err != nil {
		h.logger.Error().Err(err).Str("content_hash", contentHash).
			Msg("failed to resolve evidence")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve evidence"})
		return
	}
	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":        "evidence not ingested",
			"content_hash": contentHash,
		})
		return
	}

	c.JSON(http.StatusOK, record)
}