// Factor types — TASKS.md P5-3 (ADR-022 §5 citation coordinates).
//
// Mirrors pkg/domain/factor.go (FactorCacheEntry) and the citationTuple the
// data-service handler expands it into (cmd/data/handlers_factor.go).

/**
 * One ADR-022 §5 evidence coordinate. `content_hash` is the only field
 * guaranteed to be present: when the referenced response is not in
 * `ingest.raw`, the other four are absent — that absence is the first-class
 * "response not archived" signal and must never be padded with placeholders
 * (same semantics as the Evidence API's 404, ODR-061 §4).
 */
export interface CitationTuple {
  content_hash: string
  source?: string
  dataset?: string
  key?: string
  as_of?: string
}

/**
 * A factor_cache row as returned by GET /api/factors/:factor_name.
 *
 * `citation` is absent when the row carries none (stored as '[]' — "no A→B
 * chain established yet"), and its tuples are hash-only when the source
 * response was never archived. The data service degrades to the unexpanded
 * stored form rather than failing the read, so a tuple may legitimately
 * arrive with just `content_hash`.
 */
export interface FactorCacheEntry {
  id: number
  symbol: string
  trade_date: string
  factor_name: string
  raw_value: number
  z_score: number
  percentile: number
  citation?: CitationTuple[]
}