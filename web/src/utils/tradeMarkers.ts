import type { PortfolioPoint, Trade } from '@/types/api'

export interface TradeMarker {
  date: string
  value: number
  direction: string
  symbol: string
  price: number | undefined
}

export function buildTradeMarkers(portfolioValues: PortfolioPoint[], trades: Trade[]): TradeMarker[] {
  if (!portfolioValues?.length || !trades.length) return []
  const pvMap = new Map<string, number>()
  portfolioValues.forEach((p) => {
    const d = (p.date || '').split('T')[0]
    if (d) pvMap.set(d, p.total_value || 0)
  })

  return trades.map(t => {
    let tradeDate = ''
    let tradePrice: number | undefined = undefined

    if (t.direction === 'close') {
      tradeDate = (t.timestamp || t.exit_date || '').split('T')[0]
      tradePrice = t.price ?? t.exit_price
    } else {
      tradeDate = (t.timestamp || t.entry_date || '').split('T')[0]
      tradePrice = t.price ?? t.entry_price
    }

    return {
      date: tradeDate,
      value: pvMap.get(tradeDate) || 0,
      direction: t.direction,
      symbol: t.symbol,
      price: tradePrice,
    }
  }).filter(m => m.date && m.value > 0)
}
