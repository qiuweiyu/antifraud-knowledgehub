package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/rule"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestControlledReviewTransportApproveReplayAndNoRiskRuleSideEffect(t *testing.T) {
	db := newReviewTransportTestDB(t)
	submission := createReviewTransportPendingSubmission(t, db, "review_transport_approve")
	cfg := controlledReviewConfig()

	core, observed := observer.New(zap.InfoLevel)
	router := newRouter(cfg, zap.New(core), &database.Store{DB: db})
	body := `{"decision":"approved","reason":"  synthetic bounded review reason  "}`

	first := performReviewTransportRequest(router, uintToString(submission.ID), reviewTransportTestToken, "application/json", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first review: expected 200, got %d: %s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Data struct {
			SubmissionID  uint   `json:"submission_id"`
			Status        string `json:"status"`
			ReviewEventID uint   `json:"review_event_id"`
			Decision      string `json:"decision"`
			ActorKind     string `json:"actor_kind"`
			ActorLabel    string `json:"actor_label"`
			Replay        bool   `json:"replay"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	if firstEnvelope.Data.SubmissionID != submission.ID || firstEnvelope.Data.Status != rule.ApprovedSubmissionStatus || firstEnvelope.Data.Decision != rule.ApprovedSubmissionStatus {
		t.Fatalf("unexpected first review response: %+v", firstEnvelope.Data)
	}
	if firstEnvelope.Data.Replay || firstEnvelope.Data.ReviewEventID == 0 || firstEnvelope.Data.ActorKind != rule.ControlledMaintainerActorKind || firstEnvelope.Data.ActorLabel != reviewTransportActorLabel {
		t.Fatalf("unexpected review attribution/replay response: %+v", firstEnvelope.Data)
	}

	var eventCount int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submission.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("first review must create exactly one event, got %d", eventCount)
	}
	var riskRuleCount int64
	if err := db.Model(&database.RiskRule{}).Count(&riskRuleCount).Error; err != nil {
		t.Fatal(err)
	}
	if riskRuleCount != 0 {
		t.Fatalf("approval transport must not create RiskRule rows, got %d", riskRuleCount)
	}

	replay := performReviewTransportRequest(router, uintToString(submission.ID), reviewTransportTestToken, "application/json", `{"decision":"approved","reason":"synthetic bounded review reason"}`)
	if replay.Code != http.StatusOK {
		t.Fatalf("exact replay: expected 200, got %d: %s", replay.Code, replay.Body.String())
	}
	var replayEnvelope struct {
		Data struct {
			ReviewEventID uint `json:"review_event_id"`
			Replay        bool `json:"replay"`
		} `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayEnvelope); err != nil {
		t.Fatal(err)
	}
	if !replayEnvelope.Data.Replay || replayEnvelope.Data.ReviewEventID != firstEnvelope.Data.ReviewEventID {
		t.Fatalf("exact replay must return same review event with replay=true: first=%+v replay=%+v", firstEnvelope.Data, replayEnvelope.Data)
	}
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submission.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("exact replay must not create a second review event, got %d", eventCount)
	}

	for _, entry := range observed.All() {
		serialized, err := json.Marshal(entry.ContextMap())
		if err != nil {
			t.Fatal(err)
		}
		logText := entry.Message + string(serialized)
		if strings.Contains(logText, reviewTransportTestToken) ||
			strings.Contains(logText, submissionTransportTestToken) ||
			strings.Contains(logText, "synthetic bounded review reason") ||
			strings.Contains(logText, "synthetic review transport signal") {
			t.Fatalf("request log leaked credential or review/submission content: %s", logText)
		}
	}
}

func TestControlledReviewTransportRejectsPendingWithoutRiskRuleSideEffect(t *testing.T) {
	db := newReviewTransportTestDB(t)
	submission := createReviewTransportPendingSubmission(t, db, "review_transport_reject")
	router := newRouter(controlledReviewConfig(), zap.NewNop(), &database.Store{DB: db})

	resp := performReviewTransportRequest(router, uintToString(submission.ID), reviewTransportTestToken, "application/json", `{"decision":"rejected","reason":"synthetic bounded rejection"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("rejection: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var stored database.RuleSubmission
	if err := db.First(&stored, submission.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != rule.RejectedSubmissionStatus {
		t.Fatalf("expected rejected terminal status, got %q", stored.Status)
	}
	var eventCount int64
	if err := db.Model(&database.RuleSubmissionReviewEvent{}).Where("submission_id = ?", submission.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one rejection event, got %d", eventCount)
	}
	var riskRuleCount int64
	if err := db.Model(&database.RiskRule{}).Count(&riskRuleCount).Error; err != nil {
		t.Fatal(err)
	}
	if riskRuleCount != 0 {
		t.Fatalf("rejection transport must not create RiskRule rows, got %d", riskRuleCount)
	}
}
