package api

import (
	"database/sql"
	"net/http"

	"github.com/Spittingjiu/Sing-Matrix/backend/internal/configgen"
	"github.com/Spittingjiu/Sing-Matrix/backend/internal/models"
	"github.com/Spittingjiu/Sing-Matrix/backend/internal/realtime"
	"github.com/Spittingjiu/Sing-Matrix/backend/internal/singbox"
	"github.com/Spittingjiu/Sing-Matrix/backend/internal/system"
	"github.com/gin-gonic/gin"
)

type Dependencies struct {
	DB              *sql.DB
	SingBoxConfig   string
	SingBoxBin      string
	GeneratedConfig string
}

func NewRouter(dep Dependencies) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	svc := singbox.Service{Bin: dep.SingBoxBin, RuntimeConfig: dep.SingBoxConfig, GeneratedConfig: dep.GeneratedConfig}

	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true, "name": "Sing-Matrix"}) })

	v1 := r.Group("/api/v1")
	v1.GET("/system/status", func(c *gin.Context) { c.JSON(http.StatusOK, system.Status()) })
	v1.GET("/singbox/config", func(c *gin.Context) {
		data, err := svc.ReadConfig()
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	})
	v1.POST("/singbox/compile", func(c *gin.Context) {
		var graph models.Graph
		if err := c.ShouldBindJSON(&graph); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		cfg, err := configgen.CompileGraph(graph)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		data, err := configgen.MarshalPretty(cfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := svc.WriteGeneratedConfig(data); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := svc.Reload(); err != nil {
			c.Header("X-SMatrix-Reload-Warning", err.Error())
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	})
	v1.POST("/singbox/reload", func(c *gin.Context) {
		if err := svc.Reload(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	v1.POST("/inbounds/reality", func(c *gin.Context) {
		keys, err := singbox.GenerateRealityKeys(dep.SingBoxBin)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, keys)
	})
	v1.GET("/traffic/ws", realtime.TrafficWS)

	return r
}
