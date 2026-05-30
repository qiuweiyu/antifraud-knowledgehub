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

func Register(r gin.IRoutes, db *gorm.DB) {
	h := Handler{db: db}
	r.POST("/analysis/text", h.analyze)
	r.GET("/analysis/recent", h.recent)
}

func (h Handler) analyze(c *gin.Context) {
	var req AnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Text == "" {
		response.Fail(c, http.StatusBadRequest, "invalid_analysis_request", "text is required")
		return
	}
	var dbRules []database.RiskRule
	h.db.Where("enabled = ?", true).Find(&dbRules)
	rules := make([]riskengine.Rule, 0, len(dbRules))
	for _, item := range dbRules {
		rules = append(rules, riskengine.Rule{
			Code: item.Code, Name: item.Name, CategoryCode: item.CategoryCode, RuleType: item.RuleType,
			Pattern: item.Pattern, Weight: item.Weight, Severity: item.Severity,
			Explanation: item.Explanation, Recommendation: item.Recommendation,
		})
	}
	result := riskengine.New(rules).Analyze(req.Text)
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
