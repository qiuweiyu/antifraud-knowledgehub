package rule

import (
	"errors"
	"os"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresConcurrentIdenticalPublicationsConvergeOnOneRuleAndEvent(t *testing.T) {
	db, dsn := publicationPostgresTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("postgres_publish_same_submission"))
	command := SubmissionPublicationCommand{ActorLabel: "publisher-a"}

	const callers = 12
	sessions := openPublicationPostgresSessions(t, dsn, callers)
	type result struct {
		ruleID  uint
		eventID uint
		replay  bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, callers)
	for i := 0; i < callers; i++ {
		session := sessions[i]
		go func() {
			<-start
			outcome, err := PublishApprovedSubmission(session, submission.ID, command)
			results <- result{ruleID: outcome.RiskRule.ID, eventID: outcome.Event.ID, replay: outcome.Replay, err: err}
		}()
	}
	close(start)

	var canonicalRuleID, canonicalEventID uint
	created := 0
	for i := 0; i < callers; i++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("concurrent publication %d failed: %v", i, got.err)
		}
		if got.ruleID == 0 || got.eventID == 0 {
			t.Fatalf("concurrent publication %d returned empty identity: %+v", i, got)
		}
		if canonicalRuleID == 0 {
			canonicalRuleID, canonicalEventID = got.ruleID, got.eventID
		} else if got.ruleID != canonicalRuleID || got.eventID != canonicalEventID {
			t.Fatalf("all identical publications must converge: want rule/event %d/%d got %d/%d", canonicalRuleID, canonicalEventID, got.ruleID, got.eventID)
		}
		if !got.replay {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one first publication, got %d non-replay outcomes", created)
	}
	assertPublicationCounts(t, db, 1, 1)
	assertSubmissionStatus(t, db, submission.ID, ApprovedSubmissionStatus)
}

func TestPostgresConcurrentDifferentPublishersAllowOneWinner(t *testing.T) {
	db, dsn := publicationPostgresTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("postgres_publish_actor_conflict"))
	sessions := openPublicationPostgresSessions(t, dsn, 2)
	commands := []SubmissionPublicationCommand{{ActorLabel: "publisher-a"}, {ActorLabel: "publisher-b"}}

	type result struct {
		actor   string
		outcome SubmissionPublicationOutcome
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i, command := range commands {
		session := sessions[i]
		command := command
		go func() {
			<-start
			outcome, err := PublishApprovedSubmission(session, submission.ID, command)
			results <- result{actor: command.ActorLabel, outcome: outcome, err: err}
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	for range commands {
		got := <-results
		switch {
		case got.err == nil:
			successes++
			if got.outcome.Replay {
				t.Fatalf("different publishers cannot be exact replay on first race: %+v", got)
			}
		case errors.Is(got.err, ErrSubmissionPublicationConflict):
			conflicts++
		default:
			t.Fatalf("unexpected publication race error for %s: %v", got.actor, got.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one publication winner and one conflict, got success=%d conflict=%d", successes, conflicts)
	}
	assertPublicationCounts(t, db, 1, 1)
}

func TestPostgresConcurrentDifferentApprovedSubmissionsSameCodeHaveOneWinner(t *testing.T) {
	db, dsn := publicationPostgresTestDB(t)
	draft := reviewDraft("postgres_publish_same_code")
	first := createApprovedPublicationSubmission(t, db, draft)
	second := createApprovedPublicationSubmission(t, db, draft)
	if first.ID == second.ID {
		t.Fatal("terminal first submission must allow a distinct second approved source")
	}
	sessions := openPublicationPostgresSessions(t, dsn, 2)

	type result struct {
		submissionID uint
		outcome      SubmissionPublicationOutcome
		err          error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i, submission := range []database.RuleSubmission{first, second} {
		session := sessions[i]
		submission := submission
		go func() {
			<-start
			outcome, err := PublishApprovedSubmission(session, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
			results <- result{submissionID: submission.ID, outcome: outcome, err: err}
		}()
	}
	close(start)

	successes, conflicts := 0, 0
	var loserID uint
	for i := 0; i < 2; i++ {
		got := <-results
		switch {
		case got.err == nil:
			successes++
		case errors.Is(got.err, ErrSubmissionPublicationConflict):
			conflicts++
			loserID = got.submissionID
			if got.outcome.Validation.Valid || len(got.outcome.Validation.Errors) == 0 || got.outcome.Validation.Errors[0].Code != "duplicate_code" {
				t.Fatalf("same-code loser must carry duplicate-code validation context: %+v", got.outcome.Validation)
			}
		default:
			t.Fatalf("unexpected same-code race error for submission %d: %v", got.submissionID, got.err)
		}
	}
	if successes != 1 || conflicts != 1 || loserID == 0 {
		t.Fatalf("expected one same-code winner and loser, success=%d conflict=%d loser=%d", successes, conflicts, loserID)
	}
	assertPublicationCounts(t, db, 1, 1)
	assertSubmissionStatus(t, db, first.ID, ApprovedSubmissionStatus)
	assertSubmissionStatus(t, db, second.ID, ApprovedSubmissionStatus)
	var loserEvents int64
	if err := db.Model(&database.RuleSubmissionPublicationEvent{}).Where("submission_id = ?", loserID).Count(&loserEvents).Error; err != nil {
		t.Fatal(err)
	}
	if loserEvents != 0 {
		t.Fatalf("same-code losing source must remain unpublished, got %d events", loserEvents)
	}
}

func TestPostgresPublicationSourceForeignKeysRestrictDeletion(t *testing.T) {
	db, _ := publicationPostgresTestDB(t)
	submission := createApprovedPublicationSubmission(t, db, reviewDraft("postgres_publish_fk"))
	published, err := PublishApprovedSubmission(db, submission.ID, SubmissionPublicationCommand{ActorLabel: "publisher-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&database.RuleSubmissionReviewEvent{}, published.ReviewEvent.ID).Error; err == nil {
		t.Fatal("expected PostgreSQL publication FK to reject review-event deletion")
	}
	if err := db.Delete(&database.RuleSubmission{}, submission.ID).Error; err == nil {
		t.Fatal("expected PostgreSQL audit FKs to reject source submission deletion")
	}
	assertPublicationCounts(t, db, 1, 1)
}

func publicationPostgresTestDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dsn := os.Getenv(postgresSubmissionIntegrationDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not configured", postgresSubmissionIntegrationDSNEnv)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL publication integration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(32)
	drop := func() {
		_ = db.Migrator().DropTable(
			&database.RuleSubmissionPublicationEvent{},
			&database.RuleSubmissionReviewEvent{},
			&database.RuleSubmission{},
			&database.RiskRule{},
			&database.Category{},
		)
	}
	drop()
	t.Cleanup(func() {
		drop()
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(
		&database.Category{},
		&database.RiskRule{},
		&database.RuleSubmission{},
		&database.RuleSubmissionReviewEvent{},
		&database.RuleSubmissionPublicationEvent{},
	); err != nil {
		t.Fatalf("migrate PostgreSQL publication integration schema: %v", err)
	}
	if err := database.PrepareRuleSubmissionIdempotency(db); err != nil {
		t.Fatalf("prepare PostgreSQL publication idempotency dependencies: %v", err)
	}
	if err := db.Create(&database.Category{Code: "fake_customer_service", Name: "PostgreSQL publication category", SeverityDefault: "high"}).Error; err != nil {
		t.Fatal(err)
	}
	return db, dsn
}

func openPublicationPostgresSessions(t *testing.T, dsn string, count int) []*gorm.DB {
	t.Helper()
	sessions := make([]*gorm.DB, 0, count)
	for i := 0; i < count; i++ {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatalf("open independent PostgreSQL publication session %d: %v", i, err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
		sessions = append(sessions, db)
	}
	return sessions
}
