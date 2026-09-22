// EmergencyFlatten.test.ts — S7-P2-8
//
// Tests the kill-switch component's integration contract:
//   1. Renders arm/disarm UI correctly.
//   2. Emits 'flattened' after a successful emergencyFlatten() call,
//      so the parent can refresh its positions/orders.
//
// The emit is the integration point added in S7-P2-8 — before this,
// EmergencyFlatten was a standalone component with no way to signal
// the parent that positions have changed.
//
// AUD-19（2026-09-22）：父组件从 PaperTrading.vue 换成了 Dashboard.vue
// （前者连同 /api/paper/* 整条线已删除）。emit 契约本身没变，所以下面的
// 断言照旧 —— 只是控制台没有持仓视图，当前不监听这个事件。

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { NMessageProvider } from 'naive-ui'
import EmergencyFlatten from './EmergencyFlatten.vue'
import * as executionApi from '@/api/execution'
import type { EmergencyFlattenResult } from '@/api/execution'

// Mount helper: NMessageProvider must be an ancestor because
// EmergencyFlatten calls useMessage().
function mountFlatten() {
  const TestHost = defineComponent({
    name: 'EmergencyFlattenTestHost',
    setup() {
      return () => h(NMessageProvider, () => h(EmergencyFlatten))
    },
  })
  return mount(TestHost)
}

const mockResult: EmergencyFlattenResult = {
  sold: [
    {
      symbol: '600000.SH',
      order_id: 'ord-1',
      quantity: 1000,
      fill_price: 10.52,
      net_proceeds: 10520,
      bypassed_t1: true,
      submitted_at: '2026-06-30T10:00:00Z',
    },
  ],
  skipped: [],
  sold_total: 10520,
  started_at: '2026-06-30T10:00:00Z',
  completed_at: '2026-06-30T10:00:01Z',
  reason: 'test reason',
  latency_ms: 42,
}

describe('EmergencyFlatten', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    // window.confirm is called in confirm(); auto-accept for tests.
    vi.stubGlobal('confirm', () => true)
  })

  it('renders the arm button in safe state', () => {
    const wrapper = mountFlatten()
    expect(wrapper.text()).toContain('紧急平仓')
    expect(wrapper.text()).toContain('安全')
  })

  it('emits flattened event after successful emergency flatten', async () => {
    vi.spyOn(executionApi, 'emergencyFlatten').mockResolvedValue(mockResult)

    const wrapper = mountFlatten()
    const flatten = wrapper.findComponent(EmergencyFlatten)

    // 1. Arm the kill switch.
    const armButton = wrapper.find('button')
    expect(armButton.text()).toContain('Arm')
    await armButton.trigger('click')

    // 2. Fill in reason + token (required for canSubmit).
    const inputs = wrapper.findAll('textarea, input')
    const reasonInput = inputs.find((i) => i.element.tagName === 'TEXTAREA')!
    const tokenInput = inputs.find((i) => i.attributes('type') === 'password')!
    await reasonInput.setValue('系统检测到异常行情')
    await tokenInput.setValue('test-token-123')

    // 3. Confirm.
    const confirmButton = wrapper
      .findAll('button')
      .find((b) => b.text().includes('确认紧急平仓'))!
    await confirmButton.trigger('click')
    await flushPromises()

    // 4. Assert emit.
    expect(executionApi.emergencyFlatten).toHaveBeenCalledWith('test-token-123', '系统检测到异常行情')
    expect(flatten.emitted('flattened')).toBeTruthy()
    expect(flatten.emitted('flattened')!.length).toBe(1)
  })

  it('does NOT emit flattened on API failure', async () => {
    vi.spyOn(executionApi, 'emergencyFlatten').mockRejectedValue(new Error('server error'))

    const wrapper = mountFlatten()
    const flatten = wrapper.findComponent(EmergencyFlatten)

    const armButton = wrapper.find('button')
    await armButton.trigger('click')

    const inputs = wrapper.findAll('textarea, input')
    const reasonInput = inputs.find((i) => i.element.tagName === 'TEXTAREA')!
    const tokenInput = inputs.find((i) => i.attributes('type') === 'password')!
    await reasonInput.setValue('test')
    await tokenInput.setValue('tok')

    const confirmButton = wrapper
      .findAll('button')
      .find((b) => b.text().includes('确认紧急平仓'))!
    await confirmButton.trigger('click')
    await flushPromises()

    expect(flatten.emitted('flattened')).toBeFalsy()
  })
})
