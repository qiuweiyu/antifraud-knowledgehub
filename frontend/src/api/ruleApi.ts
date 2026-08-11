import { client, unwrap } from './client'
import type { RiskRule } from '@/types'

export interface RuleListParams {
  q?: string
  category_code?: string
  severity?: string
}

export const ruleApi = {
  list: (params?: RuleListParams) => unwrap<RiskRule[]>(client.get('/rules', { params }))
}
