package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
)

// resolveCertDir returns the directory used for panel-managed certificates.
func resolveCertDir(configured string) string {
	if configured != "" {
		return configured
	}
	if env := strings.TrimSpace(os.Getenv("SMATRIX_CERT_DIR")); env != "" {
		return env
	}
	return "./certs"
}

// ListCertsHandler lists panel-managed certificate pairs.
func ListCertsHandler(certDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		dir := resolveCertDir(certDir)
		certs, err := singbox.ListCerts(dir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "dir": dir, "certs": certs})
	}
}

// SelfSignedCertHandler creates (or reuses) a self-signed certificate pair and
// returns the paths, so the UI can fill certificate_path / key_path.
func SelfSignedCertHandler(certDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name string `json:"name"`
			SNI  string `json:"sni"`
		}
		_ = c.ShouldBindJSON(&req)
		name := strings.TrimSpace(req.Name)
		sni := strings.TrimSpace(req.SNI)
		if sni == "" {
			sni = "www.bing.com"
		}
		if name == "" {
			name = "self-" + singbox.SafeCertName(sni)
		}
		dir := resolveCertDir(certDir)
		certPath, keyPath, created, err := singbox.EnsureSelfSignedPair(dir, name, sni, singbox.LocalIPs(), []string{sni})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":              true,
			"name":            singbox.SafeCertName(name),
			"cert_path":       certPath,
			"key_path":        keyPath,
			"created":         created,
			"common_name":     sni,
			"certificate_dir": dir,
		})
	}
}
