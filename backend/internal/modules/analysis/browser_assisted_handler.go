package analysis

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/llmassist"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/riskengine"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type BrowserProfileRegistry interface {
	Resolve(id string) (llmassist.ResolvedProfile, error)
	PublicProfiles() []llmassist.ProfilePublicMetadata
}

type BrowserAssistedAnalysisRequest struct {
	Text      string `json:"text"`
	ProfileID string `json:"profile_id"`
}

type BrowserAssistedAnalysisResponse struct {
	RuleResult    riskengine.Result          `json:"rule_result"`
	LLMAssistance BrowserAssistedLLMResponse `json:"llm_assistance"`
}

type BrowserAssistedLLMResponse struct {
	Status     llmassist.Status                `json:"status"`
	Assistance llmassist.Assistance            `json:"assistance"`
	Profile    llmassist.ProfilePublicMetadata `json:"profile"`
}

func BrowserAssistedProfilesHandler(registry BrowserProfileRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if registry == nil {
			response.Fail(c, http.StatusServiceUnavailable, "assisted_profiles_unavailable", "Assisted profiles unavailable")
			return
		}
		profiles := registry.PublicProfiles()
		if profiles == nil {
			profiles = []llmassist.ProfilePublicMetadata{}
		}
		response.OK(c, profiles)
	}
}

func BrowserAssistedAnalysisHandler(db *gorm.DB, registry BrowserProfileRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if db == nil || registry == nil {
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
		var request *BrowserAssistedAnalysisRequest
		if err := decoder.Decode(&request); err != nil {
			writeBrowserAssistedDecodeError(c, err)
			return
		}
		if request == nil {
			response.Fail(c, http.StatusBadRequest, "invalid_browser_assisted_request", "Invalid browser assisted-analysis request")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeBrowserAssistedDecodeError(c, err)
			return
		}

		if strings.TrimSpace(request.Text) == "" {
			response.Fail(c, http.StatusBadRequest, "invalid_browser_assisted_request", "Assisted-analysis text is required")
			return
		}
		if len([]byte(request.Text)) > maxAssistedAnalysisTextBytes {
			response.Fail(c, http.StatusBadRequest, "invalid_browser_assisted_request", "Assisted-analysis text exceeds limit")
			return
		}
		if strings.TrimSpace(request.ProfileID) == "" {
			response.Fail(c, http.StatusBadRequest, "invalid_assisted_profile", "Assisted profile is required")
			return
		}

		profile, err := registry.Resolve(request.ProfileID)
		if err != nil {
			response.Fail(c, http.StatusBadRequest, "invalid_assisted_profile", "Assisted profile is unavailable")
			return
		}

		rules, err := loadAssistedAnalysisRules(c.Request.Context(), db)
		if err != nil {
			response.Fail(c, http.StatusServiceUnavailable, "analysis_unavailable", "Analysis unavailable")
			return
		}
		ruleResult := riskengine.New(rules).Analyze(request.Text)
		outcome := profile.Service.Assist(c.Request.Context(), llmassist.Input{
			Text:       request.Text,
			RuleResult: ruleResult,
		})

		response.OK(c, BrowserAssistedAnalysisResponse{
			RuleResult: ruleResult,
			LLMAssistance: BrowserAssistedLLMResponse{
				Status:     outcome.Status,
				Assistance: outcome.Assistance,
				Profile:    profile.Public,
			},
		})
	}
}

func writeBrowserAssistedDecodeError(c *gin.Context, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		response.Fail(c, http.StatusRequestEntityTooLarge, "browser_assisted_request_too_large", "Browser assisted-analysis request is too large")
		return
	}
	response.Fail(c, http.StatusBadRequest, "invalid_browser_assisted_request", "Invalid browser assisted-analysis request")
}
