// factor.test.ts — TASKS.md P5-3 (L0 citation coordinates on the factor face)
//
// Contract under test (ADR-022 §5):
//   1. GET /api/factors/{factor_name}?symbol=&date= — the factor name must be
//      URL-encoded so it cannot escape the route segment, and both selectors
//      must travel as query params (the row is addressed by all three).
//   2. Rejections propagate unchanged; the caller (FactorCitation) is the one
//      that turns 404 into "no such factor_cache row" UI semantics.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { getFactorCitation } from './factor'
import api from './client'
import type { FactorCacheEntry } from '@/types/factor'

vi.mock('./client', () => ({
  default: { get: vi.fn() },
}))

describe('getFactorCitation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('requests the factor_cache read with both selectors forwarded', async () => {
    const entry: FactorCacheEntry = {
      id: 1,
      symbol: '000001.SZ',
      trade_date: '2026-01-05T00:00:00Z',
      factor_name: 'momentum',
      raw_value: 1.25,
      z_score: 0.5,
      percentile: 0.69,
      citation: [{ content_hash: 'a'.repeat(64), source: 'tushare', dataset: 'daily' }],
    }
    vi.mocked(api.get).mockResolvedValue(entry)

    const result = await getFactorCitation('momentum', '000001.SZ', '20260105')

    expect(api.get).toHaveBeenCalledWith(
      '/api/factors/momentum?symbol=000001.SZ&date=20260105',
    )
    expect(result).toEqual(entry)
  })

  it('url-encodes the factor name so path separators cannot escape the route', async () => {
    vi.mocked(api.get).mockResolvedValue({})

    await getFactorCitation('a/b c', '000001.SZ', '20260105')

    expect(api.get).toHaveBeenCalledWith(
      '/api/factors/a%2Fb%20c?symbol=000001.SZ&date=20260105',
    )
  })

  it('propagates the not-found rejection unchanged', async () => {
    const notFound = new Error('factor cache entry not found')
    vi.mocked(api.get).mockRejectedValue(notFound)

    await expect(getFactorCitation('momentum', '000001.SZ', '20260105')).rejects.toBe(
      notFound,
    )
  })
})