package httpsite

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Provider names the reverse proxy the panel found on this host.
type Provider string

const (
	Nginx Provider = "nginx"
	Caddy Provider = "caddy"
)

// Result describes what Publish actually changed.
type Result struct {
	Provider   string `json:"provider"`
	ConfigPath string `json:"config_path"`
	URL        string `json:"url"`
	Reloaded   bool   `json:"reloaded"`
	Backup     string `json:"backup,omitempty"`
}

// Detect reports which reverse proxy is installed and configured here.
func Detect() Provider {
	if _, err := exec.LookPath("nginx"); err == nil {
		if _, err := os.Stat("/etc/nginx/sites-available"); err == nil {
			return Nginx
		}
	}
	if _, err := exec.LookPath("caddy"); err == nil {
		if _, err := os.Stat("/etc/caddy/Caddyfile"); err == nil {
			return Caddy
		}
	}
	return ""
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// nginxConf renders a vhost that terminates TLS with the panel-issued
// certificate and forwards everything to the panel.
func nginxConf(domain, certPath, keyPath, upstream, webroot string) string {
	var b strings.Builder
	b.WriteString("# Managed by S-Matrix. Safe to delete.\n")
	b.WriteString("server {\n")
	b.WriteString("\tlisten 80;\n\tlisten [::]:80;\n")
	fmt.Fprintf(&b, "\tserver_name %s;\n", domain)
	if webroot != "" {
		b.WriteString("\tlocation /.well-known/acme-challenge/ {\n")
		fmt.Fprintf(&b, "\t\troot %s;\n", webroot)
		b.WriteString("\t}\n")
	}
	b.WriteString("\tlocation / { return 301 https://$host$request_uri; }\n")
	b.WriteString("}\n\n")
	b.WriteString("server {\n")
	b.WriteString("\tlisten 443 ssl;\n\tlisten [::]:443 ssl;\n")
	fmt.Fprintf(&b, "\tserver_name %s;\n", domain)
	fmt.Fprintf(&b, "\tssl_certificate %s;\n", certPath)
	fmt.Fprintf(&b, "\tssl_certificate_key %s;\n", keyPath)
	b.WriteString("\tlocation / {\n")
	fmt.Fprintf(&b, "\t\tproxy_pass http://%s;\n", upstream)
	b.WriteString("\t\tproxy_http_version 1.1;\n")
	b.WriteString("\t\tproxy_set_header Host $host;\n")
	b.WriteString("\t\tproxy_set_header X-Real-IP $remote_addr;\n")
	b.WriteString("\t\tproxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	b.WriteString("\t\tproxy_set_header X-Forwarded-Proto $scheme;\n")
	b.WriteString("\t\tproxy_set_header Upgrade $http_upgrade;\n")
	b.WriteString("\t\tproxy_set_header Connection \"upgrade\";\n")
	b.WriteString("\t\tproxy_read_timeout 3600;\n")
	b.WriteString("\t}\n}\n")
	return b.String()
}

func caddyBlock(domain, certPath, keyPath, upstream string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n# Managed by S-Matrix. Safe to delete.\n%s {\n", domain)
	fmt.Fprintf(&b, "\ttls %s %s\n", certPath, keyPath)
	fmt.Fprintf(&b, "\treverse_proxy %s\n", upstream)
	b.WriteString("}\n")
	return b.String()
}

// proxyUser returns the account the reverse proxy runs as, when it is not root.
func proxyUser(provider Provider) string {
	switch provider {
	case Caddy:
		for _, u := range []string{"caddy", "www-data"} {
			if _, err := exec.LookPath("id"); err == nil {
				if out, err := run("id", "-u", u); err == nil && strings.TrimSpace(out) != "" {
					return u
				}
			}
		}
	}
	return ""
}

// ensureReadable makes the key readable by the proxy user. The panel writes
// keys as 0600 root, which nginx tolerates (master runs as root) but Caddy does
// not - it loads certificates as the caddy user and the reload then fails.
func ensureReadable(provider Provider, certPath, keyPath string) error {
	user := proxyUser(provider)
	if user == "" {
		return nil
	}
	for _, f := range []string{certPath, keyPath} {
		if out, err := run("chown", "root:"+user, f); err != nil {
			return fmt.Errorf("无法把 %s 交给 %s 组: %s", filepath.Base(f), user, strings.TrimSpace(out))
		}
		if err := os.Chmod(f, 0o640); err != nil {
			return err
		}
	}
	return nil
}

// Publish writes (or updates) a TLS vhost for domain and reloads the proxy.
// Any existing file is backed up first and restored if validation fails, so a
// broken template can never take the host's web server down.
func Publish(domain, certPath, keyPath, upstream, webroot string) (Result, error) {
	provider := Detect()
	if provider == "" {
		return Result{}, errors.New("未检测到 nginx 或 Caddy，无法自动配置 HTTPS")
	}
	if _, err := os.Stat(certPath); err != nil {
		return Result{}, fmt.Errorf("证书不存在: %s", certPath)
	}
	if _, err := os.Stat(keyPath); err != nil {
		return Result{}, fmt.Errorf("私钥不存在: %s", keyPath)
	}
	if err := ensureReadable(provider, certPath, keyPath); err != nil {
		return Result{}, err
	}
	switch provider {
	case Nginx:
		return publishNginx(domain, certPath, keyPath, upstream, webroot)
	case Caddy:
		return publishCaddy(domain, certPath, keyPath, upstream)
	}
	return Result{}, errors.New("unsupported provider")
}

func publishNginx(domain, certPath, keyPath, upstream, webroot string) (Result, error) {
	name := "smatrix-" + domain + ".conf"
	available := filepath.Join("/etc/nginx/sites-available", name)
	enabled := filepath.Join("/etc/nginx/sites-enabled", name)

	res := Result{Provider: string(Nginx), ConfigPath: available, URL: "https://" + domain}
	if _, err := os.Stat(available); err == nil {
		res.Backup = available + ".bak"
		if err := copyFile(available, res.Backup); err != nil {
			return res, err
		}
	}
	if err := os.WriteFile(available, []byte(nginxConf(domain, certPath, keyPath, upstream, webroot)), 0o644); err != nil {
		return res, err
	}
	if err := os.Symlink(available, enabled); err != nil && !os.IsExist(err) {
		if _, statErr := os.Stat(enabled); statErr != nil {
			return res, fmt.Errorf("启用站点失败: %w", err)
		}
	}
	if out, err := run("nginx", "-t"); err != nil {
		activateBackup(res.Backup, available)
		return res, fmt.Errorf("nginx 配置校验失败: %s", strings.TrimSpace(out))
	}
	if out, err := run("nginx", "-s", "reload"); err != nil {
		return res, fmt.Errorf("nginx 重载失败: %s", strings.TrimSpace(out))
	}
	res.Reloaded = true
	return res, nil
}

func publishCaddy(domain, certPath, keyPath, upstream string) (Result, error) {
	const caddyfile = "/etc/caddy/Caddyfile"
	raw, err := os.ReadFile(caddyfile)
	if err != nil {
		return Result{}, fmt.Errorf("读取 Caddyfile 失败: %w", err)
	}
	res := Result{Provider: string(Caddy), ConfigPath: caddyfile, URL: "https://" + domain}

	body := string(raw)
	block := caddyBlock(domain, certPath, keyPath, upstream)
	marker := "\n" + domain + " {"
	if idx := strings.Index(body, marker); idx >= 0 {
		// Replace the existing managed block (from its comment line if present).
		start := idx
		if c := strings.LastIndex(body[:idx], "# Managed by S-Matrix."); c >= 0 && strings.TrimSpace(body[c:idx]) == "# Managed by S-Matrix. Safe to delete." {
			start = c
		}
		end := strings.Index(body[idx:], "\n}")
		if end < 0 {
			return res, errors.New("Caddyfile 中该域名的配置块不完整，请手动检查")
		}
		body = body[:start] + strings.TrimPrefix(block, "\n") + body[idx+end+2:]
	} else {
		body += block
	}

	res.Backup = caddyfile + ".bak"
	if err := copyFile(caddyfile, res.Backup); err != nil {
		return res, err
	}
	if err := os.WriteFile(caddyfile, []byte(body), 0o644); err != nil {
		return res, err
	}
	if out, err := run("caddy", "validate", "--config", caddyfile); err != nil {
		activateBackup(res.Backup, caddyfile)
		return res, fmt.Errorf("Caddyfile 校验失败: %s", strings.TrimSpace(out))
	}
	if err := reloadCaddy(); err != nil {
		return res, err
	}
	res.Reloaded = true
	return res, nil
}

func reloadCaddy() error {
	if out, err := run("systemctl", "reload", "caddy"); err == nil {
		return nil
	} else if strings.Contains(out, "not found") || strings.Contains(out, "not loaded") {
		if out2, err2 := run("caddy", "reload", "--config", "/etc/caddy/Caddyfile"); err2 != nil {
			return fmt.Errorf("caddy 重载失败: %s", strings.TrimSpace(out2))
		}
		return nil
	} else {
		return fmt.Errorf("caddy 重载失败: %s", strings.TrimSpace(out))
	}
}

// Remove deletes the managed vhost again.
func Remove(domain string) error {
	switch Detect() {
	case Nginx:
		name := "smatrix-" + domain + ".conf"
		if err := exec.Command("rm", "-f",
			filepath.Join("/etc/nginx/sites-enabled", name),
			filepath.Join("/etc/nginx/sites-available", name),
		).Run(); err != nil {
			return err
		}
		_, _ = run("nginx", "-s", "reload")
		return nil
	case Caddy:
		const caddyfile = "/etc/caddy/Caddyfile"
		raw, err := os.ReadFile(caddyfile)
		if err != nil {
			return err
		}
		body := string(raw)
		marker := "\n" + domain + " {"
		idx := strings.Index(body, marker)
		if idx < 0 {
			return nil
		}
		start := idx
		if c := strings.LastIndex(body[:idx], "# Managed by S-Matrix."); c >= 0 && strings.TrimSpace(body[c:idx]) == "# Managed by S-Matrix. Safe to delete." {
			start = c
		}
		end := strings.Index(body[idx:], "\n}")
		if end < 0 {
			return errors.New("配置块不完整")
		}
		if err := os.WriteFile(caddyfile, []byte(body[:start]+body[idx+end+2:]), 0o644); err != nil {
			return err
		}
		return reloadCaddy()
	}
	return nil
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

func activateBackup(backup, target string) {
	if backup == "" {
		return
	}
	if raw, err := os.ReadFile(backup); err == nil {
		_ = os.WriteFile(target, raw, 0o644)
	}
}
