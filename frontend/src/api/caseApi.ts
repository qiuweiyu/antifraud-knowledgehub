import { client, unwrap } from './client'
import type { ScamCase } from '@/types'

export const caseApi = {
  list: () => unwrap<ScamCase[]>(client.get('/cases'))
}
