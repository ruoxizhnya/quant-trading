// FactorCitation.test.ts — TASKS.md P5-3 (L0 citation coordinates on the
// factor face)
//
// The component is where a citation tuple becomes user-visible truth:
//   - the four-tuple fields are shown when the source response is archived;
//   - a hash-only tuple is flagged 未归档 instead of being padded;
//   - a 404 row is "未命中", not an error;
//   - 回溯证据 hands the hash to /evidence for a one-click walk back.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import FactorCitation from './FactorCitation.vue'
import * as factorApi from '@/api/factor'
import { ApiError } from '@/api/client'
import type { FactorCacheEntry } from '@/types/factor'

const { push } = vi.hoisted(() => ({ push: vi.fn() }))

vi.mock('@/api/factor', () => ({
  getFactorCitation: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
}))

const hash = 'a'.repeat(64)

const mockEntry: FactorCacheEntry = {
  id: 1,
  symbol: '000001.SZ',
  trade_date: '2026-01-05T00:00:00Z',
  factor_name: 'momentum',
  raw_value: 1.25,
  z_score: 0.5,
  percentile: 0.69,
  citation: [
    {
      content_hash: hash,
      source: 'tushare',
      dataset: 'daily',
      key: '000001.SZ',
      as_of: '2026-01-05T00:00:00Z',
    },
  ],
}

async function load(wrapper: VueWrapper, date = '20260105') {
  await wrapper.find('input[placeholder^="因子"]').setValue('momentum')
  await wrapper.find('input[placeholder^="标的"]').setValue('000001.SZ')
  await wrapper.find('input[placeholder^="交易日"]').setValue(date)
  const submit = wrapper.findAll('button').find((b) => b.text().includes('查询'))!
  await submit.trigger('click')
  await flushPromises()
}

function action(wrapper: VueWrapper, label: string) {
  return wrapper.findAll('button').find((b) => b.text().includes(label))!
}

describe('FactorCitation', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    push.mockReset()
  })

  it('disables submit until factor, symbol and YYYYMMDD date are all present', async () => {
    const wrapper = mount(FactorCitation)
    const submit = action(wrapper, '查询')

    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.find('input[placeholder^="因子"]').setValue('momentum')
    await wrapper.find('input[placeholder^="标的"]').setValue('000001.SZ')
    // A malformed date must not unlock the submit: the row is addressed by it.
    await wrapper.find('input[placeholder^="交易日"]').setValue('2026-01-05')
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.find('input[placeholder^="交易日"]').setValue('20260105')
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('renders the archived five-tuple behind a factor row', async () => {
    vi.spyOn(factorApi, 'getFactorCitation').mockResolvedValue(mockEntry)

    const wrapper = mount(FactorCitation)
    await load(wrapper)

    expect(factorApi.getFactorCitation).toHaveBeenCalledWith('momentum', '000001.SZ', '20260105')
    expect(wrapper.text()).toContain('tushare')
    expect(wrapper.text()).toContain('daily')
    expect(wrapper.text()).toContain('000001.SZ')
    expect(wrapper.text()).toContain(hash)
    expect(wrapper.text()).toContain('已归档')
  })

  it('flags a hash-only tuple as 未归档 without inventing the missing fields', async () => {
    vi.spyOn(factorApi, 'getFactorCitation').mockResolvedValue({
      ...mockEntry,
      citation: [{ content_hash: hash }],
    })

    const wrapper = mount(FactorCitation)
    await load(wrapper)

    expect(wrapper.text()).toContain('未归档')
    expect(wrapper.text()).toContain(hash)
    expect(wrapper.text()).not.toContain('tushare')
  })

  it('reports a row-less lookup as 未命中 rather than an error', async () => {
    vi.spyOn(factorApi, 'getFactorCitation').mockRejectedValue(
      new ApiError(404, '请求的资源不存在'),
    )

    const wrapper = mount(FactorCitation)
    await load(wrapper)

    expect(wrapper.text()).toContain('未命中')
  })

  it('surfaces non-404 failures as an error', async () => {
    vi.spyOn(factorApi, 'getFactorCitation').mockRejectedValue(
      new ApiError(502, '网关服务不可用'),
    )

    const wrapper = mount(FactorCitation)
    await load(wrapper)

    expect(wrapper.text()).toContain('网关服务不可用')
    expect(wrapper.text()).not.toContain('未命中')
  })

  it('walks a coordinate back to the evidence page in one click', async () => {
    vi.spyOn(factorApi, 'getFactorCitation').mockResolvedValue(mockEntry)

    const wrapper = mount(FactorCitation)
    await load(wrapper)
    await action(wrapper, '回溯证据').trigger('click')

    expect(push).toHaveBeenCalledWith({ name: 'evidence', query: { content_hash: hash } })
  })

  it('states that a row with an empty citation has no A→B chain yet', async () => {
    vi.spyOn(factorApi, 'getFactorCitation').mockResolvedValue({
      ...mockEntry,
      citation: [],
    })

    const wrapper = mount(FactorCitation)
    await load(wrapper)

    expect(wrapper.text()).toContain('暂无证据坐标')
  })
})