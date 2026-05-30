import axios from 'axios'
import type { ApiEnvelope } from '@/types'

export const client = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api/v1',
  timeout: 10000
})

export async function unwrap<T>(request: Promise<{ data: ApiEnvelope<T> }>): Promise<T> {
  const response = await request
  if (!response.data.success) {
    throw new Error(response.data.error?.message || '请求失败')
  }
  return response.data.data
}
