// useSuitability.ts — S7-P2-9
//
// Extracted from PaperTrading.vue. Owns the P2-4 (ODR-028) investor-
// suitability precheck state machine: given a stock symbol, calls
// POST /api/compliance/check and tracks whether the current account
// is allowed to trade that symbol's board.
//
// The composable is parameterised by the message API (from naive-ui's
// useMessage) so it can be unit-tested without a NaiveUI host.

import { reactive } from 'vue'
import { checkSuitability } from '@/api/compliance'
import type { CheckResponse } from '@/api/compliance'

// Minimal message API surface — matches the subset of naive-ui's
// MessageApiInst that we use (success/error/warning). Defining it
// here keeps the composable decoupled from naive-ui's full type.
export interface SuitabilityMessageApi {
  error(content: string): void
  warning(content: string): void
  success?(content: string): void
}

export interface SuitabilityState {
  visible: boolean
  checked: boolean
  allowed: boolean
  title: string
  boardName: string
  reasons: string[]
}

const initialSuitability = (): SuitabilityState => ({
  visible: false,
  checked: false,
  allowed: false,
  title: '',
  boardName: '',
  reasons: [],
})

export function useSuitability(message: SuitabilityMessageApi) {
  const suitabilityState = reactive<SuitabilityState>(initialSuitability())

  function resetSuitability() {
    Object.assign(suitabilityState, initialSuitability())
  }

  function applySuitabilityResult(result: CheckResponse) {
    if (result.allowed) {
      suitabilityState.allowed = true
      suitabilityState.title = `适当性检查通过 (${result.board_name || result.board})`
      suitabilityState.boardName = result.board_name || result.board
      suitabilityState.reasons = []
    } else {
      suitabilityState.allowed = false
      suitabilityState.title = `适当性检查未通过 (${result.board_name || result.board})`
      suitabilityState.boardName = result.board_name || result.board
      suitabilityState.reasons = result.reasons || []
    }
    suitabilityState.checked = true
    suitabilityState.visible = true
  }

  // Called on input blur — proactively checks suitability for the
  // current symbol and renders the verdict banner.
  async function refreshSuitability(symbol: string) {
    const trimmed = symbol.trim()
    if (!trimmed) {
      resetSuitability()
      return
    }
    try {
      const result = await checkSuitability({ symbol: trimmed })
      applySuitabilityResult(result)
    } catch {
      resetSuitability()
    }
  }

  // Called on submit — defensive recheck so a rejected symbol can
  // never reach /api/execution/orders through the UI even if the
  // operator skipped the blur handler.
  async function ensureSuitability(symbol: string): Promise<boolean> {
    try {
      const result = await checkSuitability({ symbol })
      applySuitabilityResult(result)
      return result.allowed
    } catch (error) {
      message.error(extractErrorMessage(error, '适当性预检失败'))
      return false
    }
  }

  return {
    suitabilityState,
    resetSuitability,
    refreshSuitability,
    ensureSuitability,
  }
}

// Shared error extractor — used by useSuitability and by PaperTrading's
// handleStart/handleStop/handleSubmitOrder. Exported so both call-sites
// use the same message-shaping logic.
export function extractErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object') {
    const e = error as { response?: { data?: { error?: string } }; message?: string }
    return e.response?.data?.error || e.message || fallback
  }
  return fallback
}
