package rule

import (
	"net/http"
	"strings"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct{ db *gorm.DB }

func Register(r gin.IRoutes, db *gorm.DB) {
	h := Handler{db: db}
	r.GET("/rules", h.list)
	r.POST("/rules/validate", h.validate)
	r.GET("/rules/:id", h.get)
}

func (h Handler) list(c *gin.Context) {
	var items []database.RiskRule
	q := h.db.Order("id asc")
	if category := c.Query("category_code"); category != "" {
		q = q.Where("category_code = ?", category)
	}
	if severity := c.Query("severity"); severity != "" {
		q = q.Where("severity = ?", severity)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ? OR LOWER(pattern) LIKE ?", like, like, like)
	}
	q.Find(&items)
	response.OK(c, items)
}

func (h Handler) validate(c *gin.Context) {
	var draft DraftRequest
	if err := c.ShouldBindJSON(&draft); err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_rule", err.Error())
		return
	}
	response.OK(c, ValidateDraft(h.db, draft))
}

func (h Handler) get(c *gin.Context) {
	var item database.RiskRule
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	response.OK(c, item)
}
