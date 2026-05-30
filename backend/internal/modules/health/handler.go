package health

import (
	"github.com/antifraud-knowledgehub/antifraud-knowledgehub/backend/pkg/response"
	"github.com/gin-gonic/gin"
)

func Register(r gin.IRoutes) {
	r.GET("/health", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok", "service": "antifraud-knowledgehub"})
	})
}
