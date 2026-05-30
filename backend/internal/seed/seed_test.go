package seed

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		{"scam_cases_sample.zh-CN.json", &[]database.ScamCase{}, 20},
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
