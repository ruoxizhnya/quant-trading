package data

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// tushareSourceName is the `source` value of every archived tushare response
// in ingest.raw.
const tushareSourceName = "tushare"

// archiveRaw archives one raw tushare HTTP response into ingest.raw
// (ADR-022 §2 class A / TASKS.md L0-1).
//
// Why the raw HTTP body rather than the normalised rows: normalised data
// (market.*) can be recomputed from the raw response, but the raw response
// cannot be recomputed from the normalised rows. Archiving the body at the
// single request choke point (TushareClient.call) is what makes every number
// the platform derives resolvable back to exactly one evidence row — the
// coordinate cited as {source, dataset, key, as_of, content_hash} (ADR-022 §5).
//
// Archiving is best-effort. A failure here must not fail the fetch it
// describes: the fetch result is still valid data, and ingest.raw is
// append-only so the same response can be archived on the next fetch.
func (c *TushareClient) archiveRaw(ctx context.Context, apiName string, params map[string]interface{}, body []byte) {
	if c.store == nil || len(body) == 0 {
		return
	}

	hash, err := storage.ContentHashOf(body)
	if err != nil {
		c.logger.Warn().Err(err).Str("api", apiName).
			Msg("Raw tushare response is not hashable; skipping ingest.raw archive")
		return
	}

	record := &storage.RawIngest{
		ContentHash: hash,
		Source:      tushareSourceName,
		Dataset:     tushareSourceName + "." + apiName,
		Key:         rawIngestKey(apiName, params),
		// AsOf stays NULL on purpose: one tushare response has no single
		// observation date (a request may span thousands of trading days),
		// and content_hash already identifies the exact response.
		Payload:   json.RawMessage(body),
		FetchedAt: time.Now().UTC(),
	}

	if err := c.store.SaveRawIngest(ctx, record); err != nil {
		c.logger.Warn().Err(err).Str("api", apiName).Str("content_hash", hash).
			Msg("Failed to archive raw tushare response into ingest.raw")
	}
}

// rawIngestKey renders the request parameters as a deterministic, human
// readable key, e.g. "stk_factor_pro:end_date=20240930&ts_code=600519.SH".
//
// Determinism matters: the same request replayed must produce the same key,
// otherwise ingest.raw stops being a stable citation coordinate. Empty
// parameter values are dropped so that "not requested" and "requested as
// empty" do not produce two different keys for the same response.
func rawIngestKey(apiName string, params map[string]interface{}) string {
	if len(params) == 0 {
		return apiName
	}

	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		value := fmt.Sprint(params[name])
		if value == "" {
			continue
		}
		parts = append(parts, name+"="+value)
	}
	if len(parts) == 0 {
		return apiName
	}

	return apiName + ":" + strings.Join(parts, "&")
}