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

	router := newRouter(cfg, logger, store)
	addr := fmt.Sprintf(":%d", cfg.PortInt())
	logger.Info("server listening", zap.String("addr", addr))
	if err := router.Run(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func newRouter(cfg config.Config, logger *zap.Logger, store *database.Store) *gin.Engine {
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
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	return r
}
