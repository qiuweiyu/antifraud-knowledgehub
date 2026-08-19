import axios from 'axios'
import { client } from './client'
import type {
  ApiEnvelope,
  BrowserAssistedAnalysisResult,
  BrowserSession,
  AssistedProfileMetadata
} from '@/types'

export class BrowserAssistedApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'BrowserAssistedApiError'
    this.status = status
    this.code = code
  }
}

function assertSameOriginBrowserApi(): void {
  if (typeof window === 'undefined') {
    throw new BrowserAssistedApiError(0, 'browser_runtime_unavailable', '浏览器 AI 辅助入口不可用')
  }
  const baseURL = client.defaults.baseURL || '/api/v1'
  const resolved = new URL(baseURL, window.location.origin)
  if (resolved.origin !== window.location.origin) {
    throw new BrowserAssistedApiError(
      0,
      'browser_assisted_cross_origin_blocked',
      'AI 辅助分析仅允许通过同源 API 使用'
    )
  }
}

async function browserRequest<T>(request: () => Promise<{ data: ApiEnvelope<T> }>): Promise<T> {
  assertSameOriginBrowserApi()
  try {
    const response = await request()
    if (!response.data.success) {
      throw new BrowserAssistedApiError(
        0,
        response.data.error?.code || 'browser_assisted_request_failed',
        response.data.error?.message || '请求失败'
      )
    }
    return response.data.data
  } catch (error) {
    if (error instanceof BrowserAssistedApiError) {
      throw error
    }
    if (axios.isAxiosError<ApiEnvelope<unknown>>(error)) {
      throw new BrowserAssistedApiError(
        error.response?.status || 0,
        error.response?.data?.error?.code || 'browser_assisted_request_failed',
        error.response?.data?.error?.message || '浏览器 AI 辅助服务请求失败'
      )
    }
    throw new BrowserAssistedApiError(0, 'browser_assisted_request_failed', '浏览器 AI 辅助服务请求失败')
  }
}

export const browserAssistedApi = {
  exchangeSession: (accessGrant: string) =>
    browserRequest<BrowserSession>(() => client.post('/browser/session/exchange', { access_grant: accessGrant })),

  currentSession: () => browserRequest<BrowserSession>(() => client.get('/browser/session')),

  logout: (csrfToken: string) =>
    browserRequest<{ logged_out: boolean }>(() =>
      client.post('/browser/session/logout', undefined, { headers: { 'X-AFKH-CSRF': csrfToken } })
    ),

  profiles: () =>
    browserRequest<AssistedProfileMetadata[]>(() => client.get('/browser/analysis/assisted/profiles')),

  analyze: (text: string, profileID: string, csrfToken: string) =>
    browserRequest<BrowserAssistedAnalysisResult>(() =>
      client.post(
        '/browser/analysis/assisted',
        { text, profile_id: profileID },
        { headers: { 'X-AFKH-CSRF': csrfToken } }
      )
    )
}
