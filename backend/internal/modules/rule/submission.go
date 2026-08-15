package rule

import (
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

const PendingSubmissionStatus = "pending"

func CreatePendingSubmission(db *gorm.DB, draft DraftRequest) (database.RuleSubmission, ValidationResult, error) {
	result := ValidateDraft(db, draft)
	if !result.Valid {
		return database.RuleSubmission{}, result, nil
	}

	ruleSnapshot := draft.riskRule()
	submission := database.RuleSubmission{
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

	if err := db.Create(&submission).Error; err != nil {
		return database.RuleSubmission{}, result, err
	}
	return submission, result, nil
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
