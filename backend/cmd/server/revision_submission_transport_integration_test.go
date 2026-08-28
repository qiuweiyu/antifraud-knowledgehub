package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"go.uber.org/zap"
)

func TestControlledRevisionSubmissionTransportCreatesReplaysAndRejectsClientCode(t *testing.T) {
	db := newRevisionSubmissionTransportTestDB(t)
	target := createRevisionSubmissionTransportTarget(t, db, "revision_transport_success")
	client := submissionTransportRedisClient(t)
	cfg := controlledSubmissionConfig()
	cfg.RuleSubmissionCredentialLimit = 5
	cfg.RuleSubmissionGlobalLimit = 10
	cfg.RuleSubmissionRateWindow = time.Minute
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db, Redis: client})

	wrong := performRevisionSubmissionTransportRequest(router, target.ID, "wrong-token-value", revisionSubmissionTransportBody())
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token must return 401, got %d: %s", wrong.Code, wrong.Body.String())
	}

	first := performRevisionSubmissionTransportRequest(router, target.ID, submissionTransportTestToken, revisionSubmissionTransportBody())
	if first.Code != http.StatusCreated {
		t.Fatalf("valid revision submission must return 201, got %d: %s", first.Code, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), `"kind":"revision"`) || !strings.Contains(first.Body.String(), `"base_version":1`) {
		t.Fatalf("revision response must expose server-owned revision intent: %s", first.Body.String())
	}

	replay := performRevisionSubmissionTransportRequest(router, target.ID, submissionTransportTestToken, revisionSubmissionTransportBody())
	if replay.Code != http.StatusOK {
		t.Fatalf("exact revision replay must return 200, got %d: %s", replay.Code, replay.Body.String())
	}
	if replay.Body.String() != first.Body.String() {
		t.Fatalf("exact revision replay must return the existing submission payload\nfirst: %s\nreplay: %s", first.Body.String(), replay.Body.String())
	}

	clientCode := strings.TrimSuffix(revisionSubmissionTransportBody(), "}") + `,"code":"client-controlled"}`
	invalid := performRevisionSubmissionTransportRequest(router, target.ID, submissionTransportTestToken, clientCode)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":"invalid_submission_json"`) {
		t.Fatalf("client-controlled code field must be rejected by strict JSON parsing, got %d: %s", invalid.Code, invalid.Body.String())
	}

	assertSubmissionTransportWrites(t, db, 1, 1)
}

func TestCreateAndRevisionSubmissionsShareCredentialRateBudget(t *testing.T) {
	db := newRevisionSubmissionTransportTestDB(t)
	target := createRevisionSubmissionTransportTarget(t, db, "revision_shared_rate")
	client := submissionTransportRedisClient(t)
	cfg := controlledSubmissionConfig()
	cfg.RuleSubmissionCredentialLimit = 1
	cfg.RuleSubmissionGlobalLimit = 10
	cfg.RuleSubmissionRateWindow = time.Minute
	router := newRouter(cfg, zap.NewNop(), &database.Store{DB: db, Redis: client})

	create := performSubmissionTransportRequest(router, submissionTransportTestToken, "application/json", validSubmissionTransportBody(t))
	if create.Code != http.StatusCreated {
		t.Fatalf("first create submission must consume shared budget successfully, got %d: %s", create.Code, create.Body.String())
	}
	revision := performRevisionSubmissionTransportRequest(router, target.ID, submissionTransportTestToken, revisionSubmissionTransportBody())
	if revision.Code != http.StatusTooManyRequests {
		t.Fatalf("revision must share create credential rate budget and be limited, got %d: %s", revision.Code, revision.Body.String())
	}
	assertSubmissionTransportWrites(t, db, 1, 1)
}
