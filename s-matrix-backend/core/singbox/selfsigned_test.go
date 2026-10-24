package singbox

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSelfSignedPair(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, created, err := EnsureSelfSignedPair(dir, "unit-test", "www.bing.com", []string{"127.0.0.1"}, []string{"www.bing.com"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first call")
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert missing: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key missing: %v", err)
	}
	raw, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("cert is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if cert.Subject.CommonName != "www.bing.com" {
		t.Fatalf("unexpected CN: %s", cert.Subject.CommonName)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "www.bing.com" {
		t.Fatalf("unexpected DNS SANs: %v", cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Fatalf("unexpected IP SANs: %v", cert.IPAddresses)
	}

	// Second call must reuse the existing pair instead of rotating identity.
	certPath2, keyPath2, created2, err := EnsureSelfSignedPair(dir, "unit-test", "www.bing.com", nil, nil)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if created2 {
		t.Fatalf("expected created=false on second call")
	}
	if certPath2 != certPath || keyPath2 != keyPath {
		t.Fatalf("paths changed: %s/%s vs %s/%s", certPath, keyPath, certPath2, keyPath2)
	}
}

func TestSafeCertName(t *testing.T) {
	cases := map[string]string{
		"AnyTLS :45112": "anytls-45112",
		"../etc/passwd": "etc-passwd",
		"":              "",
		"JP 节点 01":      "jp-01",
	}
	for in, want := range cases {
		if got := SafeCertName(in); got != want {
			t.Fatalf("SafeCertName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestListCerts(t *testing.T) {
	dir := t.TempDir()
	if _, _, _, err := EnsureSelfSignedPair(dir, "alpha", "a.example.com", nil, []string{"a.example.com"}); err != nil {
		t.Fatalf("generate alpha: %v", err)
	}
	// A pem without a key must be ignored.
	_ = os.WriteFile(filepath.Join(dir, "orphan.pem"), []byte("not a cert"), 0o644)
	certs, err := ListCerts(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(certs) != 1 || certs[0].Name != "alpha" {
		t.Fatalf("unexpected certs: %+v", certs)
	}
	if !certs[0].SelfSigned {
		t.Fatalf("expected self_signed=true: %+v", certs[0])
	}
}
