package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/config"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/middleware"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/analysis"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/caseitem"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/category"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/health"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/modules/rule"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/seed"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

func main() {
	seedOnly := flag.Bool("seed-only", false, "import seed data and exit")
	flag.Parse()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("configuration invalid: %v", err)
	}
	browserSessionCfg := config.LoadBrowserSessionConfig()
	if err := browserSessionCfg.Validate(cfg.IsProduction()); err != nil {
		log.Fatalf("browser session configuration invalid: %v", err)
	}
	if err := browserSessionCfg.ValidateAssistedAnalysisPrerequisites(cfg); err != nil {
		log.Fatalf("browser assisted-analysis configuration invalid: %v", err)
	}
	logger, _ := zap.NewProduction()
	if !cfg.IsProduction() {
		logger, _ = zap.NewDevelopment()
	}
	defer logger.Sync()

	store, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("database connect failed: %v", err)
	}
	if err := seed.ImportIfEmpty(store.DB, "../data"); err != nil {
		log.Fatalf("seed import failed: %v", err)
	}
	if *seedOnly {
		return
	}

	router := newRouterWithBrowserSession(cfg, browserSessionCfg, logger, store)
	addr := fmt.Sprintf(":%d", cfg.PortInt())
	logger.Info("server listening", zap.String("addr", addr))
	if err := router.Run(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func newRouter(cfg config.Config, logger *zap.Logger, store *database.Store) *gin.Engine {
	return newRouterWithBrowserSession(cfg, config.BrowserSessionConfig{}, logger, store)
}

func newRouterWithBrowserSession(cfg config.Config, browserSessionCfg config.BrowserSessionConfig, logger *zap.Logger, store *database.Store) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), middleware.CORS(cfg.CORSAllowOrigins), middleware.RequestLogger(logger))
	v1 := r.Group("/api/v1")
	health.Register(v1)
	category.Register(v1, store.DB)
	rule.Register(v1, store.DB)
	caseitem.Register(v1, store.DB)
	analysis.Register(v1, store.DB)

	if browserSessionCfg.Enabled {
		sessionHandler := registerConfiguredBrowserSessionRoutes(v1, browserSessionCfg, cfg.IsProduction(), store)
		if browserSessionCfg.AnalysisEnabled {
			registerConfiguredBrowserAssistedAnalysisRoutes(v1, cfg, browserSessionCfg, store, sessionHandler)
		}
	}

	if cfg.LLMAssistedAnalysisHTTPEnabled {
		registerConfiguredLLMAssistedAnalysisRoute(v1, cfg, store)
	}

	if cfg.RuleSubmissionsEnabled {
		submissionRateConfig := middleware.SubmissionRateConfig{
			CredentialLimit: cfg.RuleSubmissionCredentialLimit,
			GlobalLimit:     cfg.RuleSubmissionGlobalLimit,
			Window:          cfg.RuleSubmissionRateWindow,
		}
		submissionAuthorization := middleware.SubmissionWriteAuthorization(cfg.RuleSubmissionWriteToken)

		v1.POST(
			"/rule-submissions",
			submissionAuthorization,
			middleware.SubmissionWriteRateLimit(
				middleware.RedisSubmissionRateBackend{Client: store.Redis},
				cfg.RuleSubmissionWriteToken,
				submissionRateConfig,
			),
			rule.SubmissionCreateHandler(store.DB),
		)
		v1.POST(
			"/rules/:id/revision-submissions",
			middleware.SubmissionWriteAuthorization(cfg.RuleSubmissionWriteToken),
			middleware.SubmissionWriteRateLimit(
				middleware.RedisSubmissionRateBackend{Client: store.Redis},
				cfg.RuleSubmissionWriteToken,
				submissionRateConfig,
			),
			rule.RevisionSubmissionCreateHandler(store.DB),
		)
	}

	if cfg.RuleSubmissionReviewsEnabled {
		v1.POST(
			"/rule-submissions/:id/reviews",
			middleware.SubmissionReviewAuthorization(cfg.RuleSubmissionReviewToken),
			rule.SubmissionReviewHandler(store.DB, cfg.RuleSubmissionReviewActorLabel),
		)
	}

	if cfg.RuleSubmissionPublicationsEnabled {
		v1.POST(
			"/rule-submissions/:id/publications",
			middleware.SubmissionPublicationAuthorization(cfg.RuleSubmissionPublicationToken),
			rule.RevisionPublicationI2Guard(store.DB),
			rule.SubmissionPublicationHandler(store.DB, cfg.RuleSubmissionPublicationActorLabel),
		)
	}

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	return r
}
