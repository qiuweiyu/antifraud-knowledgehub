package rule

import (
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
)

func TestPostgresRuleVersionPreparationLegacyBaselineAndProjectionIntegrity(t *testing.T) {
	db, _ := publicationPostgresTestDB(t)
	manual := reviewDraft("postgres_legacy_baseline").riskRule()
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareRiskRuleVersionHistory(db); err != nil {
		t.Fatal(err)
	}
	var current database.RiskRule
	if err := db.First(&current, manual.ID).Error; err != nil {
		t.Fatal(err)
	}
	var version database.RiskRuleVersion
	if err := db.Where("risk_rule_id = ? AND version = 1", manual.ID).First(&version).Error; err != nil {
		t.Fatal(err)
	}
	if current.Version != 1 || version.SourceKind != database.RiskRuleVersionSourceLegacyBaseline {
		t.Fatalf("expected PostgreSQL legacy v1 baseline: current=%+v version=%+v", current, version)
	}
	if err := database.VerifyRiskRuleMatchesVersion(current, version); err != nil {
		t.Fatal(err)
	}

	if err := db.Model(&database.RiskRule{}).Where("id = ?", current.ID).UpdateColumn("name", "same-version out-of-band change").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareRiskRuleVersionHistory(db); err == nil || !strings.Contains(err.Error(), "does not match history") {
		t.Fatalf("expected PostgreSQL projection/history mismatch to fail closed, got %v", err)
	}
}

func TestPostgresRuleVersionPreparationReconstructsControlledV1AndLegacyDriftV2(t *testing.T) {
	db, _ := publicationPostgresTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("postgres_controlled_upgrade_drift"))
	published, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a pre-I1 database: the controlled publication event/current rule
	// already exist, but no immutable version row has been introduced yet.
	if err := db.Where("risk_rule_id = ?", published.RiskRule.ID).Delete(&database.RiskRuleVersion{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&database.RiskRule{}).Where("id = ?", published.RiskRule.ID).Updates(map[string]any{
		"name": "pre-versioning PostgreSQL drift", "enabled": false,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := database.PrepareRiskRuleVersionHistory(db); err != nil {
		t.Fatal(err)
	}
	var versions []database.RiskRuleVersion
	if err := db.Where("risk_rule_id = ?", published.RiskRule.ID).Order("version asc").Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[0].SourceKind != database.RiskRuleVersionSourceControlledPublication || versions[1].Version != 2 || versions[1].SourceKind != database.RiskRuleVersionSourceLegacyBaseline {
		t.Fatalf("expected PostgreSQL controlled v1 + legacy drift v2, got %+v", versions)
	}
	if versions[0].PublicationEventID == nil || *versions[0].PublicationEventID != published.Event.ID {
		t.Fatalf("reconstructed controlled v1 must link original event: %+v", versions[0])
	}
	var current database.RiskRule
	if err := db.First(&current, published.RiskRule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 || current.Name != "pre-versioning PostgreSQL drift" || current.Enabled {
		t.Fatalf("current PostgreSQL projection must preserve legacy drift as v2: %+v", current)
	}
	if err := database.VerifyRiskRuleMatchesVersion(current, versions[1]); err != nil {
		t.Fatal(err)
	}
}