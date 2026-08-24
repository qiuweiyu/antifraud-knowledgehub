package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type publicContentBaseline struct {
	category database.Category
	rule     database.RiskRule
	caseItem database.ScamCase
}

type prohibitedPublicMutationRoute struct {
	method string
	path   func(publicContentBaseline) string
	body   string
}

func newPublicContentSecurityTestDB(t *testing.T) (*gorm.DB, publicContentBaseline) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.Category{}, &database.RiskRule{}, &database.ScamCase{}, &database.AnalysisRecord{}); err != nil {
		t.Fatal(err)
	}

	baseline := publicContentBaseline{
		category: database.Category{
			Code:            "fake_customer_service",
			Name:            "Baseline category",
			Description:     "baseline category description",
			SeverityDefault: "high",
		},
		rule: database.RiskRule{
			Code:           "baseline_rule",
			Name:           "Baseline rule",
			Description:    "baseline rule description",
			CategoryCode:   "fake_customer_service",
			RuleType:       "keyword",
			Pattern:        "baseline keyword",
			Weight:         25,
			Severity:       "high",
			Enabled:        true,
			Explanation:    "baseline explanation",
			Recommendation: "baseline recommendation",
		},
		caseItem: database.ScamCase{
			Title:        "Baseline case",
			CategoryCode: "fake_customer_service",
			Content:      "baseline case content",
			Summary:      "baseline case summary",
			SourceType:   "sample",
			Anonymized:   true,
		},
	}

	if err := db.Create(&baseline.category).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&baseline.rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&baseline.caseItem).Error; err != nil {
		t.Fatal(err)
	}
	return db, baseline
}

func newPublicContentSecurityRouter(db *gorm.DB) http.Handler {
	return newRouter(
		config.Config{CORSAllowOrigins: []string{"*"}, AppPort: "8080"},
		zap.NewNop(),
		&database.Store{DB: db},
	)
}

func prohibitedPublicMutationRoutes() []prohibitedPublicMutationRoute {
	return []prohibitedPublicMutationRoute{
		{
			method: http.MethodPost,
			path:   func(publicContentBaseline) string { return "/api/v1/categories" },
			body:   `{"code":"attacker_category","name":"Attacker category","severity_default":"critical"}`,
		},
		{
			method: http.MethodPut,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/categories/%d", b.category.ID) },
			body:   `{"name":"Mutated category"}`,
		},
		{
			method: http.MethodDelete,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/categories/%d", b.category.ID) },
		},
		{
			method: http.MethodPost,
			path:   func(publicContentBaseline) string { return "/api/v1/rules" },
			body: `{"code":"attacker_rule","name":"Attacker rule","category_code":"fake_customer_service",` +
				`"rule_type":"keyword","pattern":"attacker keyword","weight":100,"severity":"critical","enabled":true}`,
		},
		{
			method: http.MethodPut,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/rules/%d", b.rule.ID) },
			body:   `{"weight":0,"enabled":false,"name":"Mutated rule"}`,
		},
		{
			method: http.MethodPatch,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/rules/%d/toggle", b.rule.ID) },
		},
		{
			method: http.MethodDelete,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/rules/%d", b.rule.ID) },
		},
		{
			method: http.MethodPost,
			path:   func(publicContentBaseline) string { return "/api/v1/cases" },
			body:   `{"title":"Attacker case","category_code":"fake_customer_service","content":"attacker content"}`,
		},
		{
			method: http.MethodPut,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/cases/%d", b.caseItem.ID) },
			body:   `{"title":"Mutated case"}`,
		},
		{
			method: http.MethodDelete,
			path:   func(b publicContentBaseline) string { return fmt.Sprintf("/api/v1/cases/%d", b.caseItem.ID) },
		},
	}
}

func assertPublicContentBaselineUnchanged(t *testing.T, db *gorm.DB, baseline publicContentBaseline) {
	t.Helper()

	var categories []database.Category
	if err := db.Order("id asc").Find(&categories).Error; err != nil {
		t.Fatal(err)
	}
	if len(categories) != 1 || categories[0].ID != baseline.category.ID || categories[0].Code != baseline.category.Code || categories[0].Name != baseline.category.Name || categories[0].SeverityDefault != baseline.category.SeverityDefault {
		t.Fatalf("category persistence changed through prohibited public route: %#v", categories)
	}

	var rules []database.RiskRule
	if err := db.Order("id asc").Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != baseline.rule.ID || rules[0].Code != baseline.rule.Code || rules[0].Name != baseline.rule.Name || rules[0].Weight != baseline.rule.Weight || rules[0].Enabled != baseline.rule.Enabled {
		t.Fatalf("rule persistence changed through prohibited public route: %#v", rules)
	}

	var cases []database.ScamCase
	if err := db.Order("id asc").Find(&cases).Error; err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != baseline.caseItem.ID || cases[0].Title != baseline.caseItem.Title || cases[0].Content != baseline.caseItem.Content {
		t.Fatalf("case persistence changed through prohibited public route: %#v", cases)
	}
}

func TestPublicContentMutationRoutesAreAbsentAndCannotWrite(t *testing.T) {
	db, baseline := newPublicContentSecurityTestDB(t)
	router := newPublicContentSecurityRouter(db)

	for _, tc := range prohibitedPublicMutationRoutes() {
		path := tc.path(baseline)
		t.Run(tc.method+"_"+path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusNotFound {
				t.Fatalf("prohibited public mutation route %s %s must be unregistered, got %d", tc.method, path, resp.Code)
			}
			assertPublicContentBaselineUnchanged(t, db, baseline)
		})
	}
}

func TestPublicContentReadAndValidationRoutesRemainAvailable(t *testing.T) {
	db, baseline := newPublicContentSecurityTestDB(t)
	router := newPublicContentSecurityRouter(db)

	reads := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/categories"},
		{http.MethodGet, fmt.Sprintf("/api/v1/categories/%d", baseline.category.ID)},
		{http.MethodGet, "/api/v1/rules"},
		{http.MethodGet, fmt.Sprintf("/api/v1/rules/%d", baseline.rule.ID)},
		{http.MethodGet, "/api/v1/cases"},
		{http.MethodGet, fmt.Sprintf("/api/v1/cases/%d", baseline.caseItem.ID)},
	}
	for _, tc := range reads {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("public read route %s %s must remain available, got %d", tc.method, tc.path, resp.Code)
		}
	}

	validateBody := `{"code":"validated_only","name":"Validated only","category_code":"fake_customer_service",` +
		`"rule_type":"keyword","pattern":"validation keyword","weight":20,"severity":"medium"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules/validate", strings.NewReader(validateBody))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("rule validation must remain available, got %d: %s", resp.Code, resp.Body.String())
	}
	assertPublicContentBaselineUnchanged(t, db, baseline)
}
