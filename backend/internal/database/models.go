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
	ID                 uint      `json:"id" gorm:"primaryKey"`
	Code               string    `json:"code" gorm:"uniqueIndex;size:120;not null"`
	Name               string    `json:"name" gorm:"size:180;not null"`
	Description        string    `json:"description"`
	CategoryCode       string    `json:"category_code" gorm:"index;size:100;not null"`
	RuleType           string    `json:"rule_type" gorm:"size:40;not null"`
	Pattern            string    `json:"pattern" gorm:"not null"`
	Weight             int       `json:"weight" gorm:"not null"`
	Severity           string    `json:"severity" gorm:"size:40;not null"`
	Enabled            bool      `json:"enabled" gorm:"not null;default:true"`
	Explanation        string    `json:"explanation"`
	Recommendation     string    `json:"recommendation"`
	SourceSubmissionID *uint     `json:"-" gorm:"index"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type RuleSubmission struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	Status         string     `json:"status" gorm:"index;size:32;not null;default:pending"`
	Code           string     `json:"code" gorm:"index;size:120;not null"`
	Name           string     `json:"name" gorm:"size:180;not null"`
	Description    string     `json:"description"`
	CategoryCode   string     `json:"category_code" gorm:"index;size:100;not null"`
	RuleType       string     `json:"rule_type" gorm:"size:40;not null"`
	Pattern        string     `json:"pattern" gorm:"not null"`
	Weight         int        `json:"weight" gorm:"not null"`
	Severity       string     `json:"severity" gorm:"size:40;not null"`
	Enabled        bool       `json:"enabled" gorm:"not null"`
	Explanation    string     `json:"explanation"`
	Recommendation string     `json:"recommendation"`
	DraftDigest    *string    `json:"-" gorm:"size:64"`
	CreatedAt      time.Time  `json:"created_at" gorm:"index"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type RuleSubmissionReviewEvent struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	SubmissionID uint           `json:"submission_id" gorm:"not null;uniqueIndex"`
	Submission   RuleSubmission `json:"-" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:SubmissionID;references:ID"`
	Decision     string         `json:"decision" gorm:"size:32;not null;check:chk_rule_submission_review_decision,decision IN ('approved','rejected')"`
	FromStatus   string         `json:"from_status" gorm:"size:32;not null;check:chk_rule_submission_review_from_status,from_status = 'pending'"`
	ToStatus     string         `json:"to_status" gorm:"size:32;not null;check:chk_rule_submission_review_to_status,to_status = decision AND to_status IN ('approved','rejected')"`
	Reason       string         `json:"reason" gorm:"not null"`
	ActorKind    string         `json:"actor_kind" gorm:"size:40;not null;check:chk_rule_submission_review_actor_kind,actor_kind = 'controlled_maintainer'"`
	ActorLabel   string         `json:"actor_label" gorm:"size:120;not null"`
	DraftDigest  string         `json:"-" gorm:"size:64;not null"`
	CreatedAt    time.Time      `json:"created_at" gorm:"index"`
}

type RuleSubmissionPublicationEvent struct {
	ID            uint                      `json:"id" gorm:"primaryKey"`
	SubmissionID  uint                      `json:"submission_id" gorm:"not null;uniqueIndex"`
	Submission    RuleSubmission            `json:"-" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:SubmissionID;references:ID"`
	ReviewEventID uint                      `json:"review_event_id" gorm:"not null;uniqueIndex"`
	ReviewEvent   RuleSubmissionReviewEvent `json:"-" gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;foreignKey:ReviewEventID;references:ID"`
	RiskRuleID    uint                      `json:"risk_rule_id" gorm:"not null;index"`
	RiskRuleCode  string                    `json:"risk_rule_code" gorm:"size:120;not null;index"`
	ActorKind     string                    `json:"actor_kind" gorm:"size:40;not null;check:chk_rule_submission_publication_actor_kind,actor_kind = 'controlled_publisher'"`
	ActorLabel    string                    `json:"actor_label" gorm:"size:120;not null"`
	DraftDigest   string                    `json:"-" gorm:"size:64;not null"`
	CreatedAt     time.Time                 `json:"created_at" gorm:"index"`
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
