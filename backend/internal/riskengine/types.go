package riskengine

type Rule struct {
	Code           string
	Name           string
	CategoryCode   string
	RuleType       string
	Pattern        string
	Weight         int
	Severity       string
	Explanation    string
	Recommendation string
}

type MatchedRule struct {
	RuleCode       string `json:"rule_code"`
	RuleName       string `json:"rule_name"`
	CategoryCode   string `json:"category_code"`
	Weight         int    `json:"weight"`
	Severity       string `json:"severity"`
	Evidence       string `json:"evidence"`
	Explanation    string `json:"explanation"`
	Recommendation string `json:"recommendation"`
}

type Result struct {
	RiskScore       int           `json:"risk_score"`
	RiskLevel       string        `json:"risk_level"`
	MatchedRules    []MatchedRule `json:"matched_rules"`
	Summary         string        `json:"summary"`
	Recommendations []string      `json:"recommendations"`
}

type Engine struct {
	rules []Rule
}

func New(rules []Rule) Engine {
	return Engine{rules: rules}
}
