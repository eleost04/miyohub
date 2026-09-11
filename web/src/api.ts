export type ApiResponse<T> = { ok: boolean; data?: T; error?: string; message?: string }
export class ApiError extends Error {
  constructor(message: string, public status: number, public data: unknown) { super(message); this.name = 'ApiError' }
}
let generation = 0
let session = new AbortController()
export function resetApiSession() { generation++; session.abort(); session = new AbortController() }
export async function api<T>(path: string, init: RequestInit = {}, extraSignals: AbortSignal[] = []): Promise<T> {
  const current = generation
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  headers.set('X-MiyoHub-Request', '1')
  const controller = new AbortController()
  const signals = [session.signal, ...extraSignals, ...(init.signal ? [init.signal] : [])]
  const abort = () => controller.abort()
  for (const signal of signals) { if (signal.aborted) abort(); else signal.addEventListener('abort', abort, { once: true }) }
  try {
    const response = await fetch(path, { ...init, credentials: 'same-origin', headers, signal: controller.signal })
    const payload = (await response.json()) as ApiResponse<T>
    if (current !== generation) throw new DOMException('登录状态已变化', 'AbortError')
    if (response.status === 401) window.dispatchEvent(new Event('miyohub:unauthorized'))
    if (!response.ok || !payload.ok) throw new ApiError(payload.error || '请求失败', response.status, payload.data)
    return (payload.data ?? payload) as T
  } finally { signals.forEach(signal => signal.removeEventListener('abort', abort)) }
}
