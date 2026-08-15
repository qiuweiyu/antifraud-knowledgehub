import { client, unwrap } from './client'
import type { RiskRule } from '@/types'

export interface RuleListParams {
  q?: string
  category_code?: string
  severity?: string
}

export interface RuleDraft {
  code: string
  name: string
  description: string
  category_code: string
  rule_type: 'keyword' | 'pattern' | 'semantic_placeholder' | 'regex'
  pattern: string
  weight: number
  severity: 'low' | 'medium' | 'high' | 'critical'
  explanation: string
  recommendation: string
}

export interface RuleValidationMessage {
  field: string
  code: string
  message: string
}

export interface RuleValidationResult {
  valid: boolean
  errors: RuleValidationMessage[]
  warnings: RuleValidationMessage[]
}

export const ruleApi = {
  list: (params?: RuleListParams) => unwrap<RiskRule[]>(client.get('/rules', { params })),
  validate: (draft: RuleDraft) => unwrap<RuleValidationResult>(client.post('/rules/validate', draft))
}
