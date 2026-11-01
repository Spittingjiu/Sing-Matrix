package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func TestSubscriptionTokenSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	credsPath := filepath.Join(tmpDir, "panel_creds.json")
	panelCredsFile = credsPath

	c := panelCreds{
		Username:          "admin",
		PasswordHash:      "$2a$10$xyz",
		SubscriptionToken: "secret-token-12345",
	}
	saveCreds(c)

	cfgPath := filepath.Join(tmpDir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(`{"inbounds":[{"type":"vless","tag":"reality","listen_port":443,"users":[{"uuid":"11111111-1111-1111-1111-111111111111"}],"tls":{"server_name":"www.test.com","reality":{"public_key":"PUB"}}}]}`), 0644)

	r := gin.New()
	r.GET("/sub/:token", SubscriptionHandler(cfgPath))

	// 1. Wrong token -> 403 Forbidden
	req1 := httptest.NewRequest(http.MethodGet, "/sub/default", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong token, got %d: %s", w1.Code, w1.Body.String())
	}

	// 2. Correct token -> 200 OK
	req2 := httptest.NewRequest(http.MethodGet, "/sub/secret-token-12345", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for correct token, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. Bearer JWT on wrong path -> 200 OK
	jwtTok, err := buildLoginToken("admin")
	if err != nil {
		t.Fatal(err)
	}
	req3 := httptest.NewRequest(http.MethodGet, "/sub/default", nil)
	req3.Header.Set("Authorization", "Bearer "+jwtTok)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 for JWT on sub route, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestPasswordAutoMigrationAndBcrypt(t *testing.T) {
	tmpDir := t.TempDir()
	credsPath := filepath.Join(tmpDir, "panel_creds.json")
	panelCredsFile = credsPath

	// Write legacy plaintext creds
	_ = os.WriteFile(credsPath, []byte(`{"username":"admin","password":"MyLegacyPassword123!"}`), 0600)

	creds := loadCreds()
	if creds.Password != "" {
		t.Fatalf("expected plaintext password to be cleared, got %q", creds.Password)
	}
	if creds.PasswordHash == "" {
		t.Fatalf("expected password_hash to be populated")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(creds.PasswordHash), []byte("MyLegacyPassword123!")); err != nil {
		t.Fatalf("bcrypt verify failed: %v", err)
	}
	if creds.SubscriptionToken == "" {
		t.Fatalf("expected subscription token to be generated")
	}

	// Verify verifyPassword helper
	if !verifyPassword(creds, "MyLegacyPassword123!") {
		t.Fatalf("verifyPassword failed for correct password")
	}
	if verifyPassword(creds, "WrongPassword") {
		t.Fatalf("verifyPassword succeeded for wrong password")
	}
}

func TestChangePasswordSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	credsPath := filepath.Join(tmpDir, "panel_creds.json")
	panelCredsFile = credsPath

	hash, _ := bcrypt.GenerateFromPassword([]byte("InitialPassword123"), bcrypt.DefaultCost)
	saveCreds(panelCreds{Username: "admin", PasswordHash: string(hash), SubscriptionToken: "sub123"})

	r := gin.New()
	r.POST("/change-password", ChangePasswordHandler)

	// 1. Omit old_password -> must fail (closing the bypass vulnerability)
	body1 := `{"username":"admin","old_password":"","new_password":"BrandNewPassword456"}`
	req1 := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code == http.StatusOK {
		t.Fatalf("change password should reject empty old_password")
	}

	// 2. Wrong old_password -> 403 Forbidden
	body2 := `{"username":"admin","old_password":"WrongInitialPassword","new_password":"BrandNewPassword456"}`
	req2 := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for incorrect old_password, got %d", w2.Code)
	}

	// 3. Short new_password -> 400 Bad Request
	body3 := `{"username":"admin","old_password":"InitialPassword123","new_password":"short"}`
	req3 := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short new_password, got %d", w3.Code)
	}

	// 4. Correct old_password and valid new_password -> 200 OK
	body4 := `{"username":"admin","old_password":"InitialPassword123","new_password":"BrandNewPassword456"}`
	req4 := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(body4))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid change password, got %d: %s", w4.Code, w4.Body.String())
	}

	updatedCreds := loadCreds()
	if !verifyPassword(updatedCreds, "BrandNewPassword456") {
		t.Fatalf("new password verification failed")
	}
}

func TestLoginRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	credsPath := filepath.Join(tmpDir, "panel_creds.json")
	panelCredsFile = credsPath

	hash, _ := bcrypt.GenerateFromPassword([]byte("CorrectPassword123"), bcrypt.DefaultCost)
	saveCreds(panelCreds{Username: "admin", PasswordHash: string(hash), SubscriptionToken: "sub123"})

	r := gin.New()
	r.POST("/login", LoginHandler)

	loginLimiterMu.Lock()
	loginAttempts = map[string]*loginAttempt{}
	loginLimiterMu.Unlock()

	badPayload := `{"username":"admin","password":"BadPassword"}`

	// 5 failed attempts
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte(badPayload)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, w.Code)
		}
	}

	// 6th attempt must be 429 Too Many Requests
	req6 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte(badPayload)))
	req6.Header.Set("Content-Type", "application/json")
	req6.RemoteAddr = "192.168.1.100:12345"
	w6 := httptest.NewRecorder()
	r.ServeHTTP(w6, req6)
	if w6.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after 5 failures, got %d: %s", w6.Code, w6.Body.String())
	}
}
