package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = []byte("s-matrix-change-me-development-secret")
var handshakeMu sync.Mutex
var handshakeNonces = map[string]int64{}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
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
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	creds := loadCreds()
	if req.Username != creds.Username || req.Password != creds.Password {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	signed, err := buildLoginToken(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": signed, "token_type": "Bearer"})
}

func buildLoginToken(username string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  username,
		"role": "admin",
	})
	return token.SignedString(jwtSecret)
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
			// Handshake mode: derive stable token from current credentials and verify HMAC without exposing token.
			creds := loadCreds()
			stable, _ := buildLoginToken(creds.Username)
			if stable != "" && verifyHandshake(c, stable) {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) { return jwtSecret, nil })
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Next()
	}
}
