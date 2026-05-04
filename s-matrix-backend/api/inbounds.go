package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

func ListInboundsHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var inbounds []models.Inbound
		if err := db.Order("id desc").Find(&inbounds).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if inbounds == nil {
			inbounds = []models.Inbound{}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "obj": inbounds})
	}
}

func ToggleInboundHandler(db *gorm.DB, manager *singbox.SingboxManager, configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid id"})
			return
		}
		var inbound models.Inbound
		if err := db.First(&inbound, id).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "inbound not found"})
			return
		}
		inbound.Enabled = !inbound.Enabled
		if err := db.Save(&inbound).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if err := rebuildFromDB(db, manager, configPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "toggle saved, rebuild failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "obj": inbound})
	}
}

func DeleteInboundHandler(db *gorm.DB, manager *singbox.SingboxManager, configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid id"})
			return
		}
		var inbound models.Inbound
		if err := db.First(&inbound, id).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "inbound not found"})
			return
		}
		tag := inbound.Tag
		if err := db.Delete(&inbound).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		// Also remove matching outbound
		db.Where("tag = ?", tag).Delete(&models.Outbound{})
		if err := rebuildFromDB(db, manager, configPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "deleted, rebuild failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": tag})
	}
}

func InboundLinksHandler(configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		host := c.Request.Host
		links, err := BuildShareLinks(configPath, host)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "links": links})
	}
}

func rebuildFromDB(db *gorm.DB, manager *singbox.SingboxManager, configPath string) error {
	var inbounds []models.Inbound
	if err := db.Where("enabled = ?", true).Find(&inbounds).Error; err != nil {
		return err
	}
	if len(inbounds) == 0 {
		// Write empty config
		emptyConfig := singbox.Config{Log: &singbox.Log{Level: "info", Timestamp: true}, DNS: &singbox.DNS{Servers: []singbox.DNSServer{}}, Inbounds: []singbox.Inbound{}, Outbounds: []singbox.Outbound{{Type: "direct", Tag: "direct"}, {Type: "block", Tag: "block"}}, Route: singbox.Route{Final: "direct"}}
		raw, _ := json.MarshalIndent(emptyConfig, "", "  ")
		os.WriteFile(configPath, raw, 0644)
		return manager.Stop()
	}

	nodes := make([]singbox.UINode, 0)
	edges := make([]singbox.UIEdge, 0)

	for _, ib := range inbounds {
		nodeID := "in-" + strconv.FormatUint(uint64(ib.ID), 36)
		kind := "inbound-reality"
		pwd := randomHex(16)
		sid := randomHex(8)
		priv := randomHex(32)
		uid := "00000000-0000-0000-0000-000000000000"
		dest := "www.microsoft.com"

		if ib.Type == "hysteria2" {
			kind = "inbound-hy2"
		}
		// Try to extract from payload JSON if present
		nodes = append(nodes, singbox.UINode{
			ID:    nodeID,
			Label: ib.Tag,
			Data: map[string]interface{}{
				"kind":        kind,
				"tag":         ib.Tag,
				"port":        ib.Port,
				"password":    pwd,
				"short_id":    sid,
				"private_key": priv,
				"uuid":        uid,
				"dest":        dest,
			},
		})

		directID := "out-direct-" + nodeID
		nodes = append(nodes, singbox.UINode{
			ID:    directID,
			Label: "direct",
			Data: map[string]interface{}{
				"kind": "outbound-direct",
				"tag":  "direct",
			},
		})
		edges = append(edges, singbox.UIEdge{
			ID:     "e-" + nodeID + "-" + directID,
			Source: nodeID,
			Target: directID,
		})
	}

	ui := singbox.UIData{Nodes: nodes, Edges: edges}
	if _, err := singbox.CompileAndWrite(ui, configPath); err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	return manager.Restart()
}
