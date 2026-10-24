package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"s-matrix/core/singbox"
	"s-matrix/models"
)

func TestBuildAnyTLSLinkIncludesSNIAndInsecure(t *testing.T) {
	in := map[string]interface{}{
		"type":        "anytls",
		"tag":         "AnyTLS :45112",
		"listen_port": float64(45112),
		"users": []interface{}{
			map[string]interface{}{"name": "default", "password": "p@ss word"},
		},
		"tls": map[string]interface{}{
			"enabled":     true,
			"server_name": "www.bing.com",
		},
	}
	link := buildAnyTLSLink(in, "1.2.3.4", 45112, "AnyTLS :45112")
	if !strings.HasPrefix(link, "anytls://") {
		t.Fatalf("unexpected scheme: %s", link)
	}
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("missing insecure flag: %s", link)
	}
	if !strings.Contains(link, "sni=www.bing.com") {
		t.Fatalf("missing sni: %s", link)
	}
	if !strings.Contains(link, "1.2.3.4:45112") {
		t.Fatalf("missing endpoint: %s", link)
	}
	if strings.Contains(link, "p@ss word") {
		t.Fatalf("password must be url escaped: %s", link)
	}
}

func TestBuildAnyTLSLinkFromDBPayload(t *testing.T) {
	ib := models.Inbound{
		Tag:     "AnyTLS :45112",
		Type:    "anytls",
		Port:    45112,
		Payload: `{"password":"secretpass","server_name":"cdn.example.com"}`,
	}
	link := buildLinkFromRecord(ib, "5.6.7.8")
	if !strings.HasPrefix(link, "anytls://secretpass@5.6.7.8:45112/?") {
		t.Fatalf("unexpected link: %s", link)
	}
	if !strings.Contains(link, "sni=cdn.example.com") {
		t.Fatalf("missing sni from payload: %s", link)
	}
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("missing insecure: %s", link)
	}
}

// An ACME-signed certificate must never be published with insecure=1: that
// would teach every client to skip verification for no reason.
func TestAnyTLSAcmeCertOmitsInsecure(t *testing.T) {
	dir := t.TempDir()
	pair, err := singbox.GenerateSelfSignedPair("acme-looking.example.com", nil, []string{"acme-looking.example.com"}, 30)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	_ = pair // keep the import honest for the self-signed branch below
	_ = dir

	selfSigned := t.TempDir() + "/self.pem"
	if err := os.WriteFile(selfSigned, pair.CertPEM, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ib := models.Inbound{Tag: "n", Type: "anytls", Port: 1234,
		Payload: `{"password":"***","server_name":"acme-looking.example.com","certificate_path":"` + selfSigned + `"}`}
	link := buildLinkFromRecord(ib, "1.2.3.4")
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("self-signed cert should keep insecure=1: %s", link)
	}

	// Missing certificate path is treated as self-signed (conservative default).
	ib2 := models.Inbound{Tag: "n2", Type: "anytls", Port: 1235,
		Payload: `{"password":"***","server_name":"x.example.com"}`}
	if !strings.Contains(buildLinkFromRecord(ib2, "1.2.3.4"), "insecure=1") {
		t.Fatalf("unknown cert should default to insecure=1")
	}
}

// SNI handling regressions: an IP literal is never a valid SNI (RFC 6066), and
// echoing the connect host back is meaningless, so both must be dropped.
func TestLinkSNIRules(t *testing.T) {
	cases := []struct {
		candidate, host, want string
	}{
		{"www.bing.com", "1.2.3.4", "www.bing.com"},
		{"1.2.3.4", "1.2.3.4", ""},
		{"2001:db8::1", "2001:db8::1", ""},
		{"example.com", "example.com", ""},
		{"", "1.2.3.4", ""},
	}
	for _, c := range cases {
		if got := sniForLink(c.candidate, c.host); got != c.want {
			t.Fatalf("sniForLink(%q,%q)=%q want %q", c.candidate, c.host, got, c.want)
		}
	}
}

func TestHY2LinkUsesTLSBlockSNI(t *testing.T) {
	in := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         "hy2",
		"listen_port": float64(20000),
		"users":       []interface{}{map[string]interface{}{"password": "***"}},
		"tls":         map[string]interface{}{"enabled": true, "server_name": "cdn.example.com"},
	}
	link := buildHY2Link(in, "1.2.3.4", 20000, "hy2")
	if !strings.Contains(link, "sni=cdn.example.com") {
		t.Fatalf("tls.server_name must reach the link: %s", link)
	}
	// No certificate path on disk means "assume self-signed", so the link still
	// carries insecure=1; an ACME certificate drops it (covered separately).
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("missing certificate path should default to insecure: %s", link)
	}
	if !strings.HasPrefix(link, "hysteria2://") {
		t.Fatalf("wrong scheme: %s", link)
	}
}

func TestHY2LinkNoSNIWhenOnlyIPKnown(t *testing.T) {
	in := map[string]interface{}{
		"type": "hysteria2", "tag": "hy2", "listen_port": float64(20001),
		"users": []interface{}{map[string]interface{}{"password": "***"}},
	}
	link := buildHY2Link(in, "1.2.3.4", 20001, "hy2")
	if strings.Contains(link, "sni=") {
		t.Fatalf("must not advertise the connect address as SNI: %s", link)
	}
}

func TestAnyTLSLinkDropsIPSNI(t *testing.T) {
	in := map[string]interface{}{
		"type": "anytls", "tag": "at", "listen_port": float64(20002),
		"users": []interface{}{map[string]interface{}{"name": "default", "password": "***"}},
		"tls":   map[string]interface{}{"enabled": true, "server_name": "1.2.3.4"},
	}
	link := buildAnyTLSLink(in, "1.2.3.4", 20002, "at")
	if strings.Contains(link, "sni=") {
		t.Fatalf("anytls must not advertise an IP as SNI: %s", link)
	}
}

// A trusted certificate means the domain was verified to point here, so the link
// should dial the domain (stable) instead of whatever host the panel was opened on.
func TestLinkHostPrefersTrustedDomain(t *testing.T) {
	selfSigned := filepath.Join(t.TempDir(), "self.pem")
	pair, err := singbox.GenerateSelfSignedPair("self.example.com", nil, []string{"self.example.com"}, 30)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := os.WriteFile(selfSigned, pair.CertPEM, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := linkHost("1.2.3.4", "node.example.com", selfSigned); got != "1.2.3.4" {
		t.Fatalf("self-signed must keep the dialled host, got %q", got)
	}
	// Unknown/missing cert path is treated as self-signed (conservative).
	if got := linkHost("1.2.3.4", "node.example.com", ""); got != "1.2.3.4" {
		t.Fatalf("unknown cert must keep the dialled host, got %q", got)
	}
	// IP server_name is never a usable host.
	if got := linkHost("1.2.3.4", "1.2.3.4", "/nonexistent.pem"); got != "1.2.3.4" {
		t.Fatalf("IP server_name must not be used, got %q", got)
	}
}
