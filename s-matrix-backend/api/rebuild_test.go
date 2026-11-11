package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

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
		wantRoute bool
	}{
		{"enabled with secret", "true", "test-secret-32", true},
		{"enabled without secret", "true", "", false},
		{"disabled", "false", "test-secret-32", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SMATRIX_DATA_DIR", dir)
			t.Setenv("ORTT_ENABLED", tc.ortt)
			t.Setenv("SMATRIX_ACCEL_ENABLED", "")
			t.Setenv("ORTT_SECRET", tc.secret)
			t.Setenv("SMATRIX_ACCEL_SECRET", "")
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
