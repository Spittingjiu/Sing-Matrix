package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type runtimeConfig struct {
	Inbounds []map[string]interface{} `json:"inbounds"`
}

func SubscriptionHandler(configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		links, err := BuildShareLinks(configPath, publicHost(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(encoded))
	}
}

func ShareLinksHandler(configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		links, err := BuildShareLinks(configPath, publicHost(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"links": links, "subscription": subscriptionURL(c)})
	}
}

func BuildShareLinks(configPath, host string) ([]string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg runtimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	links := []string{}
	for _, in := range cfg.Inbounds {
		typ := strMap(in, "type", "")
		tag := strMap(in, "tag", "S-Matrix")
		port := intMap(in, "listen_port", 0)
		switch typ {
		case "vless":
			uuid := firstUserValue(in, "uuid", "00000000-0000-0000-0000-000000000000")
			tls, _ := in["tls"].(map[string]interface{})
			reality, _ := tls["reality"].(map[string]interface{})
			pbk := strMap(reality, "public_key", strMap(reality, "private_key", ""))
			sni := strMap(tls, "server_name", "www.cloudflare.com")
			q := url.Values{}
			q.Set("security", "reality")
			q.Set("pbk", pbk)
			q.Set("sni", sni)
			q.Set("fp", "chrome")
			q.Set("type", "tcp")
			q.Set("flow", "xtls-rprx-vision")
			links = append(links, fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuid, host, port, q.Encode(), url.QueryEscape(tag)))
		case "hysteria2":
			password := firstUserValue(in, "password", "change-me")
			sni := strMap(in, "server_name", host)
			q := url.Values{}
			q.Set("sni", sni)
			q.Set("insecure", "1")
			links = append(links, fmt.Sprintf("hy2://%s@%s:%d?%s#%s", url.QueryEscape(password), host, port, q.Encode(), url.QueryEscape(tag)))
		}
	}
	return links, nil
}

func publicHost(c *gin.Context) string {
	if h := c.Query("host"); h != "" {
		return h
	}
	host := c.Request.Host
	if xfh := c.GetHeader("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func subscriptionURL(c *gin.Context) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/api/v1/sub/default", scheme, c.Request.Host)
}

func strMap(m map[string]interface{}, key, fallback string) string {
	if m == nil {
		return fallback
	}
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intMap(m map[string]interface{}, key string, fallback int) int {
	if m == nil {
		return fallback
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

func firstUserValue(in map[string]interface{}, key, fallback string) string {
	users, _ := in["users"].([]interface{})
	if len(users) == 0 {
		return fallback
	}
	user, _ := users[0].(map[string]interface{})
	return strMap(user, key, fallback)
}
