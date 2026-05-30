import { client, unwrap } from './client'

export const healthApi = {
  get: () => unwrap<{ status: string; service: string }>(client.get('/health'))
}
