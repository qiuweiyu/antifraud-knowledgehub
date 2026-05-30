package caseitem

import (
	"net/http"

	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/internal/database"
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct{ db *gorm.DB }

func Register(r gin.IRoutes, db *gorm.DB) {
	h := Handler{db: db}
	r.GET("/cases", h.list)
	r.POST("/cases", h.create)
	r.GET("/cases/:id", h.get)
	r.PUT("/cases/:id", h.update)
	r.DELETE("/cases/:id", h.delete)
}

func (h Handler) list(c *gin.Context) {
	var items []database.ScamCase
	q := h.db.Order("id asc")
	if category := c.Query("category_code"); category != "" {
		q = q.Where("category_code = ?", category)
	}
	q.Find(&items)
	response.OK(c, items)
}

func (h Handler) create(c *gin.Context) {
	var item database.ScamCase
	if err := c.ShouldBindJSON(&item); err != nil || item.Title == "" || item.Content == "" {
		response.Fail(c, http.StatusBadRequest, "invalid_case", "title and content are required")
		return
	}
	if err := h.db.Create(&item).Error; err != nil {
		response.Fail(c, http.StatusConflict, "case_create_failed", err.Error())
		return
	}
	response.Created(c, item)
}

func (h Handler) get(c *gin.Context) {
	var item database.ScamCase
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "case_not_found", "case not found")
		return
	}
	response.OK(c, item)
}

func (h Handler) update(c *gin.Context) {
	var item database.ScamCase
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "case_not_found", "case not found")
		return
	}
	if err := c.ShouldBindJSON(&item); err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_case", err.Error())
		return
	}
	h.db.Save(&item)
	response.OK(c, item)
}

func (h Handler) delete(c *gin.Context) {
	h.db.Delete(&database.ScamCase{}, c.Param("id"))
	response.OK(c, gin.H{"deleted": true})
}
