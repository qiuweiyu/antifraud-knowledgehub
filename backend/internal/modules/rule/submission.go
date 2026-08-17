package rule

import (
	"errors"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

const PendingSubmissionStatus = "pending"

// CreatePendingSubmission preserves the existing service signature while using
// the replay-safe create path. Callers that need to distinguish create from
// replay should use CreateOrReplayPendingSubmission.
func CreatePendingSubmission(db *gorm.DB, draft DraftRequest) (database.RuleSubmission, ValidationResult, error) {
	submission, result, _, err := CreateOrReplayPendingSubmission(db, draft)
	return submission, result, err
}

func CreateOrReplayPendingSubmission(db *gorm.DB, draft DraftRequest) (database.RuleSubmission, ValidationResult, bool, error) {
	submission := pendingSubmissionSnapshot(draft)
	digest, err := database.RuleSubmissionDraftDigest(submission)
	if err != nil {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}

	existing, err := getPendingSubmissionByDigest(db, digest)
	if err == nil {
		return existing, replayValidationResult(), false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return database.RuleSubmission{}, ValidationResult{}, false, err
	}

	result := ValidateDraft(db, draft)
	if !result.Valid {
		return database.RuleSubmission{}, result, false, nil
	}

	submissionDigest := digest
	submission.DraftDigest = &submissionDigest
	if err := db.Create(&submission).Error; err == nil {
		return submission, result, true, nil
	} else {
		createErr := err
		winner, lookupErr := getPendingSubmissionByDigest(db, digest)
		if lookupErr == nil {
			return winner, replayValidationResult(), false, nil
		}
		return database.RuleSubmission{}, result, false, createErr
	}
}

func pendingSubmissionSnapshot(draft DraftRequest) database.RuleSubmission {
	ruleSnapshot := draft.riskRule()
	return database.RuleSubmission{
		Status:         PendingSubmissionStatus,
		Code:           ruleSnapshot.Code,
		Name:           ruleSnapshot.Name,
		Description:    ruleSnapshot.Description,
		CategoryCode:   ruleSnapshot.CategoryCode,
		RuleType:       ruleSnapshot.RuleType,
		Pattern:        ruleSnapshot.Pattern,
		Weight:         ruleSnapshot.Weight,
		Severity:       ruleSnapshot.Severity,
		Enabled:        ruleSnapshot.Enabled,
		Explanation:    ruleSnapshot.Explanation,
		Recommendation: ruleSnapshot.Recommendation,
	}
}

func getPendingSubmissionByDigest(db *gorm.DB, digest string) (database.RuleSubmission, error) {
	var submission database.RuleSubmission
	if err := db.
		Where("status = ? AND draft_digest = ?", PendingSubmissionStatus, digest).
		First(&submission).Error; err != nil {
		return database.RuleSubmission{}, err
	}
	return submission, nil
}

func replayValidationResult() ValidationResult {
	return ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationError{},
	}
}

func ListPendingSubmissions(db *gorm.DB) ([]database.RuleSubmission, error) {
	var submissions []database.RuleSubmission
	if err := db.
		Where("status = ?", PendingSubmissionStatus).
		Order("created_at asc").
		Order("id asc").
		Find(&submissions).Error; err != nil {
		return nil, err
	}
	return submissions, nil
}

func GetPendingSubmission(db *gorm.DB, id uint) (database.RuleSubmission, error) {
	var submission database.RuleSubmission
	if err := db.
		Where("id = ? AND status = ?", id, PendingSubmissionStatus).
		First(&submission).Error; err != nil {
		return database.RuleSubmission{}, err
	}
	return submission, nil
}
