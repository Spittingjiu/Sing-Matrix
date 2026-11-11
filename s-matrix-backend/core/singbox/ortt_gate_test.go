package singbox

import (
	"net"
	"strconv"
	"testing"
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

func TestCompileToSingbox_OrttGate(t *testing.T) {
	mkUI := func(t *testing.T, dir string) UIData {
		t.Helper()
		certPath, keyPath, _, err := EnsureSelfSignedPair(dir, "gate", "www.bing.com", nil, []string{"www.bing.com"})
		if err != nil {
			t.Fatalf("cert: %v", err)
		}
		return UIData{
			Nodes: []UINode{
				{ID: "hy2", Kind: "inbound-hy2", Data: map[string]interface{}{"tag": "hy2-main", "port": 44300, "password": "pw", "certificate_path": certPath, "key_path": keyPath}},
				{ID: "rule", Kind: "rule-srs", Data: map[string]interface{}{"tag": "youtube", "url": "https://example.com/youtube.srs"}},
				{ID: "out", Kind: "outbound-direct", Data: map[string]interface{}{"tag": "direct"}},
			},
			Edges: []UIEdge{{Source: "hy2", Target: "rule"}, {Source: "rule", Target: "out"}},
		}
	}
	cases := []struct {
		name      string
		ortt      string
		accel     string
		secret    string
		listen    bool
		wantRoute bool
	}{
		{"enabled with secret adds route", "true", "", "test-secret-32", true, true},
		{"enabled without secret adds no route", "true", "", "", true, false},
		{"disabled adds no route", "false", "", "test-secret-32", true, false},
		{"accel false suppresses route", "true", "false", "test-secret-32", true, false},
		{"default off adds no route", "", "", "test-secret-32", true, false},
		{"engine down with secret adds no route", "true", "", "test-secret-32", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("SMATRIX_DATA_DIR", dir)
			t.Setenv("ORTT_ENABLED", tc.ortt)
			t.Setenv("SMATRIX_ACCEL_ENABLED", tc.accel)
			t.Setenv("ORTT_SECRET", tc.secret)
			t.Setenv("SMATRIX_ACCEL_SECRET", "")
			if tc.listen {
				t.Setenv("ORTT_FORWARD_PORT", startTestListener(t))
			} else {
				t.Setenv("ORTT_FORWARD_PORT", closedTestPort(t))
			}
			cfg, err := CompileToSingbox(mkUI(t, dir))
			if err != nil {
				t.Fatal(err)
			}
			got := len(cfg.Route.Rules) > 0 && cfg.Route.Rules[0].Outbound == "ortt-warm-pool"
			if got != tc.wantRoute {
				t.Fatalf("ortt route present = %v, want %v (rules: %+v)", got, tc.wantRoute, cfg.Route.Rules)
			}
		})
	}
}
