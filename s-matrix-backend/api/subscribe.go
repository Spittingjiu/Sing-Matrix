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
	"s-matrix/models"
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
		seen := map[string]bool{}
		for _, link := range links {
			proto := ""
			if strings.HasPrefix(link, "vless://") {
				proto = "vless-reality"
			} else if strings.HasPrefix(link, "hy2://") || strings.HasPrefix(link, "hysteria2://") {
				proto = "hysteria2"
			} else if strings.HasPrefix(link, "vmess://") {
				proto = "vmess"
			} else if strings.HasPrefix(link, "trojan://") {
				proto = "trojan"
			} else if strings.HasPrefix(link, "ss://") {
				proto = "shadowsocks"
			} else if strings.HasPrefix(link, "socks5://") {
				proto = "socks"
			} else if strings.HasPrefix(link, "http://") {
				proto = "http"
			}
			if proto != "" && !seen[proto] {
				types = append(types, proto)
				seen[proto] = true
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
			link := buildVLESSLink(in, host, port, tag, configPath)
			if link != "" { links = append(links, link) }
		case "hysteria2":
			link := buildHY2Link(in, host, port, tag)
			if link != "" { links = append(links, link) }
		case "vmess":
			link := buildVMessLink(in, host, port, tag)
			if link != "" { links = append(links, link) }
		case "trojan":
			link := buildTrojanLink(in, host, port, tag)
			if link != "" { links = append(links, link) }
		case "shadowsocks":
			link := buildSSLink(in, host, port, tag)
			if link != "" { links = append(links, link) }
		case "socks":
			links = append(links, fmt.Sprintf("socks5://%s:%d#%s", host, port, url.QueryEscape(tag)))
		case "http":
			links = append(links, fmt.Sprintf("http://%s:%d#%s", host, port, url.QueryEscape(tag)))
		}
	}
	return links, nil
}

func buildVLESSLink(in map[string]interface{}, host string, port int, tag, configPath string) string {
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
	if sid != "" { q.Set("sid", sid) }
	q.Set("spx", "/")
	q.Set("fp", "chrome")
	q.Set("type", "tcp")
	q.Set("flow", "xtls-rprx-vision")
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuid, host, port, q.Encode(), url.QueryEscape(tag))
}

func buildHY2Link(in map[string]interface{}, host string, port int, tag string) string {
	password := firstUserValue(in, "password", "change-me")
	sni := strMap(in, "server_name", host)
	q := url.Values{}
	q.Set("sni", sni)
	q.Set("insecure", "1")
	return fmt.Sprintf("hy2://%s@%s:%d?%s#%s", url.QueryEscape(password), host, port, q.Encode(), url.QueryEscape(tag))
}

func buildVMessLink(in map[string]interface{}, host string, port int, tag string) string {
	uuid := firstUserValue(in, "uuid", "00000000-0000-0000-0000-000000000000")
	transport, _ := in["transport"].(map[string]interface{})
	tls, _ := in["tls"].(map[string]interface{})
	netType := strMap(transport, "type", "tcp")
	path := strMap(transport, "path", "")
	headers, _ := transport["headers"].(map[string]interface{})
	rHost := strMap(headers, "Host", "")
	tlsEnabled := false
	sni := ""
	if tls != nil {
		if en, _ := tls["enabled"].(bool); en {
			tlsEnabled = true
			sni = strMap(tls, "server_name", "")
		}
	}
	vmessCfg := map[string]interface{}{
		"v": "2", "ps": tag, "add": host, "port": port, "id": uuid, "aid": 0,
		"net": netType, "type": "none", "tls": "none",
	}
	if netType == "ws" {
		if path != "" { vmessCfg["path"] = path }
		if rHost != "" { vmessCfg["host"] = rHost }
	}
	if tlsEnabled {
		vmessCfg["tls"] = "tls"
		if sni != "" { vmessCfg["sni"] = sni }
	}
	b, _ := json.Marshal(vmessCfg)
	return fmt.Sprintf("vmess://%s", base64.StdEncoding.EncodeToString(b))
}

func buildTrojanLink(in map[string]interface{}, host string, port int, tag string) string {
	password := firstUserValue(in, "password", "change-me")
	tls, _ := in["tls"].(map[string]interface{})
	sni := strMap(tls, "server_name", host)
	q := url.Values{}
	q.Set("sni", sni)
	return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", url.QueryEscape(password), host, port, q.Encode(), url.QueryEscape(tag))
}

func buildSSLink(in map[string]interface{}, host string, port int, tag string) string {
	method := strMap(in, "method", "aes-128-gcm")
	password := strMap(in, "password", firstUserValue(in, "password", "change-me"))
	raw := fmt.Sprintf("%s:%s@%s:%d", method, password, host, port)
	b64 := base64.StdEncoding.EncodeToString([]byte(raw))
	return fmt.Sprintf("ss://%s#%s", b64, url.QueryEscape(tag))
}

func BuildSingleInboundLink(configPath, host string, id uint) (string, error) {
	// Load DB to find tag, then config to find the inbound
	// For simplicity, rebuild from config
	links, err := BuildShareLinks(configPath, host)
	if err != nil {
		return "", err
	}
	// Get tag from DB
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("cannot read config: %w", err)
	}
	var cfg runtimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	for _, in := range cfg.Inbounds {
		typ := strMap(in, "type", "")
		tag := strMap(in, "tag", "")
		port := intMap(in, "listen_port", 0)
		var link string
		switch typ {
		case "vless":
			link = buildVLESSLink(in, host, port, tag, configPath)
		case "hysteria2":
			link = buildHY2Link(in, host, port, tag)
		case "vmess":
			link = buildVMessLink(in, host, port, tag)
		case "trojan":
			link = buildTrojanLink(in, host, port, tag)
		case "shadowsocks":
			link = buildSSLink(in, host, port, tag)
		case "socks":
			link = fmt.Sprintf("socks5://%s:%d#%s", host, port, url.QueryEscape(tag))
		case "http":
			link = fmt.Sprintf("http://%s:%d#%s", host, port, url.QueryEscape(tag))
		}
		if link != "" {
			return link, nil
		}
		_ = links
	}
	return "", fmt.Errorf("no inbound found in config")
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

// buildLinkFromRecord builds a share link directly from DB inbound record data
// Avoids config.json index mismatch when DB id != config array index
func buildLinkFromRecord(ib models.Inbound, host string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(ib.Payload), &payload); err != nil {
		payload = map[string]interface{}{}
	}
	tag := ib.Tag
	port := ib.Port

	switch ib.Type {
	case "vless":
		uuid := strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")
		pubKey := strMap(payload, "public_key", "")
		shortID := strMap(payload, "short_id", "")
		sni := strMap(payload, "server_name", "www.cloudflare.com")
		if sni == "" {
			sni = strMap(payload, "sni", "www.cloudflare.com")
		}
		q := url.Values{}
		q.Set("security", "reality")
		q.Set("encryption", "none")
		q.Set("pbk", pubKey)
		q.Set("sni", sni)
		if shortID != "" {
			q.Set("sid", shortID)
		}
		q.Set("spx", "/")
		q.Set("fp", "chrome")
		q.Set("type", "tcp")
		q.Set("flow", "xtls-rprx-vision")
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuid, host, port, q.Encode(), url.QueryEscape(tag))

	case "hysteria2":
		pwd := strMap(payload, "password", "change-me")
		sni := strMap(payload, "server_name", host)
		q := url.Values{}
		q.Set("sni", sni)
		q.Set("insecure", "1")
		return fmt.Sprintf("hy2://%s@%s:%d?%s#%s", url.QueryEscape(pwd), host, port, q.Encode(), url.QueryEscape(tag))

	case "vmess":
		uuid := strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")
		netType := strMap(payload, "network", "tcp")
		path := strMap(payload, "path", "")
		rHost := strMap(payload, "host", "")
		security := strMap(payload, "security", "")
		sni := strMap(payload, "server_name", "")
		if sni == "" {
			sni = strMap(payload, "sni", "")
		}
		vmessCfg := map[string]interface{}{
			"v": "2", "ps": tag, "add": host, "port": port, "id": uuid, "aid": 0,
			"net": netType, "type": "none", "tls": "none",
		}
		if netType == "ws" {
			if path != "" {
				vmessCfg["path"] = path
			}
			if rHost != "" {
				vmessCfg["host"] = rHost
			}
		}
		if security == "tls" {
			vmessCfg["tls"] = "tls"
			if sni != "" {
				vmessCfg["sni"] = sni
			}
		}
		b, _ := json.Marshal(vmessCfg)
		return fmt.Sprintf("vmess://%s", base64.StdEncoding.EncodeToString(b))

	case "trojan":
		pwd := strMap(payload, "password", "change-me")
		sni := strMap(payload, "sni", host)
		if sni == "" {
			sni = strMap(payload, "server_name", host)
		}
		q := url.Values{}
		q.Set("sni", sni)
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", url.QueryEscape(pwd), host, port, q.Encode(), url.QueryEscape(tag))

	case "shadowsocks":
		method := strMap(payload, "method", "aes-128-gcm")
		pwd := strMap(payload, "password", "change-me")
		raw := fmt.Sprintf("%s:%s@%s:%d", method, pwd, host, port)
		b64 := base64.StdEncoding.EncodeToString([]byte(raw))
		return fmt.Sprintf("ss://%s#%s", b64, url.QueryEscape(tag))

	case "socks":
		return fmt.Sprintf("socks5://%s:%d#%s", host, port, url.QueryEscape(tag))

	case "http":
		return fmt.Sprintf("http://%s:%d#%s", host, port, url.QueryEscape(tag))

	default:
		return fmt.Sprintf("ss://%s@%s:%d#%s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("aes-128-gcm:change-me@%s:%d", host, port))), host, port, url.QueryEscape(tag))
	}
}
