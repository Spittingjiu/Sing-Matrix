package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

type RouterDeps struct {
	Manager    *singbox.SingboxManager
	ConfigPath string
	LogPath    string
	DB         *gorm.DB
}

func NewRouter(deps ...RouterDeps) *gin.Engine {
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
	secured.POST("/quick/reality", QuickRealityHandler(quickDeps{Manager: dep.Manager, ConfigPath: dep.ConfigPath, DB: dep.DB}))
	secured.POST("/quick/hy2", QuickHY2Handler(quickDeps{Manager: dep.Manager, ConfigPath: dep.ConfigPath, DB: dep.DB}))
	if dep.DB != nil {
		secured.GET("/inbounds", ListInboundsHandler(dep.DB))
		secured.POST("/inbounds/:id/toggle", ToggleInboundHandler(dep.DB, dep.Manager, dep.ConfigPath))
		secured.DELETE("/inbounds/:id", DeleteInboundHandler(dep.DB, dep.Manager, dep.ConfigPath))
		secured.GET("/inbounds/:id/links", InboundLinksHandler(dep.DB))
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
		// Persist inbounds to DB
		if dep.DB != nil {
			for _, ib := range cfg.Inbounds {
				dep.DB.Where("tag = ?", ib.Tag).Delete(&models.Inbound{})
				payload := map[string]interface{}{}
				if ib.TLS != nil {
					if sn, ok := ib.TLS["server_name"].(string); ok {
						payload["server_name"] = sn
					}
					if r, ok := ib.TLS["reality"].(map[string]interface{}); ok {
						if pk, ok := r["private_key"].(string); ok {
							payload["private_key"] = pk
						}
						if sids, ok := r["short_id"].([]string); ok && len(sids) > 0 {
							payload["short_id"] = sids[0]
						}
					}
				}
				if ib.Transport != nil {
					if n, ok := ib.Transport["type"].(string); ok {
						payload["network"] = n
					}
					if p, ok := ib.Transport["path"].(string); ok {
						payload["path"] = p
					}
					if h, ok := ib.Transport["headers"].(map[string]interface{}); ok {
						if host, ok := h["Host"].(string); ok {
							payload["host"] = host
						}
					}
				}
				if ib.TLS != nil {
					if _, ok := ib.TLS["enabled"]; ok && ib.TLS["reality"] == nil {
						payload["security"] = "tls"
					}
				}
				for _, u := range ib.Users {
					if u.UUID != "" {
						payload["uuid"] = u.UUID
					}
					if u.Password != "" {
						payload["password"] = u.Password
					}
				}
				if ib.Method != "" {
					payload["method"] = ib.Method
				}
				if ib.Password != "" && payload["password"] == nil {
					payload["password"] = ib.Password
				}
				payloadBytes, _ := json.Marshal(payload)
				dep.DB.Create(&models.Inbound{
					Tag:     ib.Tag,
					Type:    ib.Type,
					Port:    ib.ListenPort,
					Enabled: true,
					Payload: string(payloadBytes),
				})
			}
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
		sv := "unknown"
		if out, err := exec.Command("sing-box", "version").CombinedOutput(); err == nil {
			if m := regexp.MustCompile(`version\s+(\S+)`).FindStringSubmatch(string(out)); len(m) > 1 {
				sv = m[1]
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"cpu_percent":       cpuPercent,
			"memory_used":       vm.Used,
			"memory_total":      vm.Total,
			"memory_percent":    vm.UsedPercent,
			"sing_box_running":  dep.Manager.Status(),
			"singbox_version":   sv,
			"generated_at_unix": time.Now().Unix(),
		})
	})
	secured.POST("/system/change-password", ChangePasswordHandler)
	secured.GET("/system/gen-reality-keypair", GenRealityKeypairHandler)
	MountStatic(r)
	return r
}
