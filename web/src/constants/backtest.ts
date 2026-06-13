// P2-2 (ODR-027): persistent comparison selection lives in localStorage so
// the user can mark backtests from the engine page, then jump to the
// compare page with the selection still intact (e.g. across reloads).
//
// Storage shape: JSON array of backtest IDs (strings), capped at
// MAX_COMPARE_IDS to prevent unbounded growth. Older entries are dropped
// from the head (FIFO) when the cap is exceeded.

export const COMPARE_STORAGE_KEY = 'quantlab:backtest:compare_ids'
export const MAX_COMPARE_IDS = 8

export function loadCompareIds(): string[] {
  try {
    const raw = localStorage.getItem(COMPARE_STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((v): v is string => typeof v === 'string').slice(-MAX_COMPARE_IDS)
  } catch {
    return []
  }
}

export function saveCompareIds(ids: string[]): void {
  try {
    const trimmed = ids.slice(-MAX_COMPARE_IDS)
    localStorage.setItem(COMPARE_STORAGE_KEY, JSON.stringify(trimmed))
  } catch {
    // localStorage may be unavailable (private mode, quota); fail soft
    // and let the next read return [] — the UI degrades to "no selection"
    // rather than crashing the backtest page.
  }
}

export function toggleCompareId(id: string): string[] {
  const ids = loadCompareIds()
  const idx = ids.indexOf(id)
  if (idx >= 0) {
    ids.splice(idx, 1)
  } else {
    ids.push(id)
  }
  saveCompareIds(ids)
  return ids
}

export function clearCompareIds(): void {
  try { localStorage.removeItem(COMPARE_STORAGE_KEY) } catch { /* ignore */ }
}
