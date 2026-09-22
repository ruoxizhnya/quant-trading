// Package main — L0 ingest write door (ADR-022 §2/§3, TASKS.md L0-1).
//
// POST /api/ingest/raw is the platform's single write door into `ingest.raw`,
// the table that holds class-A raw source responses. Two kinds of producer use
// it:
//
//   - the data service itself, via TushareClient.archiveRaw at the tushare
//     request choke point (in-process, no HTTP hop);
//   - any external producer that fetches from a source the data service does
//     not own yet — in practice the akshare side of work face 1. Without this
//     door such data could never enter L0, and EquityDeep would be forced to
//     keep its own copy (exactly the duplication ADR-022 forbids).
//
// The door is deliberately narrow: it archives a verbatim payload and hands
// back the content_hash that citations must carry. It does not normalise,
// interpret, or validate domain content — normalisation is a separate,
// reproducible step that reads these rows back.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// maxIngestRawBodyBytes caps a single archived payload. ingest.raw stores the
// verbatim source response, so the limit is generous but finite: the write door
// must not buffer unbounded bodies into JSONB.
const maxIngestRawBodyBytes = 32 << 20 // 32 MiB

// ingestRawRequest is the wire format of POST /api/ingest/raw.
type ingestRawRequest struct {
	Source  string          `json:"source" binding:"required"`
	Dataset string          `json:"dataset" binding:"required"`
	Key     string          `json:"key" binding:"required"`
	Payload json.RawMessage `json:"payload" binding:"required"`
	// AsOf is the observation date the payload describes ("2024-09-30" or
	// RFC3339), not the fetch time. Optional: a payload spanning many dates
	// has no single one.
	AsOf string `json:"as_of"`
	// ContentHash is optional. When supplied it is verified against the
	// payload, so a caller can confirm it is re-sending the same response it
	// cited; a mismatch is rejected instead of poisoning the evidence index.
	ContentHash string `json:"content_hash"`
}

// ingestRawHandler archives a raw external-source response into ingest.raw.
//
// Idempotency: content_hash is the primary key and the insert is
// ON CONFLICT DO NOTHING, so re-posting byte-different-but-meaning-identical
// content is a no-op that returns the same hash. ingest.raw is immutable —
// a later post never rewrites the original attribution.
func ingestRawHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxIngestRawBodyBytes)

		var req ingestRawRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Wrap(c, http.StatusBadRequest, err, "invalid ingest request: ")
			return
		}

		hash, err := storage.ContentHashOf(req.Payload)
		if err != nil {
			httpserver.Wrap(c, http.StatusBadRequest, err, "payload must be valid JSON: ")
			return
		}
		if req.ContentHash != "" && req.ContentHash != hash {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":        "content_hash does not match payload",
				"content_hash": hash,
			})
			return
		}

		asOf, err := parseIngestAsOf(req.AsOf)
		if err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}

		record := &storage.RawIngest{
			ContentHash: hash,
			Source:      req.Source,
			Dataset:     req.Dataset,
			Key:         req.Key,
			AsOf:        asOf,
			Payload:     req.Payload,
			FetchedAt:   time.Now().UTC(),
		}
		if err := store.SaveRawIngest(c.Request.Context(), record); err != nil {
			httpserver.Fail(c, http.StatusInternalServerError, "failed to archive raw response")
			return
		}

		// 201 with the stored record: the caller now holds the exact
		// content_hash its citations must carry.
		c.JSON(http.StatusCreated, record)
	}
}

// parseIngestAsOf accepts a plain calendar date ("2024-09-30", what a Python
// producer emits for datetime.date) or RFC3339. An empty string means "the
// payload has no single observation date" and yields nil — stored as NULL.
func parseIngestAsOf(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("as_of must be YYYY-MM-DD or RFC3339, got %q", value)
}
