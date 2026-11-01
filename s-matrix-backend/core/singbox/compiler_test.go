package singbox

import (
	"os"
	"path/filepath"
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
	if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Inbound != "hy2-main" || cfg.Route.Rules[0].RuleSet != "youtube" || cfg.Route.Rules[0].Outbound != "direct" {
		t.Fatalf("bad route: %+v", cfg.Route)
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
