package api

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"s-matrix/core/acme"
)

// ACMEDeps carries what the domain/certificate endpoints need.
type ACMEDeps struct {
	Manager *acme.Manager
}

// acmeEmailPath stores the optional contact address an operator configures.
func acmeEmailPath(m *acme.Manager) string {
	return m.DataDir + "/acme_email"
}

// loadEmail prefs the configured contact address, if any.
func loadEmail(m *acme.Manager) string {
	raw, err := os.ReadFile(acmeEmailPath(m))
	if err != nil {
		return m.Email
	}
	return strings.TrimSpace(string(raw))
}

// ACMEStatusHandler reports how this installation can obtain certificates.
// There are no provider credentials involved: HTTP-01 only needs port 80 to
// reach this host under the requested name.
func ACMEStatusHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := dep.Manager.Status()
		if e := loadEmail(dep.Manager); e != "" {
			st.Email = e
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": st})
	}
}

// ACMEEmailHandler stores an optional ACME contact address.
func ACMEEmailHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad json"})
			return
		}
		email := strings.TrimSpace(req.Email)
		if email != "" && !strings.Contains(email, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "邮箱格式不正确"})
			return
		}
		if email == "" {
			_ = os.Remove(acmeEmailPath(dep.Manager))
		} else {
			if err := os.WriteFile(acmeEmailPath(dep.Manager), []byte(email), 0o600); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "email": email})
	}
}

// ACMECheckHandler validates that a domain already points at this host.
// Nothing is issued here; the UI uses it to give a precise reason before the
// operator waits through an ACME order.
func ACMECheckHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		domain := strings.TrimSpace(c.Query("domain"))
		if domain == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "domain required"})
			return
		}
		v := acme.ValidateDomain(domain, acme.DetectPublicIP())
		c.JSON(http.StatusOK, gin.H{"ok": v.OK, "validation": v, "mode": dep.Manager.ChallengeMode()})
	}
}

// ACMECertHandler issues a certificate once the domain is verified.
func ACMECertHandler(dep ACMEDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Domain string `json:"domain"`
			Force  bool   `json:"force"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad json"})
			return
		}
		domain := strings.ToLower(strings.TrimSpace(req.Domain))
		if domain == "" || !strings.Contains(domain, ".") {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "请填写完整域名，例如 node1.example.com"})
			return
		}
		v := acme.ValidateDomain(domain, acme.DetectPublicIP())
		if !v.OK && !req.Force {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": v.Reason, "validation": v})
			return
		}
		if dep.Manager.ChallengeMode() == "webroot" && strings.TrimSpace(dep.Manager.Webroot) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "80 端口被占用，且未配置 ACME webroot 目录"})
			return
		}
		cert, err := dep.Manager.Obtain(domain)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": err.Error(), "validation": v})
			return
		}
		st := dep.Manager.Status()
		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"domain":      cert.Domain,
			"cert_path":   cert.CertPath,
			"key_path":    cert.KeyPath,
			"not_after":   cert.NotAfter.Format(time.RFC3339),
			"https_url":   "https://" + domain,
			"auto_redirect": st.AutoRedirect,
			"mode":        st.Mode,
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
