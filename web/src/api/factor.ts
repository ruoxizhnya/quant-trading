import api from './client'
import type { FactorCacheEntry } from '@/types/factor'

/**
 * TASKS.md P5-3 / ADR-022 §5: read one factor_cache row together with its
 * evidence coordinates. The row is the A→B link (which archived source
 * response produced this number), and each returned tuple can be fed to
 * `getEvidence` to walk back to the original response.
 *
 * A 404 is a first-class answer — "no factor_cache row for this
 * symbol/date/factor" — not a transport failure.
 */
export async function getFactorCitation(
  factorName: string,
  symbol: string,
  date: string,
): Promise<FactorCacheEntry> {
  const query = new URLSearchParams({ symbol, date })
  return api.get<FactorCacheEntry>(
    `/api/factors/${encodeURIComponent(factorName)}?${query.toString()}`,
  )
}