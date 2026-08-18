package rule

import (
	"fmt"
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
	r.POST("/rules", h.create)
	r.GET("/rules/:id", h.get)
	r.PUT("/rules/:id", h.update)
	r.PATCH("/rules/:id/toggle", h.toggle)
	r.DELETE("/rules/:id", h.delete)
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

func (h Handler) create(c *gin.Context) {
	var draft DraftRequest
	if err := c.ShouldBindJSON(&draft); err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_rule", err.Error())
		return
	}
	result := ValidateDraft(h.db, draft)
	if !result.Valid {
		response.Fail(c, http.StatusBadRequest, "invalid_rule", result.Errors[0].Message)
		return
	}
	item := draft.riskRule()
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		// RiskRule.Enabled retains a historical GORM default:true tag. When the
		// client explicitly sends false, GORM can replace that zero value during
		// Create. Restore the validated explicit intent inside the same transaction
		// so no committed row or response can observe the wrong enabled state.
		if draft.Enabled != nil && item.Enabled != *draft.Enabled {
			updated := tx.Model(&database.RiskRule{}).
				Where("id = ?", item.ID).
				UpdateColumn("enabled", *draft.Enabled)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return fmt.Errorf("preserve direct rule enabled state: expected 1 row, updated %d", updated.RowsAffected)
			}
			item.Enabled = *draft.Enabled
		}
		return nil
	}); err != nil {
		response.Fail(c, http.StatusConflict, "rule_create_failed", err.Error())
		return
	}
	response.Created(c, item)
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

func (h Handler) update(c *gin.Context) {
	var item database.RiskRule
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	if err := c.ShouldBindJSON(&item); err != nil {
		response.Fail(c, http.StatusBadRequest, "invalid_rule", err.Error())
		return
	}
	h.db.Save(&item)
	response.OK(c, item)
}

func (h Handler) toggle(c *gin.Context) {
	var item database.RiskRule
	if err := h.db.First(&item, c.Param("id")).Error; err != nil {
		response.Fail(c, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	item.Enabled = !item.Enabled
	h.db.Save(&item)
	response.OK(c, item)
}

func (h Handler) delete(c *gin.Context) {
	h.db.Delete(&database.RiskRule{}, c.Param("id"))
	response.OK(c, gin.H{"deleted": true})
}
