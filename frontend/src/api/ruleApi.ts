import { client, unwrap } from './client'
import type { RiskRule } from '@/types'

export const ruleApi = {
  list: () => unwrap<RiskRule[]>(client.get('/rules'))
}
