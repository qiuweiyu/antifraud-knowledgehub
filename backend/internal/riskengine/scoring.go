package riskengine

func score(matches []MatchedRule) int {
	total := 0
	for _, match := range matches {
		total += match.Weight
	}
	if total > 100 {
		return 100
	}
	return total
}

func Recommendations(matches []MatchedRule) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, match := range matches {
		if match.Recommendation != "" && !seen[match.Recommendation] {
			seen[match.Recommendation] = true
			out = append(out, match.Recommendation)
		}
	}
	if len(out) == 0 {
		return []string{"保持警惕，不向陌生账户转账，不点击不明链接。"}
	}
	return out
}

func Summary(score int, matches []MatchedRule) string {
	if len(matches) == 0 {
		return "未命中明显诈骗风险规则，但仍建议通过官方渠道核实重要信息。"
	}
	return "该文本命中多个可解释风险信号，请结合证据片段和建议动作谨慎处理。"
}
