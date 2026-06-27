import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NConfigProvider, NMessageProvider } from 'naive-ui'
import type { Plugin } from 'vue'
import ReviewActions from '../ReviewActions.vue'
import * as copilotApi from '@/api/copilot'
import type { PipelineResult } from '@/types/pipeline'

// Mock the copilot API module. The component imports only the review
// function, but we mock the whole module to keep the mock surface stable.
vi.mock('@/api/copilot', () => ({
  submitPipelineReview: vi.fn()
}))

// Mock the message provider needs for n-modal's teleport / scrollbar
// (Naive UI requires either a real DOM or NConfigProvider to mount modals).
const createWrapper = (props: Record<string, unknown> = {}) => {
  return mount(ReviewActions, {
    props: {
      jobId: 'job-1',
      yamlConfig: 'strategy:\n  name: foo',
      ...props
    },
    global: {
      // Naive UI provider components expose an `install` method at
      // runtime (so app.use() works), but their DefineComponent types
      // don't surface it — hence the Plugin cast is type-only.
      plugins: [NConfigProvider, NMessageProvider] as unknown as Plugin[]
    }
  })
}

const baseReviewedResult: PipelineResult = {
  id: 'job-1',
  status: 'complete',
  started_at: '2026-06-12T10:00:00Z',
  duration_ms: 1000,
  review: {
    decision: 'approve',
    reviewed_at: '2026-06-12T10:05:00Z',
    comment: 'OK'
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ReviewActions — rendering', () => {
  it('shows three buttons when not yet reviewed', () => {
    const wrapper = createWrapper()
    const buttons = wrapper.findAll('button')
    // 3 main buttons (通过 / 拒绝 / 编辑 YAML) + comment input rendered separately
    expect(buttons.length).toBeGreaterThanOrEqual(3)
    expect(wrapper.text()).toContain('通过')
    expect(wrapper.text()).toContain('拒绝')
    expect(wrapper.text()).toContain('编辑 YAML')
  })

  it('hides action buttons when already reviewed', () => {
    const wrapper = createWrapper({ review: baseReviewedResult.review })
    // Buttons replaced by an alert showing the decision
    expect(wrapper.text()).toContain('已通过')
    expect(wrapper.text()).toContain('OK')
    expect(wrapper.text()).not.toContain('编辑 YAML')
  })

  it('disables Edit button when no yamlConfig provided', () => {
    const wrapper = createWrapper({ yamlConfig: undefined })
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    const editBtn = buttons.find(b => b.text().includes('编辑 YAML'))
    expect(editBtn?.props('disabled')).toBe(true)
  })
})

describe('ReviewActions — submission', () => {
  it('approve: calls submitPipelineReview with decision=approve', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue(baseReviewedResult)

    const wrapper = createWrapper()
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    const approveBtn = buttons.find(b => b.text().includes('通过'))!
    await approveBtn.trigger('click')
    await flushPromises()

    expect(copilotApi.submitPipelineReview).toHaveBeenCalledWith('job-1', {
      decision: 'approve'
    })
  })

  it('reject: calls submitPipelineReview with decision=reject', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue({
      ...baseReviewedResult,
      review: { decision: 'reject', reviewed_at: '2026-06-12T10:05:00Z' }
    })

    const wrapper = createWrapper()
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    const rejectBtn = buttons.find(b => b.text().includes('拒绝'))!
    await rejectBtn.trigger('click')
    await flushPromises()

    expect(copilotApi.submitPipelineReview).toHaveBeenCalledWith('job-1', {
      decision: 'reject'
    })
  })

  it('includes comment in payload when comment input has value', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue(baseReviewedResult)

    const wrapper = createWrapper()
    const vm = wrapper.vm as any
    vm.comment = 'looks-good-but-check-stoploss'

    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    const approveBtn = buttons.find(b => b.text().includes('通过'))!
    await approveBtn.trigger('click')
    await flushPromises()

    expect(copilotApi.submitPipelineReview).toHaveBeenCalledWith('job-1', {
      decision: 'approve',
      comment: 'looks-good-but-check-stoploss'
    })
  })

  it('emits reviewed event with server response', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue(baseReviewedResult)

    const wrapper = createWrapper()
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('通过'))!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('reviewed')).toBeTruthy()
    expect(wrapper.emitted('reviewed')![0][0]).toEqual(baseReviewedResult)
  })

  it('clears comment after successful submission', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue(baseReviewedResult)

    const wrapper = createWrapper()
    const vm = wrapper.vm as any
    vm.comment = 'temp comment'
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('通过'))!.trigger('click')
    await flushPromises()

    expect(vm.comment).toBe('')
  })

  it('does not emit reviewed on API error', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockRejectedValue(new Error('server error'))

    const wrapper = createWrapper()
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('通过'))!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('reviewed')).toBeFalsy()
  })
})

describe('ReviewActions — edit modal', () => {
  it('opens modal when Edit button is clicked', async () => {
    const wrapper = createWrapper()
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('编辑 YAML'))!.trigger('click')
    await flushPromises()

    // The modal renders an extra "提交编辑" button when open
    expect(wrapper.text()).toContain('提交编辑')
  })

  it('pre-fills editedYaml with current yamlConfig when modal opens', async () => {
    const yaml = 'strategy:\n  name: foo\n  params: {x: 1}'
    const wrapper = createWrapper({ yamlConfig: yaml })
    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('编辑 YAML'))!.trigger('click')
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.editedYaml).toBe(yaml)
  })

  it('edit submit: calls API with decision=edit and edited_yaml', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue({
      ...baseReviewedResult,
      review: { decision: 'edit', reviewed_at: '2026-06-12T10:05:00Z' }
    })

    const wrapper = createWrapper()
    const vm = wrapper.vm as any
    vm.editedYaml = 'strategy:\n  name: bar'

    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('编辑 YAML'))!.trigger('click')
    await flushPromises()
    await buttons.find(b => b.text().includes('提交编辑'))!.trigger('click')
    await flushPromises()

    expect(copilotApi.submitPipelineReview).toHaveBeenCalledWith('job-1', {
      decision: 'edit',
      edited_yaml: 'strategy:\n  name: bar'
    })
  })

  it('edit submit: blocks empty edited_yaml', async () => {
    const wrapper = createWrapper()
    const vm = wrapper.vm as any
    vm.editedYaml = '   '

    const buttons = wrapper.findAllComponents({ name: 'NButton' })
    await buttons.find(b => b.text().includes('编辑 YAML'))!.trigger('click')
    await flushPromises()
    const submitBtn = buttons.find(b => b.text().includes('提交编辑'))!
    expect(submitBtn.props('disabled')).toBe(true)
  })
})

describe('ReviewActions — alert states for prior decisions', () => {
  it('shows success alert for approve decision', () => {
    const wrapper = createWrapper({
      review: { decision: 'approve', reviewed_at: '2026-06-12T10:00:00Z' }
    })
    expect(wrapper.text()).toContain('已通过')
  })

  it('shows error alert for reject decision', () => {
    const wrapper = createWrapper({
      review: { decision: 'reject', reviewed_at: '2026-06-12T10:00:00Z' }
    })
    expect(wrapper.text()).toContain('已拒绝')
  })

  it('shows info alert for edit decision', () => {
    const wrapper = createWrapper({
      review: { decision: 'edit', reviewed_at: '2026-06-12T10:00:00Z' }
    })
    expect(wrapper.text()).toContain('已编辑')
  })
})
