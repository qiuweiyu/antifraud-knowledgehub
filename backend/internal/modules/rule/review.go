package rule

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

const (
	ApprovedSubmissionStatus          = "approved"
	RejectedSubmissionStatus          = "rejected"
	ControlledMaintainerActorKind     = "controlled_maintainer"
	maxSubmissionReviewReasonBytes    = 2000
	maxSubmissionReviewActorLabelBytes = 120
)

var (
	ErrInvalidSubmissionReview       = errors.New("invalid rule submission review command")
	ErrSubmissionReviewConflict      = errors.New("rule submission review conflict")
	ErrSubmissionApprovalValidation  = errors.New("rule submission approval validation failed")
	ErrSubmissionReviewIntegrity     = errors.New("rule submission review integrity violation")
)

type SubmissionReviewCommand struct {
	Decision   string
	Reason     string
	ActorLabel string
}

type SubmissionReviewOutcome struct {
	Submission database.RuleSubmission
	Event      database.RuleSubmissionReviewEvent
	Validation ValidationResult
	Replay     bool
}

type SubmissionReviewState struct {
	Submission database.RuleSubmission
	Event      *database.RuleSubmissionReviewEvent
}

type normalizedSubmissionReviewCommand struct {
	Decision   string
	Reason     string
	ActorKind  string
	ActorLabel string
}

func ReviewPendingSubmission(db *gorm.DB, submissionID uint, command SubmissionReviewCommand) (SubmissionReviewOutcome, error) {
	if db == nil {
		return SubmissionReviewOutcome{}, fmt.Errorf("%w: nil database", ErrInvalidSubmissionReview)
	}

	normalized, err := normalizeSubmissionReviewCommand(command)
	if err != nil {
		return SubmissionReviewOutcome{}, err
	}

	var outcome SubmissionReviewOutcome
	lostCAS := false
	err = db.Transaction(func(tx *gorm.DB) error {
		var submission database.RuleSubmission
		if err := tx.First(&submission, submissionID).Error; err != nil {
			return err
		}

		if submission.Status != PendingSubmissionStatus {
			resolved, err := resolveExistingSubmissionReview(tx, submission, normalized)
			if err != nil {
				return err
			}
			outcome = resolved
			return nil
		}

		validation := ValidationResult{}
		if normalized.Decision == ApprovedSubmissionStatus {
			validation = ValidateDraft(tx, draftRequestFromSubmission(submission))
			if !validation.Valid {
				outcome.Validation = validation
				return ErrSubmissionApprovalValidation
			}
		}

		digest, err := database.RuleSubmissionDraftDigest(submission)
		if err != nil {
			return fmt.Errorf("compute reviewed submission digest: %w", err)
		}
		if submission.DraftDigest != nil && *submission.DraftDigest != digest {
			return fmt.Errorf("%w: submission %d draft digest does not match stored snapshot", ErrSubmissionReviewIntegrity, submission.ID)
		}

		now := time.Now().UTC()
		transition := tx.Model(&database.RuleSubmission{}).
			Where("id = ? AND status = ?", submission.ID, PendingSubmissionStatus).
			Updates(map[string]any{
				"status":     normalized.Decision,
				"updated_at": now,
			})
		if transition.Error != nil {
			return transition.Error
		}
		if transition.RowsAffected != 1 {
			lostCAS = true
			return nil
		}

		event := database.RuleSubmissionReviewEvent{
			SubmissionID: submission.ID,
			Decision:     normalized.Decision,
			FromStatus:   PendingSubmissionStatus,
			ToStatus:     normalized.Decision,
			Reason:       normalized.Reason,
			ActorKind:    normalized.ActorKind,
			ActorLabel:   normalized.ActorLabel,
			DraftDigest:  digest,
		}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("append rule submission review event: %w", err)
		}

		if err := tx.First(&submission, submission.ID).Error; err != nil {
			return fmt.Errorf("reload reviewed submission: %w", err)
		}
		outcome = SubmissionReviewOutcome{
			Submission: submission,
			Event:      event,
			Validation: validation,
			Replay:     false,
		}
		return nil
	})
	if err != nil {
		return outcome, err
	}
	if lostCAS {
		return resolveSubmissionReviewAfterCASLoss(db, submissionID, normalized)
	}
	return outcome, nil
}

func ListSubmissionReviewEvents(db *gorm.DB, submissionID uint) ([]database.RuleSubmissionReviewEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("list rule submission review events: nil database")
	}
	var events []database.RuleSubmissionReviewEvent
	if err := db.
		Where("submission_id = ?", submissionID).
		Order("created_at asc").
		Order("id asc").
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func GetSubmissionReviewEvent(db *gorm.DB, submissionID uint) (database.RuleSubmissionReviewEvent, error) {
	if db == nil {
		return database.RuleSubmissionReviewEvent{}, fmt.Errorf("get rule submission review event: nil database")
	}
	var event database.RuleSubmissionReviewEvent
	if err := db.Where("submission_id = ?", submissionID).First(&event).Error; err != nil {
		return database.RuleSubmissionReviewEvent{}, err
	}
	return event, nil
}

func GetSubmissionReviewState(db *gorm.DB, submissionID uint) (SubmissionReviewState, error) {
	if db == nil {
		return SubmissionReviewState{}, fmt.Errorf("get rule submission review state: nil database")
	}
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		return SubmissionReviewState{}, err
	}

	var event database.RuleSubmissionReviewEvent
	err := db.Where("submission_id = ?", submissionID).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if submission.Status == ApprovedSubmissionStatus || submission.Status == RejectedSubmissionStatus {
			return SubmissionReviewState{}, fmt.Errorf("%w: terminal submission %d has no review event", ErrSubmissionReviewIntegrity, submissionID)
		}
		return SubmissionReviewState{Submission: submission}, nil
	}
	if err != nil {
		return SubmissionReviewState{}, err
	}
	if submission.Status == PendingSubmissionStatus || event.ToStatus != submission.Status || event.Decision != submission.Status {
		return SubmissionReviewState{}, fmt.Errorf("%w: submission %d status and review event disagree", ErrSubmissionReviewIntegrity, submissionID)
	}
	return SubmissionReviewState{Submission: submission, Event: &event}, nil
}

func normalizeSubmissionReviewCommand(command SubmissionReviewCommand) (normalizedSubmissionReviewCommand, error) {
	decision := strings.TrimSpace(command.Decision)
	reason := strings.TrimSpace(command.Reason)
	actorLabel := strings.TrimSpace(command.ActorLabel)

	if decision != ApprovedSubmissionStatus && decision != RejectedSubmissionStatus {
		return normalizedSubmissionReviewCommand{}, fmt.Errorf("%w: decision must be %q or %q", ErrInvalidSubmissionReview, ApprovedSubmissionStatus, RejectedSubmissionStatus)
	}
	if reason == "" {
		return normalizedSubmissionReviewCommand{}, fmt.Errorf("%w: review reason is required", ErrInvalidSubmissionReview)
	}
	if len([]byte(reason)) > maxSubmissionReviewReasonBytes {
		return normalizedSubmissionReviewCommand{}, fmt.Errorf("%w: review reason must be at most %d UTF-8 bytes", ErrInvalidSubmissionReview, maxSubmissionReviewReasonBytes)
	}
	if actorLabel == "" {
		return normalizedSubmissionReviewCommand{}, fmt.Errorf("%w: trusted actor label is required", ErrInvalidSubmissionReview)
	}
	if len([]byte(actorLabel)) > maxSubmissionReviewActorLabelBytes {
		return normalizedSubmissionReviewCommand{}, fmt.Errorf("%w: trusted actor label must be at most %d UTF-8 bytes", ErrInvalidSubmissionReview, maxSubmissionReviewActorLabelBytes)
	}

	return normalizedSubmissionReviewCommand{
		Decision:   decision,
		Reason:     reason,
		ActorKind:  ControlledMaintainerActorKind,
		ActorLabel: actorLabel,
	}, nil
}

func resolveSubmissionReviewAfterCASLoss(db *gorm.DB, submissionID uint, command normalizedSubmissionReviewCommand) (SubmissionReviewOutcome, error) {
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		return SubmissionReviewOutcome{}, err
	}
	return resolveExistingSubmissionReview(db, submission, command)
}

func resolveExistingSubmissionReview(db *gorm.DB, submission database.RuleSubmission, command normalizedSubmissionReviewCommand) (SubmissionReviewOutcome, error) {
	if submission.Status == PendingSubmissionStatus {
		return SubmissionReviewOutcome{}, fmt.Errorf("%w: submission %d remained pending after compare-and-set loss", ErrSubmissionReviewIntegrity, submission.ID)
	}
	if submission.Status != ApprovedSubmissionStatus && submission.Status != RejectedSubmissionStatus {
		return SubmissionReviewOutcome{}, fmt.Errorf("%w: submission %d has unsupported terminal status %q", ErrSubmissionReviewConflict, submission.ID, submission.Status)
	}

	var event database.RuleSubmissionReviewEvent
	if err := db.Where("submission_id = ?", submission.ID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SubmissionReviewOutcome{}, fmt.Errorf("%w: terminal submission %d has no review event", ErrSubmissionReviewIntegrity, submission.ID)
		}
		return SubmissionReviewOutcome{}, err
	}
	if event.Decision == command.Decision &&
		event.ToStatus == submission.Status &&
		event.FromStatus == PendingSubmissionStatus &&
		event.Reason == command.Reason &&
		event.ActorKind == command.ActorKind &&
		event.ActorLabel == command.ActorLabel {
		return SubmissionReviewOutcome{
			Submission: submission,
			Event:      event,
			Replay:     true,
		}, nil
	}
	return SubmissionReviewOutcome{}, fmt.Errorf("%w: submission %d already has a different terminal review", ErrSubmissionReviewConflict, submission.ID)
}

func draftRequestFromSubmission(submission database.RuleSubmission) DraftRequest {
	enabled := submission.Enabled
	return DraftRequest{
		Code:           submission.Code,
		Name:           submission.Name,
		Description:    submission.Description,
		CategoryCode:   submission.CategoryCode,
		RuleType:       submission.RuleType,
		Pattern:        submission.Pattern,
		Weight:         submission.Weight,
		Severity:       submission.Severity,
		Enabled:        &enabled,
		Explanation:    submission.Explanation,
		Recommendation: submission.Recommendation,
	}
}
