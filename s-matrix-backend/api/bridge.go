package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// panelPublicURL reconstructs the URL another panel should use to reach this
// panel. X-Forwarded-* wins because Sing-Matrix always sits behind nginx.
func panelPublicURL(c *gin.Context) string {
	host := c.Request.Host
	if xfh := c.GetHeader("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	return fmt.Sprintf("%s://%s", forwardedScheme(c), host)
}

// ConnectSubHandler pushes this panel into subgo as an SBUI source, so the
// operator does not have to copy URLs and tokens by hand.
//
// Flow: POST <subgo>/api/bridge/push-source (subgo >= bridge build) which
// upserts by panel_url. Older subgo builds without source_type support are
// still handled: the request is retried through the authenticated
// /api/auth/login + /api/sources pair.
func ConnectSubHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			SubgoURL   string `json:"subgo_url"`
			Username   string `json:"username"`
			Password   string `json:"password"`
			Name       string `json:"name"`
			PanelURL   string `json:"panel_url"`
			PanelToken string `json:"panel_token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad json: " + err.Error()})
			return
		}
		base := strings.TrimRight(strings.TrimSpace(req.SubgoURL), "/")
		if base == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "subgo_url required"})
			return
		}
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "https://" + base
		}
		username := strings.TrimSpace(req.Username)
		if username == "" {
			username = loadCreds().Username
		}
		password := req.Password
		if password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "subgo 密码必填"})
			return
		}
		panelURL := strings.TrimSpace(req.PanelURL)
		if panelURL == "" {
			panelURL = panelPublicURL(c)
		}
		panelToken := strings.TrimSpace(req.PanelToken)
		if panelToken == "" {
			tok, err := buildLoginToken(loadCreds().Username)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
				return
			}
			panelToken = tok
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = "S-Matrix"
		}

		client := &http.Client{Timeout: 20 * time.Second}
		bridgeBody := map[string]interface{}{
			"username":    username,
			"password":    password,
			"name":        name,
			"panel_url":   panelURL,
			"panel_token": panelToken,
			"source_type": "sbui",
		}
		status, body, err := postJSON(client, base+"/api/bridge/push-source", bridgeBody, nil)
		if err == nil && status == http.StatusOK {
			out := map[string]interface{}{}
			_ = json.Unmarshal(body, &out)
			c.JSON(http.StatusOK, gin.H{
				"ok":        true,
				"via":       "bridge",
				"subgo":     base,
				"panel_url": panelURL,
				"source_id": out["source_id"],
				"name":      name,
			})
			return
		}

		// Fallback for subgo builds without /api/bridge/push-source.
		jar := newCookieJar()
		loginStatus, loginBody, loginErr := postJSON(client, base+"/api/auth/login", map[string]string{
			"username": username,
			"password": password,
		}, jar)
		if loginErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "无法连接 subgo: " + loginErr.Error(), "bridge_status": status})
			return
		}
		if loginStatus != http.StatusOK {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "subgo 登录失败: " + strings.TrimSpace(string(loginBody)), "bridge_status": status})
			return
		}
		createStatus, createBody, createErr := postJSON(client, base+"/api/sources", map[string]interface{}{
			"name":        name,
			"source_type": "sbui",
			"panel_url":   panelURL,
			"panel_token": panelToken,
		}, jar)
		if createErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "写入 subgo 源失败: " + createErr.Error()})
			return
		}
		if createStatus != http.StatusOK {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "写入 subgo 源失败: " + strings.TrimSpace(string(createBody))})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":        true,
			"via":       "sources-api",
			"subgo":     base,
			"panel_url": panelURL,
			"name":      name,
		})
	}
}

// PanelTokenHandler returns the long-lived panel token for the authenticated
// caller, so operators can paste it into third-party tools without logging in
// again.
func PanelTokenHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		creds := loadCreds()
		tok, err := buildLoginToken(creds.Username)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "username": creds.Username, "token": tok, "panel_url": panelPublicURL(c)})
	}
}

func newCookieJar() http.CookieJar {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil
	}
	return jar
}

func postJSON(client *http.Client, url string, payload interface{}, jar http.CookieJar) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if jar != nil {
		for _, ck := range jar.Cookies(req.URL) {
			req.AddCookie(ck)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if jar != nil {
		if rc := resp.Cookies(); len(rc) > 0 {
			jar.SetCookies(req.URL, rc)
		}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	return resp.StatusCode, body, nil
}
