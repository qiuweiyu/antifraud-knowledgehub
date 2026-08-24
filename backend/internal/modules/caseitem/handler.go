package caseitem

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
	r.GET("/cases", h.list)
	r.GET("/cases/:id", h.get)
}

func (h Handler) list(c *gin.Context) {
	var items []database.ScamCase
	q := h.db.Order("id asc")
	if category := c.Query("category_code"); category != "" {
		q = q.Where("category_code = ?", category)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		q = q.Where("LOWER(title) LIKE ? OR LOWER(content) LIKE ? OR LOWER(summary) LIKE ?", like, like, like)
	}
	q.Find(&items)
	response.OK(c, items)
}

func (h Handler) get(c *gin.Context) {
	var item database.ScamCase
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "case_not_found", "case not found")
		return
	}
	response.OK(c, item)
}
