package riskengine

import "testing"

func testEngine() Engine {
	return New([]Rule{
		{Code: "fake_cs", Name: "冒充客服", CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "客服,账户异常", Weight: 25, Severity: "high", Explanation: "出现客服或账户异常话术。", Recommendation: "通过官方渠道核实。"},
		{Code: "safe_account", Name: "安全账户", CategoryCode: "fake_customer_service", RuleType: "keyword", Pattern: "安全账户", Weight: 35, Severity: "critical", Explanation: "要求转账到安全账户。", Recommendation: "不要向陌生账户转账。"},
		{Code: "cashback_advance", Name: "刷单垫付", CategoryCode: "cashback_task", RuleType: "pattern", Pattern: "刷单+垫付", Weight: 45, Severity: "high", Explanation: "刷单并要求垫付。", Recommendation: "拒绝垫付任务。"},
		{Code: "guaranteed_profit", Name: "保证收益", CategoryCode: "investment_fraud", RuleType: "keyword", Pattern: "保证收益,稳赚不赔", Weight: 30, Severity: "high", Explanation: "承诺保证收益。", Recommendation: "警惕高收益承诺。"},
		{Code: "phishing_url", Name: "可疑链接", CategoryCode: "phishing_link", RuleType: "regex", Pattern: `https?://[^\s]+`, Weight: 25, Severity: "medium", Explanation: "包含外部链接。", Recommendation: "不要点击不明链接。"},
	})
}

func TestFakeCustomerServiceSafeAccount(t *testing.T) {
	result := testEngine().Analyze("客服说我的账户异常，需要马上转账到安全账户验证。")
	if result.RiskLevel != "high" && result.RiskLevel != "critical" {
		t.Fatalf("expected high or critical, got %s", result.RiskLevel)
	}
	if len(result.MatchedRules) < 2 {
		t.Fatalf("expected multiple matched rules, got %d", len(result.MatchedRules))
	}
}

func TestCashbackAdvancePattern(t *testing.T) {
	result := testEngine().Analyze("这个刷单任务需要先垫付，完成后返佣。")
	if result.RiskScore < 45 {
		t.Fatalf("expected cashback risk score, got %d", result.RiskScore)
	}
}

func TestInvestmentGuaranteedProfit(t *testing.T) {
	result := testEngine().Analyze("老师带单，项目保证收益，稳赚不赔。")
	if result.RiskScore < 30 {
		t.Fatalf("expected investment risk, got %d", result.RiskScore)
	}
}

func TestPhishingURLAccountAbnormal(t *testing.T) {
	result := testEngine().Analyze("账户异常，请点击链接验证 https://example.invalid/check")
	if len(result.MatchedRules) < 2 {
		t.Fatalf("expected phishing and abnormal account matches, got %d", len(result.MatchedRules))
	}
}

func TestOrdinaryTextLowRisk(t *testing.T) {
	result := testEngine().Analyze("今天下午三点开项目例会，请大家准备周报。")
	if result.RiskLevel != "low" || result.RiskScore != 0 {
		t.Fatalf("expected low risk, got %s %d", result.RiskLevel, result.RiskScore)
	}
}
