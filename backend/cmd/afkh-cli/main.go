package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "analyze":
		cmd := flag.NewFlagSet("analyze", flag.ExitOnError)
		text := cmd.String("text", "", "text to analyze")
		_ = cmd.Parse(os.Args[2:])
		if *text == "" {
			log.Fatal("--text is required")
		}
		printJSON(loadEngine().Analyze(*text))
	case "rules":
		if len(os.Args) >= 3 && os.Args[2] == "list" {
			printJSON(loadRules())
			return
		}
		usage()
	case "categories":
		if len(os.Args) >= 3 && os.Args[2] == "list" {
			printJSON(loadCategories())
			return
		}
		usage()
	default:
		usage()
	}
}

func usage() {
	fmt.Println(`Usage:
  afkh-cli analyze --text "客服说账户异常，需要转账到安全账户"
  afkh-cli rules list
  afkh-cli categories list`)
}

func loadEngine() riskengine.Engine {
	dbRules := loadRules()
	rules := make([]riskengine.Rule, 0, len(dbRules))
	for _, item := range dbRules {
		if !item.Enabled {
			continue
		}
		rules = append(rules, riskengine.Rule{
			Code: item.Code, Name: item.Name, CategoryCode: item.CategoryCode, RuleType: item.RuleType,
			Pattern: item.Pattern, Weight: item.Weight, Severity: item.Severity,
			Explanation: item.Explanation, Recommendation: item.Recommendation,
		})
	}
	return riskengine.New(rules)
}

func loadRules() []database.RiskRule {
	var rules []database.RiskRule
	mustReadJSON("risk_rules.zh-CN.json", &rules)
	return rules
}

func loadCategories() []database.Category {
	var categories []database.Category
	mustReadJSON("scam_categories.zh-CN.json", &categories)
	return categories
}

func mustReadJSON(name string, target any) {
	candidates := []string{
		filepath.Join("..", "data", name),
		filepath.Join("data", name),
		filepath.Join("..", "..", "data", name),
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err == nil {
			if err := json.Unmarshal(raw, target); err != nil {
				log.Fatalf("parse %s: %v", path, err)
			}
			return
		}
	}
	log.Fatalf("cannot find %s", name)
}

func printJSON(value any) {
	raw, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(raw))
}
