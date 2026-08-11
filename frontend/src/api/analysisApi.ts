import { client, unwrap } from './client'
import type { AnalysisResult, AnalysisStats } from '@/types'

export const analysisApi = {
  analyzeText: (text: string) => unwrap<AnalysisResult>(client.post('/analysis/text', { text })),
  recent: () => unwrap<{ count: number }>(client.get('/analysis/recent')),
  stats: () => unwrap<AnalysisStats>(client.get('/analysis/stats'))
}
