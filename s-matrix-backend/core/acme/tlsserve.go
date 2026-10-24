package acme

import (
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TLSProvider serves HTTPS with the certificates this panel issued, picking one
// by SNI. It lets a standalone installation expose the panel itself over HTTPS
// after a domain is verified, without a reverse proxy.
type TLSProvider struct {
	CertDir string

	mu    sync.RWMutex
	cache map[string]*tls.Certificate
}

func NewTLSProvider(certDir string) *TLSProvider {
	return &TLSProvider{CertDir: certDir, cache: map[string]*tls.Certificate{}}
}

func (p *TLSProvider) certPath(domain string) (string, string, bool) {
	base := filepath.Join(p.CertDir, strings.ReplaceAll(domain, "*", "wildcard"))
	cert, key := base+".pem", base+".key"
	if _, err := os.Stat(cert); err != nil {
		return "", "", false
	}
	if _, err := os.Stat(key); err != nil {
		return "", "", false
	}
	return cert, key, true
}

// load returns a cached certificate or reads it from disk.
func (p *TLSProvider) load(domain string) (*tls.Certificate, error) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	p.mu.RLock()
	if cert, ok := p.cache[domain]; ok {
		p.mu.RUnlock()
		return cert, nil
	}
	p.mu.RUnlock()

	certPath, keyPath, ok := p.certPath(domain)
	if !ok {
		return nil, os.ErrNotExist
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.cache[domain] = &cert
	p.mu.Unlock()
	return &cert, nil
}

// Invalidate drops a cached certificate, used after a renewal.
func (p *TLSProvider) Invalidate(domain string) {
	p.mu.Lock()
	delete(p.cache, strings.ToLower(strings.TrimSuffix(domain, ".")))
	p.mu.Unlock()
}

// GetCertificate implements tls.Config.GetCertificate.
func (p *TLSProvider) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
	if name != "" {
		if cert, err := p.load(name); err == nil {
			return cert, nil
		}
	}
	// No SNI or unknown name: fall back to the first certificate on disk so the
	// panel still answers instead of failing the handshake.
	entries, err := os.ReadDir(p.CertDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pem") {
			continue
		}
		domain := strings.TrimSuffix(e.Name(), ".pem")
		if cert, err := p.load(domain); err == nil {
			return cert, nil
		}
	}
	return nil, os.ErrNotExist
}

// ServeHTTP is the plain-HTTP side: it answers ACME challenges and redirects
// everything else to HTTPS.
func (p *TLSProvider) RedirectHandler(challenge http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/.well-known/acme-challenge/") && challenge != nil {
			challenge.ServeHTTP(rw, req)
			return
		}
		host := req.Host
		if h, _, ok := strings.Cut(host, ":"); ok {
			host = h
		}
		target := "https://" + host + req.URL.RequestURI()
		http.Redirect(rw, req, target, http.StatusMovedPermanently)
	})
}

// TLSConfig builds a tls.Config that serves the panel's own certificates.
func (p *TLSProvider) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: p.GetCertificate,
	}
}
