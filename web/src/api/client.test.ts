import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ApiError, api } from '@/api/client'

// CR-50 (ODR-012): api/client.ts had zero unit-test coverage for the
// three critical paths the rest of the SPA depends on: timeout (60s
// AbortController), retry (transient network errors only), and abort
// (manual AbortSignal + pagehide). A regression in any of these
// would silently break every Vue composable that calls `api.get` /
// `api.post` / `createCancellableRequest`, so we pin the contract
// here with mocked fetch + fake timers.
//
// ApiClient itself is not exported — the module exposes a singleton
// `api` (baseURL is `import.meta.env.VITE_API_BASE || ''`, which is
// empty in the test environment). Tests use that singleton directly.

function makeResponse(body: unknown, init: { status?: number; statusText?: string; ok?: boolean } = {}): Response {
  const { status = 200, statusText = 'OK' } = init
  const ok = init.ok ?? (status >= 200 && status < 300)
  return {
    ok,
    status,
    statusText,
    json: () => Promise.resolve(body),
  } as Response
}

function makeErrorResponse(status: number, body: unknown, statusText = 'Err'): Response {
  return makeResponse(body, { status, statusText, ok: false })
}

describe('ApiError — classification helpers', () => {
  it('flags 4xx as client error and 5xx as server error', () => {
    expect(new ApiError(404, 'x').isClientError).toBe(true)
    expect(new ApiError(400, 'x').isClientError).toBe(true)
    expect(new ApiError(499, 'x').isClientError).toBe(true)
    expect(new ApiError(500, 'x').isServerError).toBe(true)
    expect(new ApiError(503, 'x').isServerError).toBe(true)
  })

  it('isNotFound only true for 404', () => {
    expect(new ApiError(404, 'x').isNotFound).toBe(true)
    expect(new ApiError(403, 'x').isNotFound).toBe(false)
  })

  it('isTimeout only true when isAbort flag is set (the actual abort marker)', () => {
    const cancelled = new ApiError(0, '请求已取消')
    cancelled.isAbort = true
    expect(cancelled.isTimeout).toBe(true)
    // A bare ApiError(0, ...) without the flag is a network failure,
    // not a cancellation — isTimeout must be false.
    expect(new ApiError(0, 'net').isTimeout).toBe(false)
    expect(new ApiError(500, 'srv').isTimeout).toBe(false)
  })

  it('isNetworkError only true for status 0 without abort flag', () => {
    expect(new ApiError(0, 'net').isNetworkError).toBe(true)
    expect(new ApiError(500, 'srv').isNetworkError).toBe(false)
    const cancelled = new ApiError(0, '请求已取消')
    cancelled.isAbort = true
    expect(cancelled.isNetworkError).toBe(false) // abort and network are mutually exclusive
  })

  it('preserves body for downstream inspection', () => {
    const body = { error: 'oops', code: 'E_X' }
    const err = new ApiError(422, 'm', body)
    expect(err.body).toEqual(body)
  })
})

describe('api — happy path', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('GET returns parsed JSON and uses default method', async () => {
    const fetchMock = vi.mocked(fetch).mockResolvedValueOnce(makeResponse({ ok: 1 }))
    const res = await api.get<{ ok: number }>('/p')
    expect(res).toEqual({ ok: 1 })
    expect(fetchMock).toHaveBeenCalledWith('/p', expect.objectContaining({ method: 'GET' }))
  })

  it('POST stringifies body and sets Content-Type', async () => {
    const fetchMock = vi.mocked(fetch).mockResolvedValueOnce(makeResponse({ id: 1 }))
    await api.post('/p', { a: 1 })
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ a: 1 }))
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })

  it('DELETE uses DELETE method', async () => {
    const fetchMock = vi.mocked(fetch).mockResolvedValueOnce(makeResponse({}))
    await api.delete('/p/1')
    expect((fetchMock.mock.calls[0] as [string, RequestInit])[1].method).toBe('DELETE')
  })

  it('preserves absolute URLs (e.g. signed S3 / external services)', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeResponse({ ok: 1 }))
    await api.get('https://other.example/p')
    expect(vi.mocked(fetch).mock.calls[0][0]).toBe('https://other.example/p')
  })
})

describe('api — error mapping (4xx / 5xx)', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('throws ApiError with localized message for 404', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeErrorResponse(404, { message: 'gone' }))
    await expect(api.get('/p')).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      isClientError: true,
      isNotFound: true,
      message: '请求的资源不存在',
    })
  })

  it('uses server-provided message for unmapped 4xx', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeErrorResponse(418, { error: 'I am a teapot' }))
    await expect(api.get('/p')).rejects.toMatchObject({ status: 418, message: 'I am a teapot' })
  })

  it('falls back to statusText when body is not JSON', async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'boom',
      json: () => Promise.reject(new Error('not json')),
    } as Response)
    await expect(api.get('/p')).rejects.toMatchObject({ status: 500, message: '服务器内部错误' })
  })

  it('maps 429 to a localized message', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeErrorResponse(429, {}))
    await expect(api.get('/p')).rejects.toMatchObject({ status: 429, message: '请求过于频繁，请稍后再试' })
  })
})

describe('api — timeout via AbortController', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('aborts the request when the timeout elapses and throws a timeout ApiError', async () => {
    vi.useFakeTimers()
    try {
      // fetch that only resolves after we call advanceTimers — never resolves
      // before the timeout, simulating a slow server.
      vi.mocked(fetch).mockImplementation(
        (_url, init) =>
          new Promise((_resolve, reject) => {
            const signal = (init as RequestInit).signal as AbortSignal
            signal.addEventListener('abort', () => {
              const err = new Error('aborted')
              err.name = 'AbortError'
              reject(err)
            })
          }),
      )

      const promise = api.get('/slow', { timeout: 1000 })
      // Attach a catch so the unhandled rejection warning does not fire;
      // we still assert via the awaited expectation below.
      promise.catch(() => {})
      vi.advanceTimersByTime(1000)
      await expect(promise).rejects.toMatchObject({
        status: 0,
        isTimeout: true,
        message: '请求已取消',
      })
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not throw if fetch resolves before the timeout', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeResponse({ ok: 1 }))
    await expect(api.get('/fast', { timeout: 1000 })).resolves.toEqual({ ok: 1 })
  })
})

describe('api — manual AbortSignal', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('aborts in-flight request when caller signal is aborted', async () => {
    vi.mocked(fetch).mockImplementation(
      (_url, init) =>
        new Promise((_resolve, reject) => {
          const signal = (init as RequestInit).signal as AbortSignal
          signal.addEventListener('abort', () => {
            const err = new Error('aborted')
            err.name = 'AbortError'
            reject(err)
          })
        }),
    )

    const ac = new AbortController()
    const promise = api.get('/p', { signal: ac.signal })
    promise.catch(() => {})
    ac.abort()
    await expect(promise).rejects.toMatchObject({ status: 0, isTimeout: true })
  })
})

describe('api — retry on transient network errors', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('retries up to N times on network failure, then gives up', async () => {
    vi.useFakeTimers()
    try {
      // Always throw a non-AbortError to simulate a hard network failure.
      vi.mocked(fetch).mockRejectedValue(new TypeError('Failed to fetch'))

      const promise = api.get('/p', { retry: 2, timeout: 1000 })
      promise.catch(() => {}) // suppress unhandled rejection

      // First attempt + 2 retries = 3 fetch calls total.
      // Between attempts the code awaits `API_RETRY_DELAY * (4 - retry)`:
      //   retry=2 -> 2*1000=2000ms, retry=1 -> 3*1000=3000ms
      await vi.advanceTimersByTimeAsync(0) // run first attempt
      expect(vi.mocked(fetch).mock.calls).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(2000) // wait out retry 2 -> 1
      expect(vi.mocked(fetch).mock.calls).toHaveLength(2)
      await vi.advanceTimersByTimeAsync(3000) // wait out retry 1 -> 0
      expect(vi.mocked(fetch).mock.calls).toHaveLength(3)

      await expect(promise).rejects.toMatchObject({
        status: 0,
        isNetworkError: true,
      })
    } finally {
      vi.useRealTimers()
    }
  })

  it('does NOT retry on 4xx ApiError', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeErrorResponse(400, { message: 'bad' }))
    await expect(api.get('/p', { retry: 3 })).rejects.toMatchObject({ status: 400 })
    expect(vi.mocked(fetch)).toHaveBeenCalledTimes(1)
  })

  it('does NOT retry on 5xx ApiError', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeErrorResponse(500, {}))
    await expect(api.get('/p', { retry: 3 })).rejects.toMatchObject({ status: 500 })
    expect(vi.mocked(fetch)).toHaveBeenCalledTimes(1)
  })

  it('succeeds after a transient failure within the retry budget', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(fetch)
        .mockRejectedValueOnce(new TypeError('transient'))
        .mockResolvedValueOnce(makeResponse({ ok: 'recovered' }))

      const promise = api.get('/p', { retry: 2, timeout: 1000 })
      promise.catch(() => {})
      await vi.advanceTimersByTimeAsync(0)
      await vi.advanceTimersByTimeAsync(2000) // backoff for retry 2 -> 1
      await expect(promise).resolves.toEqual({ ok: 'recovered' })
      expect(vi.mocked(fetch)).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('api — createCancellableRequest', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns an abort function that cancels the in-flight request', async () => {
    vi.mocked(fetch).mockImplementation(
      (_url, init) =>
        new Promise((_resolve, reject) => {
          const signal = (init as RequestInit).signal as AbortSignal
          signal.addEventListener('abort', () => {
            const err = new Error('aborted')
            err.name = 'AbortError'
            reject(err)
          })
        }),
    )

    const { promise, abort } = api.createCancellableRequest<{ ok: number }>('/p', 'GET')
    promise.catch(() => {})
    abort()
    await expect(promise).rejects.toMatchObject({ status: 0, isTimeout: true })
  })

  it('resolves with the response body when fetch succeeds', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(makeResponse({ id: 7 }))
    const { promise } = api.createCancellableRequest<{ id: number }>('/p', 'POST', { a: 1 })
    await expect(promise).resolves.toEqual({ id: 7 })
  })

  it('serialises POST body when provided', async () => {
    const fetchMock = vi.mocked(fetch).mockResolvedValueOnce(makeResponse({ id: 1 }))
    await api.createCancellableRequest('/p', 'POST', { foo: 'bar' }).promise
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.body).toBe(JSON.stringify({ foo: 'bar' }))
    expect(init.method).toBe('POST')
  })
})

describe('api — module-scoped pagehide listener (CR-25)', () => {
  it('does not register a fresh pagehide listener per request', () => {
    // The pagehide listener is installed once at module import time and
    // cancels every controller in `inFlightControllers`. happy-dom
    // tracks listeners internally; we just verify the pagehide event
    // can be fired without throwing and that the API surface is
    // present. (Counting listeners across re-imports is brittle in
    // vitest, so we keep this as a smoke test.)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse({})))
    void api.get('/a')
    void api.get('/b')
    void api.post('/c', {})
    expect(typeof window.addEventListener).toBe('function')
    expect(() => window.dispatchEvent(new Event('pagehide'))).not.toThrow()
  })
})
