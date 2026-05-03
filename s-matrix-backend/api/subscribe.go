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

func DiscoveryHandler(configPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		links, err := BuildShareLinks(configPath, publicHost(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		types := []string{}
		for _, link := range links {
			if strings.HasPrefix(link, "vless://") {
				types = append(types, "vless-reality")
			} else if strings.HasPrefix(link, "hy2://") || strings.HasPrefix(link, "hysteria2://") {
				types = append(types, "hysteria2")
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":             true,
			"name":           "S-Matrix",
			"kind":           "sbui",
			"version":        "s-matrix.sub.v1",
			"subscription":   subscriptionURL(c),
			"links_endpoint": fmt.Sprintf("%s://%s/api/v1/singbox/share-links", forwardedScheme(c), c.Request.Host),
			"node_count":     len(links),
			"protocols":      types,
		})
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
			meta := loadClientInboundMeta(configPath, tag)
			pbk := meta["public_key"]
			if pbk == "" {
				pbk = strMap(reality, "public_key", "")
			}
			sid := meta["short_id"]
			if sid == "" {
				sid = firstStringValue(reality, "short_id", "")
			}
			sni := strMap(tls, "server_name", "www.cloudflare.com")
			q := url.Values{}
			q.Set("security", "reality")
			q.Set("encryption", "none")
			q.Set("pbk", pbk)
			q.Set("sni", sni)
			if sid != "" {
				q.Set("sid", sid)
			}
			q.Set("spx", "/")
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
	return fmt.Sprintf("%s://%s/api/v1/sub/default", forwardedScheme(c), c.Request.Host)
}

func forwardedScheme(c *gin.Context) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
	}
	return scheme
}

func firstStringValue(m map[string]interface{}, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	switch x := v.(type) {
	case string:
		if x != "" {
			return x
		}
	case []interface{}:
		if len(x) > 0 {
			if s, ok := x[0].(string); ok && s != "" {
				return s
			}
		}
	case []string:
		if len(x) > 0 && x[0] != "" {
			return x[0]
		}
	}
	return fallback
}

func loadClientInboundMeta(configPath, tag string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(configPath + ".client.json")
	if err != nil {
		return out
	}
	var root struct {
		Inbounds map[string]map[string]string `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return out
	}
	if root.Inbounds == nil {
		return out
	}
	if m, ok := root.Inbounds[tag]; ok && m != nil {
		return m
	}
	return out
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
