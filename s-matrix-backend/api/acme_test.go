package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"s-matrix/core/acme"
)

func newACMEDeps(t *testing.T) ACMEDeps {
	t.Helper()
	dir := t.TempDir()
	webroot := filepath.Join(dir, "webroot")
	if err := os.MkdirAll(webroot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return ACMEDeps{Manager: acme.NewManager(filepath.Join(dir, "certs"), dir, "", webroot)}
}

func TestACMEStatusHasNoProviderCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.GET("/status", ACMEStatusHandler(deps))

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"mode"`, `"webroot"`, `"server_ip"`, `"cert_dir"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	// The whole point of the redesign: no DNS provider secret leaks through the API.
	for _, forbidden := range []string{"token_set", "cloudflare_ok", "cf_token", "api_token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("status must not expose %s: %s", forbidden, body)
		}
	}
}

func TestACMECheckRejectsUnresolvableDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.GET("/check", ACMECheckHandler(deps))

	req := httptest.NewRequest(http.MethodGet, "/check?domain=does-not-exist.invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"ok":false`) {
		t.Fatalf("expected failed validation: %s", body)
	}
	if !strings.Contains(body, `"reason"`) {
		t.Fatalf("expected a reason: %s", body)
	}
}

func TestACMECheckRequiresDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.GET("/check", ACMECheckHandler(deps))

	req := httptest.NewRequest(http.MethodGet, "/check", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestACMECertRejectsUnverifiedDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.POST("/issue", ACMECertHandler(deps))

	req := httptest.NewRequest(http.MethodPost, "/issue", strings.NewReader(`{"domain":"not-pointing-here.invalid"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"validation"`) {
		t.Fatalf("expected validation detail: %s", w.Body.String())
	}
}

func TestACMECertRequiresDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.POST("/issue", ACMECertHandler(deps))

	req := httptest.NewRequest(http.MethodPost, "/issue", strings.NewReader(`{"domain":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestACMEEmailRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.POST("/email", ACMEEmailHandler(deps))

	req := httptest.NewRequest(http.MethodPost, "/email", strings.NewReader(`{"email":"ops@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := loadEmail(deps.Manager); got != "ops@example.com" {
		t.Fatalf("email not persisted: %q", got)
	}

	bad := httptest.NewRequest(http.MethodPost, "/email", strings.NewReader(`{"email":"nope"}`))
	bad.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, bad)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid email should be rejected, got %d", w.Code)
	}
}
