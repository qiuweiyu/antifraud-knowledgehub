package riskengine

import (
	"regexp"
	"strings"
)

func matchRules(text string, rules []Rule) []MatchedRule {
	out := make([]MatchedRule, 0)
	for _, rule := range rules {
		evidence := matchEvidence(text, rule)
		if evidence == "" {
			continue
		}
		out = append(out, MatchedRule{
			RuleCode: rule.Code, RuleName: rule.Name, CategoryCode: rule.CategoryCode,
			Weight: rule.Weight, Severity: rule.Severity, Evidence: evidence,
			Explanation: rule.Explanation, Recommendation: rule.Recommendation,
		})
	}
	return out
}

func matchEvidence(text string, rule Rule) string {
	switch rule.RuleType {
	case "keyword", "pattern", "semantic_placeholder":
		for _, part := range strings.Split(rule.Pattern, ",") {
			term := strings.TrimSpace(part)
			if term != "" && strings.Contains(text, term) {
				return term
			}
		}
	case "regex":
		re, err := regexp.Compile(rule.Pattern)
		if err == nil {
			return re.FindString(text)
		}
	}
	return ""
}
