package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// IssuedCert describes one ACME-managed certificate on disk.
type IssuedCert struct {
	Domain   string    `json:"domain"`
	CertPath string    `json:"cert_path"`
	KeyPath  string    `json:"key_path"`
	NotAfter time.Time `json:"not_after"`
	IssuedAt time.Time `json:"issued_at"`
}

// Status is what the UI shows about the local ACME setup.
type Status struct {
	ServerIP      string `json:"server_ip"`
	Mode          string `json:"mode"`
	Webroot       string `json:"webroot"`
	Email         string `json:"email"`
	CertDir       string `json:"cert_dir"`
	Port80Free    bool   `json:"port80_free"`
	AutoRedirect  bool   `json:"auto_redirect"`
	HTTPSListen   string `json:"https_listen"`
}

type acmeUser struct {
	email string
	key   crypto.PrivateKey
	reg   *registration.Resource
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// Manager issues and renews certificates over HTTP-01. It never needs a DNS
// provider credential: the operator points the domain at this host and the panel
// proves control by serving a token on port 80.
type Manager struct {
	CertDir string
	DataDir string
	Email   string
	Webroot string
	// HTTPSListen mirrors SMATRIX_HTTPS_ADDR so the UI can tell the user whether
	// the panel itself can serve the new domain over HTTPS.
	HTTPSListen string

	mu      sync.Mutex
	account *acmeUser
}

func NewManager(certDir, dataDir, email, webroot string) *Manager {
	if certDir == "" {
		certDir = "./certs"
	}
	if dataDir == "" {
		dataDir = "./data"
	}
	return &Manager{CertDir: certDir, DataDir: dataDir, Email: email, Webroot: webroot}
}

func (m *Manager) metaPath() string    { return filepath.Join(m.DataDir, "acme_certs.json") }
func (m *Manager) accountPath() string { return filepath.Join(m.DataDir, "acme_account.key") }

// Port80Free reports whether the panel could bind :80 itself.
func (m *Manager) Port80Free() bool { return CanBind(":80") }

// ChallengeMode names the HTTP-01 strategy in use.
func (m *Manager) ChallengeMode() string {
	if m.Port80Free() {
		return "standalone"
	}
	return "webroot"
}

// Status summarises the setup for the UI.
func (m *Manager) Status() Status {
	return Status{
		ServerIP:     DetectPublicIP(),
		Mode:         m.ChallengeMode(),
		Webroot:      m.Webroot,
		Email:        m.Email,
		CertDir:      m.CertDir,
		Port80Free:   m.Port80Free(),
		AutoRedirect: m.HTTPSListen != "",
		HTTPSListen:  m.HTTPSListen,
	}
}

func (m *Manager) loadAccountKey() (*ecdsa.PrivateKey, error) {
	if raw, err := os.ReadFile(m.accountPath()); err == nil {
		block, _ := pem.Decode(raw)
		if block != nil {
			switch block.Type {
			case "EC PRIVATE KEY":
				return x509.ParseECPrivateKey(block.Bytes)
			case "PRIVATE KEY":
				if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
					if ec, ok := k.(*ecdsa.PrivateKey); ok {
						return ec, nil
					}
				}
			}
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.DataDir, 0o755); err != nil {
		return nil, err
	}
	enc := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(m.accountPath(), enc, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// emailFor derives a contact address when the operator did not configure one.
// Let's Encrypt sends expiry notices here, so it must be the operator's own
// address rather than anything hardcoded by the project.
func (m *Manager) emailFor(domain string) string {
	if e := strings.TrimSpace(m.Email); e != "" {
		return e
	}
	return "acme@" + domain
}

// Obtain issues (or re-issues) a certificate for domain over HTTP-01.
func (m *Manager) Obtain(domain string) (IssuedCert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || !strings.Contains(domain, ".") {
		return IssuedCert{}, fmt.Errorf("invalid domain: %q", domain)
	}

	key, err := m.loadAccountKey()
	if err != nil {
		return IssuedCert{}, fmt.Errorf("load acme account key: %w", err)
	}
	user := &acmeUser{email: m.emailFor(domain), key: key}

	cfg := lego.NewConfig(user)
	cfg.CADirURL = lego.LEDirectoryProduction
	client, err := lego.NewClient(cfg)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("acme client: %w", err)
	}

	var provider challenge.Provider
	mode := "webroot"
	if m.Port80Free() {
		// Nothing else owns :80, so the panel can answer the challenge directly.
		if err := client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "80")); err != nil {
			return IssuedCert{}, fmt.Errorf("set http01 provider: %w", err)
		}
		mode = "standalone"
	} else {
		if strings.TrimSpace(m.Webroot) == "" {
			return IssuedCert{}, errors.New("80 端口被占用且未配置 webroot，无法完成 HTTP-01 校验")
		}
		provider = &WebrootProvider{Dir: m.Webroot}
		if err := os.MkdirAll(filepath.Join(m.Webroot, ".well-known", "acme-challenge"), 0o755); err != nil {
			return IssuedCert{}, fmt.Errorf("prepare webroot: %w", err)
		}
		if err := client.Challenge.SetHTTP01Provider(provider); err != nil {
			return IssuedCert{}, fmt.Errorf("set http01 provider: %w", err)
		}
	}
	log.Printf("[acme] issuing %s via %s", domain, mode)

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return IssuedCert{}, fmt.Errorf("acme register: %w", err)
	}
	user.reg = reg

	res, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	})
	if err != nil {
		return IssuedCert{}, fmt.Errorf("acme obtain: %w", err)
	}
	return m.persist(domain, res)
}

func (m *Manager) persist(domain string, res *certificate.Resource) (IssuedCert, error) {
	if err := os.MkdirAll(m.CertDir, 0o755); err != nil {
		return IssuedCert{}, err
	}
	name := strings.ReplaceAll(domain, "*", "wildcard")
	certPath := filepath.Join(m.CertDir, name+".pem")
	keyPath := filepath.Join(m.CertDir, name+".key")
	if err := os.WriteFile(certPath, res.Certificate, 0o644); err != nil {
		return IssuedCert{}, err
	}
	if err := os.WriteFile(keyPath, res.PrivateKey, 0o600); err != nil {
		return IssuedCert{}, err
	}
	now := time.Now()
	notAfter := now.Add(90 * 24 * time.Hour)
	if block, _ := pem.Decode(res.Certificate); block != nil {
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			notAfter = cert.NotAfter
		}
	}
	entry := IssuedCert{Domain: domain, CertPath: certPath, KeyPath: keyPath, NotAfter: notAfter, IssuedAt: now}
	meta, _ := m.LoadMeta()
	meta[domain] = entry
	if err := m.SaveMeta(meta); err != nil {
		return entry, err
	}
	return entry, nil
}

// LoadMeta reads the issued certificate index.
func (m *Manager) LoadMeta() (map[string]IssuedCert, error) {
	out := map[string]IssuedCert{}
	raw, err := os.ReadFile(m.metaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]IssuedCert{}, err
	}
	return out, nil
}

// SaveMeta writes the issued certificate index.
func (m *Manager) SaveMeta(meta map[string]IssuedCert) error {
	if err := os.MkdirAll(m.DataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.metaPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.metaPath())
}

// RenewAll re-issues certificates that expire within window.
func (m *Manager) RenewAll(window time.Duration) []string {
	meta, err := m.LoadMeta()
	if err != nil {
		log.Printf("[acme] read meta: %v", err)
		return nil
	}
	renewed := []string{}
	for domain, entry := range meta {
		if time.Until(entry.NotAfter) > window {
			continue
		}
		if _, err := m.Obtain(domain); err != nil {
			log.Printf("[acme] renew %s failed: %v", domain, err)
			continue
		}
		log.Printf("[acme] renewed %s", domain)
		renewed = append(renewed, domain)
	}
	return renewed
}

// StartRenewalLoop checks twice a day and renews anything within 30 days.
func (m *Manager) StartRenewalLoop() {
	go func() {
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		for {
			<-timer.C
			m.RenewAll(30 * 24 * time.Hour)
			timer.Reset(12 * time.Hour)
		}
	}()
}
