export function fmtPercent(n: number | null | undefined): string {
  if (n == null || isNaN(n)) return '-'
  return (n >= 0 ? '+' : '') + (n * 100).toFixed(2) + '%'
}

export function fmtNumber(n: number | null | undefined, decimals = 2): string {
  if (n == null || isNaN(n)) return '-'
  return n.toLocaleString('zh-CN', { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
}

export function fmtCurrency(n: number | null | undefined): string {
  if (n == null || isNaN(n)) return '-'
  if (Math.abs(n) >= 10000) return '¥' + (n / 10000).toFixed(2) + '万'
  return '¥' + n.toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 0 })
}

export function formatDate(d: string): string {
  if (!d) return '-'
  return new Date(d).toLocaleDateString('zh-CN')
}

export function formatTime(d: string): string {
  if (!d) return '-'
  return new Date(d).toLocaleTimeString('zh-CN')
}

export function fmtVolume(v: any): string {
  if (!v) return '-'
  const num = Number(v)
  if (num >= 1e8) return (num / 1e8).toFixed(1) + '亿'
  return (num / 1e4).toFixed(0) + '万'
}

export function fmtAmount(a: any): string {
  if (!a) return '-'
  return (Number(a) / 1e8).toFixed(1)
}

export function fmtMetric(v: any): string {
  if (v == null || isNaN(Number(v))) return String(v ?? '-')
  return Math.abs(Number(v)) > 1 ? Number(v).toFixed(4) : (Number(v) * 100).toFixed(2) + '%'
}
