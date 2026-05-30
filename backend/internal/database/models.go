package database

import (
	"time"

	"gorm.io/datatypes"
)

type Category struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	Code            string    `json:"code" gorm:"uniqueIndex;size:100;not null"`
	Name            string    `json:"name" gorm:"size:160;not null"`
	Description     string    `json:"description"`
	SeverityDefault string    `json:"severity_default" gorm:"size:40;not null;default:medium"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type RiskRule struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	Code           string    `json:"code" gorm:"uniqueIndex;size:120;not null"`
	Name           string    `json:"name" gorm:"size:180;not null"`
	Description    string    `json:"description"`
	CategoryCode   string    `json:"category_code" gorm:"index;size:100;not null"`
	RuleType       string    `json:"rule_type" gorm:"size:40;not null"`
	Pattern        string    `json:"pattern" gorm:"not null"`
	Weight         int       `json:"weight" gorm:"not null"`
	Severity       string    `json:"severity" gorm:"size:40;not null"`
	Enabled        bool      `json:"enabled" gorm:"not null;default:true"`
	Explanation    string    `json:"explanation"`
	Recommendation string    `json:"recommendation"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ScamCase struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	Title        string         `json:"title" gorm:"size:220;not null"`
	CategoryCode string         `json:"category_code" gorm:"index;size:100;not null"`
	Content      string         `json:"content" gorm:"not null"`
	Summary      string         `json:"summary"`
	RiskPoints   datatypes.JSON `json:"risk_points" gorm:"type:jsonb"`
	Tags         datatypes.JSON `json:"tags" gorm:"type:jsonb"`
	SourceType   string         `json:"source_type" gorm:"size:40;not null;default:sample"`
	Anonymized   bool           `json:"anonymized" gorm:"not null;default:true"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type AnalysisRecord struct {
	ID              uint           `json:"id" gorm:"primaryKey"`
	InputText       string         `json:"input_text" gorm:"not null"`
	RiskScore       int            `json:"risk_score" gorm:"not null"`
	RiskLevel       string         `json:"risk_level" gorm:"size:40;not null"`
	MatchedRules    datatypes.JSON `json:"matched_rules" gorm:"type:jsonb"`
	Explanation     string         `json:"explanation"`
	Recommendations datatypes.JSON `json:"recommendations" gorm:"type:jsonb"`
	CreatedAt       time.Time      `json:"created_at"`
}
