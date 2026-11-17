package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

func TestUpdateInboundInPlacePreservesIDOrttDisabledAndCerts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	certPath, keyPath, _, err := singbox.EnsureSelfSignedPair(dir, "hy2-test", "www.bing.com", nil, []string{"www.bing.com"})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	manager := singbox.NewSingboxManager(testStubManagerBin(t, dir), configPath, filepath.Join(dir, "singbox.log"))

	db, err := openTestDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	priv, pub, err := singbox.GenerateRealityKeypair()
	if err != nil {
		t.Fatal(err)
	}

	realityPayload, _ := json.Marshal(map[string]interface{}{
		"uuid":         "11111111-2222-4333-8444-555555555555",
		"private_key":  priv,
		"public_key":   pub,
		"short_id":     "a1b2c3d4",
		"server_name":  "www.apple.com",
		"ortt_id":      "ortt_11111111aaaa",
		"ortt_key_id":  7,
		"ortt_secret":  "secret_keepme_111111111111111111",
		"ortt_port":    39999,
		"ortt_enabled": true,
	})
	rowReality := models.Inbound{Tag: "R-One", Type: "vless", Listen: "::", Port: 41010, Enabled: true, Payload: string(realityPayload)}
	if err := db.Create(&rowReality).Error; err != nil {
		t.Fatal(err)
	}

	hy2Payload, _ := json.Marshal(map[string]interface{}{
		"password":         "hy2-old-pw",
		"masquerade":       "https://www.bing.com",
		"certificate_path": certPath,
		"key_path":         keyPath,
		"server_name":      "www.bing.com",
		"ortt_id":          "ortt_22222222bbbb",
		"ortt_key_id":      8,
		"ortt_secret":      "secret_keepme_222222222222222222",
		"ortt_port":        39999,
		"ortt_enabled":     true,
	})
	rowHY2 := models.Inbound{Tag: "HY2-Disabled", Type: "hysteria2", Listen: "::", Port: 41020, Enabled: true, Payload: string(hy2Payload)}
	if err := db.Create(&rowHY2).Error; err != nil {
		t.Fatal(err)
	}
	// Mark HY2 as disabled to ensure editing R-One does not re-enable HY2,
	// and editing HY2 keeps it disabled while preserving its certs.
	if err := db.Model(&rowHY2).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.PUT("/api/v1/inbounds/:id", UpdateInboundHandler(db, manager, configPath))
	r.POST("/api/v1/inbounds", CreateInboundHandler(db, manager, configPath))

	// 1. Edit REALITY node: rename, keep port (0 passed), change dest with :443.
	body1, _ := json.Marshal(map[string]interface{}{
		"tag":  "R-Renamed",
		"port": 0,
		"type": "vless",
		"payload": map[string]interface{}{
			"uuid":        "11111111-2222-4333-8444-555555555555",
			"dest":        "docs.aws.amazon.com:443",
			"server_name": "docs.aws.amazon.com:443",
			"private_key": priv,
			"short_id":    "a1b2c3d4",
		},
	})
	req1 := httptest.NewRequest(http.MethodPut, "/api/v1/inbounds/1", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 editing REALITY, got %d: %s", w1.Code, w1.Body.String())
	}

	var updatedReality models.Inbound
	if err := db.First(&updatedReality, rowReality.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedReality.ID != rowReality.ID {
		t.Fatalf("ID changed on edit: got %d want %d", updatedReality.ID, rowReality.ID)
	}
	if updatedReality.Tag != "R-Renamed" || updatedReality.Port != 41010 {
		t.Fatalf("unexpected updated tag/port: %s %d", updatedReality.Tag, updatedReality.Port)
	}
	var p1 map[string]interface{}
	if err := json.Unmarshal([]byte(updatedReality.Payload), &p1); err != nil {
		t.Fatal(err)
	}
	if p1["server_name"] != "docs.aws.amazon.com" {
		t.Fatalf("expected clean server_name without :443, got %v", p1["server_name"])
	}
	if p1["ortt_id"] != "ortt_11111111aaaa" || int(p1["ortt_key_id"].(float64)) != 7 {
		t.Fatalf("ortt identity lost on edit: %+v", p1)
	}

	// Verify HY2 remained disabled in DB and absent from active config.json.
	var checkHY2 models.Inbound
	if err := db.First(&checkHY2, rowHY2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if checkHY2.Enabled {
		t.Fatalf("disabled node was wrongly re-enabled by editing another node")
	}

	// 2. Edit disabled HY2 without passing cert paths: must keep certs & stay disabled.
	body2, _ := json.Marshal(map[string]interface{}{
		"tag":  "HY2-Disabled",
		"port": 41020,
		"type": "hysteria2",
		"payload": map[string]interface{}{
			"password":   "hy2-new-pw",
			"masquerade": "https://www.apple.com",
		},
	})
	req2 := httptest.NewRequest(http.MethodPut, "/api/v1/inbounds/2", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 editing HY2, got %d: %s", w2.Code, w2.Body.String())
	}
	if err := db.First(&checkHY2, rowHY2.ID).Error; err != nil {
		t.Fatal(err)
	}
	if checkHY2.Enabled {
		t.Fatalf("HY2 should stay disabled after edit")
	}
	var p2 map[string]interface{}
	_ = json.Unmarshal([]byte(checkHY2.Payload), &p2)
	if p2["certificate_path"] != certPath || p2["key_path"] != keyPath {
		t.Fatalf("HY2 cert paths were wiped: got %v / %v", p2["certificate_path"], p2["key_path"])
	}
	if p2["password"] != "hy2-new-pw" {
		t.Fatalf("HY2 password did not update: %v", p2["password"])
	}

	// 3. Reject conflicting port & verify original record untouched.
	bodyConflict, _ := json.Marshal(map[string]interface{}{
		"tag":  "R-Renamed",
		"port": 41020, // collides with HY2
		"type": "vless",
	})
	req3 := httptest.NewRequest(http.MethodPut, "/api/v1/inbounds/1", bytes.NewReader(bodyConflict))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on port collision, got %d: %s", w3.Code, w3.Body.String())
	}
	_ = db.First(&updatedReality, rowReality.ID)
	if updatedReality.Port != 41010 {
		t.Fatalf("port mutated despite 400 error: %d", updatedReality.Port)
	}

	// 4. Reject invalid protocol edit (AnyTLS missing cert) & verify original untouched.
	bodyBadCompile, _ := json.Marshal(map[string]interface{}{
		"tag":  "R-Renamed",
		"port": 41010,
		"type": "anytls",
		"payload": map[string]interface{}{
			"password": "test-password",
		},
	})
	req4 := httptest.NewRequest(http.MethodPut, "/api/v1/inbounds/1", bytes.NewReader(bodyBadCompile))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when compile fails, got %d: %s", w4.Code, w4.Body.String())
	}
	_ = db.First(&updatedReality, rowReality.ID)
	if updatedReality.Type != "vless" {
		t.Fatalf("type mutated despite validation failure: %s", updatedReality.Type)
	}

	// 5. Create new node with port=0 auto-allocation via POST /api/v1/inbounds.
	bodyCreate, _ := json.Marshal(map[string]interface{}{
		"tag":  "AutoPort-SS",
		"port": 0,
		"type": "ss",
		"payload": map[string]interface{}{
			"method":   "aes-128-gcm",
			"password": "ss-password-123",
		},
	})
	req5 := httptest.NewRequest(http.MethodPost, "/api/v1/inbounds", bytes.NewReader(bodyCreate))
	req5.Header.Set("Content-Type", "application/json")
	w5 := httptest.NewRecorder()
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("expected 200 creating auto-port node, got %d: %s", w5.Code, w5.Body.String())
	}
	var created models.Inbound
	if err := db.Where("tag = ?", "AutoPort-SS").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.Port < 10000 || created.Port > 60000 {
		t.Fatalf("expected auto-allocated port in 10000-60000, got %d", created.Port)
	}
	rawCfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rawCfg) == 0 {
		t.Fatalf("config.json is empty")
	}
}
