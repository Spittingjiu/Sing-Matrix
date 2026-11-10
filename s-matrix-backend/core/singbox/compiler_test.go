package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileToSingbox(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _, err := EnsureSelfSignedPair(dir, "unit", "www.bing.com", nil, []string{"www.bing.com"})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	ui := UIData{
		Nodes: []UINode{
			{ID: "hy2", Kind: "inbound-hy2", Data: map[string]interface{}{"tag": "hy2-main", "port": 44300, "password": "pw", "certificate_path": certPath, "key_path": keyPath}},
			{ID: "rule", Kind: "rule-srs", Data: map[string]interface{}{"tag": "youtube", "url": "https://example.com/youtube.srs"}},
			{ID: "out", Kind: "outbound-direct", Data: map[string]interface{}{"tag": "direct"}},
		},
		Edges: []UIEdge{{Source: "hy2", Target: "rule"}, {Source: "rule", Target: "out"}},
	}
	cfg, err := CompileToSingbox(ui)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Tag != "hy2-main" {
		t.Fatalf("bad inbounds: %+v", cfg.Inbounds)
	}
	expectedRules := 1
	if os.Getenv("ORTT_ENABLED") != "false" && os.Getenv("ORTT_ENABLED") != "0" {
		expectedRules = 2
	}
	if expectedRules == 2 {
		if cfg.Route.Rules[0].Outbound != "ortt-warm-pool" || cfg.Route.Rules[0].Network != "tcp" || len(cfg.Route.Rules[0].Port) != 1 || cfg.Route.Rules[0].Port[0] != 443 {
			t.Fatalf("bad ortt route: %+v", cfg.Route.Rules[0])
		}
		if cfg.Route.Rules[1].Inbound != "hy2-main" || cfg.Route.Rules[1].RuleSet != "youtube" || cfg.Route.Rules[1].Outbound != "direct" {
			t.Fatalf("bad route: %+v", cfg.Route)
		}
	} else {
		if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Inbound != "hy2-main" || cfg.Route.Rules[0].RuleSet != "youtube" || cfg.Route.Rules[0].Outbound != "direct" {
			t.Fatalf("bad route: %+v", cfg.Route)
		}
	}
	path := filepath.Join(dir, "config.json")
	defer os.Remove(path)
	if _, err := CompileAndWrite(ui, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestCompilerReplacesWeakDefaults(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _, err := EnsureSelfSignedPair(dir, "unit", "www.bing.com", nil, []string{"www.bing.com"})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}

	ui := UIData{
		Nodes: []UINode{
			{ID: "r1", Kind: "inbound-reality", Data: map[string]interface{}{"tag": "r1", "port": 44301, "uuid": "00000000-0000-0000-0000-000000000000"}},
			{ID: "h1", Kind: "inbound-hy2", Data: map[string]interface{}{"tag": "h1", "port": 44302, "password": "change-me", "certificate_path": certPath, "key_path": keyPath}},
		},
		Edges: []UIEdge{},
	}

	cfg, err := CompileToSingbox(ui)
	if err != nil {
		t.Fatal(err)
	}

	for _, in := range cfg.Inbounds {
		if in.Tag == "r1" {
			if len(in.Users) == 0 || in.Users[0].UUID == "00000000-0000-0000-0000-000000000000" || len(in.Users[0].UUID) < 32 {
				t.Fatalf("reality inbound still has weak/zero UUID: %+v", in.Users)
			}
		}
		if in.Tag == "h1" {
			if len(in.Users) == 0 || in.Users[0].Password == "change-me" || len(in.Users[0].Password) < 16 {
				t.Fatalf("hy2 inbound still has weak 'change-me' password: %+v", in.Users)
			}
		}
	}
}

func TestCompileVMessWSTLS(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, _, err := EnsureSelfSignedPair(dir, "vmess-unit", "www.bing.com", nil, []string{"www.bing.com"})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	wsNode := UINode{ID: "vm", Kind: "inbound-vmess", Data: map[string]interface{}{
		"tag": "VMess :50123", "port": 50123, "uuid": "11111111-2222-4333-8444-555555555555",
		"network": "ws", "path": "/chat", "host": "www.bing.com",
		"security": "tls", "server_name": "www.bing.com",
		"certificate_path": certPath, "key_path": keyPath,
		"vmess_security": "aes-128-gcm",
	}}
	cfg, err := CompileToSingbox(UIData{Nodes: []UINode{wsNode, {ID: "out", Kind: "outbound-direct", Data: map[string]interface{}{"tag": "direct"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 {
		t.Fatalf("bad inbounds: %+v", cfg.Inbounds)
	}
	ib := cfg.Inbounds[0]
	if ib.Type != "vmess" {
		t.Fatalf("type: %s", ib.Type)
	}
	if ib.TLS == nil || ib.TLS["certificate_path"] != certPath || ib.TLS["key_path"] != keyPath || ib.TLS["server_name"] != "www.bing.com" {
		t.Fatalf("bad TLS: %+v", ib.TLS)
	}
	if ib.Transport == nil || ib.Transport["type"] != "ws" || ib.Transport["path"] != "/chat" {
		t.Fatalf("bad transport: %+v", ib.Transport)
	}
	headers, _ := ib.Transport["headers"].(map[string]interface{})
	if headers == nil || headers["Host"] != "www.bing.com" {
		t.Fatalf("bad headers: %+v", ib.Transport["headers"])
	}
	if len(ib.Users) != 1 {
		t.Fatalf("bad users: %+v", ib.Users)
	}
	// sing-box rejects an unknown "security" field in vmess users, so the
	// server config must not carry the cipher (it is a link-only field).
	raw, _ := json.Marshal(ib)
	if strings.Contains(string(raw), `"security"`) {
		t.Fatalf("vmess user must not carry a security field: %s", raw)
	}

	// VMess+TLS without a certificate must fail loudly, not emit a config
	// sing-box would refuse (which would take every other node down).
	noCert := wsNode
	noCert.Data = map[string]interface{}{
		"tag": "VMess :50124", "port": 50124, "uuid": "11111111-2222-4333-8444-555555555555",
		"network": "ws", "path": "/chat", "host": "www.bing.com", "security": "tls",
	}
	if _, err := CompileToSingbox(UIData{Nodes: []UINode{noCert}}); err == nil {
		t.Fatal("expected error for TLS without certificate")
	}

	// Plain VMess+TCP: no TLS block, no transport.
	tcpNode := UINode{ID: "vm2", Kind: "inbound-vmess", Data: map[string]interface{}{
		"tag": "VMess :50125", "port": 50125, "uuid": "11111111-2222-4333-8444-555555555555",
		"network": "tcp", "vmess_security": "auto",
	}}
	cfg, err = CompileToSingbox(UIData{Nodes: []UINode{tcpNode}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inbounds[0].TLS != nil || cfg.Inbounds[0].Transport != nil {
		t.Fatalf("plain TCP vmess must have no TLS/transport: %+v", cfg.Inbounds[0])
	}
	raw, _ = json.Marshal(cfg.Inbounds[0])
	if strings.Contains(string(raw), `"security"`) {
		t.Fatalf("vmess user must not carry a security field: %s", raw)
	}
}
