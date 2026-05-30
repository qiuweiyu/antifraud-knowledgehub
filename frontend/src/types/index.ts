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

export interface ApiEnvelope<T> {
  success: boolean
  data: T
  error?: { code: string; message: string }
}
