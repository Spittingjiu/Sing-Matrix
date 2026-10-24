package api

import (
	"encoding/json"
	"fmt"
	"log"

	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

// MigrateLegacyInbounds repairs rows saved by older versions that produced
// configs the core now rejects. Today that means Hysteria2 nodes created before
// certificates were mandatory: they make `sing-box check` fail, which previously
// took the whole host offline.
func MigrateLegacyInbounds(db *gorm.DB, certDir string) {
	if db == nil {
		return
	}
	var rows []models.Inbound
	if err := db.Where("type = ?", "hysteria2").Find(&rows).Error; err != nil {
		log.Printf("[migrate] read hysteria2 inbounds: %v", err)
		return
	}
	dir := resolveCertDir(certDir)
	for _, row := range rows {
		payload := map[string]interface{}{}
		if err := json.Unmarshal([]byte(row.Payload), &payload); err == nil {
			if cert, _ := payload["certificate_path"].(string); cert != "" {
				continue
			}
		}
		name := fmt.Sprintf("hy2-%d", row.Port)
		certPath, keyPath, _, err := singbox.EnsureSelfSignedPair(dir, name, "www.bing.com", singbox.LocalIPs(), []string{"www.bing.com"})
		if err != nil {
			log.Printf("[migrate] %s: generate certificate failed: %v", row.Tag, err)
			continue
		}
		payload["certificate_path"] = certPath
		payload["key_path"] = keyPath
		if _, ok := payload["server_name"]; !ok {
			payload["server_name"] = "www.bing.com"
		}
		if _, ok := payload["sni"]; !ok {
			payload["sni"] = "www.bing.com"
		}
		raw, _ := json.Marshal(payload)
		if err := db.Model(&models.Inbound{}).Where("id = ?", row.ID).Update("payload", string(raw)).Error; err != nil {
			log.Printf("[migrate] %s: update failed: %v", row.Tag, err)
			continue
		}
		log.Printf("[migrate] %s: attached self-signed certificate (hysteria2 requires TLS)", row.Tag)
	}
}

// RebuildConfig recompiles the live config from the database. It is used at
// startup to self-heal a file left behind by an older version.
func RebuildConfig(db *gorm.DB, manager *singbox.SingboxManager, configPath string) error {
	return rebuildFromDB(db, manager, configPath)
}
