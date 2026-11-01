package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var (
	cachedJWTSecret []byte
	jwtSecretMu     sync.RWMutex
)

func jwtSecretFilePath() string {
	if d := strings.TrimSpace(os.Getenv("SMATRIX_DATA_DIR")); d != "" {
		return filepath.Join(d, "jwt.secret")
	}
	dir := filepath.Dir(panelCredsFile)
	return filepath.Join(dir, "jwt.secret")
}

func getJWTSecret() []byte {
	jwtSecretMu.RLock()
	if len(cachedJWTSecret) > 0 {
		defer jwtSecretMu.RUnlock()
		return cachedJWTSecret
	}
	jwtSecretMu.RUnlock()

	jwtSecretMu.Lock()
	defer jwtSecretMu.Unlock()
	if len(cachedJWTSecret) > 0 {
		return cachedJWTSecret
	}

	if v := strings.TrimSpace(os.Getenv("SMATRIX_JWT_SECRET")); v != "" {
		cachedJWTSecret = []byte(v)
		return cachedJWTSecret
	}

	secretPath := jwtSecretFilePath()
	if data, err := os.ReadFile(secretPath); err == nil && len(strings.TrimSpace(string(data))) >= 16 {
		cachedJWTSecret = []byte(strings.TrimSpace(string(data)))
		return cachedJWTSecret
	}

	// Persist a unique 32-byte (64 hex char) random secret if unconfigured
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	sec := hex.EncodeToString(b)
	_ = os.MkdirAll(filepath.Dir(secretPath), 0755)
	_ = os.WriteFile(secretPath, []byte(sec), 0600)
	cachedJWTSecret = []byte(sec)
	return cachedJWTSecret
}

var handshakeMu sync.Mutex
var handshakeNonces = map[string]int64{}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginAttempt struct {
	failures  int
	lockUntil time.Time
}

var (
	loginLimiterMu sync.Mutex
	loginAttempts  = map[string]*loginAttempt{}
)

func checkLoginRateLimit(ip string) (bool, int) {
	loginLimiterMu.Lock()
	defer loginLimiterMu.Unlock()

	now := time.Now()
	// Clean expired entries
	for k, v := range loginAttempts {
		if now.Sub(v.lockUntil) > 30*time.Minute && v.failures == 0 {
			delete(loginAttempts, k)
		}
	}

	att, ok := loginAttempts[ip]
	if !ok {
		return true, 0
	}
	if now.Before(att.lockUntil) {
		remaining := int(att.lockUntil.Sub(now).Seconds())
		if remaining < 1 {
			remaining = 1
		}
		return false, remaining
	}
	return true, 0
}

func recordLoginResult(ip string, success bool) {
	loginLimiterMu.Lock()
	defer loginLimiterMu.Unlock()

	now := time.Now()
	att, ok := loginAttempts[ip]
	if !ok {
		att = &loginAttempt{}
		loginAttempts[ip] = att
	}

	if success {
		delete(loginAttempts, ip)
		return
	}

	att.failures++
	if att.failures >= 5 {
		att.lockUntil = now.Add(15 * time.Minute)
		log.Printf("[SECURITY] IP %s locked out for 15 minutes after %d failed login attempts", ip, att.failures)
	}
}

func randomNonce(n int) string {
	if n <= 0 {
		n = 18
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func tokenID(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])[:16]
}

func ChallengeHandler(c *gin.Context) {
	nonce := randomNonce(18)
	now := time.Now().Unix()
	handshakeMu.Lock()
	for k, exp := range handshakeNonces {
		if exp <= now {
			delete(handshakeNonces, k)
		}
	}
	handshakeNonces[nonce] = now + 90
	handshakeMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"success": true, "nonce": nonce, "timestamp": now, "algorithm": "HMAC-SHA256", "windowSeconds": 60})
}

func consumeNonce(nonce string) bool {
	now := time.Now().Unix()
	handshakeMu.Lock()
	defer handshakeMu.Unlock()
	for k, exp := range handshakeNonces {
		if exp <= now {
			delete(handshakeNonces, k)
		}
	}
	exp, ok := handshakeNonces[nonce]
	if !ok || exp < now {
		return false
	}
	delete(handshakeNonces, nonce)
	return true
}

func canonicalSignaturePayload(method, path, bodyHash, nonce, ts string) string {
	return strings.ToUpper(method) + "\n" + path + "\n" + bodyHash + "\n" + nonce + "\n" + ts
}

func verifyHandshake(c *gin.Context, token string) bool {
	tid := strings.TrimSpace(c.GetHeader("x-panel-token-id"))
	nonce := strings.TrimSpace(c.GetHeader("x-panel-nonce"))
	ts := strings.TrimSpace(c.GetHeader("x-panel-timestamp"))
	bodyHash := strings.TrimSpace(c.GetHeader("x-panel-body-sha256"))
	sig := strings.TrimSpace(c.GetHeader("x-panel-signature"))
	if token == "" || tid == "" || nonce == "" || ts == "" || bodyHash == "" || sig == "" || tid != tokenID(token) {
		return false
	}
	ti, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := time.Now().Unix() - ti; d < -60 || d > 60 {
		return false
	}
	if !consumeNonce(nonce) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(canonicalSignaturePayload(c.Request.Method, c.Request.URL.RequestURI(), bodyHash, nonce, ts)))
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(sig))
}

func LoginHandler(c *gin.Context) {
	ip := c.ClientIP()
	allowed, remaining := checkLoginRateLimit(ip)
	if !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": fmt.Sprintf("Too many failed login attempts. Locked for %d more seconds.", remaining),
		})
		return
	}

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	creds := loadCreds()
	if req.Username != creds.Username || !verifyPassword(creds, req.Password) {
		recordLoginResult(ip, false)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	recordLoginResult(ip, true)
	signed, err := buildLoginToken(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": signed, "token_type": "Bearer"})
}

func buildLoginToken(username string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  username,
		"role": "admin",
		"iat":  now.Unix(),
		"exp":  now.Add(7 * 24 * time.Hour).Unix(),
	})
	return token.SignedString(getJWTSecret())
}

func JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		raw := ""
		if strings.HasPrefix(auth, "Bearer ") {
			raw = strings.TrimPrefix(auth, "Bearer ")
		} else if q := c.Query("token"); q != "" {
			raw = q
		}
		if raw == "" {
			// Handshake mode
			creds := loadCreds()
			if creds.ApiToken != "" && verifyHandshake(c, creds.ApiToken) {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return getJWTSecret(), nil
		})
		if err == nil && token.Valid {
			c.Next()
			return
		}
		// Also accept static ApiToken if matched
		creds := loadCreds()
		if creds.ApiToken != "" && subtle.ConstantTimeCompare([]byte(raw), []byte(creds.ApiToken)) == 1 {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
	}
}
