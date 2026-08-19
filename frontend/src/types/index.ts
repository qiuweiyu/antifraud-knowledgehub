export interface Category {
  id: number
  code: string
  name: string
  description: string
  severity_default: string
}

export interface RiskRule {
  id: number
  code: string
  name: string
  description: string
  category_code: string
  rule_type: string
  pattern: string
  weight: number
  severity: string
  enabled: boolean
  explanation: string
  recommendation: string
}

export interface ScamCase {
  id: number
  title: string
  category_code: string
  content: string
  summary: string
  risk_points: string[]
  tags: string[]
  source_type: string
  anonymized: boolean
}

export interface MatchedRule {
  rule_code: string
  rule_name: string
  category_code: string
  weight: number
  severity: string
  evidence: string
  explanation: string
  recommendation: string
}

export interface AnalysisResult {
  risk_score: number
  risk_level: string
  matched_rules: MatchedRule[]
  summary: string
  recommendations: string[]
}

export interface BrowserSession {
  principal_id: string
  display_label?: string
  expires_at: string
  csrf_token: string
}

export type AssistedProfileAvailability = 'available' | 'unavailable' | 'disabled'

export interface AssistedProfileMetadata {
  id: string
  display_name: string
  provider_display_name: string
  model_display_name: string
  availability: AssistedProfileAvailability
  disclosure: string
}

export interface LLMAssistance {
  summary: string
  observations: string[] | null
  limitations: string[] | null
}

export interface BrowserAssistedLLMResult {
  status: 'available' | 'unavailable'
  assistance: LLMAssistance
  profile: AssistedProfileMetadata
}

export interface BrowserAssistedAnalysisResult {
  rule_result: AnalysisResult
  llm_assistance: BrowserAssistedLLMResult
}

export interface CategoryDistribution {
  category_code: string
  category_name: string
  rule_count: number
  case_count: number
}

export interface AnalysisStats {
  categories: number
  rules: number
  enabled_rules: number
  cases: number
  analysis_records: number
  risk_level_distribution: Record<string, number>
  category_distribution: CategoryDistribution[]
}

export interface ApiEnvelope<T> {
  success: boolean
  data: T
  error?: { code: string; message: string }
}
