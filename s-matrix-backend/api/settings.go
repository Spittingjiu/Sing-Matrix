package api

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"

	"github.com/gin-gonic/gin"
)

var panelCredsFile = "/opt/sing-matrix/data/panel_creds.json"

type panelCreds struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func loadCreds() panelCreds {
	data, err := os.ReadFile(panelCredsFile)
	if err != nil {
		return panelCreds{Username: "admin", Password: "admin"}
	}
	var c panelCreds
	if err := json.Unmarshal(data, &c); err != nil {
		return panelCreds{Username: "admin", Password: "admin"}
	}
	return c
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
	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少6位"})
		return
	}
	creds := loadCreds()
	// If old password is empty, it's first-time setup or admin/admin
	if req.OldPassword != "" && creds.Password != "admin" && req.OldPassword != creds.Password {
		c.JSON(http.StatusForbidden, gin.H{"error": "旧密码不正确"})
		return
	}
	creds.Username = req.Username
	creds.Password = req.NewPassword
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
