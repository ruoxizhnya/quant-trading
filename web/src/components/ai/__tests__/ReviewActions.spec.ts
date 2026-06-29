import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { NMessageProvider, NButton } from 'naive-ui'
import ReviewActions from '../ReviewActions.vue'
import * as copilotApi from '@/api/copilot'
import type { PipelineResult } from '@/types/pipeline'

// Mock the copilot API module. The component imports only the review
// function, but we mock the whole module to keep the mock surface stable.
vi.mock('@/api/copilot', () => ({
  submitPipelineReview: vi.fn()
}))

// S7-P0-9 (ODR-043-9): Naive UI provider components (NMessageProvider,
// NConfigProvider, ...) are COMPONENTS, not Vue plugins. Passing them via
// the global.plugins mount option triggers two failures:
//   1. "A plugin must either be a function or an object with an install
//      function" — because provider components have no `install` method.
//   2. "No outer <n-message-provider /> founded" — because providers passed
//      as plugins never establish the provide/inject context that
//      `useMessage()` depends on at setup time.
//
// Fix: render NMessageProvider as an ANCESTOR of ReviewActions by mounting
// a tiny host component whose render function wraps the SUT. This mirrors
// the established pattern in PipelineDashboard.spec.ts.
//
// Default props (jobId, yamlConfig) are merged with any per-test overrides
// so individual tests don't need to repeat them.
const DEFAULT_PROPS: Record<string, unknown> = {
  jobId: 'job-1',
  yamlConfig: 'strategy:\n  name: foo'
}

const createWrapper = (props: Record<string, unknown> = {}) => {
  const mergedProps = { ...DEFAULT_PROPS, ...props }
  const host = defineComponent({
    name: 'ReviewActionsTestHost',
    setup() {
      // Cast to any: h() infers ReviewActions' strict prop types, but
      // mergedProps is typed as Record<string, unknown> for ergonomic
      // per-test overrides. Runtime behavior is unaffected.
      return () => h(NMessageProvider, () => h(ReviewActions, { ...mergedProps } as any))
    }
  })
  return mount(host)
}

// Helper: returns the ReviewActions child wrapper so tests can access
// `vm.comment`, `vm.editedYaml`, `vm.showEditModal`, and `emitted('reviewed')`
// on the actual component under test (rather than on the host).
const reviewChild = (wrapper: VueWrapper): VueWrapper<any> =>
  wrapper.findComponent(ReviewActions)

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
    // S7-P0-9 (ODR-043-9): use the NButton component reference, not
    // a name-string query — Naive UI registers NButton with the name
    // 'Button' (no N prefix), so { name: 'NButton' } silently returns
    // nothing. Passing the imported component is name-agnostic.
    const buttons = wrapper.findAllComponents(NButton)
    const editBtn = buttons.find(b => b.text().includes('编辑 YAML'))
    expect(editBtn?.props('disabled')).toBe(true)
  })
})

describe('ReviewActions — submission', () => {
  it('approve: calls submitPipelineReview with decision=approve', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue(baseReviewedResult)

    const wrapper = createWrapper()
    const buttons = wrapper.findAllComponents(NButton)
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
    const buttons = wrapper.findAllComponents(NButton)
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
    const vm = reviewChild(wrapper).vm as any
    vm.comment = 'looks-good-but-check-stoploss'

    const buttons = wrapper.findAllComponents(NButton)
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
    const child = reviewChild(wrapper)
    const buttons = wrapper.findAllComponents(NButton)
    await buttons.find(b => b.text().includes('通过'))!.trigger('click')
    await flushPromises()

    expect(child.emitted('reviewed')).toBeTruthy()
    expect(child.emitted('reviewed')![0][0]).toEqual(baseReviewedResult)
  })

  it('clears comment after successful submission', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue(baseReviewedResult)

    const wrapper = createWrapper()
    const child = reviewChild(wrapper)
    const vm = child.vm as any
    vm.comment = 'temp comment'
    const buttons = wrapper.findAllComponents(NButton)
    await buttons.find(b => b.text().includes('通过'))!.trigger('click')
    await flushPromises()

    expect(vm.comment).toBe('')
  })

  it('does not emit reviewed on API error', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockRejectedValue(new Error('server error'))

    const wrapper = createWrapper()
    const child = reviewChild(wrapper)
    const buttons = wrapper.findAllComponents(NButton)
    await buttons.find(b => b.text().includes('通过'))!.trigger('click')
    await flushPromises()

    expect(child.emitted('reviewed')).toBeFalsy()
  })
})

describe('ReviewActions — edit modal', () => {
  // S7-P0-9 (ODR-043-9): NModal teleports its content to document.body,
  // so wrapper.text() / wrapper.findAllComponents cannot see the modal's
  // inner buttons. Tests that need to interact with the modal's submit
  // button either (a) assert on vm.showEditModal state, or (b) invoke
  // vm.onEditSubmit() directly after seeding vm state. This still
  // exercises the real submission code path — onEditSubmit is the same
  // function the rendered button's @click handler calls.

  it('opens modal when Edit button is clicked', async () => {
    const wrapper = createWrapper()
    const child = reviewChild(wrapper)
    const buttons = wrapper.findAllComponents(NButton)
    await buttons.find(b => b.text().includes('编辑 YAML'))!.trigger('click')
    await flushPromises()

    // NModal content is teleported to document.body, so we verify the
    // open state via the component's vm instead of wrapper.text().
    expect((child.vm as any).showEditModal).toBe(true)
  })

  it('pre-fills editedYaml with current yamlConfig when modal opens', async () => {
    const yaml = 'strategy:\n  name: foo\n  params: {x: 1}'
    const wrapper = createWrapper({ yamlConfig: yaml })
    const buttons = wrapper.findAllComponents(NButton)
    await buttons.find(b => b.text().includes('编辑 YAML'))!.trigger('click')
    await flushPromises()

    const vm = reviewChild(wrapper).vm as any
    expect(vm.editedYaml).toBe(yaml)
  })

  it('edit submit: calls API with decision=edit and edited_yaml', async () => {
    vi.mocked(copilotApi.submitPipelineReview).mockResolvedValue({
      ...baseReviewedResult,
      review: { decision: 'edit', reviewed_at: '2026-06-12T10:05:00Z' }
    })

    const wrapper = createWrapper()
    const vm = reviewChild(wrapper).vm as any
    vm.editedYaml = 'strategy:\n  name: bar'

    // Open the modal and invoke the submit handler directly. NModal
    // teleports its content to document.body, so the "提交编辑" button
    // is not reachable via wrapper.findAllComponents. Calling
    // onEditSubmit() exercises the same code path as the button click.
    vm.showEditModal = true
    await flushPromises()
    vm.onEditSubmit()
    await flushPromises()

    expect(copilotApi.submitPipelineReview).toHaveBeenCalledWith('job-1', {
      decision: 'edit',
      edited_yaml: 'strategy:\n  name: bar'
    })
  })

  it('edit submit: blocks empty edited_yaml', async () => {
    const wrapper = createWrapper()
    const vm = reviewChild(wrapper).vm as any
    vm.editedYaml = '   '
    vm.showEditModal = true
    await flushPromises()

    // When edited_yaml is empty/whitespace, onEditSubmit should show a
    // warning and NOT call the API. The rendered button's :disabled
    // binding prevents the click in the real UI; here we verify the
    // handler's guard directly.
    vm.onEditSubmit()
    await flushPromises()

    expect(copilotApi.submitPipelineReview).not.toHaveBeenCalled()
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

// ============================================================
// S7-P0-9 (ODR-043-9): Regression guards
// ------------------------------------------------------------
// Prevents reintroducing the two bugs that caused all 16 original
// ReviewActions tests to fail:
//   1. Mounting Naive UI providers via global.plugins (they're components,
//      not plugins — use a host component that renders them as ancestors).
//   2. Querying NButton by { name: 'NButton' } — Naive UI registers NButton
//      as 'Button' (no N prefix), so the string query returns nothing.
//      Use the imported component reference instead.
//
// We strip JS comments before scanning so this guard's own explanatory
// comments don't trip the assertion. The forbidden name-string is built
// dynamically for the same reason.
// ============================================================
function stripJsComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '') // block comments
    .replace(/(^|[^:])\/\/.*$/gm, '$1') // line comments (avoid http:// false positives)
}

describe('ReviewActions.spec — regression guards', () => {
  it('does not pass Naive UI providers via the global.plugins mount option', async () => {
    const source = await import('./ReviewActions.spec.ts?raw')
    const text: string = (source as any).default ?? String(source)
    const code = stripJsComments(text)
    // Forbidden: a naive-ui provider name inside a plugins: [...] array.
    // The host-component pattern uses h(NMessageProvider, ...) which is fine.
    expect(code).not.toMatch(/plugins:\s*\[[^\]]*N(?:Message|Config|Dialog)Provider/)
  })

  it('does not query NButton by the wrong name string', async () => {
    const source = await import('./ReviewActions.spec.ts?raw')
    const text: string = (source as any).default ?? String(source)
    const code = stripJsComments(text)
    // Build the forbidden literal dynamically so this assertion doesn't
    // contain the string it's looking for.
    const wrongName = ['{', ' ', 'name:', ' ', "'NBu", "tton'", ' ', '}'].join('')
    expect(code).not.toContain(wrongName)
  })
})
