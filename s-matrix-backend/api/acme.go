package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"s-matrix/core/acme"
)

// ACMEDeps carries what the domain/certificate endpoints need.
type ACMEDeps struct {
	Manager *acme.Manager
	DataDir string
}

func cfTokenPath(dataDir string) string {
	return filepath.Join(dataDir, "cf_token")
}

// cfToken resolves the Cloudflare token from the environment first, then from
// the file the panel writes when the operator pastes one in the UI.
func cfToken(dataDir string) string {
	if v := strings.TrimSpace(os.Getenv("SMATRIX_CF_TOKEN")); v != "" {
		return v
	}
	raw, err := os.ReadFile(cfTokenPath(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// ACMEZonesHandler reports the token state plus the zones it can manage.
func ACMEZonesHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := cfToken(dep.DataDir)
		out := gin.H{
			"ok":            true,
			"token_set":     token != "",
			"server_ip":     acme.DetectPublicIP(),
			"cert_dir":      dep.Manager.CertDir,
			"acme_email":    dep.Manager.Email,
			"zones":         []acme.Zone{},
			"cloudflare_ok": false,
		}
		if token == "" {
			c.JSON(http.StatusOK, out)
			return
		}
		zones, err := acme.NewDNS(token).Zones()
		if err != nil {
			out["error"] = err.Error()
			c.JSON(http.StatusOK, out)
			return
		}
		out["zones"] = zones
		out["cloudflare_ok"] = true
		c.JSON(http.StatusOK, out)
	}
}

// ACMETokenHandler stores the Cloudflare API token used for DNS-01.
func ACMETokenHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad json"})
			return
		}
		token := strings.TrimSpace(req.Token)
		if token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "token required"})
			return
		}
		zones, err := acme.NewDNS(token).Zones()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "token 校验失败: " + err.Error()})
			return
		}
		if err := os.MkdirAll(dep.DataDir, 0o755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		if err := os.WriteFile(cfTokenPath(dep.DataDir), []byte(token), 0o600); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "zones": zones, "zone_count": len(zones)})
	}
}

// ACMECertHandler issues a Let's Encrypt certificate over Cloudflare DNS-01 and
// (optionally) points the name at this host first.
func ACMECertHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Domain  string `json:"domain"`
			IP      string `json:"ip"`
			SkipDNS bool   `json:"skip_dns"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad json"})
			return
		}
		domain := strings.ToLower(strings.TrimSpace(req.Domain))
		if domain == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "domain required"})
			return
		}
		ip := strings.TrimSpace(req.IP)
		dns := acme.NewDNS(cfToken(dep.DataDir))
		dnsUpdated := false
		if !req.SkipDNS {
			if ip == "" {
				ip = acme.DetectPublicIP()
			}
			if ip == "" {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "无法探测本机公网 IP，请手动填写"})
				return
			}
			rec, changed, err := dns.EnsureARecord(domain, ip)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "DNS 记录写入失败: " + err.Error()})
				return
			}
			dnsUpdated = changed
			if rec.Name == "" {
				dnsUpdated = true
			}
		}
		cert, err := dep.Manager.Obtain(domain)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": err.Error(), "dns_updated": dnsUpdated, "ip": ip})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"domain":      cert.Domain,
			"cert_path":   cert.CertPath,
			"key_path":    cert.KeyPath,
			"not_after":   cert.NotAfter.Format(time.RFC3339),
			"ip":          ip,
			"dns_updated": dnsUpdated,
		})
	}
}

// ACMECertListHandler lists certificates the panel issued.
func ACMECertListHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		meta, err := dep.Manager.LoadMeta()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		list := make([]acme.IssuedCert, 0, len(meta))
		for _, v := range meta {
			list = append(list, v)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "certs": list})
	}
}
