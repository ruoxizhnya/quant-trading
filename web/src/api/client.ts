export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

interface RequestOptions extends RequestInit {
  timeout?: number
  retry?: number
}

class ApiClient {
  private baseURL: string

  constructor(baseURL: string = '') {
    this.baseURL = baseURL || import.meta.env.VITE_API_BASE || ''
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const { timeout = 60000, retry = 0, ...init } = options

    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), timeout)

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
        try {
          const errBody = await res.json()
          errMsg = errBody.error || errBody.message || res.statusText
        } catch {
          errMsg = res.statusText
        }
        throw new ApiError(res.status, errMsg)
      }

      return await res.json()
    } catch (e: any) {
      clearTimeout(timer)
      if (e?.name === 'AbortError') throw e
      if (retry > 0) {
        await new Promise(r => setTimeout(r, 1000))
        return this.request<T>(path, { ...options, retry: retry - 1 })
      }
      throw e
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
}

export const api = new ApiClient()
export default api
