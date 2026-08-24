package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const (
	RiskRuleSnapshotDigestVersion                 = "afkh-risk-rule-snapshot:v1"
	RiskRuleVersionSourceControlledPublication    = "controlled_publication"
	RiskRuleVersionSourceLegacyBaseline           = "legacy_baseline"
	controlledPublicationActorKind                = "controlled_publisher"
	controlledMaintainerActorKind                 = "controlled_maintainer"
	approvedRuleSubmissionStatus                  = "approved"
	pendingRuleSubmissionStatusForVersionIntegrity = "pending"
)

type canonicalRiskRuleSnapshotV1 struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	CategoryCode   string `json:"category_code"`
	RuleType       string `json:"rule_type"`
	Pattern        string `json:"pattern"`
	Weight         int    `json:"weight"`
	Severity       string `json:"severity"`
	Enabled        bool   `json:"enabled"`
	Explanation    string `json:"explanation"`
	Recommendation string `json:"recommendation"`
}

// RiskRuleSnapshotDigest fingerprints versioned rule content only. Database
// identity, version numbers, provenance IDs, actors and timestamps are excluded.
func RiskRuleSnapshotDigest(rule RiskRule) (string, error) {
	canonical := canonicalRiskRuleSnapshotV1{
		Code:           rule.Code,
		Name:           rule.Name,
		Description:    rule.Description,
		CategoryCode:   rule.CategoryCode,
		RuleType:       rule.RuleType,
		Pattern:        rule.Pattern,
		Weight:         rule.Weight,
		Severity:       rule.Severity,
		Enabled:        rule.Enabled,
		Explanation:    rule.Explanation,
		Recommendation: rule.Recommendation,
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical risk rule snapshot: %w", err)
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(RiskRuleSnapshotDigestVersion + "\n"))
	_, _ = hash.Write(raw)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// BuildRiskRuleVersion snapshots a current/proposed rule under the requested
// history version and source kind. Callers add controlled provenance IDs before
// persistence when sourceKind is controlled_publication.
func BuildRiskRuleVersion(rule RiskRule, version uint, sourceKind string) (RiskRuleVersion, error) {
	if version == 0 {
		return RiskRuleVersion{}, fmt.Errorf("risk rule version must be positive")
	}
	if sourceKind != RiskRuleVersionSourceControlledPublication && sourceKind != RiskRuleVersionSourceLegacyBaseline {
		return RiskRuleVersion{}, fmt.Errorf("unsupported risk rule version source kind %q", sourceKind)
	}
	digest, err := RiskRuleSnapshotDigest(rule)
	if err != nil {
		return RiskRuleVersion{}, err
	}
	return RiskRuleVersion{
		RiskRuleID:     rule.ID,
		Version:        version,
		Code:           rule.Code,
		Name:           rule.Name,
		Description:    rule.Description,
		CategoryCode:   rule.CategoryCode,
		RuleType:       rule.RuleType,
		Pattern:        rule.Pattern,
		Weight:         rule.Weight,
		Severity:       rule.Severity,
		Enabled:        rule.Enabled,
		Explanation:    rule.Explanation,
		Recommendation: rule.Recommendation,
		SourceKind:     sourceKind,
		SnapshotDigest: digest,
	}, nil
}

// RiskRuleFromVersion reconstructs the exact historical rule snapshot. It is
// useful for publication replay and never implies that the snapshot is current.
func RiskRuleFromVersion(version RiskRuleVersion) RiskRule {
	return RiskRule{
		ID:                 version.RiskRuleID,
		Code:               version.Code,
		Name:               version.Name,
		Description:        version.Description,
		CategoryCode:       version.CategoryCode,
		RuleType:           version.RuleType,
		Pattern:            version.Pattern,
		Weight:             version.Weight,
		Severity:           version.Severity,
		Enabled:            version.Enabled,
		Explanation:        version.Explanation,
		Recommendation:     version.Recommendation,
		Version:            version.Version,
		SourceSubmissionID: version.SourceSubmissionID,
		CreatedAt:          version.CreatedAt,
		UpdatedAt:          version.CreatedAt,
	}
}

// VerifyRiskRuleMatchesVersion proves that a mutable current projection still
// matches the immutable history snapshot it claims to represent.
func VerifyRiskRuleMatchesVersion(rule RiskRule, version RiskRuleVersion) error {
	if rule.ID != version.RiskRuleID || rule.Version != version.Version {
		return fmt.Errorf("risk rule %d identity/version does not match history %d/v%d", rule.ID, version.RiskRuleID, version.Version)
	}
	digest, err := RiskRuleSnapshotDigest(rule)
	if err != nil {
		return err
	}
	if digest != version.SnapshotDigest {
		return fmt.Errorf("risk rule %d current projection does not match history version %d", rule.ID, version.Version)
	}
	return nil
}

// PrepareRiskRuleVersionHistory upgrades pre-versioning databases without
// pretending that unrecorded mutable history is known. It is intentionally
// idempotent and fails closed when existing audit/projection/history evidence
// disagrees.
func PrepareRiskRuleVersionHistory(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("prepare risk rule version history: nil database")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var existingVersions []RiskRuleVersion
		if err := tx.Order("risk_rule_id asc").Order("version asc").Find(&existingVersions).Error; err != nil {
			return fmt.Errorf("load existing risk rule versions: %w", err)
		}
		for _, version := range existingVersions {
			if err := validateStoredRiskRuleVersion(tx, version); err != nil {
				return err
			}
		}

		var events []RuleSubmissionPublicationEvent
		if err := tx.Order("created_at asc").Order("id asc").Find(&events).Error; err != nil {
			return fmt.Errorf("load rule publication events for version preparation: %w", err)
		}

		unlinked := make(map[uint][]RuleSubmissionPublicationEvent)
		for _, event := range events {
			var version RiskRuleVersion
			err := tx.Where("publication_event_id = ?", event.ID).First(&version).Error
			if err == nil {
				if err := validateControlledVersionAgainstEvent(tx, version, event); err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("lookup history for publication event %d: %w", event.ID, err)
			}
			unlinked[event.RiskRuleID] = append(unlinked[event.RiskRuleID], event)
		}

		for riskRuleID, pendingEvents := range unlinked {
			if len(pendingEvents) != 1 {
				return fmt.Errorf("risk rule %d has %d unlinked publication events; pre-version ordering cannot be inferred", riskRuleID, len(pendingEvents))
			}
			var historyCount int64
			if err := tx.Model(&RiskRuleVersion{}).Where("risk_rule_id = ?", riskRuleID).Count(&historyCount).Error; err != nil {
				return fmt.Errorf("count history for risk rule %d: %w", riskRuleID, err)
			}
			if historyCount != 0 {
				return fmt.Errorf("risk rule %d has existing history but publication event %d is not linked to a version", riskRuleID, pendingEvents[0].ID)
			}
			if err := backfillUnlinkedPublication(tx, pendingEvents[0]); err != nil {
				return err
			}
		}

		var currentRules []RiskRule
		if err := tx.Order("id asc").Find(&currentRules).Error; err != nil {
			return fmt.Errorf("load current risk rules for version preparation: %w", err)
		}
		for _, rule := range currentRules {
			var versions []RiskRuleVersion
			if err := tx.Where("risk_rule_id = ?", rule.ID).Order("version asc").Find(&versions).Error; err != nil {
				return fmt.Errorf("load history for risk rule %d: %w", rule.ID, err)
			}
			if len(versions) == 0 {
				if rule.SourceSubmissionID != nil {
					return fmt.Errorf("risk rule %d has controlled source submission %d but no publication-backed history", rule.ID, *rule.SourceSubmissionID)
				}
				if err := createLegacyBaseline(tx, rule, 1); err != nil {
					return err
				}
				if rule.Version != 1 {
					if err := updateCurrentRuleVersion(tx, rule.ID, 1); err != nil {
						return err
					}
				}
				continue
			}

			latest := versions[len(versions)-1]
			if rule.Version == 0 || rule.Version != latest.Version {
				return fmt.Errorf("risk rule %d current version %d does not equal latest history version %d", rule.ID, rule.Version, latest.Version)
			}
			if err := VerifyRiskRuleMatchesVersion(rule, latest); err != nil {
				return err
			}
		}
		return nil
	})
}

func backfillUnlinkedPublication(tx *gorm.DB, event RuleSubmissionPublicationEvent) error {
	submission, review, digest, err := loadAndValidatePublicationSource(tx, event)
	if err != nil {
		return err
	}
	publishedSnapshot := riskRuleFromSubmissionSnapshot(submission)
	publishedSnapshot.ID = event.RiskRuleID
	publishedSnapshot.Version = 1
	publishedSnapshot.SourceSubmissionID = uintPtr(event.SubmissionID)

	controlled, err := BuildRiskRuleVersion(publishedSnapshot, 1, RiskRuleVersionSourceControlledPublication)
	if err != nil {
		return err
	}
	controlled.SourceSubmissionID = uintPtr(event.SubmissionID)
	controlled.ReviewEventID = uintPtr(event.ReviewEventID)
	controlled.PublicationEventID = uintPtr(event.ID)
	controlled.CreatedAt = event.CreatedAt
	if controlled.SnapshotDigest == "" || event.DraftDigest != digest {
		return fmt.Errorf("publication event %d controlled snapshot digest preparation failed", event.ID)
	}
	if err := tx.Create(&controlled).Error; err != nil {
		return fmt.Errorf("create controlled history for publication event %d: %w", event.ID, err)
	}

	var current RiskRule
	err = tx.First(&current, event.RiskRuleID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load current risk rule %d for publication backfill: %w", event.RiskRuleID, err)
	}
	if current.SourceSubmissionID == nil || *current.SourceSubmissionID != event.SubmissionID {
		return fmt.Errorf("risk rule %d current publication provenance disagrees with event %d", current.ID, event.ID)
	}
	currentDigest, err := RiskRuleSnapshotDigest(current)
	if err != nil {
		return err
	}
	if currentDigest == controlled.SnapshotDigest {
		if current.Version != 1 {
			if err := updateCurrentRuleVersion(tx, current.ID, 1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := createLegacyBaseline(tx, current, 2); err != nil {
		return err
	}
	return updateCurrentRuleVersion(tx, current.ID, 2)
}

func createLegacyBaseline(tx *gorm.DB, rule RiskRule, version uint) error {
	snapshot, err := BuildRiskRuleVersion(rule, version, RiskRuleVersionSourceLegacyBaseline)
	if err != nil {
		return err
	}
	snapshot.SourceSubmissionID = nil
	snapshot.ReviewEventID = nil
	snapshot.PublicationEventID = nil
	// For an observed legacy baseline, CreatedAt records when the history system
	// first captured this state rather than pretending to know its mutation time.
	snapshot.CreatedAt = time.Time{}
	if err := tx.Create(&snapshot).Error; err != nil {
		return fmt.Errorf("create legacy baseline for risk rule %d version %d: %w", rule.ID, version, err)
	}
	return nil
}

func updateCurrentRuleVersion(tx *gorm.DB, riskRuleID, version uint) error {
	result := tx.Model(&RiskRule{}).Where("id = ?", riskRuleID).UpdateColumn("version", version)
	if result.Error != nil {
		return fmt.Errorf("update current risk rule %d version: %w", riskRuleID, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("update current risk rule %d version affected %d rows", riskRuleID, result.RowsAffected)
	}
	return nil
}

func validateStoredRiskRuleVersion(tx *gorm.DB, version RiskRuleVersion) error {
	if version.RiskRuleID == 0 || version.Version == 0 {
		return fmt.Errorf("risk rule history row %d has invalid identity/version", version.ID)
	}
	recomputed, err := RiskRuleSnapshotDigest(RiskRuleFromVersion(version))
	if err != nil {
		return err
	}
	if recomputed != version.SnapshotDigest {
		return fmt.Errorf("risk rule history row %d snapshot digest does not match stored content", version.ID)
	}
	switch version.SourceKind {
	case RiskRuleVersionSourceLegacyBaseline:
		if version.SourceSubmissionID != nil || version.ReviewEventID != nil || version.PublicationEventID != nil {
			return fmt.Errorf("legacy baseline history row %d must not claim controlled provenance", version.ID)
		}
	case RiskRuleVersionSourceControlledPublication:
		if version.SourceSubmissionID == nil || version.ReviewEventID == nil || version.PublicationEventID == nil {
			return fmt.Errorf("controlled history row %d is missing publication provenance", version.ID)
		}
		var event RuleSubmissionPublicationEvent
		if err := tx.First(&event, *version.PublicationEventID).Error; err != nil {
			return fmt.Errorf("load publication event for controlled history row %d: %w", version.ID, err)
		}
		if err := validateControlledVersionAgainstEvent(tx, version, event); err != nil {
			return err
		}
	default:
		return fmt.Errorf("risk rule history row %d has unsupported source kind %q", version.ID, version.SourceKind)
	}
	return nil
}

func validateControlledVersionAgainstEvent(tx *gorm.DB, version RiskRuleVersion, event RuleSubmissionPublicationEvent) error {
	if version.SourceKind != RiskRuleVersionSourceControlledPublication || version.SourceSubmissionID == nil || version.ReviewEventID == nil || version.PublicationEventID == nil {
		return fmt.Errorf("risk rule history row %d is not a complete controlled publication version", version.ID)
	}
	if *version.PublicationEventID != event.ID || *version.SourceSubmissionID != event.SubmissionID || *version.ReviewEventID != event.ReviewEventID || version.RiskRuleID != event.RiskRuleID {
		return fmt.Errorf("risk rule history row %d disagrees with publication event %d identifiers", version.ID, event.ID)
	}
	submission, _, _, err := loadAndValidatePublicationSource(tx, event)
	if err != nil {
		return err
	}
	expected := riskRuleFromSubmissionSnapshot(submission)
	expected.ID = event.RiskRuleID
	expected.Version = version.Version
	digest, err := RiskRuleSnapshotDigest(expected)
	if err != nil {
		return err
	}
	if digest != version.SnapshotDigest || version.Code != event.RiskRuleCode || version.Code != submission.Code {
		return fmt.Errorf("risk rule history row %d content disagrees with publication event %d source snapshot", version.ID, event.ID)
	}
	return nil
}

func loadAndValidatePublicationSource(tx *gorm.DB, event RuleSubmissionPublicationEvent) (RuleSubmission, RuleSubmissionReviewEvent, string, error) {
	if event.ID == 0 || event.SubmissionID == 0 || event.ReviewEventID == 0 || event.RiskRuleID == 0 || event.ActorKind != controlledPublicationActorKind {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", fmt.Errorf("publication event %d has invalid versioning provenance metadata", event.ID)
	}
	var submission RuleSubmission
	if err := tx.First(&submission, event.SubmissionID).Error; err != nil {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", fmt.Errorf("load publication event %d source submission: %w", event.ID, err)
	}
	var review RuleSubmissionReviewEvent
	if err := tx.First(&review, event.ReviewEventID).Error; err != nil {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", fmt.Errorf("load publication event %d review event: %w", event.ID, err)
	}
	if submission.Status != approvedRuleSubmissionStatus || review.SubmissionID != submission.ID || review.Decision != approvedRuleSubmissionStatus || review.FromStatus != pendingRuleSubmissionStatusForVersionIntegrity || review.ToStatus != approvedRuleSubmissionStatus || review.ActorKind != controlledMaintainerActorKind {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", fmt.Errorf("publication event %d source/review state is inconsistent", event.ID)
	}
	digest, err := RuleSubmissionDraftDigest(submission)
	if err != nil {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", err
	}
	if review.DraftDigest != digest || event.DraftDigest != digest || event.ReviewEventID != review.ID || event.RiskRuleCode != submission.Code {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", fmt.Errorf("publication event %d digest/code provenance is inconsistent", event.ID)
	}
	if submission.DraftDigest != nil && *submission.DraftDigest != digest {
		return RuleSubmission{}, RuleSubmissionReviewEvent{}, "", fmt.Errorf("publication event %d source submission digest is inconsistent", event.ID)
	}
	return submission, review, digest, nil
}

func riskRuleFromSubmissionSnapshot(submission RuleSubmission) RiskRule {
	return RiskRule{
		Code:           submission.Code,
		Name:           submission.Name,
		Description:    submission.Description,
		CategoryCode:   submission.CategoryCode,
		RuleType:       submission.RuleType,
		Pattern:        submission.Pattern,
		Weight:         submission.Weight,
		Severity:       submission.Severity,
		Enabled:        submission.Enabled,
		Explanation:    submission.Explanation,
		Recommendation: submission.Recommendation,
	}
}

func uintPtr(value uint) *uint {
	copy := value
	return &copy
}