// evidence.test.ts — P5-1 slice A (L0 Evidence API frontend consumption)
//
// Contract under test (ADR-022 §5 / TASKS.md L0-3):
//   1. GET /api/evidence/{content_hash} — the hash must be URL-encoded so
//      it cannot escape the route segment.
//   2. The 404 "not ingested" answer is propagated unchanged; the caller
//      (EvidenceLookup) is the one that turns it into UI semantics.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { getEvidence } from './evidence'
import api from './client'

vi.mock('./client', () => ({
  default: { get: vi.fn() },
}))

describe('getEvidence', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('requests the L0 evidence endpoint for the given hash', async () => {
    const record = {
      content_hash: 'a'.repeat(64),
      source: 'tushare',
      dataset: 'daily',
      key: '000001.SZ',
      payload: { close: 10.5 },
      fetched_at: '2026-09-15T00:00:00Z',
    }
    vi.mocked(api.get).mockResolvedValue(record)

    const result = await getEvidence('a'.repeat(64))

    expect(api.get).toHaveBeenCalledWith(`/api/evidence/${'a'.repeat(64)}`)
    expect(result).toEqual(record)
  })

  it('url-encodes the hash so path separators cannot escape the route', async () => {
    vi.mocked(api.get).mockResolvedValue({})

    await getEvidence('a/b c')

    expect(api.get).toHaveBeenCalledWith('/api/evidence/a%2Fb%20c')
  })

  it('propagates the not-ingested rejection unchanged', async () => {
    const notIngested = new Error('evidence not ingested')
    vi.mocked(api.get).mockRejectedValue(notIngested)

    await expect(getEvidence('deadbeef')).rejects.toBe(notIngested)
  })
})