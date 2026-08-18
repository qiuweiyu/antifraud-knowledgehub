package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxAssistedAnalysisBodyBytes = 16 * 1024
	maxAssistedAnalysisTextBytes = 12 * 1024
)

type assistedAnalysisService interface {
	Assist(context.Context, llmassist.Input) llmassist.Outcome
}

type AssistedAnalysisRequest struct {
	Text string `json:"text"`
}

type AssistedAnalysisResponse struct {
	RuleResult    riskengine.Result          `json:"rule_result"`
	LLMAssistance AssistedLLMResponse        `json:"llm_assistance"`
}

type AssistedLLMResponse struct {
	Status     llmassist.Status     `json:"status"`
	Provider   string               `json:"provider"`
	Model      string               `json:"model"`
	Assistance llmassist.Assistance `json:"assistance"`
}

func AssistedAnalysisHandler(db *gorm.DB, service assistedAnalysisService, provider, model string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil || service == nil {
			response.Fail(c, http.StatusServiceUnavailable, "analysis_unavailable", "Analysis unavailable")
			return
		}

		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" {
			response.Fail(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAssistedAnalysisBodyBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()

		var request *AssistedAnalysisRequest
		if err := decoder.Decode(&request); err != nil {
			writeAssistedDecodeError(c, err)
			return
		}
		if request == nil {
			response.Fail(c, http.StatusBadRequest, "invalid_assisted_analysis_request", "Invalid assisted-analysis request")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeAssistedDecodeError(c, err)
			return
		}

		if strings.TrimSpace(request.Text) == "" || len([]byte(request.Text)) > maxAssistedAnalysisTextBytes {
			if len([]byte(request.Text)) > maxAssistedAnalysisTextBytes {
				response.Fail(c, http.StatusBadRequest, "invalid_assisted_analysis_request", "Assisted-analysis text exceeds limit")
				return
			}
			response.Fail(c, http.StatusBadRequest, "invalid_assisted_analysis_request", "Assisted-analysis text is required")
			return
		}

		rules, err := loadAssistedAnalysisRules(c.Request.Context(), db)
		if err != nil {
			response.Fail(c, http.StatusServiceUnavailable, "analysis_unavailable", "Analysis unavailable")
			return
		}
		ruleResult := riskengine.New(rules).Analyze(request.Text)
		outcome := service.Assist(c.Request.Context(), llmassist.Input{
			Text:       request.Text,
			RuleResult: ruleResult,
		})

		response.OK(c, AssistedAnalysisResponse{
			RuleResult: ruleResult,
			LLMAssistance: AssistedLLMResponse{
				Status:     outcome.Status,
				Provider:   provider,
				Model:      model,
				Assistance: outcome.Assistance,
			},
		})
	}
}

func loadAssistedAnalysisRules(ctx context.Context, db *gorm.DB) ([]riskengine.Rule, error) {
	var dbRules []database.RiskRule
	result := db.WithContext(ctx).Where("enabled = ?", true).Find(&dbRules)
	if result.Error != nil {
		return nil, result.Error
	}

	rules := make([]riskengine.Rule, 0, len(dbRules))
	for _, item := range dbRules {
		rules = append(rules, riskengine.Rule{
			Code:           item.Code,
			Name:           item.Name,
			CategoryCode:   item.CategoryCode,
			RuleType:       item.RuleType,
			Pattern:        item.Pattern,
			Weight:         item.Weight,
			Severity:       item.Severity,
			Explanation:    item.Explanation,
			Recommendation: item.Recommendation,
		})
	}
	return rules, nil
}

func writeAssistedDecodeError(c *gin.Context, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		response.Fail(c, http.StatusRequestEntityTooLarge, "assisted_analysis_request_too_large", "Assisted-analysis request is too large")
		return
	}
	response.Fail(c, http.StatusBadRequest, "invalid_assisted_analysis_request", "Invalid assisted-analysis request")
}
