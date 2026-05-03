package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
)

func NewRouter() *gin.Engine {
	r := gin.Default()
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true, "name": "s-matrix-backend"}) })
	r.GET("/api/v1/singbox/test-config", func(c *gin.Context) { c.JSON(http.StatusOK, singbox.BuildTestConfig()) })
	return r
}
