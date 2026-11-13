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
// configs the core now rejects (missing Hysteria2 certificates and missing
// per-node ORTT v2 identity credentials).
func MigrateLegacyInbounds(db *gorm.DB, certDir string) {
	if db == nil {
		return
	}
	dir := resolveCertDir(certDir)
	var hyRows []models.Inbound
	if err := db.Where("type = ?", "hysteria2").Find(&hyRows).Error; err != nil {
		log.Printf("[migrate] read hysteria2 inbounds: %v", err)
	} else {
		for _, row := range hyRows {
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

	var allRows []models.Inbound
	if err := db.Order("id asc").Find(&allRows).Error; err != nil {
		log.Printf("[migrate] read all inbounds for ORTT backfill: %v", err)
		return
	}
	if len(allRows) == 0 {
		return
	}

	orttAllocMu.Lock()
	defer orttAllocMu.Unlock()

	seenKeys := make(map[uint16]bool, len(allRows))
	seenIDs := make(map[string]bool, len(allRows))
	var maxKey uint16 = 1
	parsedPayloads := make([]map[string]interface{}, len(allRows))
	// Pass 1: preserve already-assigned valid ORTT IDs and KeyIDs.
	for i, row := range allRows {
		p := map[string]interface{}{}
		if err := json.Unmarshal([]byte(row.Payload), &p); err != nil || p == nil {
			p = map[string]interface{}{}
		}
		parsedPayloads[i] = p
		if oid := parseStringAny(p["ortt_id"]); isValidOrttID(oid) && !seenIDs[oid] {
			seenIDs[oid] = true
		}
		if kid, ok := parseIntAny(p["ortt_key_id"]); ok && kid >= int(minNodeOrttKeyID) && kid <= int(maxNodeOrttKeyID) {
			uk := uint16(kid)
			if !seenKeys[uk] {
				seenKeys[uk] = true
				if uk > maxKey {
					maxKey = uk
				}
			}
		}
	}
	if maxKey > orttLastKeyID {
		orttLastKeyID = maxKey
	}

	// Pass 2: claim preserved identities cleanly and backfill missing/conflicting ones.
	claimIDs := make(map[string]bool, len(allRows))
	claimKeys := make(map[uint16]bool, len(allRows))
	for i, row := range allRows {
		p := parsedPayloads[i]
		dirty := false

		oid := parseStringAny(p["ortt_id"])
		if isValidOrttID(oid) && !claimIDs[oid] {
			claimIDs[oid] = true
		} else {
			oid = allocateUniqueOrttIDLocked(seenIDs)
			claimIDs[oid] = true
			p["ortt_id"] = oid
			dirty = true
		}

		var keyID uint16
		if kInt, ok := parseIntAny(p["ortt_key_id"]); ok && kInt >= int(minNodeOrttKeyID) && kInt <= int(maxNodeOrttKeyID) {
			uk := uint16(kInt)
			if !claimKeys[uk] {
				keyID = uk
				claimKeys[uk] = true
			}
		}
		if keyID < minNodeOrttKeyID {
			keyID = allocateNextOrttKeyIDLocked(seenKeys, maxKey)
			claimKeys[keyID] = true
			if keyID > maxKey {
				maxKey = keyID
			}
			p["ortt_key_id"] = int(keyID)
			dirty = true
		}

		if len(parseStringAny(p["ortt_secret"])) < 16 {
			p["ortt_secret"] = generateOrttSecret()
			dirty = true
		}
		if port, ok := parseIntAny(p["ortt_port"]); !ok || port <= 0 || port > 65535 {
			p["ortt_port"] = defaultOrttPort()
			dirty = true
		}
		if en, ok := parseBoolAny(p["ortt_enabled"]); !ok {
			p["ortt_enabled"] = true
			dirty = true
		} else if _, isBool := p["ortt_enabled"].(bool); !isBool {
			p["ortt_enabled"] = en
			dirty = true
		}

		if !dirty {
			continue
		}
		raw, err := json.Marshal(p)
		if err != nil {
			log.Printf("[migrate] %s: marshal ORTT backfill failed: %v", row.Tag, err)
			continue
		}
		if err := db.Model(&models.Inbound{}).Where("id = ?", row.ID).Update("payload", string(raw)).Error; err != nil {
			log.Printf("[migrate] %s: save ORTT backfill failed: %v", row.Tag, err)
			continue
		}
		log.Printf("[migrate] %s: backfilled ORTT identity (ortt_id=%s, key_id=%d)", row.Tag, oid, keyID)
	}
}

// MigrateLegacyInboundsWithConfig runs MigrateLegacyInbounds, syncs .client.json,
// and reloads the active ORTT key ring.
func MigrateLegacyInboundsWithConfig(db *gorm.DB, certDir, configPath string) {
	MigrateLegacyInbounds(db, certDir)
	if err := syncClientMetaFromDB(db, configPath); err != nil {
		log.Printf("[migrate] sync client meta failed: %v", err)
	}
	RefreshOrttKeyRing(db)
}

// RebuildConfig recompiles the live config from the database. It is used at
// startup to self-heal a file left behind by an older version.
func RebuildConfig(db *gorm.DB, manager *singbox.SingboxManager, configPath string) error {
	return rebuildFromDB(db, manager, configPath)
}
