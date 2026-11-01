package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// panelCredsFile follows SMATRIX_DATA_DIR so an installation keeps its
// credentials next to its own data instead of a hardcoded /opt path.
var panelCredsFile = func() string {
	if d := strings.TrimSpace(os.Getenv("SMATRIX_DATA_DIR")); d != "" {
		return filepath.Join(d, "panel_creds.json")
	}
	return "/opt/sing-matrix/data/panel_creds.json"
}()

type panelCreds struct {
	Username          string `json:"username"`
	PasswordHash      string `json:"password_hash,omitempty"`
	Password          string `json:"password,omitempty"` // legacy plaintext for auto-migration
	SubscriptionToken string `json:"subscription_token"`
	ApiToken          string `json:"api_token,omitempty"`
}

func randomAlphanumeric(n int) string {
	const charset = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

func loadCreds() panelCreds {
	data, err := os.ReadFile(panelCredsFile)
	if err != nil {
		// Generate random strong credentials on first run instead of defaulting to admin/admin.
		pwd := randomAlphanumeric(20)
		hash, _ := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		c := panelCreds{
			Username:          "admin",
			PasswordHash:      string(hash),
			SubscriptionToken: randomHex(16),
			ApiToken:          randomHex(24),
		}
		_ = os.MkdirAll(filepath.Dir(panelCredsFile), 0755)
		_ = saveCreds(c)
		log.Printf("[SECURITY] Generated initial panel credentials saved to %s (username: admin, password: %s)", panelCredsFile, pwd)
		return c
	}
	var c panelCreds
	if err := json.Unmarshal(data, &c); err != nil {
		log.Printf("[ERROR] failed to parse %s: %v", panelCredsFile, err)
		return c
	}
	needSave := false
	if c.SubscriptionToken == "" {
		c.SubscriptionToken = randomHex(16)
		needSave = true
	}
	if c.ApiToken == "" {
		c.ApiToken = randomHex(24)
		needSave = true
	}
	// Seamless auto-migration: if legacy plaintext password exists, hash with bcrypt and clear plaintext.
	if c.PasswordHash == "" && c.Password != "" {
		if hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost); err == nil {
			c.PasswordHash = string(hash)
			c.Password = ""
			needSave = true
		}
	}
	if needSave {
		_ = saveCreds(c)
	}
	return c
}

func verifyPassword(c panelCreds, input string) bool {
	if c.PasswordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(input)) == nil
	}
	if c.Password != "" {
		return subtle.ConstantTimeCompare([]byte(c.Password), []byte(input)) == 1
	}
	return false
}

func saveCreds(c panelCreds) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(panelCredsFile, data, 0600)
}

func ChangePasswordHandler(c *gin.Context) {
	var req struct {
		Username    string `json:"username"`
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.OldPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "旧密码不能为空"})
		return
	}
	if len(req.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码长度不能少于8位"})
		return
	}
	creds := loadCreds()
	if !verifyPassword(creds, req.OldPassword) {
		c.JSON(http.StatusForbidden, gin.H{"error": "旧密码不正确"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}
	if req.Username != "" {
		creds.Username = req.Username
	}
	creds.PasswordHash = string(hash)
	creds.Password = ""
	if err := saveCreds(creds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func GenRealityKeypairHandler(c *gin.Context) {
	result := map[string]string{"private_key": randomHex(43), "public_key": randomHex(43)}
	if out, err := exec.Command("sing-box", "generate", "reality-keypair").CombinedOutput(); err == nil {
		text := string(out)
		rePriv := regexp.MustCompile(`(?m)^PrivateKey:\s*(\S+)`)
		rePub := regexp.MustCompile(`(?m)^PublicKey:\s*(\S+)`)
		if m := rePriv.FindStringSubmatch(text); len(m) == 2 {
			result["private_key"] = m[1]
		}
		if m := rePub.FindStringSubmatch(text); len(m) == 2 {
			result["public_key"] = m[1]
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "private_key": result["private_key"], "public_key": result["public_key"]})
}
