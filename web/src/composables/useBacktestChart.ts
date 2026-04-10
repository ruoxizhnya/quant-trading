import { ref, nextTick, onBeforeUnmount } from 'vue'
import { Chart, registerables, type ChartData as ChartJSChartData, type TooltipItem } from 'chart.js'
import type { PortfolioPoint, Trade } from '@/types/api'

Chart.register(...registerables)

const MAX_CHART_POINTS = 120

export interface TradeMarker {
  date: string
  value: number
  direction: string
  symbol: string
  price: number | undefined
}

export interface ChartData {
  labels: string[]
  datasets: ChartJSChartData['datasets']
}

export function useBacktestChart(canvasRef: { value: HTMLCanvasElement | null }) {
  let chartInstance: Chart | null = null
  const chartData = ref<{ date: string; value: number }[]>([])

  function sampleData(raw: { date: string; value: number }[]) {
    if (raw.length <= MAX_CHART_POINTS) return raw
    const step = Math.ceil(raw.length / MAX_CHART_POINTS)
    const sampled: { date: string; value: number }[] = []
    for (let i = 0; i < raw.length; i += step) {
      sampled.push(raw[i])
    }
    const last = raw[raw.length - 1]
    if (!sampled.length || sampled[sampled.length - 1].date !== last.date) {
      sampled.push(last)
    }
    return sampled
  }

  function buildTradeMarkers(portfolioValues: PortfolioPoint[], trades: Trade[]): TradeMarker[] {
    if (!portfolioValues?.length || !trades.length) return []
    const pvMap = new Map<string, number>()
    portfolioValues.forEach((p) => {
      const d = (p.date || '').split('T')[0]
      if (d) pvMap.set(d, p.total_value || 0)
    })
    return trades.map(t => ({
      date: (t.entry_date || '').split('T')[0],
      value: pvMap.get((t.entry_date || '').split('T')[0]) || 0,
      direction: t.direction,
      symbol: t.symbol,
      price: t.entry_price,
    })).filter(m => m.date && m.value > 0)
  }

  async function renderChart(
    portfolioValues: PortfolioPoint[],
    trades: Trade[],
  ) {
    if (!portfolioValues?.length) return
    try {
      destroyChart()

      const rawData = portfolioValues.map(p => ({
        date: (p.date || '').split('T')[0],
        value: Number(p.total_value) || 0,
      })).filter(d => d.date && d.value > 0)

      if (rawData.length === 0) return

      const data = sampleData(rawData)
      chartData.value = data

      await nextTick()

      if (!canvasRef.value) return
      const ctx = canvasRef.value.getContext('2d')
      if (!ctx) return

      const tradeMarkers = buildTradeMarkers(portfolioValues, trades)
      const datasets: ChartJSChartData['datasets'] = [{
        type: 'line' as const,
        label: '净值',
        data: data.map(d => d.value),
        borderColor: '#58a6ff',
        backgroundColor: 'rgba(88,166,255,0.08)',
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        borderWidth: 2,
        order: 2,
      }]

      if (tradeMarkers.length > 0) {
        const buyData: { x: number; y: number }[] = []
        const sellData: { x: number; y: number }[] = []
        const dateIndex = new Map<string, number>()
        data.forEach((d, i) => dateIndex.set(d.date, i))

        tradeMarkers.forEach(m => {
          const idx = dateIndex.get(m.date)
          if (idx != null) {
            const pt = { x: idx, y: m.value }
            if (m.direction === 'long') buyData.push(pt)
            else sellData.push(pt)
          }
        })

        if (buyData.length > 0) {
          datasets.push({
            type: 'scatter' as const,
            label: '买入',
            data: buyData,
            backgroundColor: '#3fb950',
            borderColor: '#3fb950',
            pointRadius: 5,
            pointHoverRadius: 7,
            pointStyle: 'triangle',
            order: 1,
          })
        }
        if (sellData.length > 0) {
          datasets.push({
            type: 'scatter' as const,
            label: '卖出',
            data: sellData,
            backgroundColor: '#f85149',
            borderColor: '#f85149',
            pointRadius: 5,
            pointHoverRadius: 7,
            pointStyle: 'crossRot',
            order: 1,
          })
        }
      }

      chartInstance = new Chart(ctx, {
        type: 'line',
        data: { labels: data.map(d => d.date), datasets },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: { duration: 500 },
          interaction: { mode: 'index', intersect: false },
          plugins: {
            legend: { display: true, position: 'top', labels: { color: '#8b949e', boxWidth: 12, padding: 12, font: { size: 11 } } },
            tooltip: {
              callbacks: {
                label(ctx: TooltipItem<'line' | 'scatter'>) {
                  if (ctx.dataset.type === 'scatter') {
                    const marker = tradeMarkers[ctx.dataIndex]
                    return `${marker.direction === 'long' ? '买入' : '卖出'} ${marker.symbol} @ ${marker.price?.toFixed(2)}`
                  }
                  return `${ctx.dataset.label}: ¥${(ctx.parsed.y / 1000).toFixed(0)}K`
                },
              },
            },
          },
          scales: {
            x: { grid: { color: '#21262d' }, ticks: { color: '#484f58', maxTicksLimit: 10, font: { size: 11 } } },
            y: { grid: { color: '#21262d' }, ticks: { color: '#484f58', font: { size: 11 } } },
          },
        },
      })
    } catch (e) {
      console.warn('Chart render error:', e)
    }
  }

  function destroyChart() {
    if (chartInstance) {
      chartInstance.destroy()
      chartInstance = null
    }
  }

  onBeforeUnmount(() => {
    destroyChart()
  })

  return {
    chartData,
    renderChart,
    destroyChart,
    buildTradeMarkers,
  }
}
