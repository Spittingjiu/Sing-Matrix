package singbox

import (
	"os"
	"testing"
)

func TestCustomAANodeCompilesAsOutbound(t *testing.T) {
	certPath, keyPath, _, err := EnsureSelfSignedPair(t.TempDir(), "unit2", "www.bing.com", nil, []string{"www.bing.com"})
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	ui := UIData{
		Nodes: []UINode{
			{ID: "hy2", Kind: "inbound-hy2", Data: map[string]interface{}{"tag": "hy2-main", "port": 50123, "password": "pw", "certificate_path": certPath, "key_path": keyPath}},
			{ID: "aa", Kind: "AA", Label: "AA", Data: map[string]interface{}{"tag": "AA"}},
		},
		Edges: []UIEdge{{Source: "hy2", Target: "aa"}},
	}
	cfg, err := CompileToSingbox(ui)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, out := range cfg.Outbounds {
		if out.Tag == "AA" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AA outbound not generated: %+v", cfg.Outbounds)
	}
	expectedRules := 1
	if os.Getenv("ORTT_ENABLED") != "false" && os.Getenv("ORTT_ENABLED") != "0" {
		expectedRules = 2
	}
	if len(cfg.Route.Rules) != expectedRules {
		t.Fatalf("expected %d rules, got: %+v", expectedRules, cfg.Route.Rules)
	}
	aaIndex := 0
	if expectedRules == 2 {
		aaIndex = 1
	}
	if cfg.Route.Rules[aaIndex].Outbound != "AA" {
		t.Fatalf("AA route not generated: %+v", cfg.Route.Rules)
	}
}
