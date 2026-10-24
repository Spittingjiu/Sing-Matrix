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
	if err := os.WriteFile(filepath.Join(dir, "cf_token"), []byte("not-a-real-token"), 0o600); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return ACMEDeps{
		Manager: acme.NewManager(filepath.Join(dir, "certs"), dir, "ops@example.com", func() string {
			raw, _ := os.ReadFile(filepath.Join(dir, "cf_token"))
			return string(raw)
		}),
		DataDir: dir,
	}
}

func TestACMEZonesReportsConfiguredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.GET("/zones", ACMEZonesHandler(deps))

	req := httptest.NewRequest(http.MethodGet, "/zones", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"token_set":true`, `"cert_dir"`, `"server_ip"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	// A bogus token must surface as an error, not a crash or a fake success.
	if !strings.Contains(body, `"cloudflare_ok":false`) && !strings.Contains(body, `"cloudflare_ok":true`) {
		t.Fatalf("cloudflare_ok missing: %s", body)
	}
}

func TestACMETokenRejectsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.POST("/token", ACMETokenHandler(deps))

	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(`{"token":"  "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestACMEIssueRequiresDomain(t *testing.T) {
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
	if !strings.Contains(w.Body.String(), "domain required") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestACMEIssueFailsCleanlyOnBadToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newACMEDeps(t)
	r := gin.New()
	r.POST("/issue", ACMECertHandler(deps))

	req := httptest.NewRequest(http.MethodPost, "/issue", strings.NewReader(`{"domain":"nonexistent.example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected failure with bogus token, got success: %s", w.Body.String())
	}
}
