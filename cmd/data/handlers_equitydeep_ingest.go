// Package main — C-3 equitydeep ingest (ADR-022, TASKS.md EQD-P1-2).
//
// POST /api/ingest/equitydeep normalizes archived EquityDeep snapshots into
// fundamentals_detail. The parsing, unit conversion and PIT rules live in the
// pure package pkg/data/equitydeep; this file is only the transport plus the
// provenance gate.
//
// Provenance gate: the caller must first archive the exact snapshot file
// through POST /api/ingest/raw, and then quote the content_hash it received.
// The handler refuses a hash that ingest.raw does not know, and stamps every
// written row with that hash as its snapshot_uri — so no number can enter
// fundamentals_detail without a resolvable raw response behind it.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ruoxizhnya/quant-trading/pkg/data/equitydeep"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// maxEquityDeepBodyBytes caps one ndjson upload; maxEquityDeepLineBytes caps a
// single snapshot record inside it. Both exist so a malformed producer cannot
// make the ingest buffer unbounded content.
const (
	maxEquityDeepBodyBytes = 32 << 20 // 32 MiB
	maxEquityDeepLineBytes = 1 << 20  // 1 MiB per snapshot
)

// equityDeepIngestResponse reports what the door accepted. Dropped fields are
// returned rather than silently swallowed: a field the contract does not know
// is a producer bug that has to be visible in the response.
type equityDeepIngestResponse struct {
	ContentHash string                   `json:"content_hash"`
	Snapshots   int                      `json:"snapshots"`
	Rows        int                      `json:"rows"`
	Dropped     []equityDeepDroppedField `json:"dropped"`
}

// equityDeepDroppedField is a snapshot field the normalizer refused to map,
// located by its line in the uploaded file.
type equityDeepDroppedField struct {
	Line    int    `json:"line"`
	RawName string `json:"raw_name"`
	Reason  string `json:"reason"`
}

// equityDeepIngestHandler normalizes an ndjson stream of snapshots
// (contracts/snapshot.schema.json, one record per line) into
// fundamentals_detail.
//
// A nil dictionary means the field dictionary was not loadable at startup; the
// endpoint then answers 503 instead of guessing a mapping. Without the
// dictionary there is no whitelist and no conversion table, and guessing either
// would defeat contract C1-a.
func equityDeepIngestHandler(store *storage.PostgresStore, dict *equitydeep.Dictionary) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dict == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "equitydeep ingest unavailable: the field dictionary is not loaded",
			})
			return
		}

		contentHash := strings.TrimSpace(c.Query("content_hash"))
		if !isContentHash(contentHash) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "content_hash must be the 64-character lowercase hex hash of the archived raw response",
			})
			return
		}

		raw, err := store.GetRawIngest(c.Request.Context(), contentHash)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve content_hash"})
			return
		}
		if raw == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "content_hash was never ingested; archive the raw response via POST /api/ingest/raw first",
			})
			return
		}

		body := http.MaxBytesReader(c.Writer, c.Request.Body, maxEquityDeepBodyBytes)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64<<10), maxEquityDeepLineBytes)

		// The archived record supplies the fallback source and the fetch time
		// for snapshots that omit them (the contract marks both optional).
		normalizer := equitydeep.NewNormalizer(dict, raw.Source)
		snapshotURI := "ingest.raw:" + contentHash

		var (
			rows      []domain.FundamentalsDetailRow
			dropped   []equityDeepDroppedField
			snapshots int
			lineNo    int
		)
		for scanner.Scan() {
			lineNo++
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}

			snapshot, err := equitydeep.DecodeSnapshot(line)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("line %d: %v", lineNo, err),
				})
				return
			}

			res, err := normalizer.Normalize(snapshot, snapshotURI, raw.FetchedAt)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("line %d: %v", lineNo, err),
				})
				return
			}

			rows = append(rows, res.Rows...)
			for _, d := range res.Dropped {
				dropped = append(dropped, equityDeepDroppedField{
					Line:    lineNo,
					RawName: d.RawName,
					Reason:  d.Reason,
				})
			}
			snapshots++
		}
		if err := scanner.Err(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read ndjson body: " + err.Error()})
			return
		}
		if snapshots == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body contains no snapshot records"})
			return
		}

		for _, d := range dropped {
			logging.Logger.Warn().
				Int("line", d.Line).
				Str("raw_name", d.RawName).
				Str("reason", d.Reason).
				Str("content_hash", contentHash).
				Msg("Dropped snapshot field")
		}

		if err := store.SaveFundamentalsDetailBatch(c.Request.Context(), rows); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save fundamentals detail"})
			return
		}

		c.JSON(http.StatusCreated, equityDeepIngestResponse{
			ContentHash: contentHash,
			Snapshots:   snapshots,
			Rows:        len(rows),
			Dropped:     dropped,
		})
	}
}

// isContentHash reports whether value is the lowercase sha256 hex form produced
// by storage.ContentHashOf — the only shape ingest.raw can hold.
func isContentHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
