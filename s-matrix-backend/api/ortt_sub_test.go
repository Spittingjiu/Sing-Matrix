package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"s-matrix/core/ortt"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

func setupOrttAPITestEnv(t *testing.T) (*gin.Engine, *gorm.DB, string, string, *ortt.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	logPath := filepath.Join(dir, "singbox.log")

	oldCreds := panelCredsFile
	panelCredsFile = filepath.Join(dir, "panel_creds.json")
	t.Cleanup(func() { panelCredsFile = oldCreds })

	saveCreds(panelCreds{
		Username:          "admin",
		PasswordHash:      "$2a$10$xyz",
		SubscriptionToken: "sub-tok-ortt-test",
	})
	jwtTok, err := buildLoginToken("admin")
	if err != nil {
		t.Fatalf("buildLoginToken: %v", err)
	}

	db, err := openTestDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	orttCfg := ortt.DefaultConfig()
	orttCfg.SecretKey = []byte("global-test-secret-key-for-v2!!!")
	orttCfg.KeyRing[1] = orttCfg.SecretKey
	srv := ortt.NewServer(orttCfg)
	ortt.SetGlobalServer(srv)
	t.Cleanup(func() { ortt.SetGlobalServer(nil) })

	t.Setenv("ORTT_PORT", "41234")
	manager := singbox.NewSingboxManager(testStubManagerBin(t, dir), configPath, logPath)
	r := NewRouter(RouterDeps{
		Manager:    manager,
		ConfigPath: configPath,
		LogPath:    logPath,
		DB:         db,
		CertDir:    dir,
		DataDir:    dir,
	})
	return r, db, configPath, jwtTok, srv
}

func doAuthJSON(t *testing.T, r *gin.Engine, method, path, jwtTok string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "203.0.113.10:8080"
	if jwtTok != "" {
		req.Header.Set("Authorization", "Bearer "+jwtTok)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertValidOrttFields(t *testing.T, oid string, keyID uint16, secret string, port int, enabled bool) {
	t.Helper()
	if !isValidOrttID(oid) {
		t.Fatalf("invalid ortt_id: %q", oid)
	}
	if keyID < 2 {
		t.Fatalf("expected ortt_key_id >= 2, got %d", keyID)
	}
	if len(secret) != 32 {
		t.Fatalf("expected 32-char ortt_secret, got len=%d (%q)", len(secret), secret)
	}
	if port != 41234 {
		t.Fatalf("expected ortt_port 41234, got %d", port)
	}
	if !enabled {
		t.Fatalf("expected ortt_enabled=true")
	}
}

func decodeAeroLink(t *testing.T, link string) (AeroV1Payload, string) {
	t.Helper()
	if !strings.HasPrefix(link, "aero://v1/") {
		t.Fatalf("expected aero://v1/ prefix, got: %s", link)
	}
	rest := strings.TrimPrefix(link, "aero://v1/")
	parts := strings.SplitN(rest, "#", 2)
	if len(parts) != 2 || parts[0] == "" {
		t.Fatalf("malformed aero link: %s", link)
	}
	if strings.Contains(parts[0], "=") || strings.ContainsAny(parts[0], "+/") {
		t.Fatalf("expected Base64URL without padding, got: %s", parts[0])
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("base64 RawURLEncoding decode failed: %v", err)
	}
	var payload AeroV1Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal aero JSON: %v (raw=%s)", err, raw)
	}
	name, err := url.PathUnescape(parts[1])
	if err != nil {
		t.Fatalf("fragment PathUnescape failed: %v", err)
	}
	return payload, name
}

func TestPerNodeOrttIdentityQuickstartAndManualLifecycle(t *testing.T) {
	r, db, configPath, jwtTok, srv := setupOrttAPITestEnv(t)

	// 1. Create REALITY, HY2, AnyTLS, VMess via quickstart endpoints.
	for _, ep := range []string{"/api/v1/quick/reality", "/api/v1/quick/hy2", "/api/v1/quick/anytls", "/api/v1/quick/vmess"} {
		w := doAuthJSON(t, r, http.MethodPost, ep, jwtTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s failed (%d): %s", ep, w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s: %v", ep, err)
		}
		aeroLinks, _ := resp["aero_links"].([]interface{})
		stdLinks, _ := resp["links"].([]interface{})
		if len(aeroLinks) == 0 || len(stdLinks) == 0 {
			t.Fatalf("%s expected aero_links and links, got: %s", ep, w.Body.String())
		}
	}

	// 2. Manual create via POST /api/v1/inbounds (Trojan).
	wManual := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds", jwtTok, map[string]interface{}{
		"tag":      "Manual Trojan Node",
		"type":     "trojan",
		"port":     44555,
		"password": "trojan-secret-password",
		"sni":      "example.net",
	})
	if wManual.Code != http.StatusOK {
		t.Fatalf("POST /inbounds failed (%d): %s", wManual.Code, wManual.Body.String())
	}

	// 3. Verify all 5 nodes in SQLite have unique KeyIDs >= 2 and valid ORTT identities.
	var rows []models.Inbound
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("query inbounds: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 inbounds, got %d", len(rows))
	}
	seenIDs := map[string]bool{}
	seenKeys := map[uint16]bool{}
	for _, row := range rows {
		meta, ok := extractOrttMetaFromPayloadJSON(row.Payload)
		if !ok {
			t.Fatalf("row %s missing ORTT metadata: %s", row.Tag, row.Payload)
		}
		assertValidOrttFields(t, meta.OrttID, meta.OrttKeyID, meta.OrttSecret, meta.OrttPort, meta.OrttEnabled)
		if seenIDs[meta.OrttID] {
			t.Fatalf("duplicate ortt_id %s", meta.OrttID)
		}
		if seenKeys[meta.OrttKeyID] {
			t.Fatalf("duplicate ortt_key_id %d", meta.OrttKeyID)
		}
		seenIDs[meta.OrttID] = true
		seenKeys[meta.OrttKeyID] = true
	}

	// 4. Verify .client.json holds string compatibility map AND typed ortt map.
	metaBytes, err := os.ReadFile(configPath + ".client.json")
	if err != nil {
		t.Fatalf("read .client.json: %v", err)
	}
	var legacyRoot struct {
		Inbounds map[string]map[string]string `json:"inbounds"`
	}
	if err := json.Unmarshal(metaBytes, &legacyRoot); err != nil {
		t.Fatalf(".client.json must remain map[string]string compatible: %v", err)
	}
	doc := readClientMetaDoc(configPath)
	for _, row := range rows {
		if legacyRoot.Inbounds[row.Tag]["ortt_id"] == "" || legacyRoot.Inbounds[row.Tag]["ortt_secret"] == "" {
			t.Fatalf(".client.json missing string ortt fields for %s: %+v", row.Tag, legacyRoot.Inbounds[row.Tag])
		}
		if doc.Ortt[row.Tag].OrttKeyID < 2 {
			t.Fatalf(".client.json typed ortt block missing for %s", row.Tag)
		}
	}

	// 5. Verify GET /api/v1/inbounds exposes ortt_id, ortt_key_id, ortt_port, ortt_enabled, ortt_stats.
	wList := doAuthJSON(t, r, http.MethodGet, "/api/v1/inbounds", jwtTok, nil)
	if wList.Code != http.StatusOK {
		t.Fatalf("GET /inbounds failed: %d", wList.Code)
	}
	var listResp struct {
		OK  bool              `json:"ok"`
		Obj []InboundResponse `json:"obj"`
	}
	if err := json.Unmarshal(wList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode /inbounds: %v", err)
	}
	if len(listResp.Obj) != 5 {
		t.Fatalf("expected 5 items in /inbounds, got %d", len(listResp.Obj))
	}
	for _, item := range listResp.Obj {
		if !isValidOrttID(item.OrttID) || item.OrttKeyID < 2 || item.OrttStats == nil {
			t.Fatalf("incomplete InboundResponse ORTT fields: %+v", item)
		}
	}

	// 6. Verify live ORTT server KeyRing works for KeyID >= 2 and rejects after rotate & toggle.
	target := rows[0]
	targetMeta, _ := extractOrttMetaFromPayloadJSON(target.Payload)
	other := rows[1]
	otherMeta, _ := extractOrttMetaFromPayloadJSON(other.Payload)

	keyMapBefore := srv.KeyRingCopy()
	if string(keyMapBefore[targetMeta.OrttKeyID]) != targetMeta.OrttSecret {
		t.Fatalf("live server missing target KeyID=%d", targetMeta.OrttKeyID)
	}

	// Rotate node 0 -> keeps ortt_id, rotates ortt_key_id & ortt_secret, drops old key from live ring.
	wRot := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds/"+strconvFormatUint(target.ID)+"/ortt/rotate", jwtTok, nil)
	if wRot.Code != http.StatusOK {
		t.Fatalf("rotate failed (%d): %s", wRot.Code, wRot.Body.String())
	}
	var rotResp struct {
		OK       bool     `json:"ok"`
		OldKeyID uint16   `json:"old_key_id"`
		Ortt     OrttMeta `json:"ortt"`
	}
	if err := json.Unmarshal(wRot.Body.Bytes(), &rotResp); err != nil {
		t.Fatalf("decode rotate: %v", err)
	}
	if rotResp.Ortt.OrttID != targetMeta.OrttID {
		t.Fatalf("rotate changed ortt_id: %s -> %s", targetMeta.OrttID, rotResp.Ortt.OrttID)
	}
	if rotResp.Ortt.OrttKeyID == targetMeta.OrttKeyID || rotResp.Ortt.OrttKeyID < 2 {
		t.Fatalf("rotate did not assign new distinct KeyID: old=%d new=%d", targetMeta.OrttKeyID, rotResp.Ortt.OrttKeyID)
	}
	if rotResp.Ortt.OrttSecret == targetMeta.OrttSecret {
		t.Fatal("rotate did not change ortt_secret")
	}
	keyMapAfterRot := srv.KeyRingCopy()
	if _, stillExists := keyMapAfterRot[targetMeta.OrttKeyID]; stillExists {
		t.Fatalf("old KeyID=%d must be removed immediately after rotate", targetMeta.OrttKeyID)
	}
	if string(keyMapAfterRot[rotResp.Ortt.OrttKeyID]) != rotResp.Ortt.OrttSecret {
		t.Fatalf("new KeyID=%d not loaded into live server after rotate", rotResp.Ortt.OrttKeyID)
	}
	if string(keyMapAfterRot[otherMeta.OrttKeyID]) != otherMeta.OrttSecret {
		t.Fatalf("unaffected node KeyID=%d missing after rotation", otherMeta.OrttKeyID)
	}

	// Toggle node 0 ORTT OFF -> live KeyRing removes it, standard & aero links drop ORTT credentials.
	wTog := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds/"+strconvFormatUint(target.ID)+"/ortt/toggle", jwtTok, nil)
	if wTog.Code != http.StatusOK {
		t.Fatalf("toggle failed (%d): %s", wTog.Code, wTog.Body.String())
	}
	keyMapAfterOff := srv.KeyRingCopy()
	if _, exists := keyMapAfterOff[rotResp.Ortt.OrttKeyID]; exists {
		t.Fatalf("disabled node KeyID=%d must be removed from live KeyRing", rotResp.Ortt.OrttKeyID)
	}
	wLinksOff := doAuthJSON(t, r, http.MethodGet, "/api/v1/inbounds/"+strconvFormatUint(target.ID)+"/links", jwtTok, nil)
	var linksOffResp map[string]interface{}
	_ = json.Unmarshal(wLinksOff.Body.Bytes(), &linksOffResp)
	stdOff, _ := linksOffResp["standard_link"].(string)
	aeroOff, _ := linksOffResp["aero_link"].(string)
	if strings.Contains(stdOff, "okey=") || strings.Contains(stdOff, "oid=") {
		t.Fatalf("standard link must omit ORTT params when disabled: %s", stdOff)
	}
	decodedOff, _ := decodeAeroLink(t, aeroOff)
	if decodedOff.Ortt != nil {
		t.Fatalf("aero link must omit ortt block when disabled, got: %+v", decodedOff.Ortt)
	}

	// Toggle node 0 ORTT back ON -> restored in KeyRing.
	wTogOn := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds/"+strconvFormatUint(target.ID)+"/ortt/toggle", jwtTok, map[string]bool{"enabled": true})
	if wTogOn.Code != http.StatusOK {
		t.Fatalf("toggle ON failed (%d): %s", wTogOn.Code, wTogOn.Body.String())
	}
	if _, exists := srv.KeyRingCopy()[rotResp.Ortt.OrttKeyID]; !exists {
		t.Fatalf("re-enabled KeyID=%d not restored to live KeyRing", rotResp.Ortt.OrttKeyID)
	}
}

func TestSingboxCompilePreservesOrttAndToggleDeleteSyncsKeyRing(t *testing.T) {
	r, db, _, jwtTok, srv := setupOrttAPITestEnv(t)

	// 1. Seed a VMess node via quickstart.
	wQuick := doAuthJSON(t, r, http.MethodPost, "/api/v1/quick/vmess", jwtTok, nil)
	if wQuick.Code != http.StatusOK {
		t.Fatalf("quick/vmess failed (%d): %s", wQuick.Code, wQuick.Body.String())
	}
	var vmRow models.Inbound
	if err := db.First(&vmRow).Error; err != nil {
		t.Fatalf("load seeded vmess: %v", err)
	}
	vmMeta, ok := extractOrttMetaFromPayloadJSON(vmRow.Payload)
	if !ok {
		t.Fatalf("seeded vmess missing ORTT meta: %s", vmRow.Payload)
	}
	var vmPayload map[string]interface{}
	_ = json.Unmarshal([]byte(vmRow.Payload), &vmPayload)

	// 2. Call /api/v1/singbox/compile editing VMess port + adding a Shadowsocks node (without ORTT fields in request).
	allocPort := func() int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("allocPort: %v", err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		return p
	}
	vmPort := allocPort()
	ssPort := allocPort()

	compileBody := map[string]interface{}{
		"nodes": []map[string]interface{}{
			{
				"id":   "in_1",
				"kind": "inbound-vmess",
				"data": map[string]interface{}{
					"tag":            vmRow.Tag,
					"port":           vmPort,
					"uuid":           vmPayload["uuid"],
					"vmess_security": "auto",
					"network":        "ws",
					"path":           vmPayload["path"],
				},
			},
			{
				"id":   "in_2",
				"kind": "inbound-ss",
				"data": map[string]interface{}{
					"tag":      "compile-ss-node",
					"port":     ssPort,
					"method":   "2022-blake3-aes-128-gcm",
					"password": "short-seed-ss",
				},
			},
			{
				"id":   "out_direct",
				"kind": "outbound-direct",
				"data": map[string]interface{}{
					"tag": "direct",
				},
			},
		},
		"edges": []map[string]interface{}{
			{"id": "e1", "source": "in_1", "target": "out_direct"},
			{"id": "e2", "source": "in_2", "target": "out_direct"},
		},
	}
	wComp := doAuthJSON(t, r, http.MethodPost, "/api/v1/singbox/compile", jwtTok, compileBody)
	if wComp.Code != http.StatusOK {
		t.Fatalf("/singbox/compile failed (%d): %s", wComp.Code, wComp.Body.String())
	}

	var rows []models.Inbound
	if err := db.Order("id asc").Find(&rows).Error; err != nil || len(rows) != 2 {
		t.Fatalf("expected 2 compiled inbounds, got len=%d err=%v", len(rows), err)
	}
	var vmAfter, ssRow models.Inbound
	for _, row := range rows {
		if row.Tag == vmRow.Tag {
			vmAfter = row
		} else if row.Tag == "compile-ss-node" {
			ssRow = row
		}
	}
	if vmAfter.Port != vmPort {
		t.Fatalf("expected compiled port %d, got %d", vmPort, vmAfter.Port)
	}
	vmAfterMeta, ok := extractOrttMetaFromPayloadJSON(vmAfter.Payload)
	if !ok || vmAfterMeta.OrttID != vmMeta.OrttID || vmAfterMeta.OrttKeyID != vmMeta.OrttKeyID || vmAfterMeta.OrttSecret != vmMeta.OrttSecret {
		t.Fatalf("compile mutated existing ORTT identity: before=%+v after=%+v", vmMeta, vmAfterMeta)
	}
	ssMeta, ok := extractOrttMetaFromPayloadJSON(ssRow.Payload)
	if !ok || ssMeta.OrttKeyID == vmMeta.OrttKeyID {
		t.Fatalf("expected valid unique ORTT meta for compiled ss node, got %+v", ssMeta)
	}
	assertValidOrttFields(t, ssMeta.OrttID, ssMeta.OrttKeyID, ssMeta.OrttSecret, ssMeta.OrttPort, ssMeta.OrttEnabled)

	// Verify /inbounds/:id/links produces valid aero://v1 for compiled ss node.
	wSSLinks := doAuthJSON(t, r, http.MethodGet, "/api/v1/inbounds/"+strconvFormatUint(ssRow.ID)+"/links", jwtTok, nil)
	if wSSLinks.Code != http.StatusOK {
		t.Fatalf("GET ss links failed (%d): %s", wSSLinks.Code, wSSLinks.Body.String())
	}
	var ssLinksResp map[string]interface{}
	_ = json.Unmarshal(wSSLinks.Body.Bytes(), &ssLinksResp)
	ssAero, _ := ssLinksResp["aero_link"].(string)
	pSS, _ := decodeAeroLink(t, ssAero)
	if pSS.OB["type"] != "shadowsocks" || pSS.Ortt == nil || pSS.Ortt.Kid != ssMeta.OrttKeyID {
		t.Fatalf("unexpected shadowsocks aero payload: %+v", pSS)
	}

	// 3. Inbound-level toggle OFF removes VMess KeyID from live KeyRing without disturbing ss KeyID.
	wInTogOff := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds/"+strconvFormatUint(vmAfter.ID)+"/toggle", jwtTok, nil)
	if wInTogOff.Code != http.StatusOK {
		t.Fatalf("POST /inbounds/:id/toggle OFF failed (%d): %s", wInTogOff.Code, wInTogOff.Body.String())
	}
	ringOff := srv.KeyRingCopy()
	if _, exists := ringOff[vmMeta.OrttKeyID]; exists {
		t.Fatalf("disabled inbound KeyID=%d must be removed from live KeyRing", vmMeta.OrttKeyID)
	}
	if _, exists := ringOff[ssMeta.OrttKeyID]; !exists {
		t.Fatalf("enabled ss inbound KeyID=%d missing from live KeyRing", ssMeta.OrttKeyID)
	}

	// Toggle back ON restores VMess KeyID to live KeyRing.
	wInTogOn := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds/"+strconvFormatUint(vmAfter.ID)+"/toggle", jwtTok, nil)
	if wInTogOn.Code != http.StatusOK {
		t.Fatalf("POST /inbounds/:id/toggle ON failed (%d): %s", wInTogOn.Code, wInTogOn.Body.String())
	}
	if _, exists := srv.KeyRingCopy()[vmMeta.OrttKeyID]; !exists {
		t.Fatalf("re-enabled inbound KeyID=%d missing from live KeyRing", vmMeta.OrttKeyID)
	}

	// 4. DELETE /api/v1/inbounds/:id removes VMess KeyID from live KeyRing immediately.
	wDel := doAuthJSON(t, r, http.MethodDelete, "/api/v1/inbounds/"+strconvFormatUint(vmAfter.ID), jwtTok, nil)
	if wDel.Code != http.StatusOK {
		t.Fatalf("DELETE /inbounds/:id failed (%d): %s", wDel.Code, wDel.Body.String())
	}
	ringAfterDel := srv.KeyRingCopy()
	if _, exists := ringAfterDel[vmMeta.OrttKeyID]; exists {
		t.Fatalf("deleted inbound KeyID=%d still present in live KeyRing", vmMeta.OrttKeyID)
	}
	if _, exists := ringAfterDel[ssMeta.OrttKeyID]; !exists {
		t.Fatalf("remaining ss inbound KeyID=%d missing after delete", ssMeta.OrttKeyID)
	}
}

func TestMigrateLegacyInboundsBackfillsOrttIdentityWithoutCollision(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	db, err := openTestDB(filepath.Join(dir, "legacy.db"))
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	orttCfg := ortt.DefaultConfig()
	orttCfg.SecretKey = []byte("global-secret-migrate-test-32ch!")
	srv := ortt.NewServer(orttCfg)
	ortt.SetGlobalServer(srv)
	t.Cleanup(func() { ortt.SetGlobalServer(nil) })

	// Row 1: legacy REALITY without ORTT fields.
	// Row 2: already has key_id=2 (row 1 backfill must NOT collide with key_id=2!).
	// Row 3: legacy HY2 missing both cert and ORTT fields.
	rows := []models.Inbound{
		{Tag: "legacy-reality", Type: "vless", Port: 41001, Enabled: true, Payload: `{"uuid":"11111111-1111-4111-8111-111111111111","private_key":"pk1","short_id":"abcd1234","server_name":"www.apple.com"}`},
		{Tag: "existing-ortt", Type: "anytls", Port: 41002, Enabled: true, Payload: `{"password":"pw2","ortt_id":"ortt_aabbccddeeff","ortt_key_id":2,"ortt_secret":"existing-secret-32-chars-minimum","ortt_port":39999,"ortt_enabled":true}`},
		{Tag: "legacy-hy2", Type: "hysteria2", Port: 41003, Enabled: true, Payload: `{"password":"pw3"}`},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	t.Setenv("ORTT_PORT", "39999")
	MigrateLegacyInboundsWithConfig(db, dir, configPath)

	var after []models.Inbound
	if err := db.Order("id asc").Find(&after).Error; err != nil {
		t.Fatalf("reload rows: %v", err)
	}
	keys := map[uint16]bool{}
	for _, r := range after {
		meta, ok := extractOrttMetaFromPayloadJSON(r.Payload)
		if !ok || !isValidOrttID(meta.OrttID) || meta.OrttKeyID < 2 || len(meta.OrttSecret) < 16 {
			t.Fatalf("row %s failed ORTT backfill: %+v (%s)", r.Tag, meta, r.Payload)
		}
		if keys[meta.OrttKeyID] {
			t.Fatalf("backfill produced duplicate key_id %d", meta.OrttKeyID)
		}
		keys[meta.OrttKeyID] = true
	}
	m2, _ := extractOrttMetaFromPayloadJSON(after[1].Payload)
	if m2.OrttKeyID != 2 || m2.OrttID != "ortt_aabbccddeeff" {
		t.Fatalf("existing row 2 identity mutated during migration: %+v", m2)
	}
	if _, ok := srv.KeyRingCopy()[2]; !ok || len(srv.KeyRingCopy()) < 4 { // Global 1 + 3 nodes
		t.Fatalf("expected all 3 migrated nodes in live KeyRing, got: %d keys", len(srv.KeyRingCopy()))
	}
}

func TestAeroLinksAndSMatrixSubV2Formats(t *testing.T) {
	r, _, _, jwtTok, _ := setupOrttAPITestEnv(t)

	// Deploy REALITY, AnyTLS, HY2, VMess (with WS+TLS).
	for _, ep := range []string{"/api/v1/quick/reality", "/api/v1/quick/anytls", "/api/v1/quick/hy2", "/api/v1/quick/vmess"} {
		w := doAuthJSON(t, r, http.MethodPost, ep, jwtTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s failed (%d): %s", ep, w.Code, w.Body.String())
		}
	}

	// 1. Test GET /api/v1/discover (s-matrix.sub.v2).
	wDisc := doAuthJSON(t, r, http.MethodGet, "/api/v1/discover?token=sub-tok-ortt-test", "", nil)
	if wDisc.Code != http.StatusOK {
		t.Fatalf("discover failed (%d): %s", wDisc.Code, wDisc.Body.String())
	}
	var disc map[string]interface{}
	if err := json.Unmarshal(wDisc.Body.Bytes(), &disc); err != nil {
		t.Fatalf("decode discover: %v", err)
	}
	if disc["kind"] != "sbui" || disc["version"] != "s-matrix.sub.v1" || disc["sub_version"] != "s-matrix.sub.v2" {
		t.Fatalf("unexpected discover versions: %+v", disc)
	}
	endpoints, _ := disc["endpoints"].(map[string]interface{})
	if endpoints["sub_aero"] != "/api/v1/sub/:token?format=aero" || endpoints["sub_json"] != "/api/v1/sub/:token?format=aero-json" {
		t.Fatalf("unexpected endpoints map: %+v", endpoints)
	}

	// 2. Test GET /api/v1/sub/:token?format=aero (Base64) and ?raw=1.
	wAeroB64 := doAuthJSON(t, r, http.MethodGet, "/api/v1/sub/sub-tok-ortt-test?format=aero", "", nil)
	if wAeroB64.Code != http.StatusOK {
		t.Fatalf("sub?format=aero failed: %d", wAeroB64.Code)
	}
	rawBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(wAeroB64.Body.String()))
	if err != nil {
		t.Fatalf("decode base64 aero sub: %v", err)
	}
	wAeroRaw := doAuthJSON(t, r, http.MethodGet, "/api/v1/sub/sub-tok-ortt-test?format=aero&raw=1", "", nil)
	if wAeroRaw.Code != http.StatusOK || string(rawBytes) != wAeroRaw.Body.String() {
		t.Fatalf("raw=1 mismatch against base64 decoded aero subscription")
	}
	lines := strings.Split(strings.TrimSpace(string(rawBytes)), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 aero links, got %d: %s", len(lines), string(rawBytes))
	}

	seenTypes := map[string]bool{}
	for _, line := range lines {
		p, fragName := decodeAeroLink(t, line)
		if p.V != 1 || p.Name != fragName || p.Ortt == nil || p.Ortt.Ver != 2 || p.Ortt.Kid < 2 || p.Ortt.Port != 41234 || p.Ortt.Mode != "auto" {
			t.Fatalf("invalid aero v1 payload: %+v (frag=%q)", p, fragName)
		}
		typ, _ := p.OB["type"].(string)
		seenTypes[typ] = true
		// Server-only fields MUST NOT leak into client outbound.
		for _, forbidden := range []string{"listen", "listen_port", "users", "private_key", "certificate_path", "key_path", "padding_scheme"} {
			if _, leaked := p.OB[forbidden]; leaked {
				t.Fatalf("outbound type %s leaked server-only field %q: %+v", typ, forbidden, p.OB)
			}
		}
		switch typ {
		case "vless":
			tlsObj, _ := p.OB["tls"].(map[string]interface{})
			realObj, _ := tlsObj["reality"].(map[string]interface{})
			if p.OB["flow"] != "xtls-rprx-vision" || realObj["public_key"] == "" {
				t.Fatalf("vless reality outbound incomplete: %+v", p.OB)
			}
		case "vmess":
			trObj, _ := p.OB["transport"].(map[string]interface{})
			if trObj["type"] != "ws" || p.OB["security"] == "" {
				t.Fatalf("vmess outbound incomplete: %+v", p.OB)
			}
		case "anytls", "hysteria2":
			tlsObj, _ := p.OB["tls"].(map[string]interface{})
			if tlsObj["enabled"] != true || p.OB["password"] == "" {
				t.Fatalf("%s outbound incomplete: %+v", typ, p.OB)
			}
		}
	}
	for _, want := range []string{"vless", "anytls", "hysteria2", "vmess"} {
		if !seenTypes[want] {
			t.Fatalf("missing protocol %s in aero subscription", want)
		}
	}

	// 3. Test GET /api/v1/sub/:token (default Base64 standard links carry ORTT query params & vmess JSON ORTT fields).
	wStd := doAuthJSON(t, r, http.MethodGet, "/api/v1/sub/sub-tok-ortt-test", "", nil)
	if wStd.Code != http.StatusOK {
		t.Fatalf("default sub failed: %d", wStd.Code)
	}
	stdRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(wStd.Body.String()))
	if err != nil {
		t.Fatalf("decode default sub: %v", err)
	}
	stdLines := strings.Split(strings.TrimSpace(string(stdRaw)), "\n")
	if len(stdLines) != 4 {
		t.Fatalf("expected 4 standard links, got %d", len(stdLines))
	}
	for _, l := range stdLines {
		if strings.HasPrefix(l, "vmess://") {
			vm := decodeVMessLink(t, l)
			if vm["ortt"] != float64(2) || vm["oid"] == "" || vm["okey"] == "" || vm["oport"] != float64(41234) {
				t.Fatalf("vmess JSON missing ORTT metadata: %+v", vm)
			}
		} else {
			if !strings.Contains(l, "ortt=2") || !strings.Contains(l, "oid=ortt_") || !strings.Contains(l, "okid=") || !strings.Contains(l, "okey=") || !strings.Contains(l, "oport=41234") {
				t.Fatalf("standard link missing ORTT query params: %s", l)
			}
		}
	}

	// 4. Test GET /api/v1/sub/:token?format=aero-json (s-matrix.sub.v2 JSON contract).
	wJSON := doAuthJSON(t, r, http.MethodGet, "/api/v1/sub/sub-tok-ortt-test?format=aero-json", "", nil)
	if wJSON.Code != http.StatusOK {
		t.Fatalf("sub?format=aero-json failed (%d): %s", wJSON.Code, wJSON.Body.String())
	}
	var subV2 AeroSubV2Response
	if err := json.Unmarshal(wJSON.Body.Bytes(), &subV2); err != nil {
		t.Fatalf("decode aero-json: %v", err)
	}
	if subV2.Schema != "s-matrix.sub.v2" || subV2.ServerHost != "203.0.113.10" || len(subV2.Nodes) != 4 {
		t.Fatalf("unexpected aero-json header/count: schema=%s host=%s nodes=%d", subV2.Schema, subV2.ServerHost, len(subV2.Nodes))
	}
	for _, node := range subV2.Nodes {
		if node.InboundID == 0 || node.Tag == "" || !node.Enabled || node.Outbound == nil || node.Ortt == nil {
			t.Fatalf("incomplete aero-json node: %+v", node)
		}
		if node.Outbound["tag"] != node.Tag {
			t.Fatalf("expected outbound.tag == node.tag (%s), got %v", node.Tag, node.Outbound["tag"])
		}
		if node.Ortt.Ver != 2 || node.Ortt.Kid < 2 || !node.Ortt.Enabled || node.Ortt.Port != 41234 {
			t.Fatalf("unexpected node.ortt: %+v", node.Ortt)
		}
		if !strings.HasPrefix(node.Links.Aero, "aero://v1/") || node.Links.Standard == "" {
			t.Fatalf("incomplete node.links: %+v", node.Links)
		}
	}
}

func TestTrojanSNIAndHY2ObfsObjectParity(t *testing.T) {
	r, db, configPath, jwtTok, _ := setupOrttAPITestEnv(t)

	allocPort := func() int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("allocPort: %v", err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		return p
	}

	// 1. Manual Trojan with sni (NewTrojanInbound omits TLS from config.json, so standard link must match dbPayload["sni"]).
	wTr := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds", jwtTok, map[string]interface{}{
		"tag":      "trojan-sni-node",
		"type":     "trojan",
		"port":     allocPort(),
		"password": "tr-password-1",
		"sni":      "tr.example.net",
	})
	if wTr.Code != http.StatusOK {
		t.Fatalf("create trojan failed (%d): %s", wTr.Code, wTr.Body.String())
	}

	// 2. Manual Trojan without SNI must not invent sni=203.0.113.10.
	wTrNoSni := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds", jwtTok, map[string]interface{}{
		"tag":      "trojan-nosni-node",
		"type":     "trojan",
		"port":     allocPort(),
		"password": "tr-password-2",
	})
	if wTrNoSni.Code != http.StatusOK {
		t.Fatalf("create trojan-nosni failed (%d): %s", wTrNoSni.Code, wTrNoSni.Body.String())
	}

	// 3. Manual Hysteria2 with object obfs: {"type":"salamander","password":"obfs-secret-pw"}.
	certPath, keyPath, _, err := singbox.EnsureSelfSignedPair(t.TempDir(), "hy2-obfs", "hy2.example.net", nil, []string{"hy2.example.net"})
	if err != nil {
		t.Fatalf("EnsureSelfSignedPair: %v", err)
	}
	wHy := doAuthJSON(t, r, http.MethodPost, "/api/v1/inbounds", jwtTok, map[string]interface{}{
		"tag":              "hy2-obfs-obj",
		"type":             "hysteria2",
		"port":             allocPort(),
		"password":         "hy2-password-1",
		"sni":              "hy2.example.net",
		"certificate_path": certPath,
		"key_path":         keyPath,
		"obfs": map[string]interface{}{
			"type":     "salamander",
			"password": "obfs-secret-pw",
		},
	})
	if wHy.Code != http.StatusOK {
		t.Fatalf("create hy2-obfs failed (%d): %s", wHy.Code, wHy.Body.String())
	}

	// Check /sub/:token?format=aero-json parity across both outbound & standard link.
	wSub := doAuthJSON(t, r, http.MethodGet, "/api/v1/sub/sub-tok-ortt-test?format=aero-json", "", nil)
	if wSub.Code != http.StatusOK {
		t.Fatalf("sub?format=aero-json failed (%d): %s", wSub.Code, wSub.Body.String())
	}
	var subV2 AeroSubV2Response
	if err := json.Unmarshal(wSub.Body.Bytes(), &subV2); err != nil {
		t.Fatalf("decode aero-json: %v", err)
	}
	byTag := map[string]AeroSubNode{}
	for _, n := range subV2.Nodes {
		byTag[n.Tag] = n
	}

	// Trojan with SNI: both aero and trojan:// MUST use tr.example.net (never 203.0.113.10).
	trNode := byTag["trojan-sni-node"]
	trTLS, _ := trNode.Outbound["tls"].(map[string]interface{})
	if trTLS["server_name"] != "tr.example.net" {
		t.Fatalf("trojan aero outbound expected tls.server_name=tr.example.net, got %+v", trNode.Outbound)
	}
	trAero, _ := decodeAeroLink(t, trNode.Links.Aero)
	trAeroTLS, _ := trAero.OB["tls"].(map[string]interface{})
	if trAeroTLS["server_name"] != "tr.example.net" {
		t.Fatalf("trojan aero://v1 expected tls.server_name=tr.example.net, got %+v", trAero.OB)
	}
	if !strings.Contains(trNode.Links.Standard, "sni=tr.example.net") || strings.Contains(trNode.Links.Standard, "sni=203.0.113.10") {
		t.Fatalf("trojan standard link SNI mismatch: %s", trNode.Links.Standard)
	}

	// Verify GET /inbounds/:id/links record path matches too.
	var trDB models.Inbound
	if err := db.Where("tag = ?", "trojan-sni-node").First(&trDB).Error; err != nil {
		t.Fatalf("load trojan-sni-node: %v", err)
	}
	wTrLinks := doAuthJSON(t, r, http.MethodGet, "/api/v1/inbounds/"+strconvFormatUint(trDB.ID)+"/links", jwtTok, nil)
	var trLinksResp map[string]interface{}
	_ = json.Unmarshal(wTrLinks.Body.Bytes(), &trLinksResp)
	if std, _ := trLinksResp["standard_link"].(string); !strings.Contains(std, "sni=tr.example.net") {
		t.Fatalf("trojan /links standard_link missing sni=tr.example.net: %s", std)
	}

	// Trojan without SNI: neither aero nor trojan:// sets sni=203.0.113.10.
	trNoSniNode := byTag["trojan-nosni-node"]
	trNoTLS, _ := trNoSniNode.Outbound["tls"].(map[string]interface{})
	if sni, _ := trNoTLS["server_name"].(string); sni != "" {
		t.Fatalf("trojan-nosni aero outbound unexpectedly set server_name=%q", sni)
	}
	if strings.Contains(trNoSniNode.Links.Standard, "sni=") {
		t.Fatalf("trojan-nosni standard link must not invent IP SNI: %s", trNoSniNode.Links.Standard)
	}

	// Hysteria2 object obfs: both aero outbound and standard link carry type=salamander & password=obfs-secret-pw.
	hyNode := byTag["hy2-obfs-obj"]
	hyObfs, _ := hyNode.Outbound["obfs"].(map[string]interface{})
	if hyObfs["type"] != "salamander" || hyObfs["password"] != "obfs-secret-pw" {
		t.Fatalf("hy2 aero outbound dropped object obfs: %+v", hyNode.Outbound)
	}
	if !strings.Contains(hyNode.Links.Standard, "obfs=salamander") || !strings.Contains(hyNode.Links.Standard, "obfs-password=obfs-secret-pw") {
		t.Fatalf("hy2 standard link missing obfs/obfs-password: %s", hyNode.Links.Standard)
	}
	hyDirect := buildHY2Link(map[string]interface{}{
		"type":        "hysteria2",
		"tag":         "direct-hy2",
		"listen_port": float64(44333),
		"users":       []interface{}{map[string]interface{}{"password": "pw"}},
		"obfs":        map[string]interface{}{"type": "salamander", "password": "pw2"},
	}, "1.2.3.4", 44333, "direct-hy2")
	if !strings.Contains(hyDirect, "obfs=salamander") || !strings.Contains(hyDirect, "obfs-password=pw2") {
		t.Fatalf("buildHY2Link dropped map obfs: %s", hyDirect)
	}

	// Also verify default Base64 /sub/:token matches Standard links when configPath is present.
	_ = configPath
	wStd := doAuthJSON(t, r, http.MethodGet, "/api/v1/sub/sub-tok-ortt-test?raw=1", "", nil)
	if wStd.Code != http.StatusOK {
		t.Fatalf("sub?raw=1 failed (%d)", wStd.Code)
	}
	rawSub := wStd.Body.String()
	if !strings.Contains(rawSub, "sni=tr.example.net") || !strings.Contains(rawSub, "obfs-password=obfs-secret-pw") {
		t.Fatalf("raw standard sub missing trojan sni or hy2 obfs-password:\n%s", rawSub)
	}
}

func strconvFormatUint(v uint) string {
	return jsonNumberUint(v)
}

func jsonNumberUint(v uint) string {
	b, _ := json.Marshal(v)
	return string(b)
}
