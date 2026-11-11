package api

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

func startTestListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func closedTestPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return strconv.Itoa(port)
}

// TestRebuildFromDBKeepsEveryEnabledInbound guards the regression where a
// one-click create compiled a single-inbound config and silently dropped the
// other nodes from sing-box while they stayed in SQLite.
func TestRebuildFromDBKeepsEveryEnabledInbound(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	// Real certificate files: the compiler now validates against sing-box, which
	// refuses inbounds whose certificate is missing.
	for _, name := range []string{"a", "b", "c"} {
		if _, _, _, err := singbox.EnsureSelfSignedPair(dir, name, "www.bing.com", nil, []string{"www.bing.com"}); err != nil {
			t.Fatalf("cert %s: %v", name, err)
		}
	}
	// The real manager wants a runnable sing-box; config compilation is what
	// this test guards, so a stub binary is enough and restart errors are fine.
	manager := singbox.NewSingboxManager("/bin/true", configPath, filepath.Join(dir, "singbox.log"))

	db, err := openTestDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	seed := []struct {
		tag     string
		typ     string
		port    int
		payload map[string]interface{}
	}{
		{"AnyTLS-a", "anytls", 31001, map[string]interface{}{
			"password": "pw-a", "server_name": "www.bing.com",
			"certificate_path": filepath.Join(dir, "a.pem"), "key_path": filepath.Join(dir, "a.key"),
		}},
		{"AnyTLS-b", "anytls", 31002, map[string]interface{}{
			"password": "pw-b", "server_name": "www.bing.com",
			"certificate_path": filepath.Join(dir, "b.pem"), "key_path": filepath.Join(dir, "b.key"),
		}},
		{"disabled-one", "anytls", 31003, map[string]interface{}{
			"password": "pw-c", "certificate_path": filepath.Join(dir, "c.pem"), "key_path": filepath.Join(dir, "c.key"),
		}},
	}
	for _, s := range seed {
		raw, _ := json.Marshal(s.payload)
		row := models.Inbound{Tag: s.tag, Type: s.typ, Port: s.port, Payload: string(raw), Enabled: true}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed %s: %v", s.tag, err)
		}
		if s.tag == "disabled-one" {
			// GORM drops zero-value false on Create because the column has
			// default:true, so disable explicitly afterwards.
			if err := db.Model(&row).Update("enabled", false).Error; err != nil {
				t.Fatalf("disable %s: %v", s.tag, err)
			}
		}
	}

	if err := rebuildFromDB(db, manager, configPath); err != nil && !strings.Contains(err.Error(), "restart") && !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("rebuild: %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		Inbounds []map[string]interface{} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if len(cfg.Inbounds) != 2 {
		t.Fatalf("expected 2 inbounds (disabled skipped), got %d: %s", len(cfg.Inbounds), raw)
	}
	ports := map[int]bool{}
	for _, in := range cfg.Inbounds {
		if p, ok := in["listen_port"].(float64); ok {
			ports[int(p)] = true
		}
	}
	if !ports[31001] || !ports[31002] {
		t.Fatalf("missing expected ports: %v", ports)
	}
	if ports[31003] {
		t.Fatalf("disabled inbound leaked into config")
	}
}

func openTestDB(path string) (*gorm.DB, error) {
	return models.OpenDatabase(path)
}

// TestRebuildFromDBEmptyOrttRoute guards the P0 fresh-install case: with no
// nodes, the rebuild writes a hand-built empty config, which must still
// carry the ORTT warm-pool route when ORTT is enabled with a secret, and
// must not carry it when ORTT is off.
func TestRebuildFromDBEmptyOrttRoute(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	manager := singbox.NewSingboxManager("/bin/true", configPath, filepath.Join(dir, "singbox.log"))
	db, err := openTestDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	// No inbounds seeded: the empty-DB path.
	cases := []struct {
		name      string
		ortt      string
		secret    string
		listen    bool
		wantRoute bool
	}{
		{"enabled with secret", "true", "test-secret-32", true, true},
		{"enabled without secret", "true", "", true, false},
		{"disabled", "false", "test-secret-32", true, false},
		{"engine down with secret", "true", "test-secret-32", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SMATRIX_DATA_DIR", dir)
			t.Setenv("ORTT_ENABLED", tc.ortt)
			t.Setenv("SMATRIX_ACCEL_ENABLED", "")
			t.Setenv("ORTT_SECRET", tc.secret)
			t.Setenv("SMATRIX_ACCEL_SECRET", "")
			if tc.listen {
				t.Setenv("ORTT_FORWARD_PORT", startTestListener(t))
			} else {
				t.Setenv("ORTT_FORWARD_PORT", closedTestPort(t))
			}
			if err := rebuildFromDB(db, manager, configPath); err != nil {
				t.Fatalf("rebuild: %v", err)
			}
			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			got := strings.Contains(string(raw), "ortt-warm-pool")
			if got != tc.wantRoute {
				t.Fatalf("ortt-warm-pool present = %v, want %v (config: %s)", got, tc.wantRoute, raw)
			}
		})
	}
}

func TestRebuildFromDBSyncsPortAndRenameClientMeta(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	manager := singbox.NewSingboxManager("/bin/true", configPath, filepath.Join(dir, "singbox.log"))

	if _, _, _, err := singbox.EnsureSelfSignedPair(dir, "sync", "www.bing.com", nil, []string{"www.bing.com"}); err != nil {
		t.Fatalf("cert: %v", err)
	}
	db, err := openTestDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	// Reserved port 19088 forces compiler to reallocate a free port; DB must stay in sync.
	payload, _ := json.Marshal(map[string]interface{}{
		"password": "pw-sync", "server_name": "www.bing.com",
		"certificate_path": filepath.Join(dir, "sync.pem"), "key_path": filepath.Join(dir, "sync.key"),
	})
	row := models.Inbound{Tag: "sync-node", Type: "anytls", Port: 19088, Payload: string(payload), Enabled: true}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := rebuildFromDB(db, manager, configPath); err != nil && !strings.Contains(err.Error(), "restart") && !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("rebuild: %v", err)
	}
	var updated models.Inbound
	if err := db.First(&updated, row.ID).Error; err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if updated.Port == 19088 || updated.Port <= 0 {
		t.Fatalf("expected DB port synced away from reserved 19088, got %d", updated.Port)
	}

	// Verify renameClientMeta moves key cleanly.
	metaInit := []byte(`{"inbounds":{"old-name":{"public_key":"pk1","short_id":"sid1"}}}`)
	if err := os.WriteFile(configPath+".client.json", metaInit, 0644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if err := renameClientMeta(configPath, "old-name", "new-name"); err != nil {
		t.Fatalf("renameClientMeta: %v", err)
	}
	metaRaw, _ := os.ReadFile(configPath + ".client.json")
	if strings.Contains(string(metaRaw), "old-name") || !strings.Contains(string(metaRaw), "new-name") {
		t.Fatalf("expected key renamed in client meta, got: %s", metaRaw)
	}
}
