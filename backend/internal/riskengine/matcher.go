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
		if rule.RuleType == "pattern" && strings.Contains(rule.Pattern, "+") {
			parts := strings.Split(rule.Pattern, "+")
			evidence := make([]string, 0, len(parts))
			for _, part := range parts {
				term := strings.TrimSpace(part)
				if term == "" || !strings.Contains(text, term) {
					return ""
				}
				evidence = append(evidence, term)
			}
			return strings.Join(evidence, " + ")
		}
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
