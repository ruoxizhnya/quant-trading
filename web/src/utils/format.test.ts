import { describe, it, expect } from 'vitest'
import { fmtPercent, fmtNumber, fmtCurrency, formatDate, fmtVolume, fmtMetric } from '@/utils/format'

describe('fmtPercent', () => {
  it('formats positive percentage with plus sign', () => {
    expect(fmtPercent(0.15)).toBe('+15.00%')
  })

  it('formats negative percentage without plus sign', () => {
    expect(fmtPercent(-0.08)).toBe('-8.00%')
  })

  it('formats zero percentage', () => {
    expect(fmtPercent(0)).toBe('+0.00%')
  })

  it('returns dash for null', () => {
    expect(fmtPercent(null)).toBe('-')
  })

  it('returns dash for undefined', () => {
    expect(fmtPercent(undefined)).toBe('-')
  })

  it('returns dash for NaN', () => {
    expect(fmtPercent(NaN)).toBe('-')
  })

  it('formats small percentage correctly', () => {
    expect(fmtPercent(0.0001)).toBe('+0.01%')
  })

  it('formats large percentage correctly', () => {
    expect(fmtPercent(1.5)).toBe('+150.00%')
  })
})

describe('fmtNumber', () => {
  it('formats number with default 2 decimals', () => {
    expect(fmtNumber(1234.567)).toBe('1,234.57')
  })

  it('formats number with custom decimals', () => {
    expect(fmtNumber(1234.567, 0)).toBe('1,235')
  })

  it('returns dash for null', () => {
    expect(fmtNumber(null)).toBe('-')
  })

  it('returns dash for undefined', () => {
    expect(fmtNumber(undefined)).toBe('-')
  })

  it('formats zero', () => {
    expect(fmtNumber(0)).toBe('0.00')
  })

  it('formats negative number', () => {
    expect(fmtNumber(-500.5)).toBe('-500.50')
  })
})

describe('fmtCurrency', () => {
  it('formats large number as wan', () => {
    expect(fmtCurrency(150000)).toBe('¥15.00万')
  })

  it('formats small number as integer', () => {
    expect(fmtCurrency(9999)).toBe('¥9,999')
  })

  it('returns dash for null', () => {
    expect(fmtCurrency(null)).toBe('-')
  })

  it('formats exactly 10000 as wan', () => {
    expect(fmtCurrency(10000)).toBe('¥1.00万')
  })
})

describe('formatDate', () => {
  it('formats ISO date string', () => {
    const result = formatDate('2024-01-15T00:00:00Z')
    expect(result).toBeTruthy()
    expect(result).not.toBe('-')
  })

  it('returns dash for empty string', () => {
    expect(formatDate('')).toBe('-')
  })
})

describe('fmtVolume', () => {
  it('formats large volume as yi', () => {
    expect(fmtVolume(200000000)).toBe('2.0亿')
  })

  it('formats medium volume as wan', () => {
    expect(fmtVolume(50000)).toBe('5万')
  })

  it('returns dash for falsy value', () => {
    expect(fmtVolume(0)).toBe('-')
    expect(fmtVolume(null)).toBe('-')
  })
})

describe('fmtMetric', () => {
  it('formats absolute value > 1 as fixed decimal', () => {
    expect(fmtMetric(12.3456)).toBe('12.3456')
  })

  it('formats absolute value <= 1 as percentage', () => {
    expect(fmtMetric(0.15)).toBe('15.00%')
  })

  it('returns dash for null', () => {
    expect(fmtMetric(null)).toBe('-')
  })

  it('returns dash for undefined', () => {
    expect(fmtMetric(undefined)).toBe('-')
  })

  it('handles negative values', () => {
    expect(fmtMetric(-0.05)).toBe('-5.00%')
  })
})
