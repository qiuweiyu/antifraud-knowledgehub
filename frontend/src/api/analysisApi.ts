import { client, unwrap } from './client'
import type { AnalysisResult } from '@/types'

export const analysisApi = {
  analyzeText: (text: string) => unwrap<AnalysisResult>(client.post('/analysis/text', { text })),
  recent: () => unwrap<{ count: number }>(client.get('/analysis/recent'))
}
