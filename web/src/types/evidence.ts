// Evidence types — ADR-022 §5, TASKS.md L0-3.
//
// Mirrors pkg/storage/ingest_raw.go (RawIngest): one archived external-source
// response. `ingest.raw` is the single evidence coordinate of the platform, so
// every number produced anywhere must resolve back to exactly one record here.
export interface RawIngest {
  /** sha256 (lowercase hex, 64 chars) of the canonicalized source payload. */
  content_hash: string
  /** External source the response came from (e.g. "tushare"). */
  source: string
  /** Logical dataset within the source (e.g. "daily"). */
  dataset: string
  /** Source-specific lookup key (e.g. "000001.SZ"). */
  key: string
  /** Source as-of timestamp; omitted when the response carried none. */
  as_of?: string
  /** Archived source JSON, verbatim. */
  payload: unknown
  /** When this record was archived into ingest.raw. */
  fetched_at: string
}