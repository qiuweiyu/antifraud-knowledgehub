package rule

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

const (
	maxCodeLength         = 120
	maxNameLength         = 180
	maxCategoryCodeLength = 100
	maxRuleTypeLength     = 40
	maxSeverityLength     = 40
	minRuleWeight         = 0
	maxRuleWeight         = 100
)

var supportedRuleTypes = map[string]bool{
	"keyword":              true,
	"pattern":              true,
	"semantic_placeholder": true,
	"regex":                true,
}

var supportedSeverities = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

type DraftRequest struct {
	Code           string `json:"code"`
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

type ValidationError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors"`
	Warnings []ValidationError `json:"warnings"`
}

func ValidateDraft(db *gorm.DB, draft DraftRequest) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationError{},
	}

	draft.Code = strings.TrimSpace(draft.Code)
	draft.Name = strings.TrimSpace(draft.Name)
	draft.CategoryCode = strings.TrimSpace(draft.CategoryCode)
	draft.RuleType = strings.TrimSpace(draft.RuleType)
	draft.Pattern = strings.TrimSpace(draft.Pattern)
	draft.Severity = strings.TrimSpace(draft.Severity)

	require(&result, "code", draft.Code)
	require(&result, "name", draft.Name)
	require(&result, "category_code", draft.CategoryCode)
	require(&result, "rule_type", draft.RuleType)
	require(&result, "pattern", draft.Pattern)
	require(&result, "severity", draft.Severity)

	maxLength(&result, "code", draft.Code, maxCodeLength)
	maxLength(&result, "name", draft.Name, maxNameLength)
	maxLength(&result, "category_code", draft.CategoryCode, maxCategoryCodeLength)
	maxLength(&result, "rule_type", draft.RuleType, maxRuleTypeLength)
	maxLength(&result, "severity", draft.Severity, maxSeverityLength)

	if draft.Code != "" {
		var count int64
		db.Model(&database.RiskRule{}).Where("code = ?", draft.Code).Count(&count)
		if count > 0 {
			addError(&result, "code", "duplicate_code", "rule code already exists")
		}
	}

	if draft.CategoryCode != "" {
		var count int64
		db.Model(&database.Category{}).Where("code = ?", draft.CategoryCode).Count(&count)
		if count == 0 {
			addError(&result, "category_code", "category_not_found", "category does not exist")
		}
	}

	if draft.RuleType != "" && !supportedRuleTypes[draft.RuleType] {
		addError(&result, "rule_type", "unsupported_rule_type", "rule type is not supported")
	}

	if draft.Severity != "" && !supportedSeverities[draft.Severity] {
		addError(&result, "severity", "unsupported_severity", "severity is not supported")
	}

	if draft.Weight < minRuleWeight || draft.Weight > maxRuleWeight {
		addError(&result, "weight", "weight_out_of_range", fmt.Sprintf("weight must be between %d and %d", minRuleWeight, maxRuleWeight))
	}

	if draft.Pattern != "" && draft.RuleType == "regex" {
		if _, err := regexp.Compile(draft.Pattern); err != nil {
			addError(&result, "pattern", "invalid_regex", err.Error())
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

func (draft DraftRequest) riskRule() database.RiskRule {
	enabled := true
	if draft.Enabled != nil {
		enabled = *draft.Enabled
	}
	return database.RiskRule{
		Code:           strings.TrimSpace(draft.Code),
		Name:           strings.TrimSpace(draft.Name),
		Description:    draft.Description,
		CategoryCode:   strings.TrimSpace(draft.CategoryCode),
		RuleType:       strings.TrimSpace(draft.RuleType),
		Pattern:        strings.TrimSpace(draft.Pattern),
		Weight:         draft.Weight,
		Severity:       strings.TrimSpace(draft.Severity),
		Enabled:        enabled,
		Explanation:    draft.Explanation,
		Recommendation: draft.Recommendation,
	}
}

func require(result *ValidationResult, field, value string) {
	if value == "" {
		addError(result, field, "required", field+" is required")
	}
}

func maxLength(result *ValidationResult, field, value string, limit int) {
	if len(value) > limit {
		addError(result, field, "too_long", fmt.Sprintf("%s must be at most %d characters", field, limit))
	}
}

func addError(result *ValidationResult, field, code, message string) {
	result.Errors = append(result.Errors, ValidationError{Field: field, Code: code, Message: message})
}
