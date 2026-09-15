import api from './client'
import type { RawIngest } from '@/types/evidence'

/**
 * ADR-022 §5 / TASKS.md L0-3: resolve a content_hash to its unique archived
 * source response (`ingest.raw`, immutable).
 *
 * A 404 is a first-class answer — it means "never ingested", not "lookup
 * failed". Callers must branch on `ApiError.isNotFound` instead of treating
 * it as a transport error.
 */
export async function getEvidence(contentHash: string): Promise<RawIngest> {
  return api.get<RawIngest>(`/api/evidence/${encodeURIComponent(contentHash)}`)
}