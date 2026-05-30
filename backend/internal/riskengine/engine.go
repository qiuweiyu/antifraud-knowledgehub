package riskengine

func (e Engine) Analyze(text string) Result {
	matches := matchRules(text, e.rules)
	score := score(matches)
	return Result{
		RiskScore:       score,
		RiskLevel:       Level(score),
		MatchedRules:    matches,
		Summary:         Summary(score, matches),
		Recommendations: Recommendations(matches),
	}
}
