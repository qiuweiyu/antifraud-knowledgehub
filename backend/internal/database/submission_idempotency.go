package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const (
	RuleSubmissionDraftDigestVersion       = "afkh-rule-submission-draft:v1"
	RuleSubmissionRequestDigestVersion     = "afkh-rule-submission-request:v1"
	RuleSubmissionPendingDigestIndex       = "ux_rule_submissions_pending_digest"
	RuleSubmissionPendingRequestDigestIndex = "ux_rule_submissions_pending_request_digest"
	pendingSubmissionStatus                = "pending"
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

type canonicalSubmissionRequestV1 struct {
	Kind             string `json:"kind"`
	TargetRiskRuleID *uint  `json:"target_risk_rule_id"`
	BaseVersion      *uint  `json:"base_version"`
	DraftDigest      string `json:"draft_digest"`
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

// RuleSubmissionRequestDigest fingerprints mutation intent separately from the
// exact content snapshot. It is the pending idempotency authority after I2.
func RuleSubmissionRequestDigest(submission RuleSubmission) (string, error) {
	kind, err := validateRuleSubmissionIntent(submission)
	if err != nil {
		return "", err
	}
	draftDigest, err := RuleSubmissionDraftDigest(submission)
	if err != nil {
		return "", err
	}
	if submission.DraftDigest != nil && *submission.DraftDigest != draftDigest {
		return "", fmt.Errorf("rule submission draft digest does not match stored snapshot")
	}

	canonical := canonicalSubmissionRequestV1{
		Kind:             kind,
		TargetRiskRuleID: submission.TargetRiskRuleID,
		BaseVersion:      submission.BaseVersion,
		DraftDigest:      draftDigest,
	}
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal canonical rule submission request: %w", err)
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(RuleSubmissionRequestDigestVersion + "\n"))
	_, _ = hash.Write(canonicalJSON)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateRuleSubmissionIntent(submission RuleSubmission) (string, error) {
	kind := strings.TrimSpace(submission.Kind)
	if kind == "" {
		kind = RuleSubmissionKindCreate
	}
	switch kind {
	case RuleSubmissionKindCreate:
		if submission.TargetRiskRuleID != nil || submission.BaseVersion != nil {
			return "", fmt.Errorf("create rule submission must not contain revision target/base metadata")
		}
	case RuleSubmissionKindRevision:
		if submission.TargetRiskRuleID == nil || *submission.TargetRiskRuleID == 0 {
			return "", fmt.Errorf("revision rule submission requires a positive target risk rule id")
		}
		if submission.BaseVersion == nil || *submission.BaseVersion == 0 {
			return "", fmt.Errorf("revision rule submission requires a positive base version")
		}
	default:
		return "", fmt.Errorf("unsupported rule submission kind %q", submission.Kind)
	}
	return kind, nil
}

// PrepareRuleSubmissionIdempotency upgrades legacy RuleSubmission rows and
// migrates pending replay identity from content-only draft_digest to the
// intent-aware request_digest. Existing non-null audit digests are verified,
// never rewritten. Legacy duplicate pending rows are preserved; only the first
// exact request receives a non-null guarded request digest.
func PrepareRuleSubmissionIdempotency(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("prepare rule submission idempotency: nil database")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		for _, column := range []string{"DraftDigest", "Kind", "TargetRiskRuleID", "BaseVersion", "RequestDigest"} {
			if tx.Migrator().HasColumn(&RuleSubmission{}, column) {
				continue
			}
			if err := tx.Migrator().AddColumn(&RuleSubmission{}, column); err != nil {
				return fmt.Errorf("add rule submission %s column: %w", column, err)
			}
		}

		if err := tx.Model(&RuleSubmission{}).
			Where("kind IS NULL OR TRIM(kind) = ''").
			UpdateColumn("kind", RuleSubmissionKindCreate).Error; err != nil {
			return fmt.Errorf("backfill rule submission kind=create: %w", err)
		}

		// The old partial index prevents duplicate legacy draft digests from being
		// fully backfilled. Remove it transactionally before request-digest migration.
		if err := tx.Exec("DROP INDEX IF EXISTS " + RuleSubmissionPendingDigestIndex).Error; err != nil {
			return fmt.Errorf("drop legacy pending rule submission draft digest index: %w", err)
		}

		var submissions []RuleSubmission
		if err := tx.Order("created_at asc").Order("id asc").Find(&submissions).Error; err != nil {
			return fmt.Errorf("load rule submissions for request digest backfill: %w", err)
		}

		seenPendingRequests := make(map[string]uint)
		for i := range submissions {
			submission := submissions[i]
			kind, err := validateRuleSubmissionIntent(submission)
			if err != nil {
				return fmt.Errorf("rule submission %d has invalid intent: %w", submission.ID, err)
			}
			if submission.Kind != kind {
				result := tx.Model(&RuleSubmission{}).
					Where("id = ?", submission.ID).
					UpdateColumn("kind", kind)
				if result.Error != nil {
					return fmt.Errorf("normalize rule submission %d kind: %w", submission.ID, result.Error)
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("normalize rule submission %d kind affected %d rows", submission.ID, result.RowsAffected)
				}
				submission.Kind = kind
			}

			draftDigest, err := RuleSubmissionDraftDigest(submission)
			if err != nil {
				return err
			}
			if submission.DraftDigest != nil {
				if *submission.DraftDigest != draftDigest {
					return fmt.Errorf("rule submission %d has a draft digest inconsistent with its stored snapshot", submission.ID)
				}
			} else {
				result := tx.Model(&RuleSubmission{}).
					Where("id = ? AND draft_digest IS NULL", submission.ID).
					UpdateColumn("draft_digest", draftDigest)
				if result.Error != nil {
					return fmt.Errorf("backfill draft digest for submission %d: %w", submission.ID, result.Error)
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("backfill draft digest for submission %d affected %d rows", submission.ID, result.RowsAffected)
				}
				submission.DraftDigest = stringPointer(draftDigest)
			}

			requestDigest, err := RuleSubmissionRequestDigest(submission)
			if err != nil {
				return fmt.Errorf("compute request digest for submission %d: %w", submission.ID, err)
			}
			if submission.RequestDigest != nil && *submission.RequestDigest != requestDigest {
				return fmt.Errorf("rule submission %d has a request digest inconsistent with its stored intent/snapshot", submission.ID)
			}

			if submission.Status == pendingSubmissionStatus {
				if representativeID, duplicate := seenPendingRequests[requestDigest]; duplicate {
					if submission.RequestDigest != nil {
						return fmt.Errorf("pending submissions %d and %d already contain the same non-null request digest", representativeID, submission.ID)
					}
					// Preserve later legacy duplicate pending rows without inventing a
					// second active idempotency identity.
					continue
				}
				seenPendingRequests[requestDigest] = submission.ID
			}

			if submission.RequestDigest == nil {
				result := tx.Model(&RuleSubmission{}).
					Where("id = ? AND request_digest IS NULL", submission.ID).
					UpdateColumn("request_digest", requestDigest)
				if result.Error != nil {
					return fmt.Errorf("backfill request digest for submission %d: %w", submission.ID, result.Error)
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("backfill request digest for submission %d affected %d rows", submission.ID, result.RowsAffected)
				}
			}
		}

		indexSQL := fmt.Sprintf(
			"CREATE UNIQUE INDEX IF NOT EXISTS %s ON rule_submissions (request_digest) WHERE status = 'pending' AND request_digest IS NOT NULL",
			RuleSubmissionPendingRequestDigestIndex,
		)
		if err := tx.Exec(indexSQL).Error; err != nil {
			return fmt.Errorf("create pending rule submission request digest index: %w", err)
		}
		return nil
	})
}

func stringPointer(value string) *string {
	copy := value
	return &copy
}
