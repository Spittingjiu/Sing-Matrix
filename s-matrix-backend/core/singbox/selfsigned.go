package singbox

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SelfSignedPair holds a freshly generated self-signed certificate.
type SelfSignedPair struct {
	CertPEM []byte
	KeyPEM  []byte
}

// GenerateSelfSignedPair creates a long-lived self-signed certificate suitable
// for proxy inbounds (AnyTLS / TUIC / Naive / Hysteria). Clients are expected to
// use skip-cert-verify, so the CN/SAN mainly matters for plausibility and for
// clients that do check the certificate against the dialed IP.
func GenerateSelfSignedPair(commonName string, sanIPs []string, sanDNS []string, days int) (*SelfSignedPair, error) {
	if days <= 0 {
		days = 3650
	}
	if commonName == "" {
		commonName = "s-matrix"
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"S-Matrix"}},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	for _, d := range sanDNS {
		d = strings.TrimSpace(d)
		if d != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, d)
		}
	}
	for _, ip := range sanIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		if parsed := net.ParseIP(ip); parsed != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, parsed)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &SelfSignedPair{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

// EnsureSelfSignedPair writes a self-signed certificate into dir/name.{pem,key}.
// When the files already exist they are reused, so repeated create calls are
// idempotent and the node keeps its identity.
func EnsureSelfSignedPair(dir, name, commonName string, sanIPs []string, sanDNS []string) (certPath, keyPath string, created bool, err error) {
	safe := SafeCertName(name)
	if safe == "" {
		safe = "self-signed"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", false, err
	}
	certPath = filepath.Join(dir, safe+".pem")
	keyPath = filepath.Join(dir, safe+".key")
	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, false, nil
	}
	pair, err := GenerateSelfSignedPair(commonName, sanIPs, sanDNS, 3650)
	if err != nil {
		return "", "", false, err
	}
	if err := os.WriteFile(certPath, pair.CertPEM, 0o644); err != nil {
		return "", "", false, err
	}
	if err := os.WriteFile(keyPath, pair.KeyPEM, 0o600); err != nil {
		return "", "", false, err
	}
	return certPath, keyPath, true, nil
}

var certNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// SafeCertName converts a free-form remark into a filesystem-safe name.
func SafeCertName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = certNameUnsafe.ReplaceAllString(name, "-")
	return strings.Trim(name, "-.")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// CertInfo is a lightweight certificate listing entry.
type CertInfo struct {
	Name       string `json:"name"`
	CertPath   string `json:"cert_path"`
	KeyPath    string `json:"key_path"`
	Subject    string `json:"subject"`
	DNSNames   string `json:"dns_names"`
	NotAfter   string `json:"not_after"`
	SelfSigned bool   `json:"self_signed"`
}

// ListCerts scans dir for *.pem files with a matching *.key.
func ListCerts(dir string) ([]CertInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []CertInfo{}, nil
		}
		return nil, err
	}
	out := make([]CertInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pem") {
			continue
		}
		certPath := filepath.Join(dir, e.Name())
		keyPath := strings.TrimSuffix(certPath, ".pem") + ".key"
		if !fileExists(keyPath) {
			continue
		}
		info := CertInfo{
			Name:     strings.TrimSuffix(e.Name(), ".pem"),
			CertPath: certPath,
			KeyPath:  keyPath,
		}
		if raw, err := os.ReadFile(certPath); err == nil {
			if block, _ := pem.Decode(raw); block != nil {
				if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
					info.Subject = cert.Subject.CommonName
					info.DNSNames = strings.Join(cert.DNSNames, ",")
					info.NotAfter = cert.NotAfter.Format(time.RFC3339)
					info.SelfSigned = cert.Issuer.CommonName == cert.Subject.CommonName
				}
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// LocalIPs returns non-loopback IPv4 addresses of this host, used for SANs.
func LocalIPs() []string {
	ips := []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}

// DescribePair returns a short human readable summary for API responses.
func DescribePair(certPath string) string {
	return fmt.Sprintf("self-signed certificate at %s", certPath)
}
