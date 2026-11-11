package singbox

import (
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
	// The ORTT route is only compiled when ORTT is enabled AND a secret is
	// configured (P0 fix); opt in explicitly for this assertion.
	t.Setenv("ORTT_ENABLED", "true")
	t.Setenv("ORTT_SECRET", "test-secret-32-bytes-for-unit-test")
	t.Setenv("SMATRIX_DATA_DIR", t.TempDir())
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
	expectedRules := 2
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
