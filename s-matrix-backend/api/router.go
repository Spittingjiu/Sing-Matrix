package api

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"gorm.io/gorm"
	"s-matrix/core/singbox"
)

type RouterDeps struct {
	Manager    *singbox.SingboxManager
	ConfigPath string
	LogPath    string
	DB         *gorm.DB
}

func NewRouter(staticFS fs.FS, deps ...RouterDeps) *gin.Engine {
	dep := RouterDeps{ConfigPath: "./config.json", LogPath: "./singbox.log", Manager: singbox.NewSingboxManager("sing-box", "./config.json", "./singbox.log")}
	if len(deps) > 0 {
		dep = deps[0]
	}
	r := gin.Default()
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true, "name": "s-matrix-backend"}) })
	v1 := r.Group("/api/v1")
	v1.POST("/login", LoginHandler)
	v1.GET("/sub/:token", SubscriptionHandler(dep.ConfigPath))
	v1.GET("/discover", DiscoveryHandler(dep.ConfigPath))
	secured := v1.Group("")
	secured.Use(JWTMiddleware())
	secured.GET("/singbox/test-config", func(c *gin.Context) { c.JSON(http.StatusOK, singbox.BuildTestConfig()) })
	secured.GET("/singbox/share-links", ShareLinksHandler(dep.ConfigPath))
	secured.GET("/logs/ws", LogsWSHandler(dep.LogPath))
	secured.GET("/ports/available", AvailablePortHandler)
	secured.POST("/quick/reality", QuickRealityHandler(quickDeps{Manager: dep.Manager, ConfigPath: dep.ConfigPath, DB: dep.DB}))
	secured.POST("/quick/hy2", QuickHY2Handler(quickDeps{Manager: dep.Manager, ConfigPath: dep.ConfigPath, DB: dep.DB}))
	secured.PUT("/inbounds/rename", RenameInboundHandler(quickDeps{Manager: dep.Manager, ConfigPath: dep.ConfigPath}))
	if dep.DB != nil {
		secured.GET("/inbounds", ListInboundsHandler(dep.DB))
		secured.POST("/inbounds/:id/toggle", ToggleInboundHandler(dep.DB, dep.Manager, dep.ConfigPath))
		secured.DELETE("/inbounds/:id", DeleteInboundHandler(dep.DB, dep.Manager, dep.ConfigPath))
		secured.GET("/inbounds/:id/links", InboundLinksHandler(dep.ConfigPath))
	}
	secured.POST("/singbox/compile", func(c *gin.Context) {
		var ui singbox.UIData
		if err := c.ShouldBindJSON(&ui); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		cfg, err := singbox.CompileAndWrite(ui, dep.ConfigPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := dep.Manager.Restart(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "config": cfg})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "running": dep.Manager.Status(), "config": cfg})
	})
	secured.GET("/system/status", func(c *gin.Context) {
		if !dep.Manager.Status() {
			_ = dep.Manager.Start()
		}
		cpuVals, _ := cpu.Percent(200*time.Millisecond, false)
		vm, _ := mem.VirtualMemory()
		cpuPercent := 0.0
		if len(cpuVals) > 0 {
			cpuPercent = cpuVals[0]
		}
		c.JSON(http.StatusOK, gin.H{
			"cpu_percent":       cpuPercent,
			"memory_used":       vm.Used,
			"memory_total":      vm.Total,
			"memory_percent":    vm.UsedPercent,
			"sing_box_running":  dep.Manager.Status(),
			"generated_at_unix": time.Now().Unix(),
		})
	})
	MountStatic(r, staticFS)
	return r
}
