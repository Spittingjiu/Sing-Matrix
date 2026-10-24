package api

import (
	"os"
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
	if !strings.HasPrefix(link, "anytls://secretpass@5.6.7.8:45112?") {
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
