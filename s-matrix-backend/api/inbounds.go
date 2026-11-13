package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"s-matrix/core/ortt"
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
		c.JSON(http.StatusOK, gin.H{"ok": true, "obj": buildInboundResponses(inbounds)})
	}
}

func CreateInboundHandler(db *gorm.DB, manager *singbox.SingboxManager, configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		tag := parseStringAny(body["tag"])
		if tag == "" {
			tag = parseStringAny(body["remark"])
		}
		typ := strings.ToLower(parseStringAny(body["type"]))
		port, _ := parseIntAny(body["port"])
		if tag == "" || typ == "" || port <= 0 || port > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "valid tag, type, and port required"})
			return
		}
		listen := parseStringAny(body["listen"])
		if listen == "" {
			listen = "::"
		}
		enabled := true
		if rawEn, hasEn := body["enabled"]; hasEn {
			if b, ok := parseBoolAny(rawEn); ok {
				enabled = b
			}
		}

		payload := map[string]interface{}{}
		if nested, ok := body["payload"].(map[string]interface{}); ok {
			for k, v := range nested {
				payload[k] = v
			}
		} else if rawStr, ok := body["payload"].(string); ok && strings.TrimSpace(rawStr) != "" {
			_ = json.Unmarshal([]byte(rawStr), &payload)
		}
		reservedTop := map[string]bool{
			"tag": true, "remark": true, "type": true, "listen": true, "port": true, "enabled": true, "payload": true,
		}
		for k, v := range body {
			if !reservedTop[k] {
				if _, exists := payload[k]; !exists {
					payload[k] = v
				}
			}
		}

		var existing models.Inbound
		var excludeID uint
		if err := db.Where("tag = ?", tag).First(&existing).Error; err == nil {
			excludeID = existing.ID
		}
		ensurePayloadOrttIdentity(db, payload, excludeID)
		payloadBytes, _ := json.Marshal(payload)

		var saved models.Inbound
		if excludeID != 0 {
			existing.Type = typ
			existing.Listen = listen
			existing.Port = port
			existing.Enabled = enabled
			existing.Payload = string(payloadBytes)
			if err := db.Save(&existing).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
				return
			}
			saved = existing
		} else {
			saved = models.Inbound{
				Tag:     tag,
				Type:    typ,
				Listen:  listen,
				Port:    port,
				Enabled: enabled,
				Payload: string(payloadBytes),
			}
			if err := db.Create(&saved).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
				return
			}
			if !enabled {
				_ = db.Model(&saved).Update("enabled", false).Error
				saved.Enabled = false
			}
		}

		if err := rebuildFromDB(db, manager, configPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "created, rebuild failed: " + err.Error()})
			return
		}
		_ = db.First(&saved, saved.ID).Error
		host := publicHost(c)
		stdLink := buildLinkFromRecord(saved, host)
		aeroLink, _ := buildAeroLinkFromRecord(saved, host)
		c.JSON(http.StatusOK, gin.H{
			"ok":            true,
			"obj":           buildInboundResponse(saved),
			"standard_link": stdLink,
			"aero_link":     aeroLink,
		})
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
		c.JSON(http.StatusOK, gin.H{"ok": true, "obj": buildInboundResponse(inbound)})
	}
}

func RenameInboundHandler(db *gorm.DB, manager *singbox.SingboxManager, configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid id"})
			return
		}
		var req struct {
			Remark string `json:"remark"`
			Tag    string `json:"tag"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		newTag := req.Remark
		if newTag == "" {
			newTag = req.Tag
		}
		if newTag == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "remark required"})
			return
		}
		var inbound models.Inbound
		if err := db.First(&inbound, id).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "inbound not found"})
			return
		}
		oldTag := inbound.Tag
		inbound.Tag = newTag
		if err := db.Save(&inbound).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if oldTag != newTag {
			db.Model(&models.Outbound{}).Where("tag = ?", oldTag).Update("tag", newTag)
			_ = renameClientMeta(configPath, oldTag, newTag)
		}
		if err := rebuildFromDB(db, manager, configPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "renamed, rebuild failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "obj": buildInboundResponse(inbound)})
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

func InboundLinksHandler(db *gorm.DB) gin.HandlerFunc {
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
		host := publicHost(c)
		stdLink := buildLinkFromRecord(inbound, host)
		aeroLink, _ := buildAeroLinkFromRecord(inbound, host)
		meta, _ := extractOrttMetaFromPayloadJSON(inbound.Payload)
		links := []string{}
		if stdLink != "" {
			links = append(links, stdLink)
		}
		aeroLinks := []string{}
		if aeroLink != "" {
			aeroLinks = append(aeroLinks, aeroLink)
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":            true,
			"links":         links,
			"standard_link": stdLink,
			"aero_link":     aeroLink,
			"aero_links":    aeroLinks,
			"ortt":          meta,
		})
	}
}

// OrttRotateHandler rotates a single node's ortt_key_id and ortt_secret while keeping its
// persistent ortt_id intact, and atomically reloads the live ORTT key ring without restarting.
func OrttRotateHandler(db *gorm.DB, configPath string) gin.HandlerFunc {
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
		payload := map[string]interface{}{}
		if err := json.Unmarshal([]byte(inbound.Payload), &payload); err != nil || payload == nil {
			payload = map[string]interface{}{}
		}

		orttAllocMu.Lock()
		usedKeys, usedIDs, maxKey := collectUsedOrttStateLocked(db, inbound.ID)
		oldKeyInt, _ := parseIntAny(payload["ortt_key_id"])
		var oldKeyID uint16
		if oldKeyInt >= int(minNodeOrttKeyID) && oldKeyInt <= int(maxNodeOrttKeyID) {
			oldKeyID = uint16(oldKeyInt)
		}

		oid := parseStringAny(payload["ortt_id"])
		if !isValidOrttID(oid) || usedIDs[oid] {
			oid = allocateUniqueOrttIDLocked(usedIDs)
		}
		newKeyID := allocateNextOrttKeyIDLocked(usedKeys, maxKey, oldKeyID)
		newSecret := generateOrttSecret()
		port, okPort := parseIntAny(payload["ortt_port"])
		if !okPort || port <= 0 || port > 65535 {
			port = defaultOrttPort()
		}
		enabled := true
		if rawEn, ok := payload["ortt_enabled"]; ok {
			if b, okB := parseBoolAny(rawEn); okB {
				enabled = b
			}
		}
		payload["ortt_id"] = oid
		payload["ortt_key_id"] = int(newKeyID)
		payload["ortt_secret"] = newSecret
		payload["ortt_port"] = port
		payload["ortt_enabled"] = enabled
		orttAllocMu.Unlock()

		raw, err := json.Marshal(payload)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		inbound.Payload = string(raw)
		if err := db.Save(&inbound).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		_ = syncClientMetaFromDB(db, configPath)
		RefreshOrttKeyRing(db)

		c.JSON(http.StatusOK, gin.H{
			"ok":         true,
			"old_key_id": oldKeyID,
			"ortt": OrttMeta{
				OrttID:      oid,
				OrttKeyID:   newKeyID,
				OrttSecret:  newSecret,
				OrttPort:    port,
				OrttEnabled: enabled,
			},
			"obj": buildInboundResponse(inbound),
		})
	}
}

// OrttToggleHandler toggles or explicitly sets a node's ortt_enabled flag and reloads
// the live ORTT key ring immediately without restarting sing-box.
func OrttToggleHandler(db *gorm.DB, configPath string) gin.HandlerFunc {
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
		payload := map[string]interface{}{}
		if err := json.Unmarshal([]byte(inbound.Payload), &payload); err != nil || payload == nil {
			payload = map[string]interface{}{}
		}
		meta, _ := ensurePayloadOrttIdentity(db, payload, inbound.ID)

		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if req.Enabled != nil {
			meta.OrttEnabled = *req.Enabled
		} else {
			meta.OrttEnabled = !meta.OrttEnabled
		}
		payload["ortt_enabled"] = meta.OrttEnabled

		raw, err := json.Marshal(payload)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		inbound.Payload = string(raw)
		if err := db.Save(&inbound).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		_ = syncClientMetaFromDB(db, configPath)
		RefreshOrttKeyRing(db)

		c.JSON(http.StatusOK, gin.H{
			"ok":   true,
			"ortt": meta,
			"obj":  buildInboundResponse(inbound),
		})
	}
}

func rebuildFromDB(db *gorm.DB, manager *singbox.SingboxManager, configPath string) error {
	var allInbounds []models.Inbound
	if err := db.Order("id asc").Find(&allInbounds).Error; err != nil {
		return err
	}
	inbounds := make([]models.Inbound, 0, len(allInbounds))
	for _, ib := range allInbounds {
		if ib.Enabled {
			inbounds = append(inbounds, ib)
		}
	}
	if len(inbounds) == 0 {
		emptyConfig := singbox.Config{Log: &singbox.Log{Level: "info", Timestamp: true}, DNS: &singbox.DNS{Servers: []singbox.DNSServer{}}, Inbounds: []singbox.Inbound{}, Outbounds: []singbox.Outbound{{Type: "direct", Tag: "direct"}, {Type: "block", Tag: "block"}}, Route: singbox.Route{Final: "direct"}}
		// Fresh installs have no nodes yet; the empty config must still
		// carry the warm-pool route when ORTT is enabled, or the startup
		// self-heal rebuilds it on every boot without ever fixing it.
		if ortt.EffectiveEnabled() {
			emptyConfig.Outbounds = append(emptyConfig.Outbounds, singbox.OrttWarmPoolOutbound())
			emptyConfig.Route.Rules = []singbox.RouteRule{singbox.OrttWarmPoolRule()}
		}
		raw, _ := json.MarshalIndent(emptyConfig, "", "  ")
		_ = os.WriteFile(configPath, raw, 0644)
		_ = syncClientMetaFromDB(db, configPath)
		RefreshOrttKeyRing(db)
		if manager == nil {
			return nil
		}
		return manager.Stop()
	}

	nodes := make([]singbox.UINode, 0)
	edges := make([]singbox.UIEdge, 0)

	typeMap := map[string]string{
		"vless": "inbound-reality", "hysteria2": "inbound-hy2", "hysteria": "inbound-hysteria", "vmess": "inbound-vmess",
		"trojan": "inbound-trojan", "shadowsocks": "inbound-ss", "socks": "inbound-socks", "http": "inbound-http",
		"tuic": "inbound-tuic", "naive": "inbound-naive", "shadowtls": "inbound-shadowtls", "anytls": "inbound-anytls",
	}

	for _, ib := range inbounds {
		nodeID := "in-" + strconv.FormatUint(uint64(ib.ID), 36)
		kind := typeMap[ib.Type]
		if kind == "" {
			continue
		}
		data := map[string]interface{}{"kind": kind, "tag": ib.Tag, "port": ib.Port}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(ib.Payload), &payload); err == nil {
			for k, v := range payload {
				data[k] = v
			}
		}
		nodes = append(nodes, singbox.UINode{ID: nodeID, Label: ib.Tag, Data: data})
		directID := "out-direct-" + nodeID
		nodes = append(nodes, singbox.UINode{ID: directID, Label: "direct", Data: map[string]interface{}{"kind": "outbound-direct", "tag": "direct"}})
		edges = append(edges, singbox.UIEdge{ID: "e-" + nodeID + "-" + directID, Source: nodeID, Target: directID})
	}

	ui := singbox.UIData{Nodes: nodes, Edges: edges}
	cfg, err := singbox.CompileAndWrite(ui, configPath)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	// Keep SQLite ports in sync if the compiler had to reallocate any invalid/colliding port.
	compiledPorts := map[string]int{}
	for _, ci := range cfg.Inbounds {
		if ci.Tag != "" && ci.ListenPort > 0 {
			compiledPorts[ci.Tag] = ci.ListenPort
		}
	}
	for _, ib := range inbounds {
		if p, ok := compiledPorts[ib.Tag]; ok && p != ib.Port {
			_ = db.Model(&models.Inbound{}).Where("id = ?", ib.ID).Update("port", p).Error
		}
	}
	_ = syncClientMetaFromDB(db, configPath)
	RefreshOrttKeyRing(db)
	if manager == nil {
		return nil
	}
	return manager.Restart()
}

func renameClientMeta(configPath, oldTag, newTag string) error {
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return nil
	}
	metaPath := configPath + ".client.json"
	if _, err := os.Stat(metaPath); err != nil {
		return nil
	}
	doc := readClientMetaDoc(configPath)
	moved := false
	if entry, ok := doc.Inbounds[oldTag]; ok {
		doc.Inbounds[newTag] = entry
		delete(doc.Inbounds, oldTag)
		moved = true
	}
	if om, ok := doc.Ortt[oldTag]; ok {
		doc.Ortt[newTag] = om
		delete(doc.Ortt, oldTag)
		moved = true
	}
	if !moved {
		return nil
	}
	return writeClientMetaDoc(configPath, doc)
}
