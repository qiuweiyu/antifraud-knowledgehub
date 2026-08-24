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
	r.GET("/categories/:id", h.get)
}

func (h Handler) list(c *gin.Context) {
	var items []database.Category
	h.db.Order("id asc").Find(&items)
	response.OK(c, items)
}

func (h Handler) get(c *gin.Context) {
	var item database.Category
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "category_not_found", "category not found")
		return
	}
	response.OK(c, item)
}
