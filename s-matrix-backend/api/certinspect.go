package api

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"sync"
)

var (
	selfSignedMu    sync.Mutex
	selfSignedCache = map[string]bool{}
)

// certIsSelfSigned reports whether the certificate at path was self-issued.
// Share links use it to decide whether the client needs insecure/skip-cert-verify:
// an ACME certificate must never teach a client to skip verification.
func certIsSelfSigned(path string) bool {
	if path == "" {
		return true
	}
	selfSignedMu.Lock()
	if v, ok := selfSignedCache[path]; ok {
		selfSignedMu.Unlock()
		return v
	}
	selfSignedMu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	selfSigned := cert.Issuer.String() == cert.Subject.String()

	selfSignedMu.Lock()
	selfSignedCache[path] = selfSigned
	selfSignedMu.Unlock()
	return selfSigned
}

// tlsCertPathFromContext extracts certificate_path from a TLS map.
func tlsCertPathFromContext(tlsMap map[string]interface{}) string {
	if tlsMap == nil {
		return ""
	}
	if v, ok := tlsMap["certificate_path"].(string); ok {
		return v
	}
	return ""
}
