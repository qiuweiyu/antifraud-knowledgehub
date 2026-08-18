package seed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
)

func TestSeedDataParses(t *testing.T) {
	root := filepath.Join("..", "..", "..", "data")
	files := []struct {
		name   string
		target any
		min    int
	}{
		{"scam_categories.zh-CN.json", &[]database.Category{}, 10},
		{"risk_rules.zh-CN.json", &[]database.RiskRule{}, 35},
		{"scam_cases_sample.zh-CN.json", &[]database.ScamCase{}, 28},
	}
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(root, file.name))
		if err != nil {
			t.Fatalf("read %s: %v", file.name, err)
		}
		if err := json.Unmarshal(raw, file.target); err != nil {
			t.Fatalf("parse %s: %v", file.name, err)
		}
		switch items := file.target.(type) {
		case *[]database.Category:
			if len(*items) < file.min {
				t.Fatalf("expected at least %d categories", file.min)
			}
		case *[]database.RiskRule:
			if len(*items) < file.min {
				t.Fatalf("expected at least %d rules", file.min)
			}
		case *[]database.ScamCase:
			if len(*items) < file.min {
				t.Fatalf("expected at least %d cases", file.min)
			}
		}
	}
}

func TestScamCaseSeedQuality(t *testing.T) {
	root := filepath.Join("..", "..", "..", "data")
	categories := readSeedJSON[database.Category](t, root, "scam_categories.zh-CN.json")
	cases := readSeedJSON[database.ScamCase](t, root, "scam_cases_sample.zh-CN.json")

	knownCategories := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		knownCategories[category.Code] = struct{}{}
	}

	seenTitles := make(map[string]struct{}, len(cases))
	for i, item := range cases {
		caseNumber := i + 1
		title := strings.TrimSpace(item.Title)
		if title == "" {
			t.Fatalf("case %d has empty title", caseNumber)
		}
		if _, exists := seenTitles[title]; exists {
			t.Fatalf("case %d duplicates title %q", caseNumber, title)
		}
		seenTitles[title] = struct{}{}

		if strings.TrimSpace(item.CategoryCode) == "" {
			t.Fatalf("case %d %q has empty category_code", caseNumber, title)
		}
		if _, exists := knownCategories[item.CategoryCode]; !exists {
			t.Fatalf("case %d %q references unknown category_code %q", caseNumber, title, item.CategoryCode)
		}
		if strings.TrimSpace(item.Content) == "" {
			t.Fatalf("case %d %q has empty content", caseNumber, title)
		}
		if strings.TrimSpace(item.Summary) == "" {
			t.Fatalf("case %d %q has empty summary", caseNumber, title)
		}
		if item.SourceType != "sample" {
			t.Fatalf("case %d %q must use source_type=sample, got %q", caseNumber, title, item.SourceType)
		}
		if !item.Anonymized {
			t.Fatalf("case %d %q must be anonymized", caseNumber, title)
		}

		assertNonEmptyStringList(t, caseNumber, title, "risk_points", item.RiskPoints)
		assertNonEmptyStringList(t, caseNumber, title, "tags", item.Tags)
	}
}

func readSeedJSON[T any](t *testing.T, root, name string) []T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return items
}

func assertNonEmptyStringList(t *testing.T, caseNumber int, title, field string, raw []byte) {
	t.Helper()
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("case %d %q has invalid %s: %v", caseNumber, title, field, err)
	}
	if len(values) == 0 {
		t.Fatalf("case %d %q has empty %s", caseNumber, title, field)
	}
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("case %d %q has blank %s[%d]", caseNumber, title, field, i)
		}
	}
}
