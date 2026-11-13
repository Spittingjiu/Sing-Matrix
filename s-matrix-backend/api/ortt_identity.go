package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"
	"s-matrix/core/ortt"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

const (
	minNodeOrttKeyID uint16 = 2
	maxNodeOrttKeyID uint16 = 65535
)

var (
	orttAllocMu   sync.Mutex
	orttLastKeyID uint16 = 1
)

// OrttMeta holds per-node ORTT v2 control-plane identity and credentials.
type OrttMeta struct {
	OrttID      string `json:"ortt_id"`
	OrttKeyID   uint16 `json:"ortt_key_id"`
	OrttSecret  string `json:"ortt_secret,omitempty"`
	OrttPort    int    `json:"ortt_port"`
	OrttEnabled bool   `json:"ortt_enabled"`
}

// InboundResponse enriches models.Inbound with top-level ORTT identity and live telemetry.
type InboundResponse struct {
	models.Inbound
	OrttID      string              `json:"ortt_id,omitempty"`
	OrttKeyID   uint16              `json:"ortt_key_id,omitempty"`
	OrttPort    int                 `json:"ortt_port,omitempty"`
	OrttEnabled bool                `json:"ortt_enabled"`
	OrttStats   *ortt.NodeOrttStats `json:"ortt_stats,omitempty"`
}

type clientMetaDocument struct {
	Inbounds map[string]map[string]string `json:"inbounds"`
	Ortt     map[string]OrttMeta          `json:"ortt,omitempty"`
}

func defaultOrttPort() int {
	return ortt.ResolveControlPort()
}

func generateOrttID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ortt_000000000000"
	}
	return "ortt_" + hex.EncodeToString(b[:])
}

// generateOrttSecret returns a 32-char URL-safe random secret whose raw UTF-8 bytes
// serve directly as the HMAC-SHA256 key.
func generateOrttSecret() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ortt_fallback_secret_32_bytes_00"
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func isValidOrttID(id string) bool {
	if !strings.HasPrefix(id, "ortt_") || len(id) != 17 {
		return false
	}
	for _, ch := range id[5:] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func parseIntAny(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case uint16:
		return int(x), true
	case uint64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return 0, false
		}
		n, err := strconv.Atoi(x)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseBoolAny(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		x = strings.TrimSpace(strings.ToLower(x))
		if x == "" {
			return false, false
		}
		if x == "1" || x == "true" || x == "yes" || x == "on" {
			return true, true
		}
		if x == "0" || x == "false" || x == "no" || x == "off" {
			return false, true
		}
		return false, false
	case float64:
		return x != 0, true
	case int:
		return x != 0, true
	default:
		return false, false
	}
}

func parseStringAny(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func collectUsedOrttStateLocked(db *gorm.DB, excludeRowID uint) (map[uint16]bool, map[string]bool, uint16) {
	usedKeys := make(map[uint16]bool)
	usedIDs := make(map[string]bool)
	var maxKey uint16 = 1
	if db == nil {
		return usedKeys, usedIDs, maxKey
	}
	var rows []models.Inbound
	if err := db.Find(&rows).Error; err != nil {
		return usedKeys, usedIDs, maxKey
	}
	for _, r := range rows {
		if excludeRowID != 0 && r.ID == excludeRowID {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(r.Payload), &payload); err != nil || payload == nil {
			continue
		}
		if kid, ok := parseIntAny(payload["ortt_key_id"]); ok && kid >= int(minNodeOrttKeyID) && kid <= int(maxNodeOrttKeyID) {
			uk := uint16(kid)
			usedKeys[uk] = true
			if uk > maxKey {
				maxKey = uk
			}
		}
		if oid := parseStringAny(payload["ortt_id"]); oid != "" {
			usedIDs[oid] = true
		}
	}
	return usedKeys, usedIDs, maxKey
}

func allocateNextOrttKeyIDLocked(usedKeys map[uint16]bool, maxKey uint16, disallow ...uint16) uint16 {
	blocked := make(map[uint16]bool, len(usedKeys)+len(disallow))
	for k, v := range usedKeys {
		if v {
			blocked[k] = true
		}
	}
	for _, d := range disallow {
		if d >= minNodeOrttKeyID {
			blocked[d] = true
		}
	}
	start := orttLastKeyID
	if maxKey > start {
		start = maxKey
	}
	if start < minNodeOrttKeyID-1 {
		start = minNodeOrttKeyID - 1
	}
	span := uint32(maxNodeOrttKeyID) - uint32(minNodeOrttKeyID) + 1
	firstCandidate := uint32(start) + 1
	for offset := uint32(0); offset < span; offset++ {
		cand := uint16(uint32(minNodeOrttKeyID) + (firstCandidate-uint32(minNodeOrttKeyID)+offset)%span)
		if !blocked[cand] {
			orttLastKeyID = cand
			if usedKeys != nil {
				usedKeys[cand] = true
			}
			return cand
		}
	}
	return minNodeOrttKeyID
}

func allocateUniqueOrttIDLocked(usedIDs map[string]bool) string {
	for i := 0; i < 16; i++ {
		cand := generateOrttID()
		if usedIDs == nil || !usedIDs[cand] {
			if usedIDs != nil {
				usedIDs[cand] = true
			}
			return cand
		}
	}
	return generateOrttID()
}

// ensurePayloadOrttIdentity guarantees that payload carries valid, unique ORTT v2
// identity fields (ortt_id, ortt_key_id in 2..65535, ortt_secret, ortt_port, ortt_enabled).
func ensurePayloadOrttIdentity(db *gorm.DB, payload map[string]interface{}, excludeRowID uint) (OrttMeta, bool) {
	orttAllocMu.Lock()
	defer orttAllocMu.Unlock()
	usedKeys, usedIDs, maxKey := collectUsedOrttStateLocked(db, excludeRowID)
	return ensurePayloadOrttIdentityLocked(payload, usedKeys, usedIDs, &maxKey)
}

func ensurePayloadOrttIdentityLocked(payload map[string]interface{}, usedKeys map[uint16]bool, usedIDs map[string]bool, maxKey *uint16) (OrttMeta, bool) {
	if payload == nil {
		return OrttMeta{}, false
	}
	changed := false

	oid := parseStringAny(payload["ortt_id"])
	if !isValidOrttID(oid) || (usedIDs != nil && usedIDs[oid]) {
		oid = allocateUniqueOrttIDLocked(usedIDs)
		payload["ortt_id"] = oid
		changed = true
	} else if usedIDs != nil {
		usedIDs[oid] = true
	}

	var keyID uint16
	if kInt, ok := parseIntAny(payload["ortt_key_id"]); ok && kInt >= int(minNodeOrttKeyID) && kInt <= int(maxNodeOrttKeyID) {
		cand := uint16(kInt)
		if usedKeys == nil || !usedKeys[cand] {
			keyID = cand
			if usedKeys != nil {
				usedKeys[cand] = true
			}
			if maxKey != nil && cand > *maxKey {
				*maxKey = cand
			}
		}
	}
	if keyID < minNodeOrttKeyID {
		curMax := uint16(1)
		if maxKey != nil {
			curMax = *maxKey
		}
		keyID = allocateNextOrttKeyIDLocked(usedKeys, curMax)
		if maxKey != nil && keyID > *maxKey {
			*maxKey = keyID
		}
		payload["ortt_key_id"] = int(keyID)
		changed = true
	}

	secret := parseStringAny(payload["ortt_secret"])
	if len(secret) < 16 {
		secret = generateOrttSecret()
		payload["ortt_secret"] = secret
		changed = true
	}

	port, okPort := parseIntAny(payload["ortt_port"])
	if !okPort || port <= 0 || port > 65535 {
		port = defaultOrttPort()
		payload["ortt_port"] = port
		changed = true
	}

	enabled, okEnabled := parseBoolAny(payload["ortt_enabled"])
	if !okEnabled {
		enabled = true
		payload["ortt_enabled"] = true
		changed = true
	} else if _, isBool := payload["ortt_enabled"].(bool); !isBool {
		payload["ortt_enabled"] = enabled
		changed = true
	}

	return OrttMeta{
		OrttID:      oid,
		OrttKeyID:   keyID,
		OrttSecret:  secret,
		OrttPort:    port,
		OrttEnabled: enabled,
	}, changed
}

func extractOrttMetaFromMap(m map[string]interface{}) (OrttMeta, bool) {
	if m == nil {
		return OrttMeta{}, false
	}
	oid := parseStringAny(m["ortt_id"])
	kidInt, okKid := parseIntAny(m["ortt_key_id"])
	secret := parseStringAny(m["ortt_secret"])
	port, okPort := parseIntAny(m["ortt_port"])
	if !okPort || port <= 0 || port > 65535 {
		port = defaultOrttPort()
	}
	enabled := true
	if rawEn, exists := m["ortt_enabled"]; exists {
		if b, ok := parseBoolAny(rawEn); ok {
			enabled = b
		}
	}
	if oid == "" && !okKid && secret == "" {
		return OrttMeta{}, false
	}
	var kid uint16
	if okKid && kidInt > 0 && kidInt <= 65535 {
		kid = uint16(kidInt)
	}
	return OrttMeta{
		OrttID:      oid,
		OrttKeyID:   kid,
		OrttSecret:  secret,
		OrttPort:    port,
		OrttEnabled: enabled,
	}, true
}

func extractOrttMetaFromPayloadJSON(raw string) (OrttMeta, bool) {
	if strings.TrimSpace(raw) == "" {
		return OrttMeta{}, false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return OrttMeta{}, false
	}
	return extractOrttMetaFromMap(payload)
}

func readClientMetaDoc(configPath string) *clientMetaDocument {
	doc := &clientMetaDocument{
		Inbounds: make(map[string]map[string]string),
		Ortt:     make(map[string]OrttMeta),
	}
	if configPath == "" {
		return doc
	}
	data, err := os.ReadFile(configPath + ".client.json")
	if err != nil {
		return doc
	}
	var parsed clientMetaDocument
	if err := json.Unmarshal(data, &parsed); err != nil {
		return doc
	}
	if parsed.Inbounds != nil {
		doc.Inbounds = parsed.Inbounds
	}
	if parsed.Ortt != nil {
		doc.Ortt = parsed.Ortt
	}
	return doc
}

func writeClientMetaDoc(configPath string, doc *clientMetaDocument) error {
	if configPath == "" || doc == nil {
		return nil
	}
	if doc.Inbounds == nil {
		doc.Inbounds = make(map[string]map[string]string)
	}
	if len(doc.Ortt) == 0 {
		doc.Ortt = nil
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath+".client.json", raw, 0644)
}

func extractOrttMetaFromClientDoc(doc *clientMetaDocument, tag string) (OrttMeta, bool) {
	if doc == nil || tag == "" {
		return OrttMeta{}, false
	}
	if m, ok := doc.Ortt[tag]; ok && (m.OrttID != "" || m.OrttSecret != "" || m.OrttKeyID > 0) {
		if m.OrttPort <= 0 || m.OrttPort > 65535 {
			m.OrttPort = defaultOrttPort()
		}
		return m, true
	}
	if sMap, ok := doc.Inbounds[tag]; ok && len(sMap) > 0 {
		anyMap := make(map[string]interface{}, len(sMap))
		for k, v := range sMap {
			anyMap[k] = v
		}
		return extractOrttMetaFromMap(anyMap)
	}
	return OrttMeta{}, false
}

func applyOrttMetaToStringMap(dst map[string]string, meta OrttMeta) {
	if dst == nil {
		return
	}
	if meta.OrttID != "" {
		dst["ortt_id"] = meta.OrttID
	}
	if meta.OrttKeyID > 0 {
		dst["ortt_key_id"] = strconv.Itoa(int(meta.OrttKeyID))
	}
	if meta.OrttSecret != "" {
		dst["ortt_secret"] = meta.OrttSecret
	}
	if meta.OrttPort > 0 {
		dst["ortt_port"] = strconv.Itoa(meta.OrttPort)
	}
	dst["ortt_enabled"] = strconv.FormatBool(meta.OrttEnabled)
}

// syncClientMetaFromDB reconciles <configPath>.client.json with all stored DB inbounds,
// persisting per-node ORTT metadata alongside Reality/TLS client parameters.
func syncClientMetaFromDB(db *gorm.DB, configPath string) error {
	if db == nil || configPath == "" {
		return nil
	}
	var rows []models.Inbound
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		return err
	}
	_, statErr := os.Stat(configPath + ".client.json")
	if len(rows) == 0 && statErr != nil {
		return nil
	}

	existing := readClientMetaDoc(configPath)
	next := &clientMetaDocument{
		Inbounds: make(map[string]map[string]string, len(rows)),
		Ortt:     make(map[string]OrttMeta, len(rows)),
	}
	metaKeys := []string{
		"public_key", "short_id", "sni", "server_name", "host", "path",
		"vmess_security", "security", "network", "service_name", "masquerade",
	}
	for _, r := range rows {
		entry := make(map[string]string)
		if prev, ok := existing.Inbounds[r.Tag]; ok {
			for k, v := range prev {
				entry[k] = v
			}
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(r.Payload), &payload); err == nil && payload != nil {
			for _, k := range metaKeys {
				if v, exists := payload[k]; exists && v != nil {
					if s, ok := v.(string); ok && s != "" {
						entry[k] = s
					} else if b, ok := v.(bool); ok {
						entry[k] = strconv.FormatBool(b)
					} else if n, ok := parseIntAny(v); ok {
						entry[k] = strconv.Itoa(n)
					}
				}
			}
			if entry["public_key"] == "" {
				if pk := parseStringAny(payload["private_key"]); pk != "" {
					if derived := singbox.DeriveRealityPublicKey(pk); derived != "" {
						entry["public_key"] = derived
					}
				}
			}
			if meta, ok := extractOrttMetaFromMap(payload); ok {
				applyOrttMetaToStringMap(entry, meta)
				next.Ortt[r.Tag] = meta
			}
		}
		if len(entry) > 0 {
			next.Inbounds[r.Tag] = entry
		}
	}
	return writeClientMetaDoc(configPath, next)
}

// RefreshOrttKeyRing reloads KeyID (>= 2) per-node credentials from DB into the live
// ORTT server without restarting Sing-Matrix or interrupting existing traffic.
func RefreshOrttKeyRing(db *gorm.DB) {
	gs := ortt.GlobalServer()
	if gs == nil || db == nil {
		return
	}
	var rows []models.Inbound
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		return
	}
	entries := make([]ortt.NodeKeyEntry, 0, len(rows))
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		meta, ok := extractOrttMetaFromPayloadJSON(r.Payload)
		if !ok || !meta.OrttEnabled || meta.OrttKeyID < minNodeOrttKeyID || meta.OrttSecret == "" {
			continue
		}
		entries = append(entries, ortt.NodeKeyEntry{
			OrttID:     meta.OrttID,
			KeyID:      meta.OrttKeyID,
			InboundTag: r.Tag,
			Secret:     []byte(meta.OrttSecret),
		})
	}
	gs.ReplaceNodeKeys(entries)
}

func buildInboundResponse(inbound models.Inbound) InboundResponse {
	resp := InboundResponse{
		Inbound: inbound,
	}
	if meta, ok := extractOrttMetaFromPayloadJSON(inbound.Payload); ok {
		resp.OrttID = meta.OrttID
		resp.OrttKeyID = meta.OrttKeyID
		resp.OrttPort = meta.OrttPort
		resp.OrttEnabled = meta.OrttEnabled
		if meta.OrttID != "" {
			if st, ok := ortt.GetNodeStats(meta.OrttID); ok {
				cp := st
				resp.OrttStats = &cp
			} else {
				resp.OrttStats = &ortt.NodeOrttStats{
					OrttID:     meta.OrttID,
					KeyID:      meta.OrttKeyID,
					InboundTag: inbound.Tag,
				}
			}
		}
	}
	return resp
}

func buildInboundResponses(inbounds []models.Inbound) []InboundResponse {
	out := make([]InboundResponse, 0, len(inbounds))
	for _, ib := range inbounds {
		out = append(out, buildInboundResponse(ib))
	}
	return out
}
