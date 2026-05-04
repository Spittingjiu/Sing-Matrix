package singbox

import "testing"

func TestCompileTLSProtocolsRequireCertificates(t *testing.T) {
	for _, tc := range []struct{ kind string }{{"inbound-tuic"}, {"inbound-naive"}, {"inbound-hysteria"}, {"inbound-anytls"}} {
		_, err := CompileToSingbox(UIData{Nodes: []UINode{{ID: "n1", Kind: tc.kind, Data: map[string]interface{}{"tag": "test", "port": 39001, "password": "p"}}}})
		if err == nil {
			t.Fatalf("%s should require certificate paths", tc.kind)
		}
	}
}

func TestCompileExpandedProtocols(t *testing.T) {
	cert := "/tmp/cert.pem"
	key := "/tmp/key.pem"
	cases := []struct {
		kind string
		want string
		data map[string]interface{}
	}{
		{"inbound-tuic", "tuic", map[string]interface{}{"uuid": "00000000-0000-0000-0000-000000000000", "password": "p", "certificate_path": cert, "key_path": key}},
		{"inbound-naive", "naive", map[string]interface{}{"username": "u", "password": "p", "certificate_path": cert, "key_path": key}},
		{"inbound-hysteria", "hysteria", map[string]interface{}{"password": "p", "certificate_path": cert, "key_path": key, "up_mbps": 100, "down_mbps": 100}},
		{"inbound-shadowtls", "shadowtls", map[string]interface{}{"password": "p", "server_name": "www.microsoft.com"}},
		{"inbound-anytls", "anytls", map[string]interface{}{"password": "p", "certificate_path": cert, "key_path": key}},
	}
	for _, tc := range cases {
		data := map[string]interface{}{"tag": tc.want + "-in", "port": 0}
		for k, v := range tc.data { data[k] = v }
		cfg, err := CompileToSingbox(UIData{Nodes: []UINode{{ID: "n1", Kind: tc.kind, Data: data}}})
		if err != nil { t.Fatalf("%s compile failed: %v", tc.kind, err) }
		if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].Type != tc.want { t.Fatalf("%s got %+v", tc.kind, cfg.Inbounds) }
	}
}

func TestFinalMaskTransport(t *testing.T) {
	cfg, err := CompileToSingbox(UIData{Nodes: []UINode{{ID: "n1", Kind: "inbound-reality", Data: map[string]interface{}{"tag": "r", "port": 0, "finalmask": "/fm"}}}})
	if err != nil { t.Fatal(err) }
	if cfg.Inbounds[0].Transport == nil || cfg.Inbounds[0].Transport["path"] != "/fm" { t.Fatalf("finalmask not mapped: %+v", cfg.Inbounds[0].Transport) }
}
