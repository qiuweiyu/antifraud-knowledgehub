import { client, unwrap } from './client'
import type { ScamCase } from '@/types'

export interface CaseListParams {
  q?: string
  category_code?: string
}

export const caseApi = {
  list: (params?: CaseListParams) => unwrap<ScamCase[]>(client.get('/cases', { params }))
}
