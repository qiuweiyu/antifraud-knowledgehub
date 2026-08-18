package rule

import (
	"errors"
	"fmt"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/gorm"
)

const (
	ControlledPublisherActorKind      = "controlled_publisher"
	maxSubmissionPublisherLabelBytes = 120
)

var (
	ErrInvalidSubmissionPublication      = errors.New("invalid rule submission publication command")
	ErrSubmissionNotPublishable          = errors.New("rule submission is not publishable")
	ErrSubmissionPublicationValidation   = errors.New("rule submission publication validation failed")
	ErrSubmissionPublicationConflict     = errors.New("rule submission publication conflict")
	ErrSubmissionPublicationIntegrity    = errors.New("rule submission publication integrity violation")
)

type SubmissionPublicationCommand struct {
	ActorLabel string
}

type SubmissionPublicationOutcome struct {
	Submission  database.RuleSubmission
	ReviewEvent database.RuleSubmissionReviewEvent
	RiskRule    database.RiskRule
	Event       database.RuleSubmissionPublicationEvent
	Validation  ValidationResult
	Replay      bool
}

type normalizedSubmissionPublicationCommand struct {
	ActorKind  string
	ActorLabel string
}

func PublishApprovedSubmission(db *gorm.DB, submissionID uint, command SubmissionPublicationCommand) (SubmissionPublicationOutcome, error) {
	if db == nil {
		return SubmissionPublicationOutcome{}, fmt.Errorf("%w: nil database", ErrInvalidSubmissionPublication)
	}
	normalized, err := normalizeSubmissionPublicationCommand(command)
	if err != nil {
		return SubmissionPublicationOutcome{}, err
	}

	var outcome SubmissionPublicationOutcome
	var targetCode string
	riskRuleInsertFailed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		var submission database.RuleSubmission
		if err := tx.First(&submission, submissionID).Error; err != nil {
			return err
		}

		var existing database.RuleSubmissionPublicationEvent
		err := tx.Where("submission_id = ?", submission.ID).First(&existing).Error
		if err == nil {
			resolved, resolveErr := resolveExistingSubmissionPublication(tx, submission, existing, normalized)
			if resolveErr != nil {
				return resolveErr
			}
			outcome = resolved
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if submission.Status != ApprovedSubmissionStatus {
			return fmt.Errorf("%w: submission %d has status %q", ErrSubmissionNotPublishable, submission.ID, submission.Status)
		}

		reviewEvent, digest, err := loadApprovedPublicationSource(tx, submission)
		if err != nil {
			return err
		}

		validation := ValidateDraft(tx, draftRequestFromSubmission(submission))
		if !validation.Valid {
			outcome.Validation = validation
			return ErrSubmissionPublicationValidation
		}

		riskRule := draftRequestFromSubmission(submission).riskRule()
		sourceSubmissionID := submission.ID
		riskRule.SourceSubmissionID = &sourceSubmissionID
		targetCode = riskRule.Code
		if err := tx.Create(&riskRule).Error; err != nil {
			riskRuleInsertFailed = true
			return fmt.Errorf("create published risk rule: %w", err)
		}

		publicationEvent := database.RuleSubmissionPublicationEvent{
			SubmissionID:  submission.ID,
			ReviewEventID: reviewEvent.ID,
			RiskRuleID:    riskRule.ID,
			RiskRuleCode:  riskRule.Code,
			ActorKind:     normalized.ActorKind,
			ActorLabel:    normalized.ActorLabel,
			DraftDigest:   digest,
		}
		if err := tx.Create(&publicationEvent).Error; err != nil {
			return fmt.Errorf("append rule submission publication event: %w", err)
		}

		outcome = SubmissionPublicationOutcome{
			Submission:  submission,
			ReviewEvent: reviewEvent,
			RiskRule:    riskRule,
			Event:       publicationEvent,
			Validation:  validation,
			Replay:      false,
		}
		return nil
	})
	if err == nil {
		return outcome, nil
	}
	if riskRuleInsertFailed {
		if resolved, ok, resolveErr := resolveSubmissionPublicationAfterRiskRuleInsertFailure(db, submissionID, targetCode, normalized); ok {
			return resolved, resolveErr
		}
	}
	return outcome, err
}

func GetSubmissionPublicationEvent(db *gorm.DB, submissionID uint) (database.RuleSubmissionPublicationEvent, error) {
	if db == nil {
		return database.RuleSubmissionPublicationEvent{}, fmt.Errorf("get rule submission publication event: nil database")
	}
	var event database.RuleSubmissionPublicationEvent
	if err := db.Where("submission_id = ?", submissionID).First(&event).Error; err != nil {
		return database.RuleSubmissionPublicationEvent{}, err
	}
	return event, nil
}

func GetSubmissionPublicationEventByReviewEvent(db *gorm.DB, reviewEventID uint) (database.RuleSubmissionPublicationEvent, error) {
	if db == nil {
		return database.RuleSubmissionPublicationEvent{}, fmt.Errorf("get publication event by review event: nil database")
	}
	var event database.RuleSubmissionPublicationEvent
	if err := db.Where("review_event_id = ?", reviewEventID).First(&event).Error; err != nil {
		return database.RuleSubmissionPublicationEvent{}, err
	}
	return event, nil
}

func ListSubmissionPublicationEventsByRiskRuleID(db *gorm.DB, riskRuleID uint) ([]database.RuleSubmissionPublicationEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("list publication events by risk rule id: nil database")
	}
	var events []database.RuleSubmissionPublicationEvent
	if err := db.Where("risk_rule_id = ?", riskRuleID).Order("created_at asc").Order("id asc").Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func ListSubmissionPublicationEventsByRiskRuleCode(db *gorm.DB, riskRuleCode string) ([]database.RuleSubmissionPublicationEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("list publication events by risk rule code: nil database")
	}
	var events []database.RuleSubmissionPublicationEvent
	if err := db.Where("risk_rule_code = ?", strings.TrimSpace(riskRuleCode)).Order("created_at asc").Order("id asc").Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func GetRiskRulePublicationSource(db *gorm.DB, riskRuleID uint) (database.RuleSubmission, error) {
	if db == nil {
		return database.RuleSubmission{}, fmt.Errorf("get risk rule publication source: nil database")
	}
	var riskRule database.RiskRule
	if err := db.First(&riskRule, riskRuleID).Error; err != nil {
		return database.RuleSubmission{}, err
	}
	if riskRule.SourceSubmissionID == nil {
		return database.RuleSubmission{}, gorm.ErrRecordNotFound
	}
	var submission database.RuleSubmission
	if err := db.First(&submission, *riskRule.SourceSubmissionID).Error; err != nil {
		return database.RuleSubmission{}, err
	}
	return submission, nil
}

func normalizeSubmissionPublicationCommand(command SubmissionPublicationCommand) (normalizedSubmissionPublicationCommand, error) {
	actorLabel := strings.TrimSpace(command.ActorLabel)
	if actorLabel == "" {
		return normalizedSubmissionPublicationCommand{}, fmt.Errorf("%w: trusted publisher actor label is required", ErrInvalidSubmissionPublication)
	}
	if len([]byte(actorLabel)) > maxSubmissionPublisherLabelBytes {
		return normalizedSubmissionPublicationCommand{}, fmt.Errorf("%w: trusted publisher actor label must be at most %d UTF-8 bytes", ErrInvalidSubmissionPublication, maxSubmissionPublisherLabelBytes)
	}
	return normalizedSubmissionPublicationCommand{
		ActorKind:  ControlledPublisherActorKind,
		ActorLabel: actorLabel,
	}, nil
}

func loadApprovedPublicationSource(db *gorm.DB, submission database.RuleSubmission) (database.RuleSubmissionReviewEvent, string, error) {
	var reviewEvent database.RuleSubmissionReviewEvent
	if err := db.Where("submission_id = ?", submission.ID).First(&reviewEvent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return database.RuleSubmissionReviewEvent{}, "", fmt.Errorf("%w: approved submission %d has no review event", ErrSubmissionPublicationIntegrity, submission.ID)
		}
		return database.RuleSubmissionReviewEvent{}, "", err
	}

	digest, err := database.RuleSubmissionDraftDigest(submission)
	if err != nil {
		return database.RuleSubmissionReviewEvent{}, "", fmt.Errorf("compute publication source digest: %w", err)
	}
	if err := verifyPublicationSourceIntegrity(submission, reviewEvent, digest); err != nil {
		return database.RuleSubmissionReviewEvent{}, "", err
	}
	return reviewEvent, digest, nil
}

func verifyPublicationSourceIntegrity(submission database.RuleSubmission, reviewEvent database.RuleSubmissionReviewEvent, digest string) error {
	if submission.Status != ApprovedSubmissionStatus {
		return fmt.Errorf("%w: submission %d is not approved", ErrSubmissionPublicationIntegrity, submission.ID)
	}
	if reviewEvent.SubmissionID != submission.ID ||
		reviewEvent.Decision != ApprovedSubmissionStatus ||
		reviewEvent.FromStatus != PendingSubmissionStatus ||
		reviewEvent.ToStatus != ApprovedSubmissionStatus ||
		reviewEvent.ActorKind != ControlledMaintainerActorKind {
		return fmt.Errorf("%w: submission %d review event does not represent its approved terminal review", ErrSubmissionPublicationIntegrity, submission.ID)
	}
	if reviewEvent.DraftDigest != digest {
		return fmt.Errorf("%w: submission %d review digest does not match stored snapshot", ErrSubmissionPublicationIntegrity, submission.ID)
	}
	if submission.DraftDigest != nil && *submission.DraftDigest != digest {
		return fmt.Errorf("%w: submission %d stored draft digest does not match stored snapshot", ErrSubmissionPublicationIntegrity, submission.ID)
	}
	return nil
}

func resolveExistingSubmissionPublication(db *gorm.DB, submission database.RuleSubmission, event database.RuleSubmissionPublicationEvent, command normalizedSubmissionPublicationCommand) (SubmissionPublicationOutcome, error) {
	if submission.Status != ApprovedSubmissionStatus {
		return SubmissionPublicationOutcome{}, fmt.Errorf("%w: published submission %d no longer has approved review status", ErrSubmissionPublicationIntegrity, submission.ID)
	}
	if event.SubmissionID != submission.ID || event.ActorKind != ControlledPublisherActorKind {
		return SubmissionPublicationOutcome{}, fmt.Errorf("%w: publication event source/actor metadata is invalid for submission %d", ErrSubmissionPublicationIntegrity, submission.ID)
	}

	var reviewEvent database.RuleSubmissionReviewEvent
	if err := db.First(&reviewEvent, event.ReviewEventID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SubmissionPublicationOutcome{}, fmt.Errorf("%w: publication event %d references a missing review event", ErrSubmissionPublicationIntegrity, event.ID)
		}
		return SubmissionPublicationOutcome{}, err
	}
	digest, err := database.RuleSubmissionDraftDigest(submission)
	if err != nil {
		return SubmissionPublicationOutcome{}, fmt.Errorf("compute publication replay digest: %w", err)
	}
	if err := verifyPublicationSourceIntegrity(submission, reviewEvent, digest); err != nil {
		return SubmissionPublicationOutcome{}, err
	}
	if event.ReviewEventID != reviewEvent.ID || event.DraftDigest != digest || event.RiskRuleCode != submission.Code {
		return SubmissionPublicationOutcome{}, fmt.Errorf("%w: publication event %d disagrees with approved source metadata", ErrSubmissionPublicationIntegrity, event.ID)
	}
	if event.ActorLabel != command.ActorLabel {
		return SubmissionPublicationOutcome{}, fmt.Errorf("%w: submission %d was already published by a different trusted publisher", ErrSubmissionPublicationConflict, submission.ID)
	}

	var riskRule database.RiskRule
	if err := db.First(&riskRule, event.RiskRuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SubmissionPublicationOutcome{}, fmt.Errorf("%w: submission %d was already published but its current RiskRule is missing", ErrSubmissionPublicationConflict, submission.ID)
		}
		return SubmissionPublicationOutcome{}, err
	}
	if riskRule.SourceSubmissionID == nil || *riskRule.SourceSubmissionID != submission.ID {
		return SubmissionPublicationOutcome{}, fmt.Errorf("%w: current RiskRule %d has inconsistent publication provenance", ErrSubmissionPublicationIntegrity, riskRule.ID)
	}

	return SubmissionPublicationOutcome{
		Submission:  submission,
		ReviewEvent: reviewEvent,
		RiskRule:    riskRule,
		Event:       event,
		Replay:      true,
	}, nil
}

func resolveSubmissionPublicationAfterRiskRuleInsertFailure(db *gorm.DB, submissionID uint, targetCode string, command normalizedSubmissionPublicationCommand) (SubmissionPublicationOutcome, bool, error) {
	var submission database.RuleSubmission
	if err := db.First(&submission, submissionID).Error; err != nil {
		return SubmissionPublicationOutcome{}, false, nil
	}

	var event database.RuleSubmissionPublicationEvent
	err := db.Where("submission_id = ?", submissionID).First(&event).Error
	if err == nil {
		outcome, resolveErr := resolveExistingSubmissionPublication(db, submission, event, command)
		return outcome, true, resolveErr
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return SubmissionPublicationOutcome{}, true, err
	}

	var winner database.RiskRule
	if targetCode != "" {
		err = db.Where("code = ?", targetCode).First(&winner).Error
		if err == nil {
			validation := duplicateCodePublicationValidation()
			return SubmissionPublicationOutcome{Submission: submission, Validation: validation}, true,
				fmt.Errorf("%w: rule code %q was published or created concurrently", ErrSubmissionPublicationConflict, targetCode)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return SubmissionPublicationOutcome{}, true, err
		}
	}
	return SubmissionPublicationOutcome{}, false, nil
}

func duplicateCodePublicationValidation() ValidationResult {
	return ValidationResult{
		Valid: false,
		Errors: []ValidationError{{
			Field:   "code",
			Code:    "duplicate_code",
			Message: "rule code already exists",
		}},
		Warnings: []ValidationError{},
	}
}
