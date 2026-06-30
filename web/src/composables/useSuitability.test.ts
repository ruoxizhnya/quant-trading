// useSuitability.test.ts — S7-P2-9
//
// Tests the investor-suitability precheck state machine extracted from
// PaperTrading.vue. The composable has no lifecycle hooks (no
// onMounted/onUnmounted), so it can be exercised directly without
// mounting a host component.
//
// Coverage:
//   1. resetSuitability restores the initial hidden state.
//   2. refreshSuitability with empty symbol resets (no API call).
//   3. refreshSuitability with a valid symbol calls checkSuitability
//      and renders the verdict banner (allowed or rejected).
//   4. refreshSuitability on API failure resets to hidden.
//   5. ensureSuitability returns the boolean verdict and surfaces
//      transport errors via message.error.
//   6. extractErrorMessage shapes axios-like errors, plain Errors,
//      and unknown values into a user-facing string.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useSuitability, extractErrorMessage } from '@/composables/useSuitability'
import type { CheckResponse } from '@/api/compliance'

vi.mock('@/api/compliance', () => ({
  checkSuitability: vi.fn(),
}))

import { checkSuitability } from '@/api/compliance'

const mockCheck = checkSuitability as unknown as ReturnType<typeof vi.fn>

// Minimal message recorder — captures calls so assertions can inspect
// what the composable surfaced to the user.
function makeMessageRecorder() {
  const calls: { method: string; content: string }[] = []
  return {
    calls,
    api: {
      error(content: string) {
        calls.push({ method: 'error', content })
      },
      warning(content: string) {
        calls.push({ method: 'warning', content })
      },
      success(content: string) {
        calls.push({ method: 'success', content })
      },
    },
  }
}

const allowedResponse: CheckResponse = {
  allowed: true,
  board: 'main',
  board_name: '主板',
  reasons: [],
  user_id: 'u-1',
  profile_age_months: 36,
  asset_daily_avg_cny: 200000,
  risk_level: 'C3',
  checked_at: '2026-06-30T00:00:00Z',
}

const rejectedResponse: CheckResponse = {
  allowed: false,
  board: 'chinext',
  board_name: '创业板',
  reasons: ['日均资产不足 10 万元', '风险测评等级不达标'],
  user_id: 'u-1',
  profile_age_months: 1,
  asset_daily_avg_cny: 5000,
  risk_level: 'C1',
  checked_at: '2026-06-30T00:00:00Z',
}

beforeEach(() => {
  mockCheck.mockReset()
})

describe('useSuitability — resetSuitability', () => {
  it('restores the initial hidden state', async () => {
    mockCheck.mockResolvedValueOnce(allowedResponse)
    const recorder = makeMessageRecorder()
    const { suitabilityState, resetSuitability, refreshSuitability } = useSuitability(recorder.api)

    await refreshSuitability('000001.SZ')
    expect(suitabilityState.visible).toBe(true)
    expect(suitabilityState.checked).toBe(true)

    resetSuitability()
    expect(suitabilityState.visible).toBe(false)
    expect(suitabilityState.checked).toBe(false)
    expect(suitabilityState.allowed).toBe(false)
    expect(suitabilityState.title).toBe('')
    expect(suitabilityState.boardName).toBe('')
    expect(suitabilityState.reasons).toHaveLength(0)
  })
})

describe('useSuitability — refreshSuitability', () => {
  it('resets without calling the API when symbol is empty', async () => {
    const recorder = makeMessageRecorder()
    const { suitabilityState, refreshSuitability } = useSuitability(recorder.api)

    await refreshSuitability('   ')
    expect(mockCheck).not.toHaveBeenCalled()
    expect(suitabilityState.visible).toBe(false)
  })

  it('renders the allowed banner when the verdict is allowed', async () => {
    mockCheck.mockResolvedValueOnce(allowedResponse)
    const recorder = makeMessageRecorder()
    const { suitabilityState, refreshSuitability } = useSuitability(recorder.api)

    await refreshSuitability('000001.SZ')

    expect(mockCheck).toHaveBeenCalledWith({ symbol: '000001.SZ' })
    expect(suitabilityState.visible).toBe(true)
    expect(suitabilityState.checked).toBe(true)
    expect(suitabilityState.allowed).toBe(true)
    expect(suitabilityState.boardName).toBe('主板')
    expect(suitabilityState.reasons).toHaveLength(0)
    expect(suitabilityState.title).toContain('适当性检查通过')
  })

  it('renders the rejected banner with reasons when the verdict is rejected', async () => {
    mockCheck.mockResolvedValueOnce(rejectedResponse)
    const recorder = makeMessageRecorder()
    const { suitabilityState, refreshSuitability } = useSuitability(recorder.api)

    await refreshSuitability('300750.SZ')

    expect(suitabilityState.visible).toBe(true)
    expect(suitabilityState.allowed).toBe(false)
    expect(suitabilityState.boardName).toBe('创业板')
    expect(suitabilityState.reasons).toHaveLength(2)
    expect(suitabilityState.title).toContain('适当性检查未通过')
  })

  it('resets to hidden on API failure so the operator is not blocked by a stale verdict', async () => {
    mockCheck.mockRejectedValueOnce(new Error('network down'))
    const recorder = makeMessageRecorder()
    const { suitabilityState, refreshSuitability } = useSuitability(recorder.api)

    await refreshSuitability('600000.SH')

    expect(suitabilityState.visible).toBe(false)
    expect(suitabilityState.checked).toBe(false)
    // No message is surfaced here — refreshSuitability is a passive
    // blur-time check; only ensureSuitability (the submit-time gate)
    // surfaces errors to the user.
    expect(recorder.calls).toHaveLength(0)
  })
})

describe('useSuitability — ensureSuitability', () => {
  it('returns true and renders the allowed banner when the verdict is allowed', async () => {
    mockCheck.mockResolvedValueOnce(allowedResponse)
    const recorder = makeMessageRecorder()
    const { suitabilityState, ensureSuitability } = useSuitability(recorder.api)

    const ok = await ensureSuitability('000001.SZ')

    expect(ok).toBe(true)
    expect(suitabilityState.allowed).toBe(true)
    expect(suitabilityState.visible).toBe(true)
  })

  it('returns false when the verdict is rejected without surfacing a message', async () => {
    mockCheck.mockResolvedValueOnce(rejectedResponse)
    const recorder = makeMessageRecorder()
    const { ensureSuitability } = useSuitability(recorder.api)

    const ok = await ensureSuitability('300750.SZ')

    expect(ok).toBe(false)
    // The rejected verdict is rendered in the banner; ensureSuitability
    // does NOT additionally call message.error for a clean rejection.
    expect(recorder.calls).toHaveLength(0)
  })

  it('returns false and surfaces message.error on transport failure', async () => {
    mockCheck.mockRejectedValueOnce(new Error('500 Internal Server Error'))
    const recorder = makeMessageRecorder()
    const { ensureSuitability } = useSuitability(recorder.api)

    const ok = await ensureSuitability('600000.SH')

    expect(ok).toBe(false)
    expect(recorder.calls).toHaveLength(1)
    expect(recorder.calls[0].method).toBe('error')
    expect(recorder.calls[0].content).toContain('500 Internal Server Error')
  })
})

describe('extractErrorMessage', () => {
  it('extracts the response.data.error field from an axios-like error', () => {
    const error = {
      response: { data: { error: 'insufficient capital' } },
      message: 'Request failed with status code 422',
    }
    expect(extractErrorMessage(error, 'fallback')).toBe('insufficient capital')
  })

  it('falls back to the error.message when response.data.error is absent', () => {
    const error = new Error('network timeout')
    expect(extractErrorMessage(error, 'fallback')).toBe('network timeout')
  })

  it('returns the fallback for non-object errors', () => {
    expect(extractErrorMessage('string error', 'fallback')).toBe('fallback')
    expect(extractErrorMessage(undefined, 'fallback')).toBe('fallback')
    expect(extractErrorMessage(null, 'fallback')).toBe('fallback')
  })

  it('returns the fallback when neither response.data.error nor message is present', () => {
    const error = { unrelated: 'field' }
    expect(extractErrorMessage(error, 'fallback')).toBe('fallback')
  })
})
