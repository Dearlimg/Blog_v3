package stat

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func Middleware(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if shouldRecord(c) {
			svc.Record(c.ClientIP())
		}
	}
}

func shouldRecord(c *gin.Context) bool {
	path := c.Request.URL.Path

	if c.Request.Method != "GET" {
		return false
	}

	// Health checks (docker healthcheck hits /health every 30s) must not count as visits
	if path == "/health" {
		return false
	}

	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/view/") || strings.HasPrefix(path, "/static/") {
		return false
	}

	if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") ||
		strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") ||
		strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".ico") {
		return false
	}

	return true
}
