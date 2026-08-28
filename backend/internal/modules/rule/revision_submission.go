package rule

import (
	"errors"
	"fmt"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

var (
	ErrInvalidRevisionSubmission      = errors.New("invalid rule revision submission")
	ErrRevisionRuleNotFound           = errors.New("revision target rule not found")
	ErrRevisionStaleBaseVersion       = errors.New("revision base version is stale")
	ErrRevisionRuleVersionIntegrity   = errors.New("revision target rule version integrity violation")
)

type RevisionDraftRequest struct {
	BaseVersion    uint   `json:"base_version"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	CategoryCode   string `json:"category_code"`
	RuleType       string `json:"rule_type"`
	Pattern        string `json:"pattern"`
	Weight         int    `json:"weight"`
	Severity       string `json:"severity"`
	Enabled        *bool  `json:"enabled"`
	Explanation    string `json:"explanation"`
	Recommendation string `json:"recommendation"`
}

// CreateOrReplayPendingRevisionSubmission creates only a non-executable pending
// proposal. The current RiskRule is never mutated here. Target/base/history
// integrity is revalidated before replay lookup so an old pending request cannot
// be replayed as valid after its base becomes stale or inconsistent.
func CreateOrReplayPendingRevisionSubmission(db *gorm.DB, targetID uint, request RevisionDraftRequest) (database.RuleSubmission, ValidationResult, bool, error) {
	if db == nil {
		return database.RuleSubmission{}, ValidationResult{}, false, fmt.Errorf("%w: nil database", ErrInvalidRevisionSubmission)
	}
	if targetID == 0 || request.BaseVersion == 0 {
		return database.RuleSubmission{}, ValidationResult{}, false, fmt.Errorf("%w: target id and base_version must be positive", ErrInvalidRevisionSubmission)
	}

	target, base, err := loadTrustedRevisionBase(db, targetID, request.BaseVersion)
	if err != nil {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}

	draft := request.draftRequest(target.Code)
	validation, err := ValidateRevisionDraft(db, target, base, draft)
	if err != nil {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}
	if !validation.Valid {
		return database.RuleSubmission{}, validation, false, nil
	}

	submission := pendingRevisionSubmissionSnapshot(target, request, draft)
	draftDigest, err := database.RuleSubmissionDraftDigest(submission)
	if err != nil {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}
	submission.DraftDigest = stringPtr(draftDigest)
	requestDigest, err := database.RuleSubmissionRequestDigest(submission)
	if err != nil {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}
	submission.RequestDigest = stringPtr(requestDigest)

	existing, err := getPendingSubmissionByRequestDigest(db, requestDigest)
	if err == nil {
		if err := verifyStoredRevisionSubmissionIdentity(existing, targetID, request.BaseVersion, draftDigest); err != nil {
			return database.RuleSubmission{}, ValidationResult{}, false, err
		}
		return existing, replayValidationResult(), false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}

	if err := db.Create(&submission).Error; err == nil {
		return submission, validation, true, nil
	} else {
		createErr := err
		winner, lookupErr := getPendingSubmissionByRequestDigest(db, requestDigest)
		if lookupErr == nil {
			if identityErr := verifyStoredRevisionSubmissionIdentity(winner, targetID, request.BaseVersion, draftDigest); identityErr != nil {
				return database.RuleSubmission{}, ValidationResult{}, false, identityErr
			}
			return winner, replayValidationResult(), false, nil
		}
		return database.RuleSubmission{}, validation, false, createErr
	}
}

func loadTrustedRevisionBase(db *gorm.DB, targetID, baseVersion uint) (database.RiskRule, database.RiskRuleVersion, error) {
	var target database.RiskRule
	if err := db.First(&target, targetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return database.RiskRule{}, database.RiskRuleVersion{}, fmt.Errorf("%w: risk rule %d", ErrRevisionRuleNotFound, targetID)
		}
		return database.RiskRule{}, database.RiskRuleVersion{}, err
	}
	if target.Version != baseVersion {
		return database.RiskRule{}, database.RiskRuleVersion{}, fmt.Errorf("%w: risk rule %d current version %d, requested base %d", ErrRevisionStaleBaseVersion, targetID, target.Version, baseVersion)
	}

	var base database.RiskRuleVersion
	if err := db.Where("risk_rule_id = ? AND version = ?", targetID, baseVersion).First(&base).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return database.RiskRule{}, database.RiskRuleVersion{}, fmt.Errorf("%w: missing history for risk rule %d version %d", ErrRevisionRuleVersionIntegrity, targetID, baseVersion)
		}
		return database.RiskRule{}, database.RiskRuleVersion{}, err
	}
	if err := database.VerifyRiskRuleMatchesVersion(target, base); err != nil {
		return database.RiskRule{}, database.RiskRuleVersion{}, fmt.Errorf("%w: %v", ErrRevisionRuleVersionIntegrity, err)
	}
	return target, base, nil
}

func pendingRevisionSubmissionSnapshot(target database.RiskRule, request RevisionDraftRequest, draft DraftRequest) database.RuleSubmission {
	ruleSnapshot := draft.riskRule()
	targetID := target.ID
	baseVersion := request.BaseVersion
	return database.RuleSubmission{
		Status:           PendingSubmissionStatus,
		Kind:             database.RuleSubmissionKindRevision,
		TargetRiskRuleID: &targetID,
		BaseVersion:      &baseVersion,
		Code:             ruleSnapshot.Code,
		Name:             ruleSnapshot.Name,
		Description:      ruleSnapshot.Description,
		CategoryCode:     ruleSnapshot.CategoryCode,
		RuleType:         ruleSnapshot.RuleType,
		Pattern:          ruleSnapshot.Pattern,
		Weight:           ruleSnapshot.Weight,
		Severity:         ruleSnapshot.Severity,
		Enabled:          ruleSnapshot.Enabled,
		Explanation:      ruleSnapshot.Explanation,
		Recommendation:   ruleSnapshot.Recommendation,
	}
}

func (request RevisionDraftRequest) draftRequest(code string) DraftRequest {
	return DraftRequest{
		Code:           code,
		Name:           request.Name,
		Description:    request.Description,
		CategoryCode:   request.CategoryCode,
		RuleType:       request.RuleType,
		Pattern:        request.Pattern,
		Weight:         request.Weight,
		Severity:       request.Severity,
		Enabled:        request.Enabled,
		Explanation:    request.Explanation,
		Recommendation: request.Recommendation,
	}
}

func verifyStoredRevisionSubmissionIdentity(submission database.RuleSubmission, targetID, baseVersion uint, draftDigest string) error {
	if submission.Kind != database.RuleSubmissionKindRevision || submission.TargetRiskRuleID == nil || *submission.TargetRiskRuleID != targetID || submission.BaseVersion == nil || *submission.BaseVersion != baseVersion {
		return fmt.Errorf("%w: replayed submission %d intent does not match requested revision", ErrRevisionRuleVersionIntegrity, submission.ID)
	}
	if submission.DraftDigest == nil || *submission.DraftDigest != draftDigest {
		return fmt.Errorf("%w: replayed submission %d draft digest does not match requested revision", ErrRevisionRuleVersionIntegrity, submission.ID)
	}
	return nil
}
