package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
	"s-matrix/core/acme"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

type runtimeConfig struct {
	Inbounds []map[string]interface{} `json:"inbounds"`
}

func isAuthorizedSubscription(c *gin.Context, validToken string) bool {
	tok := strings.TrimSpace(c.Param("token"))
	if tok != "" && validToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(validToken)) == 1 {
		return true
	}
	q := strings.TrimSpace(c.Query("token"))
	if q != "" && validToken != "" && subtle.ConstantTimeCompare([]byte(q), []byte(validToken)) == 1 {
		return true
	}
	auth := c.GetHeader("Authorization")
	raw := ""
	if strings.HasPrefix(auth, "Bearer ") {
		raw = strings.TrimPrefix(auth, "Bearer ")
	} else if q != "" {
		raw = q
	}
	if raw != "" {
		token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return getJWTSecret(), nil
		})
		if err == nil && token.Valid {
			return true
		}
	}
	return false
}

func SubscriptionHandler(configPath string) gin.HandlerFunc {
	return SubscriptionHandlerWithDB(configPath, nil)
}

func SubscriptionHandlerWithDB(configPath string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		creds := loadCreds()
		if !isAuthorizedSubscription(c, creds.SubscriptionToken) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid subscription token"})
			return
		}
		host := publicHost(c)
		format := strings.ToLower(strings.TrimSpace(c.Query("format")))
		rawMode := strings.TrimSpace(c.Query("raw")) == "1" || strings.EqualFold(strings.TrimSpace(c.Query("raw")), "true")

		switch format {
		case "aero-json", "aerojson", "json":
			subResp, err := BuildAeroSubV2(db, configPath, host)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, subResp)
			return
		case "aero":
			links, err := BuildAeroLinksWithDB(db, configPath, host)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			joined := strings.Join(links, "\n")
			if rawMode {
				c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(joined))
				return
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(joined))
			c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(encoded))
			return
		default:
			links, err := BuildShareLinksWithDB(db, configPath, host)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			joined := strings.Join(links, "\n")
			if rawMode {
				c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(joined))
				return
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(joined))
			c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(encoded))
		}
	}
}

func ShareLinksHandler(configPath string) gin.HandlerFunc {
	return ShareLinksHandlerWithDB(configPath, nil)
}

func ShareLinksHandlerWithDB(configPath string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		host := publicHost(c)
		links, err := BuildShareLinksWithDB(db, configPath, host)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		aeroLinks, err := BuildAeroLinksWithDB(db, configPath, host)
		if err != nil {
			aeroLinks = []string{}
		}
		c.JSON(http.StatusOK, gin.H{
			"links":        links,
			"aero_links":   aeroLinks,
			"subscription": subscriptionURL(c),
		})
	}
}

func DiscoveryHandler(configPath string) gin.HandlerFunc {
	return DiscoveryHandlerWithDB(configPath, nil)
}

func DiscoveryHandlerWithDB(configPath string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		creds := loadCreds()
		if !isAuthorizedSubscription(c, creds.SubscriptionToken) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		links, err := BuildShareLinksWithDB(db, configPath, publicHost(c))
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
			} else if strings.HasPrefix(link, "tuic://") {
				proto = "tuic"
			} else if strings.HasPrefix(link, "naive+https://") {
				proto = "naive"
			} else if strings.HasPrefix(link, "shadowtls://") {
				proto = "shadowtls"
			} else if strings.HasPrefix(link, "hysteria://") {
				proto = "hysteria"
			} else if strings.HasPrefix(link, "anytls://") {
				proto = "anytls"
			}
			if proto != "" && !seen[proto] {
				types = append(types, proto)
				seen[proto] = true
			}
		}
		subURL := subscriptionURL(c)
		c.JSON(http.StatusOK, gin.H{
			"ok":                true,
			"name":              "S-Matrix",
			"kind":              "sbui",
			"version":           "s-matrix.sub.v1",
			"sub_version":       "s-matrix.sub.v2",
			"capabilities":      []string{"aero-link-v1", "aero-json-v2", "ortt-per-node-v2"},
			"endpoints": gin.H{
				"sub":      "/api/v1/sub/:token",
				"sub_aero": "/api/v1/sub/:token?format=aero",
				"sub_json": "/api/v1/sub/:token?format=aero-json",
			},
			"subscription":      subURL,
			"subscription_aero": subURL + "?format=aero",
			"subscription_json": subURL + "?format=aero-json",
			"links_endpoint":    fmt.Sprintf("%s://%s/api/v1/singbox/share-links", forwardedScheme(c), c.Request.Host),
			"node_count":        len(links),
			"protocols":         types,
		})
	}
}

func BuildShareLinks(configPath, host string) ([]string, error) {
	return BuildShareLinksWithDB(nil, configPath, host)
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
	if pbk == "" {
		// Server config only holds the private key; without the derived public
		// half the link is unusable in every client.
		pbk = singbox.DeriveRealityPublicKey(strMap(reality, "private_key", ""))
	}
	sid := meta["short_id"]
	if sid == "" {
		sid = firstStringValue(reality, "short_id", "")
	}
	sni := strMap(tls, "server_name", "www.apple.com")
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
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuid, host, port, q.Encode(), url.QueryEscape(tag))
}

// linkHost picks the address a client should dial. When the inbound carries a
// publicly trusted certificate its server_name is the name that was verified to
// point here, so a domain link is both correct and stable across IP changes.
// Self-signed inbounds keep the caller-provided host.
func linkHost(connectedHost, serverName, certPath string) string {
	name := strings.TrimSpace(serverName)
	if name == "" || net.ParseIP(name) != nil || certIsSelfSigned(certPath) {
		return connectedHost
	}
	return name
}

// sniForLink picks the SNI to advertise in a share link. RFC 6066 forbids IP
// literals in SNI, and echoing the connect address back as SNI is meaningless,
// so links omit the parameter in both cases and clients fall back to the host.
func sniForLink(candidate, connectHost string) string {
	v := strings.TrimSpace(candidate)
	if v == "" || net.ParseIP(v) != nil {
		return ""
	}
	if strings.EqualFold(v, strings.TrimSpace(connectHost)) {
		return ""
	}
	return v
}

// linkInsecure reports whether a client must skip certificate verification,
// which is only true for self-signed certificates.
func linkInsecure(certPath string) bool {
	return certIsSelfSigned(certPath)
}

func buildHY2Link(in map[string]interface{}, host string, port int, tag string) string {
	return buildHY2LinkWithOptions(in, host, port, tag, "", nil)
}

func buildHY2LinkWithOptions(in map[string]interface{}, host string, port int, tag, configPath string, dbPayload map[string]interface{}) string {
	password := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
	sniRaw, certPath := resolveNodeSNIAndCert(in, configPath, tag, dbPayload)
	host = linkHost(host, sniRaw, certPath)
	q := url.Values{}
	if sni := sniForLink(sniRaw, host); sni != "" {
		q.Set("sni", sni)
	}
	_, hasTLS := in["tls"].(map[string]interface{})
	if (hasTLS || certPath != "") && linkInsecure(certPath) {
		q.Set("insecure", "1")
	}
	if obfsType, obfsPass := extractHY2Obfs(in, dbPayload); obfsType != "" {
		q.Set("obfs", obfsType)
		if obfsPass != "" {
			q.Set("obfs-password", obfsPass)
		}
	}
	link := fmt.Sprintf("hysteria2://%s@%s:%d/", url.QueryEscape(password), host, port)
	if enc := q.Encode(); enc != "" {
		link += "?" + enc
	}
	return link + "#" + url.QueryEscape(tag)
}

func buildVMessLink(in map[string]interface{}, host string, port int, tag string) string {
	uuid := firstUserValue(in, "uuid", "00000000-0000-0000-0000-000000000000")
	security := firstUserValue(in, "security", "auto")
	transport, _ := in["transport"].(map[string]interface{})
	tls, _ := in["tls"].(map[string]interface{})
	netType := strMap(transport, "type", "tcp")
	path := strMap(transport, "path", "")
	serviceName := strMap(transport, "service_name", "")
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
		"net": netType, "type": "none", "tls": "none", "scy": security,
	}
	if netType == "ws" || netType == "httpupgrade" {
		if path != "" {
			vmessCfg["path"] = path
		}
		if rHost != "" {
			vmessCfg["host"] = rHost
		}
	}
	if netType == "grpc" {
		if serviceName != "" {
			vmessCfg["path"] = serviceName
		} else if path != "" {
			vmessCfg["path"] = path
		}
	}
	if tlsEnabled {
		vmessCfg["tls"] = "tls"
		if sni != "" {
			vmessCfg["sni"] = sni
		}
	}
	b, _ := json.Marshal(vmessCfg)
	return fmt.Sprintf("vmess://%s", base64.StdEncoding.EncodeToString(b))
}

func buildTrojanLink(in map[string]interface{}, host string, port int, tag string) string {
	return buildTrojanLinkWithOptions(in, host, port, tag, "", nil)
}

func buildTrojanLinkWithOptions(in map[string]interface{}, host string, port int, tag, configPath string, dbPayload map[string]interface{}) string {
	password := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
	sniRaw, certPath := resolveNodeSNIAndCert(in, configPath, tag, dbPayload)
	host = linkHost(host, sniRaw, certPath)
	q := url.Values{}
	if sni := sniForLink(firstNonEmpty(sniRaw, host), host); sni != "" {
		q.Set("sni", sni)
	}
	rawLink := fmt.Sprintf("trojan://%s@%s:%d", url.QueryEscape(password), host, port)
	if enc := q.Encode(); enc != "" {
		rawLink += "?" + enc
	}
	return rawLink + "#" + url.QueryEscape(tag)
}

func buildSSLink(in map[string]interface{}, host string, port int, tag string) string {
	method := strMap(in, "method", "aes-128-gcm")
	password := strMap(in, "password", firstUserValue(in, "password", "change-me"))
	raw := fmt.Sprintf("%s:%s@%s:%d", method, password, host, port)
	b64 := base64.StdEncoding.EncodeToString([]byte(raw))
	return fmt.Sprintf("ss://%s#%s", b64, url.QueryEscape(tag))
}

func buildTUICLink(in map[string]interface{}, host string, port int, tag string) string {
	uuid := firstUserValue(in, "uuid", "00000000-0000-0000-0000-000000000000")
	password := firstUserValue(in, "password", "change-me")
	q := url.Values{}
	q.Set("congestion_control", strMap(in, "congestion_control", "cubic"))
	q.Set("sni", host)
	q.Set("insecure", "1")
	return fmt.Sprintf("tuic://%s:%s@%s:%d?%s#%s", uuid, url.QueryEscape(password), host, port, q.Encode(), url.QueryEscape(tag))
}

func buildNaiveLink(in map[string]interface{}, host string, port int, tag string) string {
	username := firstUserValue(in, "username", "user")
	password := firstUserValue(in, "password", "change-me")
	return fmt.Sprintf("naive+https://%s:%s@%s:%d#%s", url.QueryEscape(username), url.QueryEscape(password), host, port, url.QueryEscape(tag))
}

func buildShadowTLSLink(in map[string]interface{}, host string, port int, tag string) string {
	password := firstUserValue(in, "password", strMap(in, "password", "change-me"))
	handshake, _ := in["handshake"].(map[string]interface{})
	sni := strMap(handshake, "server", "www.microsoft.com")
	q := url.Values{}
	q.Set("version", fmt.Sprintf("%d", intMap(in, "version", 3)))
	q.Set("sni", sni)
	return fmt.Sprintf("shadowtls://%s@%s:%d?%s#%s", url.QueryEscape(password), host, port, q.Encode(), url.QueryEscape(tag))
}

func buildHysteriaLink(in map[string]interface{}, host string, port int, tag string) string {
	password := firstUserValue(in, "auth_str", firstUserValue(in, "password", "change-me"))
	q := url.Values{}
	q.Set("insecure", "1")
	if obfsType, _ := extractHY2Obfs(in); obfsType != "" {
		q.Set("obfs", obfsType)
	}
	return fmt.Sprintf("hysteria://%s@%s:%d?%s#%s", url.QueryEscape(password), host, port, q.Encode(), url.QueryEscape(tag))
}

func buildAnyTLSLink(in map[string]interface{}, host string, port int, tag string) string {
	password := firstUserValue(in, "password", "change-me")
	certPath := ""
	if tlsMap, ok := in["tls"].(map[string]interface{}); ok {
		certPath = tlsCertPathFromContext(tlsMap)
	}
	host = linkHost(host, inboundServerName(in), certPath)
	q := url.Values{}
	if linkInsecure(certPath) {
		q.Set("insecure", "1")
	}
	if sni := sniForLink(inboundServerName(in), host); sni != "" {
		q.Set("sni", sni)
	}
	link := fmt.Sprintf("anytls://%s@%s:%d/", url.QueryEscape(password), host, port)
	if enc := q.Encode(); enc != "" {
		link += "?" + enc
	}
	return link + "#" + url.QueryEscape(tag)
}

// inboundServerName extracts tls.server_name from a sing-box inbound map.
func inboundServerName(in map[string]interface{}) string {
	tlsMap, ok := in["tls"].(map[string]interface{})
	if !ok {
		return ""
	}
	if v, ok := tlsMap["server_name"].(string); ok {
		return v
	}
	return ""
}

func publicHost(c *gin.Context) string {
	host := strings.TrimSpace(c.Query("host"))
	if host == "" {
		host = c.Request.Host
		if xfh := c.GetHeader("X-Forwarded-Host"); xfh != "" {
			host = xfh
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	host = strings.Trim(host, "[]")
	// A share link must never point at loopback or an internal address: that is
	// what the panel sees when it is opened through a local tunnel or proxy, and
	// such links are dead for anyone else.
	if isUnroutableHost(host) {
		if ip := acme.DetectPublicIP(); ip != "" {
			return ip
		}
	}
	return host
}

func isUnroutableHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()
}

func subscriptionURL(c *gin.Context) string {
	creds := loadCreds()
	token := creds.SubscriptionToken
	if token == "" {
		token = "default"
	}
	return fmt.Sprintf("%s://%s/api/v1/sub/%s", forwardedScheme(c), c.Request.Host, token)
}

func forwardedScheme(c *gin.Context) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			return "https"
		}
		return "http"
	}
	return strings.TrimSpace(strings.Split(scheme, ",")[0])
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
	meta, _ := extractOrttMetaFromMap(payload)

	var link string
	switch ib.Type {
	case "vless":
		uuid := strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")
		pubKey := strMap(payload, "public_key", "")
		if pubKey == "" {
			pubKey = singbox.DeriveRealityPublicKey(strMap(payload, "private_key", ""))
		}
		shortID := strMap(payload, "short_id", "")
		sni := firstNonEmpty(strMap(payload, "server_name", ""), strMap(payload, "sni", ""), "www.apple.com")
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
		link = fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuid, host, port, q.Encode(), url.QueryEscape(tag))

	case "hysteria2":
		inMap, dbPayload := inboundRecordToConfigMap(ib)
		link = buildHY2LinkWithOptions(inMap, host, port, tag, "", dbPayload)

	case "vmess":
		uuid := strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")
		netType := strMap(payload, "network", "tcp")
		path := strMap(payload, "path", "")
		rHost := strMap(payload, "host", "")
		security := strMap(payload, "security", "")
		// "security" in the payload is the TLS flag ("tls"); the VMess
		// cipher lives in "vmess_security" and maps to "scy".
		cipher := strMap(payload, "vmess_security", "auto")
		serviceName := strMap(payload, "service_name", "")
		sni := strMap(payload, "server_name", "")
		if sni == "" {
			sni = strMap(payload, "sni", "")
		}
		vmessCfg := map[string]interface{}{
			"v": "2", "ps": tag, "add": host, "port": port, "id": uuid, "aid": 0,
			"net": netType, "type": "none", "tls": "none", "scy": cipher,
		}
		if netType == "ws" || netType == "httpupgrade" {
			if path != "" {
				vmessCfg["path"] = path
			}
			if rHost != "" {
				vmessCfg["host"] = rHost
			}
		}
		if netType == "grpc" {
			if serviceName != "" {
				vmessCfg["path"] = serviceName
			} else if path != "" {
				vmessCfg["path"] = path
			}
		}
		if security == "tls" {
			vmessCfg["tls"] = "tls"
			if sni != "" {
				vmessCfg["sni"] = sni
			}
		}
		b, _ := json.Marshal(vmessCfg)
		link = fmt.Sprintf("vmess://%s", base64.StdEncoding.EncodeToString(b))

	case "trojan":
		inMap, dbPayload := inboundRecordToConfigMap(ib)
		link = buildTrojanLinkWithOptions(inMap, host, port, tag, "", dbPayload)

	case "shadowsocks":
		method := strMap(payload, "method", "aes-128-gcm")
		pwd := singbox.NormalizeShadowsocksPassword(method, strMap(payload, "password", "change-me"))
		raw := fmt.Sprintf("%s:%s@%s:%d", method, pwd, host, port)
		b64 := base64.StdEncoding.EncodeToString([]byte(raw))
		link = fmt.Sprintf("ss://%s#%s", b64, url.QueryEscape(tag))

	case "socks":
		link = fmt.Sprintf("socks5://%s:%d#%s", host, port, url.QueryEscape(tag))

	case "http":
		link = fmt.Sprintf("http://%s:%d#%s", host, port, url.QueryEscape(tag))

	case "tuic":
		uuid := strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")
		pwd := strMap(payload, "password", "change-me")
		q := url.Values{}
		q.Set("congestion_control", strMap(payload, "congestion_control", "cubic"))
		q.Set("sni", host)
		q.Set("insecure", "1")
		link = fmt.Sprintf("tuic://%s:%s@%s:%d?%s#%s", uuid, url.QueryEscape(pwd), host, port, q.Encode(), url.QueryEscape(tag))

	case "naive":
		user := strMap(payload, "username", "user")
		pwd := strMap(payload, "password", "change-me")
		link = fmt.Sprintf("naive+https://%s:%s@%s:%d#%s", url.QueryEscape(user), url.QueryEscape(pwd), host, port, url.QueryEscape(tag))

	case "shadowtls":
		pwd := strMap(payload, "password", "change-me")
		sni := strMap(payload, "server_name", "www.microsoft.com")
		q := url.Values{}
		q.Set("version", "3")
		q.Set("sni", sni)
		link = fmt.Sprintf("shadowtls://%s@%s:%d?%s#%s", url.QueryEscape(pwd), host, port, q.Encode(), url.QueryEscape(tag))

	case "hysteria":
		pwd := strMap(payload, "password", strMap(payload, "auth_str", "change-me"))
		q := url.Values{}
		q.Set("insecure", "1")
		if obfs := strMap(payload, "obfs", ""); obfs != "" {
			q.Set("obfs", obfs)
		}
		link = fmt.Sprintf("hysteria://%s@%s:%d?%s#%s", url.QueryEscape(pwd), host, port, q.Encode(), url.QueryEscape(tag))

	case "anytls":
		pwd := strMap(payload, "password", "change-me")
		sniRaw := strMap(payload, "server_name", strMap(payload, "sni", ""))
		certPath := strMap(payload, "certificate_path", "")
		host = linkHost(host, sniRaw, certPath)
		q := url.Values{}
		if linkInsecure(certPath) {
			q.Set("insecure", "1")
		}
		if sni := sniForLink(sniRaw, host); sni != "" {
			q.Set("sni", sni)
		}
		rawLink := fmt.Sprintf("anytls://%s@%s:%d/", url.QueryEscape(pwd), host, port)
		if enc := q.Encode(); enc != "" {
			rawLink += "?" + enc
		}
		link = rawLink + "#" + url.QueryEscape(tag)

	default:
		link = fmt.Sprintf("ss://%s@%s:%d#%s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("aes-128-gcm:change-me@%s:%d", host, port))), host, port, url.QueryEscape(tag))
	}
	return appendOrttQueryToLink(link, meta)
}
