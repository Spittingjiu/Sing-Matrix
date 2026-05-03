package singbox

import "testing"

func TestCustomAANodeCompilesAsOutbound(t *testing.T) {
	ui := UIData{
		Nodes: []UINode{
			{ID: "hy2", Kind: "inbound-hy2", Data: map[string]interface{}{"tag": "hy2-main", "port": 50123, "password": "pw"}},
			{ID: "aa", Kind: "AA", Label: "AA", Data: map[string]interface{}{"tag": "AA"}},
		},
		Edges: []UIEdge{{Source: "hy2", Target: "aa"}},
	}
	cfg, err := CompileToSingbox(ui)
	if err != nil { t.Fatal(err) }
	found := false
	for _, out := range cfg.Outbounds { if out.Tag == "AA" { found = true } }
	if !found { t.Fatalf("AA outbound not generated: %+v", cfg.Outbounds) }
	if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Outbound != "AA" { t.Fatalf("AA route not generated: %+v", cfg.Route.Rules) }
}
