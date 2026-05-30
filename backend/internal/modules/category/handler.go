package category

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
	r.GET("/categories", h.list)
	r.POST("/categories", h.create)
	r.GET("/categories/:id", h.get)
	r.PUT("/categories/:id", h.update)
	r.DELETE("/categories/:id", h.delete)
}

func (h Handler) list(c *gin.Context) {
	var items []database.Category
	h.db.Order("id asc").Find(&items)
	response.OK(c, items)
}

func (h Handler) create(c *gin.Context) {
	var item database.Category
	if err := c.ShouldBindJSON(&item); err != nil || item.Code == "" || item.Name == "" {
		response.Fail(c, http.StatusBadRequest, "invalid_category", "code and name are required")
		return
	}
	if err := h.db.Create(&item).Error; err != nil {
		response.Fail(c, http.StatusConflict, "category_create_failed", err.Error())
		return
	}
	response.Created(c, item)
}

func (h Handler) get(c *gin.Context) {
	var item database.Category
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "category_not_found", "category not found")
		return
	}
	response.OK(c, item)
}

func (h Handler) update(c *gin.Context) {
	var item database.Category
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "category_not_found", "category not found")
		return
	}
	if err := c.ShouldBindJSON(&item); err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_category", err.Error())
		return
	}
	h.db.Save(&item)
	response.OK(c, item)
}

func (h Handler) delete(c *gin.Context) {
	h.db.Delete(&database.Category{}, c.Param("id"))
	response.OK(c, gin.H{"deleted": true})
}
