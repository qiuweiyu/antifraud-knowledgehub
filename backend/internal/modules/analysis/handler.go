package analysis

import (
	"encoding/json"
	"net/http"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Handler struct{ db *gorm.DB }

type AnalyzeRequest struct {
	Text string `json:"text" binding:"required"`
}

type StatsResponse struct {
	Categories            int64                  `json:"categories"`
	Rules                 int64                  `json:"rules"`
	EnabledRules          int64                  `json:"enabled_rules"`
	Cases                 int64                  `json:"cases"`
	AnalysisRecords       int64                  `json:"analysis_records"`
	RiskLevelDistribution map[string]int64       `json:"risk_level_distribution"`
	CategoryDistribution  []CategoryDistribution `json:"category_distribution"`
}

type CategoryDistribution struct {
	CategoryCode string `json:"category_code"`
	CategoryName string `json:"category_name"`
	RuleCount    int64  `json:"rule_count"`
	CaseCount    int64  `json:"case_count"`
}

func Register(r gin.IRoutes, db *gorm.DB) {
	h := Handler{db: db}
	r.POST("/analysis/text", h.analyze)
	r.POST("/analysis/preview", h.preview)
	r.GET("/analysis/recent", h.recent)
	r.GET("/analysis/stats", h.stats)
}

func (h Handler) bindAnalyzeRequest(c *gin.Context) (AnalyzeRequest, bool) {
	var req AnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Text == "" {
		response.Fail(c, http.StatusBadRequest, "invalid_analysis_request", "text is required")
		return AnalyzeRequest{}, false
	}
	return req, true
}

func (h Handler) analyzeText(text string) riskengine.Result {
	var dbRules []database.RiskRule
	h.db.Where("enabled = ?", true).Find(&dbRules)
	rules := make([]riskengine.Rule, 0, len(dbRules))
	for _, item := range dbRules {
		rules = append(rules, riskengine.Rule{
			Code: item.Code, Name: item.Name, CategoryCode: item.CategoryCode, RuleType: item.RuleType,
			Pattern: item.Pattern, Weight: item.Weight, Severity: item.Severity,
			Explanation: item.Explanation, Recommendation: item.Recommendation, Version: item.Version,
		})
	}
	return riskengine.New(rules).Analyze(text)
}

func (h Handler) preview(c *gin.Context) {
	req, ok := h.bindAnalyzeRequest(c)
	if !ok {
		return
	}
	response.OK(c, h.analyzeText(req.Text))
}

func (h Handler) analyze(c *gin.Context) {
	req, ok := h.bindAnalyzeRequest(c)
	if !ok {
		return
	}
	result := h.analyzeText(req.Text)
	matched, _ := json.Marshal(result.MatchedRules)
	recs, _ := json.Marshal(result.Recommendations)
	h.db.Create(&database.AnalysisRecord{
		InputText: req.Text, RiskScore: result.RiskScore, RiskLevel: result.RiskLevel,
		MatchedRules: datatypes.JSON(matched), Explanation: result.Summary, Recommendations: datatypes.JSON(recs),
	})
	response.OK(c, result)
}

func (h Handler) recent(c *gin.Context) {
	var count int64
	h.db.Model(&database.AnalysisRecord{}).Count(&count)
	response.OK(c, gin.H{"count": count})
}

func (h Handler) stats(c *gin.Context) {
	var result StatsResponse
	h.db.Model(&database.Category{}).Count(&result.Categories)
	h.db.Model(&database.RiskRule{}).Count(&result.Rules)
	h.db.Model(&database.RiskRule{}).Where("enabled = ?", true).Count(&result.EnabledRules)
	h.db.Model(&database.ScamCase{}).Count(&result.Cases)
	h.db.Model(&database.AnalysisRecord{}).Count(&result.AnalysisRecords)
	result.RiskLevelDistribution = map[string]int64{"low": 0, "medium": 0, "high": 0, "critical": 0}

	var levels []struct {
		RiskLevel string
		Count     int64
	}
	h.db.Model(&database.AnalysisRecord{}).
		Select("risk_level, count(*) as count").
		Group("risk_level").
		Scan(&levels)
	for _, item := range levels {
		result.RiskLevelDistribution[item.RiskLevel] = item.Count
	}

	var categories []database.Category
	h.db.Order("id asc").Find(&categories)
	result.CategoryDistribution = make([]CategoryDistribution, 0, len(categories))
	for _, category := range categories {
		var ruleCount int64
		var caseCount int64
		h.db.Model(&database.RiskRule{}).Where("category_code = ?", category.Code).Count(&ruleCount)
		h.db.Model(&database.ScamCase{}).Where("category_code = ?", category.Code).Count(&caseCount)
		result.CategoryDistribution = append(result.CategoryDistribution, CategoryDistribution{
			CategoryCode: category.Code,
			CategoryName: category.Name,
			RuleCount:    ruleCount,
			CaseCount:    caseCount,
		})
	}

	response.OK(c, result)
}