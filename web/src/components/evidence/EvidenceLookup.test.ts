// EvidenceLookup.test.ts — P5-1 slice A (L0 Evidence API frontend consumption)
//
// The component is the boundary where the API's 404 becomes user-visible
// semantics: "not ingested" is an answer, not an error (ADR-022 §5).

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import EvidenceLookup from './EvidenceLookup.vue'
import * as evidenceApi from '@/api/evidence'
import { ApiError } from '@/api/client'
import type { RawIngest } from '@/types/evidence'

vi.mock('@/api/evidence', () => ({
  getEvidence: vi.fn(),
}))

const mockRecord: RawIngest = {
  content_hash: 'a'.repeat(64),
  source: 'tushare',
  dataset: 'daily',
  key: '000001.SZ',
  as_of: '2026-09-14T00:00:00Z',
  payload: { close: 10.5 },
  fetched_at: '2026-09-15T00:00:00Z',
}

async function lookup(wrapper: VueWrapper, hash: string) {
  await wrapper.find('input').setValue(hash)
  const submit = wrapper.findAll('button').find((b) => b.text().includes('查询'))!
  await submit.trigger('click')
  await flushPromises()
}

describe('EvidenceLookup', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('disables submit until a hash is entered', async () => {
    const wrapper = mount(EvidenceLookup)
    const submit = wrapper.findAll('button').find((b) => b.text().includes('查询'))!

    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.find('input').setValue(mockRecord.content_hash)
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('renders the archived record resolved from a hash', async () => {
    vi.spyOn(evidenceApi, 'getEvidence').mockResolvedValue(mockRecord)

    const wrapper = mount(EvidenceLookup)
    await lookup(wrapper, mockRecord.content_hash)

    expect(evidenceApi.getEvidence).toHaveBeenCalledWith(mockRecord.content_hash)
    expect(wrapper.text()).toContain('tushare')
    expect(wrapper.text()).toContain('daily')
    expect(wrapper.text()).toContain('000001.SZ')
    expect(wrapper.text()).toContain('"close": 10.5')
  })

  it('reports a 404 as "not ingested" rather than an error', async () => {
    vi.spyOn(evidenceApi, 'getEvidence').mockRejectedValue(
      new ApiError(404, '请求的资源不存在'),
    )

    const wrapper = mount(EvidenceLookup)
    await lookup(wrapper, 'deadbeef')

    expect(wrapper.text()).toContain('未摄取')
  })

  it('surfaces non-404 failures as an error', async () => {
    vi.spyOn(evidenceApi, 'getEvidence').mockRejectedValue(
      new ApiError(500, '服务器内部错误'),
    )

    const wrapper = mount(EvidenceLookup)
    await lookup(wrapper, 'deadbeef')

    expect(wrapper.text()).toContain('服务器内部错误')
    expect(wrapper.text()).not.toContain('未摄取')
  })
})