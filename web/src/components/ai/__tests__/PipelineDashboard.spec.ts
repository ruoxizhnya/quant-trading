import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'
import PipelineDashboard from '../PipelineDashboard.vue'
import * as copilotApi from '@/api/copilot'

// Mock the API module
vi.mock('@/api/copilot', () => ({
  runPipeline: vi.fn()
}))

// Mock navigator.clipboard
Object.defineProperty(navigator, 'clipboard', {
  value: {
    writeText: vi.fn().mockResolvedValue(undefined)
  },
  writable: true,
  configurable: true
})

// Mock URL.createObjectURL and URL.revokeObjectURL
global.URL.createObjectURL = vi.fn(() => 'blob:test')
global.URL.revokeObjectURL = vi.fn()

describe('PipelineDashboard', () => {
  const createWrapper = (options = {}) => {
    return mount({
      components: { NMessageProvider, PipelineDashboard },
      template: '<n-message-provider><pipeline-dashboard ref="dashboard" /></n-message-provider>',
      ...options
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Component Rendering', () => {
    it('renders within message provider context', () => {
      const wrapper = createWrapper()
      expect(wrapper.find('.pipeline-dashboard').exists()).toBe(true)
    })

    it('has correct CSS classes', () => {
      const wrapper = createWrapper()
      expect(wrapper.find('.pipeline-dashboard').exists()).toBe(true)
      expect(wrapper.find('.dashboard-card').exists()).toBe(true)
    })
  })

  describe('API Integration', () => {
    it('calls runPipeline with correct parameters', async () => {
      const mockResult = {
        id: 'test-job-1',
        status: 'complete' as const,
        intent: {
          strategy_type: 'momentum',
          strategy_name: 'test_strategy',
          description: '测试策略',
          parameters: [],
          indicators: ['rsi'],
          timeframe: '1d',
          universe: 'csi300',
          confidence: 0.85,
          raw_text: '测试'
        },
        yaml_config: 'strategy:\n  name: test',
        generated_code: 'package plugins',
        started_at: new Date().toISOString(),
        duration_ms: 1000,
        logs: ['开始执行', '完成']
      }

      vi.mocked(copilotApi.runPipeline).mockResolvedValue(mockResult)

      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)

      // Set description and trigger generation
      ;(dashboard.vm as any).strategyDescription = '动量策略'
      await (dashboard.vm as any).handleGenerate()
      await flushPromises()

      expect(copilotApi.runPipeline).toHaveBeenCalledWith('动量策略')
    })

    it('handles API errors', async () => {
      vi.mocked(copilotApi.runPipeline).mockRejectedValue(new Error('API Error'))

      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      ;(dashboard.vm as any).strategyDescription = '测试策略'

      await (dashboard.vm as any).handleGenerate()
      await flushPromises()

      expect((dashboard.vm as any).isProcessing).toBe(false)
    })
  })

  describe('Computed Properties', () => {
    it('computes progress percentage correctly', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      vm.currentResult = { status: 'parse', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressPercentage).toBe(20)

      vm.currentResult = { status: 'generate', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressPercentage).toBe(40)

      vm.currentResult = { status: 'complete', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressPercentage).toBe(100)

      vm.currentResult = { status: 'failed', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressPercentage).toBe(0)
    })

    it('computes progress status correctly', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      vm.currentResult = { status: 'complete', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressStatus).toBe('success')

      vm.currentResult = { status: 'failed', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressStatus).toBe('error')

      vm.currentResult = { status: 'parse', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.progressStatus).toBe('default')
    })

    it('computes step status correctly', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      vm.currentResult = { status: 'complete', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.stepStatus).toBe('finish')

      vm.currentResult = { status: 'failed', id: '1', started_at: '', duration_ms: 0 }
      expect(vm.stepStatus).toBe('error')
    })

    it('formats logs text correctly', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      vm.currentResult = {
        id: '1',
        status: 'complete',
        started_at: '',
        duration_ms: 0,
        logs: ['log1', 'log2', 'log3']
      }

      expect(vm.logsText).toBe('log1\nlog2\nlog3')
    })

    it('returns empty string when no logs', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any
      expect(vm.logsText).toBe('')
    })
  })

  describe('Strategy Type Tags', () => {
    it('returns correct tag type for each strategy type', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      expect(vm.getStrategyTypeTag('momentum')).toBe('success')
      expect(vm.getStrategyTypeTag('mean_reversion')).toBe('warning')
      expect(vm.getStrategyTypeTag('breakout')).toBe('error')
      expect(vm.getStrategyTypeTag('multi_factor')).toBe('info')
      expect(vm.getStrategyTypeTag('value')).toBe('success')
      expect(vm.getStrategyTypeTag('quality')).toBe('info')
      expect(vm.getStrategyTypeTag('custom')).toBe('default')
      expect(vm.getStrategyTypeTag(undefined)).toBe('default')
      expect(vm.getStrategyTypeTag('unknown')).toBe('default')
    })
  })

  describe('State Management', () => {
    it('clears state when handleClear is called', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      vm.strategyDescription = '测试策略'
      vm.currentResult = { status: 'complete', id: '1', started_at: '', duration_ms: 0 }
      vm.currentJob = 'job-1'

      vm.handleClear()

      expect(vm.strategyDescription).toBe('')
      expect(vm.currentResult).toBeNull()
      expect(vm.currentJob).toBeNull()
    })

    it('initializes with correct default values', () => {
      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any

      expect(vm.strategyDescription).toBe('')
      expect(vm.isProcessing).toBe(false)
      expect(vm.currentJob).toBeNull()
      expect(vm.currentResult).toBeNull()
      expect(vm.jobHistory).toEqual([])
      expect(vm.currentStep).toBe(0)
      expect(vm.statusMessage).toBe('')
    })
  })

  describe('History Management', () => {
    it('adds completed job to history', async () => {
      const mockResult = {
        id: 'test-job-1',
        status: 'complete' as const,
        intent: {
          strategy_type: 'momentum',
          strategy_name: 'test',
          description: 'test',
          parameters: [],
          indicators: [],
          timeframe: '1d',
          universe: 'csi300',
          confidence: 0.8,
          raw_text: 'test'
        },
        started_at: new Date().toISOString(),
        duration_ms: 1000,
        logs: []
      }

      vi.mocked(copilotApi.runPipeline).mockResolvedValue(mockResult)

      const wrapper = createWrapper()
      const dashboard = wrapper.findComponent(PipelineDashboard)
      const vm = dashboard.vm as any
      vm.strategyDescription = '测试策略'

      await vm.handleGenerate()
      await flushPromises()

      expect(vm.jobHistory).toHaveLength(1)
      expect(vm.jobHistory[0].id).toBe('test-job-1')
    })
  })
})
