import apiClient from './client'

export interface FactorReturn {
  id: number
  factor_name: string
  trade_date: string
  quintile: number
  avg_return: number
  cumulative_return: number
  top_minus_bot: number
}

export interface ICEntry {
  id: number
  factor_name: string
  trade_date: string
  ic: number
  p_value: number
  top_ic: number
}

export interface ComputeFactorReturnsRequest {
  factor: string
  trade_date: string
}

export interface ComputeICRequest {
  factor: string
  trade_date: string
  forward_days?: number
}

// Get factor returns time series for a given factor and date range
export async function getFactorReturns(factor: string, startDate: string, endDate: string) {
  return apiClient.get<{ total: number; data: FactorReturn[] }>(
    `/api/factor/returns/${factor}?start_date=${startDate}&end_date=${endDate}`
  )
}

// Get IC time series for a given factor and date range
export async function getICEntries(factor: string, startDate: string, endDate: string) {
  return apiClient.get<{ total: number; data: ICEntry[] }>(
    `/api/factor/ic/${factor}?start_date=${startDate}&end_date=${endDate}`
  )
}

// Compute factor quintile returns for a specific date
export async function computeFactorReturns(req: ComputeFactorReturnsRequest) {
  return apiClient.post('/api/factor/compute-returns', req)
}

// Compute IC (Information Coefficient) for a specific date
export async function computeIC(req: ComputeICRequest) {
  return apiClient.post('/api/factor/compute-ic', req)
}

// List available factor types
export async function listFactors() {
  return apiClient.get<{ total: number; factors: string[] }>('/api/factor/list')
}
