package rule

import "testing"

func TestDraftRiskRulePreservesEnabledIntent(t *testing.T) {
	explicitTrue := true
	explicitFalse := false
	tests := []struct {
		name        string
		enabled     *bool
		wantEnabled bool
	}{
		{name: "omitted defaults true", enabled: nil, wantEnabled: true},
		{name: "explicit true stays true", enabled: &explicitTrue, wantEnabled: true},
		{name: "explicit false stays false", enabled: &explicitFalse, wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft := DraftRequest{
				Code:         "draft_enabled_mapping",
				Name:         "Draft enabled mapping",
				CategoryCode: "fake_customer_service",
				RuleType:     "keyword",
				Pattern:      "draft enabled mapping signal",
				Weight:       20,
				Severity:     "high",
				Enabled:      tt.enabled,
			}
			got := draft.riskRule()
			if got.Enabled != tt.wantEnabled {
				t.Fatalf("enabled mismatch: want %v got %v", tt.wantEnabled, got.Enabled)
			}
			if got.SourceSubmissionID != nil {
				t.Fatalf("draft mapping must not invent publication provenance, got source_submission_id=%d", *got.SourceSubmissionID)
			}
		})
	}
}
