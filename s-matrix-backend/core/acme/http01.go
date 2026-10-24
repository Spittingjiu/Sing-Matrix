package acme

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WebrootProvider serves HTTP-01 challenges from a directory instead of binding
// port 80 itself. The reverse proxy in front of the panel (nginx/Caddy/anything)
// only has to expose /.well-known/acme-challenge/ from the same directory, which
// keeps the panel free of any third-party DNS credentials.
type WebrootProvider struct {
	Dir string
}

func (w *WebrootProvider) challengePath(token string) string {
	return filepath.Join(w.Dir, ".well-known", "acme-challenge", token)
}

func (w *WebrootProvider) Present(domain, token, keyAuth string) error {
	path := w.challengePath(token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(keyAuth), 0o644)
}

func (w *WebrootProvider) CleanUp(domain, token, keyAuth string) error {
	err := os.Remove(w.challengePath(token))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Timeout keeps the ACME client polling long enough for a slow proxy.
func (w *WebrootProvider) Timeout() (time.Duration, time.Duration) {
	return 90 * time.Second, 4 * time.Second
}

// ServeHTTP answers challenge requests when the panel itself owns port 80.
func (w *WebrootProvider) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	const prefix = "/.well-known/acme-challenge/"
	if !strings.HasPrefix(req.URL.Path, prefix) {
		http.NotFound(rw, req)
		return
	}
	token := strings.TrimPrefix(req.URL.Path, prefix)
	if token == "" || strings.ContainsAny(token, `/\`) {
		http.NotFound(rw, req)
		return
	}
	f, err := os.Open(w.challengePath(token))
	if err != nil {
		http.NotFound(rw, req)
		return
	}
	defer f.Close()
	rw.Header().Set("Content-Type", "text/plain")
	_, _ = io.Copy(rw, f)
}

// CanBind reports whether the given address can be listened on right now. It is
// used to decide between the standalone and webroot HTTP-01 modes.
func CanBind(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// ResolvePublicIPs returns the addresses a domain currently points at.
func ResolvePublicIPs(domain string) ([]string, error) {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if domain == "" {
		return nil, errors.New("empty domain")
	}
	addrs, err := net.LookupHost(domain)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil {
			out = append(out, ip.String())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s resolved to no address", domain)
	}
	return out, nil
}

// LocalIPs lists this host's addresses, including loopback, used for
// certificate SANs.
func LocalIPs() []string {
	out := []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			out = append(out, ipnet.IP.String())
		}
	}
	return out
}

// publicLocalIPs lists interface addresses a domain could legitimately point at:
// global unicast only, so docker bridges and loopback do not weaken validation.
func publicLocalIPs() []string {
	out := []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || ip.IsUnspecified() {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

// DetectPublicIP returns the egress IPv4 of this host, falling back to the
// primary interface address.
func DetectPublicIP() string {
	for _, url := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://api.ip.sb/ip"} {
		client := &http.Client{Timeout: 6 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

// Validation explains whether a domain may be issued a certificate here.
type Validation struct {
	Domain   string   `json:"domain"`
	OK       bool     `json:"ok"`
	Resolved []string `json:"resolved"`
	Expected []string `json:"expected"`
	Reason   string   `json:"reason,omitempty"`
}

// ValidateDomain checks that the domain currently resolves to this host. That is
// the only honest precondition for HTTP-01: the CA must be able to reach us on
// port 80 through the same name.
func ValidateDomain(domain string, extraIPs ...string) Validation {
	v := Validation{Domain: strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))}
	if !strings.Contains(v.Domain, ".") || strings.HasPrefix(v.Domain, ".") {
		v.Reason = "域名格式不正确"
		return v
	}
	expected := map[string]bool{}
	for _, ip := range extraIPs {
		if ip = strings.TrimSpace(ip); ip != "" {
			expected[ip] = true
			v.Expected = append(v.Expected, ip)
		}
	}
	for _, ip := range publicLocalIPs() {
		if !expected[ip] {
			expected[ip] = true
			v.Expected = append(v.Expected, ip)
		}
	}
	resolved, err := ResolvePublicIPs(v.Domain)
	if err != nil {
		v.Reason = "DNS 解析失败：" + err.Error()
		return v
	}
	v.Resolved = resolved
	for _, ip := range resolved {
		if expected[ip] {
			v.OK = true
			return v
		}
	}
	v.Reason = fmt.Sprintf("域名解析到 %s，与本机地址 %s 不一致；请先把 A 记录指向本机（并关闭 CDN 代理）",
		strings.Join(resolved, ", "), strings.Join(v.Expected, ", "))
	return v
}
