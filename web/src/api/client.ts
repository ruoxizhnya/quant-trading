import { API_TIMEOUT, API_RETRY_DELAY } from '@/constants/api'

export class ApiError extends Error {
  constructor(public status: number, message: string, public body?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
  }

  get isClientError() { return this.status >= 400 && this.status < 500 }
  get isServerError() { return this.status >= 500 }
  get isNotFound() { return this.status === 404 }
  get isTimeout() { return this.status === 0 && this.message.includes('abort') }
  get isNetworkError() { return this.status === 0 }
}

interface RequestOptions extends RequestInit {
  timeout?: number
  retry?: number
  signal?: AbortSignal
}

const STATUS_MESSAGES: Record<number, string> = {
  400: '请求参数有误',
  401: '未授权，请重新登录',
  403: '无权限访问该资源',
  404: '请求的资源不存在',
  409: '资源冲突，请刷新后重试',
  422: '数据验证失败',
  429: '请求过于频繁，请稍后再试',
  500: '服务器内部错误',
  502: '网关服务不可用',
  503: '服务暂时不可用，请稍后重试',
  504: '服务响应超时',
}

function getStatusMessage(status: number, fallback: string): string {
  return STATUS_MESSAGES[status] || fallback
}

class ApiClient {
  private baseURL: string

  constructor(baseURL: string = '') {
    this.baseURL = baseURL || import.meta.env.VITE_API_BASE || ''
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { timeout = API_TIMEOUT, retry = 0, signal, ...init } = options

    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), timeout)

    if (signal) {
      signal.addEventListener('abort', () => controller.abort(), { once: true })
    }

    window.addEventListener('pagehide', () => controller.abort(), { once: true })

    try {
      const url = path.startsWith('http') ? path : `${this.baseURL}${path}`
      const res = await fetch(url, {
        ...init,
        signal: controller.signal,
        headers: {
          'Content-Type': 'application/json',
          ...init.headers,
        },
      })

      clearTimeout(timer)

      if (!res.ok) {
        let errMsg: string
      let errBody: Record<string, unknown> | undefined
      try {
        errBody = await res.json()
        errMsg = (errBody?.error as string) || (errBody?.message as string) || res.statusText
      } catch {
        errMsg = res.statusText
      }
      const message = getStatusMessage(res.status, errMsg)
      throw new ApiError(res.status, message, errBody)
      }

      return await res.json()
    } catch (e: unknown) {
      clearTimeout(timer)
      if (e instanceof DOMException || (e instanceof Error && e.name === 'AbortError')) {
        throw new ApiError(0, '请求已取消')
      }
      if (e instanceof ApiError) throw e
      if (retry > 0) {
        await new Promise(r => setTimeout(r, API_RETRY_DELAY * (4 - retry)))
        return this.request<T>(path, { ...options, retry: retry - 1 })
      }
      throw new ApiError(0, e instanceof Error ? e.message : '网络连接失败')
    }
  }

  get<T>(path: string, options?: RequestOptions) {
    return this.request<T>(path, { ...options, method: 'GET' })
  }

  post<T>(path: string, body?: unknown, options?: RequestOptions) {
    return this.request<T>(path, {
      ...options,
      method: 'POST',
      body: body ? JSON.stringify(body) : undefined,
    })
  }

  delete<T>(path: string, options?: RequestOptions) {
    return this.request<T>(path, { ...options, method: 'DELETE' })
  }

  createCancellableRequest<T>(path: string, method: string = 'GET', body?: unknown) {
    const controller = new AbortController()
    const promise = this.request<T>(path, {
      method,
      signal: controller.signal,
      body: body ? JSON.stringify(body) : undefined,
    }).catch(e => {
      if (e instanceof ApiError && e.isTimeout) throw e
      throw e
    })
    return { promise, abort: () => controller.abort() }
  }
}

export const api = new ApiClient()
export default api
