package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

const (
	RuleSubmissionDraftDigestVersion = "afkh-rule-submission-draft:v1"
	RuleSubmissionPendingDigestIndex = "ux_rule_submissions_pending_digest"
	pendingSubmissionStatus           = "pending"
)

type canonicalSubmissionDraftV1 struct {
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

// RuleSubmissionDraftDigest fingerprints the exact persisted draft snapshot.
// It intentionally excludes system-owned fields such as ID, status and timestamps.
func RuleSubmissionDraftDigest(submission RuleSubmission) (string, error) {
	canonical := canonicalSubmissionDraftV1{
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

	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical rule submission draft: %w", err)
	}

	hash := sha256.New()
	_, _ = hash.Write([]byte(RuleSubmissionDraftDigestVersion + "\n"))
	_, _ = hash.Write(canonicalJSON)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// PrepareRuleSubmissionIdempotency upgrades legacy RuleSubmission rows and
// installs the database invariant that prevents duplicate active pending drafts.
// Draft content is never deleted or rewritten during preparation.
func PrepareRuleSubmissionIdempotency(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("prepare rule submission idempotency: nil database")
	}

	if !db.Migrator().HasColumn(&RuleSubmission{}, "DraftDigest") {
		if err := db.Migrator().AddColumn(&RuleSubmission{}, "DraftDigest"); err != nil {
			return fmt.Errorf("add rule submission draft_digest column: %w", err)
		}
	}

	var pending []RuleSubmission
	if err := db.
		Where("status = ?", pendingSubmissionStatus).
		Order("created_at asc").
		Order("id asc").
		Find(&pending).Error; err != nil {
		return fmt.Errorf("load pending submissions for digest backfill: %w", err)
	}

	seen := make(map[string]uint, len(pending))
	for _, submission := range pending {
		digest, err := RuleSubmissionDraftDigest(submission)
		if err != nil {
			return err
		}

		if submission.DraftDigest != nil {
			if *submission.DraftDigest != digest {
				return fmt.Errorf("rule submission %d has a draft digest inconsistent with its stored snapshot", submission.ID)
			}
			if representativeID, exists := seen[digest]; exists {
				return fmt.Errorf("pending submissions %d and %d already contain the same non-null draft digest", representativeID, submission.ID)
			}
			seen[digest] = submission.ID
			continue
		}

		if _, duplicate := seen[digest]; duplicate {
			// Preserve later pre-digest duplicate rows as historical nullable rows.
			continue
		}

		result := db.Model(&RuleSubmission{}).
			Where("id = ? AND draft_digest IS NULL", submission.ID).
			UpdateColumn("draft_digest", digest)
		if result.Error != nil {
			return fmt.Errorf("backfill draft digest for submission %d: %w", submission.ID, result.Error)
		}
		if result.RowsAffected == 0 {
			var current RuleSubmission
			if err := db.First(&current, submission.ID).Error; err != nil {
				return fmt.Errorf("reload concurrently updated submission %d: %w", submission.ID, err)
			}
			if current.DraftDigest == nil || *current.DraftDigest != digest {
				return fmt.Errorf("submission %d changed during digest backfill", submission.ID)
			}
		}
		seen[digest] = submission.ID
	}

	indexSQL := fmt.Sprintf(
		"CREATE UNIQUE INDEX IF NOT EXISTS %s ON rule_submissions (draft_digest) WHERE status = 'pending' AND draft_digest IS NOT NULL",
		RuleSubmissionPendingDigestIndex,
	)
	if err := db.Exec(indexSQL).Error; err != nil {
		return fmt.Errorf("create pending rule submission digest index: %w", err)
	}
	return nil
}
