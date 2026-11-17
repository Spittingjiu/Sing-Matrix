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
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"s-matrix/core/ortt"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

// inboundOpMu serializes all inbound mutations and config rebuilds so concurrent
// clicks or overlapping API calls cannot race on SQLite, config.json, or sing-box.
var inboundOpMu sync.Mutex

var orttPreserveKeys = map[string]bool{
	"ortt_id":      true,
	"ortt_key_id":  true,
	"ortt_secret":  true,
	"ortt_port":    true,
	"ortt_enabled": true,
}

func normalizeInboundType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
	case "reality", "inbound-reality", "inbound-vless":
		return "vless"
	case "hy2", "inbound-hy2", "inbound-hysteria2":
		return "hysteria2"
	case "ss", "inbound-ss", "inbound-shadowsocks":
		return "shadowsocks"
	case "inbound-hysteria":
		return "hysteria"
	case "inbound-vmess":
		return "vmess"
	case "inbound-trojan":
		return "trojan"
	case "inbound-socks":
		return "socks"
	case "inbound-http":
		return "http"
	case "inbound-tuic":
		return "tuic"
	case "inbound-naive":
		return "naive"
	case "inbound-shadowtls":
		return "shadowtls"
	case "inbound-anytls":
		return "anytls"
	default:
		return t
	}
}

func extractInboundRequestPayload(body map[string]interface{}) map[string]interface{} {
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
	return payload
}

func mergeInboundPayload(existingType, newType string, oldPayload, incoming map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	if existingType == newType && oldPayload != nil {
		for k, v := range oldPayload {
			merged[k] = v
		}
		for k, v := range incoming {
			if orttPreserveKeys[k] {
				continue
			}
			// Preserve existing cert/key paths when an edit form leaves them blank or omits them.
			if k == "certificate_path" || k == "cert_path" || k == "key_path" {
				if strings.TrimSpace(parseStringAny(v)) == "" {
					continue
				}
			}
			merged[k] = v
		}
		return merged
	}
	for k, v := range incoming {
		if orttPreserveKeys[k] {
			continue
		}
		merged[k] = v
	}
	if oldPayload != nil {
		for k := range orttPreserveKeys {
			if v, ok := oldPayload[k]; ok {
				merged[k] = v
			}
		}
	}
	return merged
}

func prepareProtocolDefaults(typ string, port int, payload map[string]interface{}, oldPayload map[string]interface{}) {
	switch typ {
	case "vless":
		pk := strings.TrimSpace(parseStringAny(payload["private_key"]))
		if pk == "" {
			if genPriv, genPub, err := singbox.GenerateRealityKeypair(); err == nil {
				payload["private_key"] = genPriv
				payload["public_key"] = genPub
			}
		} else {
			oldPK := ""
			if oldPayload != nil {
				oldPK = strings.TrimSpace(parseStringAny(oldPayload["private_key"]))
			}
			if pk != oldPK || strings.TrimSpace(parseStringAny(payload["public_key"])) == "" {
				if derived := singbox.DeriveRealityPublicKey(pk); derived != "" {
					payload["public_key"] = derived
				}
			}
		}
		dest := strings.TrimSpace(parseStringAny(payload["dest"]))
		sni := strings.TrimSpace(parseStringAny(payload["server_name"]))
		if sni == "" {
			sni = strings.TrimSpace(parseStringAny(payload["sni"]))
		}
		hHost, hPort, cleanSNI := singbox.ParseRealityTarget(dest, sni)
		payload["server_name"] = cleanSNI
		if dest == "" {
			payload["dest"] = fmt.Sprintf("%s:%d", hHost, hPort)
		}
	case "hysteria2":
		cert := strings.TrimSpace(parseStringAny(payload["certificate_path"]))
		if cert == "" {
			cert = strings.TrimSpace(parseStringAny(payload["cert_path"]))
		}
		key := strings.TrimSpace(parseStringAny(payload["key_path"]))
		if cert == "" || key == "" {
			sni := strings.TrimSpace(parseStringAny(payload["server_name"]))
			if sni == "" {
				sni = strings.TrimSpace(parseStringAny(payload["sni"]))
			}
			if sni == "" {
				sni = "www.bing.com"
			}
			certName := fmt.Sprintf("hy2-%d", port)
			if cp, kp, _, err := singbox.EnsureSelfSignedPair(resolveCertDir(""), certName, sni, singbox.LocalIPs(), []string{sni}); err == nil {
				payload["certificate_path"] = cp
				payload["key_path"] = kp
				if strings.TrimSpace(parseStringAny(payload["server_name"])) == "" {
					payload["server_name"] = sni
				}
			}
		}
	}
}

func validateCandidateInbounds(db *gorm.DB, override *models.Inbound, extraNew *models.Inbound) error {
	var allInbounds []models.Inbound
	if err := db.Order("id asc").Find(&allInbounds).Error; err != nil {
		return err
	}
	candidates := make([]models.Inbound, 0, len(allInbounds)+1)
	replaced := false
	for _, ib := range allInbounds {
		if override != nil && ib.ID == override.ID {
			replaced = true
			if override.Enabled {
				candidates = append(candidates, *override)
			}
			continue
		}
		if ib.Enabled {
			candidates = append(candidates, ib)
		}
	}
	if override != nil && !replaced && override.Enabled {
		candidates = append(candidates, *override)
	}
	if extraNew != nil && extraNew.Enabled {
		candidates = append(candidates, *extraNew)
	}
	if len(candidates) == 0 {
		return nil
	}
	ui := buildUIDataFromInbounds(candidates)
	cfg, err := singbox.CompileToSingbox(ui)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return singbox.CheckConfig(data)
}

func collectUsedPortsFromDB(db *gorm.DB, excludeID uint) (map[int]bool, map[int]string) {
	used := map[int]bool{}
	owners := map[int]string{}
	var rows []models.Inbound
	if err := db.Find(&rows).Error; err != nil {
		return used, owners
	}
	for _, r := range rows {
		if excludeID != 0 && r.ID == excludeID {
			continue
		}
		if r.Port > 0 {
			used[r.Port] = true
			owners[r.Port] = r.Tag
		}
	}
	return used, owners
}

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
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		tag := strings.TrimSpace(parseStringAny(body["tag"]))
		if tag == "" {
			tag = strings.TrimSpace(parseStringAny(body["remark"]))
		}
		typ := normalizeInboundType(parseStringAny(body["type"]))
		port, _ := parseIntAny(body["port"])
		if tag == "" || typ == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "节点名称和协议类型不能为空"})
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

		var existing models.Inbound
		var excludeID uint
		var oldPayload map[string]interface{}
		if err := db.Where("tag = ?", tag).First(&existing).Error; err == nil {
			excludeID = existing.ID
			_ = json.Unmarshal([]byte(existing.Payload), &oldPayload)
		}

		usedPorts, portOwners := collectUsedPortsFromDB(db, excludeID)
		if port <= 0 {
			port = singbox.PickAvailablePort(0, usedPorts)
			if port == 0 {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "无可用端口可分配"})
				return
			}
		} else {
			if port > 65535 || singbox.ReservedPorts[port] {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("端口 %d 无效或属于系统保留端口", port)})
				return
			}
			if owner, taken := portOwners[port]; taken {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("端口 %d 已被节点 %q 占用", port, owner)})
				return
			}
			if excludeID == 0 || port != existing.Port {
				if !singbox.IsPortAvailable(port) {
					c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("端口 %d 已被本机其他程序占用", port)})
					return
				}
			}
		}

		payload := extractInboundRequestPayload(body)
		if excludeID != 0 {
			payload = mergeInboundPayload(existing.Type, typ, oldPayload, payload)
		}
		prepareProtocolDefaults(typ, port, payload, oldPayload)
		ensurePayloadOrttIdentity(db, payload, excludeID)
		payloadBytes, _ := json.Marshal(payload)

		candidate := models.Inbound{
			ID:      excludeID,
			Tag:     tag,
			Type:    typ,
			Listen:  listen,
			Port:    port,
			Enabled: enabled,
			Payload: string(payloadBytes),
		}
		var overridePtr, extraPtr *models.Inbound
		if excludeID != 0 {
			overridePtr = &candidate
		} else {
			extraPtr = &candidate
		}
		if err := validateCandidateInbounds(db, overridePtr, extraPtr); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}

		var saved models.Inbound
		if excludeID != 0 {
			if err := db.Model(&existing).Updates(map[string]interface{}{
				"type":    typ,
				"listen":  listen,
				"port":    port,
				"enabled": enabled,
				"payload": string(payloadBytes),
			}).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
				return
			}
			_ = db.First(&saved, existing.ID).Error
		} else {
			saved = candidate
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
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "配置已保存，但重启服务失败: " + err.Error()})
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

func UpdateInboundHandler(db *gorm.DB, manager *singbox.SingboxManager, configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid id"})
			return
		}
		var existing models.Inbound
		if err := db.First(&existing, id).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "节点不存在"})
			return
		}

		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}

		newTag := strings.TrimSpace(parseStringAny(body["tag"]))
		if newTag == "" {
			newTag = strings.TrimSpace(parseStringAny(body["remark"]))
		}
		if newTag == "" {
			newTag = existing.Tag
		}
		if newTag != existing.Tag {
			var dup models.Inbound
			if err := db.Where("tag = ? AND id <> ?", newTag, existing.ID).First(&dup).Error; err == nil {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("节点名称 %q 已存在，请换一个名称", newTag)})
				return
			}
		}

		newType := normalizeInboundType(parseStringAny(body["type"]))
		if newType == "" {
			newType = existing.Type
		}

		newPort := existing.Port
		if rawPort, ok := parseIntAny(body["port"]); ok && rawPort > 0 {
			if rawPort > 65535 || singbox.ReservedPorts[rawPort] {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("端口 %d 无效或属于系统保留端口", rawPort)})
				return
			}
			_, portOwners := collectUsedPortsFromDB(db, existing.ID)
			if owner, taken := portOwners[rawPort]; taken {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("端口 %d 已被节点 %q 占用", rawPort, owner)})
				return
			}
			if rawPort != existing.Port && !singbox.IsPortAvailable(rawPort) {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("端口 %d 已被本机其他程序占用", rawPort)})
				return
			}
			newPort = rawPort
		}

		newListen := strings.TrimSpace(parseStringAny(body["listen"]))
		if newListen == "" {
			newListen = existing.Listen
			if newListen == "" {
				newListen = "::"
			}
		}

		newEnabled := existing.Enabled
		if rawEn, hasEn := body["enabled"]; hasEn {
			if b, ok := parseBoolAny(rawEn); ok {
				newEnabled = b
			}
		}

		var oldPayload map[string]interface{}
		_ = json.Unmarshal([]byte(existing.Payload), &oldPayload)
		incomingPayload := extractInboundRequestPayload(body)
		mergedPayload := mergeInboundPayload(existing.Type, newType, oldPayload, incomingPayload)
		prepareProtocolDefaults(newType, newPort, mergedPayload, oldPayload)
		ensurePayloadOrttIdentity(db, mergedPayload, existing.ID)
		payloadBytes, _ := json.Marshal(mergedPayload)

		candidate := existing
		candidate.Tag = newTag
		candidate.Type = newType
		candidate.Listen = newListen
		candidate.Port = newPort
		candidate.Enabled = newEnabled
		candidate.Payload = string(payloadBytes)

		if err := validateCandidateInbounds(db, &candidate, nil); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}

		oldTag := existing.Tag
		if err := db.Model(&existing).Updates(map[string]interface{}{
			"tag":     newTag,
			"type":    newType,
			"listen":  newListen,
			"port":    newPort,
			"enabled": newEnabled,
			"payload": string(payloadBytes),
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if oldTag != newTag {
			db.Model(&models.Outbound{}).Where("tag = ?", oldTag).Update("tag", newTag)
			_ = renameClientMeta(configPath, oldTag, newTag)
		}

		if err := rebuildFromDB(db, manager, configPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "配置已保存，但重启服务失败: " + err.Error()})
			return
		}

		_ = db.First(&existing, existing.ID).Error
		host := publicHost(c)
		stdLink := buildLinkFromRecord(existing, host)
		aeroLink, _ := buildAeroLinkFromRecord(existing, host)
		c.JSON(http.StatusOK, gin.H{
			"ok":            true,
			"obj":           buildInboundResponse(existing),
			"standard_link": stdLink,
			"aero_link":     aeroLink,
		})
	}
}

func ToggleInboundHandler(db *gorm.DB, manager *singbox.SingboxManager, configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

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
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

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
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

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
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

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
		inboundOpMu.Lock()
		defer inboundOpMu.Unlock()

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

func buildUIDataFromInbounds(inbounds []models.Inbound) singbox.UIData {
	nodes := make([]singbox.UINode, 0, len(inbounds)*2)
	edges := make([]singbox.UIEdge, 0, len(inbounds))

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
	return singbox.UIData{Nodes: nodes, Edges: edges}
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

	ui := buildUIDataFromInbounds(inbounds)
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
