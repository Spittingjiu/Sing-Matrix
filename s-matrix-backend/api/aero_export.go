package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"s-matrix/core/singbox"
	"s-matrix/models"
)

// AeroV1Ortt matches Section 3.1 of the aero-ortt contract.
type AeroV1Ortt struct {
	ID   string `json:"id"`
	Ver  int    `json:"ver"`
	Kid  uint16 `json:"kid"`
	Key  string `json:"key"`
	Port int    `json:"port"`
	Mode string `json:"mode"`
}

// AeroV1Payload is the compact JSON serialized inside aero://v1/<b64url>#<name>.
type AeroV1Payload struct {
	V    int                    `json:"v"`
	Name string                 `json:"name"`
	OB   map[string]interface{} `json:"ob"`
	Ortt *AeroV1Ortt            `json:"ortt,omitempty"`
}

// AeroSubOrtt matches Section 4.2 of the aero-ortt contract (s-matrix.sub.v2).
type AeroSubOrtt struct {
	ID      string `json:"id"`
	Ver     int    `json:"ver"`
	Kid     uint16 `json:"kid"`
	Key     string `json:"key"`
	Port    int    `json:"port"`
	Enabled bool   `json:"enabled"`
}

// AeroSubLinks carries dual-track export links for each node in s-matrix.sub.v2.
type AeroSubLinks struct {
	Aero     string `json:"aero"`
	Standard string `json:"standard"`
}

// AeroSubNode represents a single exported proxy node in s-matrix.sub.v2.
type AeroSubNode struct {
	InboundID uint                   `json:"inbound_id"`
	Tag       string                 `json:"tag"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Enabled   bool                   `json:"enabled"`
	Outbound  map[string]interface{} `json:"outbound"`
	Ortt      *AeroSubOrtt           `json:"ortt,omitempty"`
	Links     AeroSubLinks           `json:"links"`
}

// AeroSubV2Response is returned by GET /api/v1/sub/:token?format=aero-json.
type AeroSubV2Response struct {
	Schema      string        `json:"schema"`
	GeneratedAt int64         `json:"generated_at"`
	ServerHost  string        `json:"server_host"`
	Nodes       []AeroSubNode `json:"nodes"`
}

func shouldAttachOrtt(meta OrttMeta) bool {
	return meta.OrttEnabled && meta.OrttSecret != "" && meta.OrttID != "" && meta.OrttKeyID >= 1
}

func resolveOrttPort(meta OrttMeta) int {
	if meta.OrttPort > 0 && meta.OrttPort <= 65535 {
		return meta.OrttPort
	}
	return defaultOrttPort()
}

func buildAeroV1Link(name string, ob map[string]interface{}, meta OrttMeta) string {
	if ob == nil {
		return ""
	}
	payload := AeroV1Payload{
		V:    1,
		Name: name,
		OB:   ob,
	}
	if shouldAttachOrtt(meta) {
		payload.Ortt = &AeroV1Ortt{
			ID:   meta.OrttID,
			Ver:  2,
			Kid:  meta.OrttKeyID,
			Key:  meta.OrttSecret,
			Port: resolveOrttPort(meta),
			Mode: "auto",
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return "aero://v1/" + encoded + "#" + url.PathEscape(name)
}

func decodeBase64Flexible(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return base64.StdEncoding.DecodeString(s)
}

// appendOrttQueryToLink injects Section 3.2 ORTT parameters (ortt, oid, okid, okey, oport)
// into standard URI links and vmess:// Base64 JSON when node ORTT is enabled.
func appendOrttQueryToLink(link string, meta OrttMeta) string {
	if link == "" || !shouldAttachOrtt(meta) {
		return link
	}
	port := resolveOrttPort(meta)

	if strings.HasPrefix(link, "vmess://") {
		rawB64 := strings.TrimPrefix(link, "vmess://")
		if idx := strings.IndexByte(rawB64, '#'); idx >= 0 {
			rawB64 = rawB64[:idx]
		}
		rawJSON, err := decodeBase64Flexible(rawB64)
		if err != nil {
			return link
		}
		var vmessCfg map[string]interface{}
		if err := json.Unmarshal(rawJSON, &vmessCfg); err != nil || vmessCfg == nil {
			return link
		}
		vmessCfg["ortt"] = 2
		vmessCfg["oid"] = meta.OrttID
		vmessCfg["okid"] = meta.OrttKeyID
		vmessCfg["okey"] = meta.OrttSecret
		vmessCfg["oport"] = port
		updated, err := json.Marshal(vmessCfg)
		if err != nil {
			return link
		}
		return "vmess://" + base64.StdEncoding.EncodeToString(updated)
	}

	frag := ""
	mainPart := link
	if idx := strings.IndexByte(link, '#'); idx >= 0 {
		mainPart = link[:idx]
		frag = link[idx:]
	}
	basePart := mainPart
	q := url.Values{}
	if qIdx := strings.IndexByte(mainPart, '?'); qIdx >= 0 {
		basePart = mainPart[:qIdx]
		if parsed, err := url.ParseQuery(mainPart[qIdx+1:]); err == nil {
			q = parsed
		}
	}
	q.Set("ortt", "2")
	q.Set("oid", meta.OrttID)
	q.Set("okid", strconv.Itoa(int(meta.OrttKeyID)))
	q.Set("okey", meta.OrttSecret)
	q.Set("oport", strconv.Itoa(port))
	return basePart + "?" + q.Encode() + frag
}

func inboundRecordToConfigMap(ib models.Inbound) (map[string]interface{}, map[string]interface{}) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(ib.Payload), &payload); err != nil || payload == nil {
		payload = map[string]interface{}{}
	}
	in := map[string]interface{}{
		"type":        ib.Type,
		"tag":         ib.Tag,
		"listen_port": float64(ib.Port),
	}
	sni := strMap(payload, "server_name", strMap(payload, "sni", ""))
	certPath := strMap(payload, "certificate_path", "")
	switch ib.Type {
	case "vless":
		uuid := strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")
		in["users"] = []interface{}{map[string]interface{}{"uuid": uuid, "flow": "xtls-rprx-vision"}}
		tlsMap := map[string]interface{}{"enabled": true, "server_name": firstNonEmpty(sni, "www.apple.com")}
		reality := map[string]interface{}{"enabled": true}
		if pk := strMap(payload, "private_key", ""); pk != "" {
			reality["private_key"] = pk
		}
		if pbk := strMap(payload, "public_key", ""); pbk != "" {
			reality["public_key"] = pbk
		}
		if sid := strMap(payload, "short_id", ""); sid != "" {
			reality["short_id"] = []interface{}{sid}
		}
		tlsMap["reality"] = reality
		in["tls"] = tlsMap
	case "hysteria2":
		in["users"] = []interface{}{map[string]interface{}{"password": strMap(payload, "password", "change-me")}}
		if obfsType, obfsPass := extractHY2Obfs(payload); obfsType != "" {
			obfsObj := map[string]interface{}{"type": obfsType}
			if obfsPass != "" {
				obfsObj["password"] = obfsPass
			}
			in["obfs"] = obfsObj
		}
		tlsMap := map[string]interface{}{"enabled": true}
		if sni != "" {
			tlsMap["server_name"] = sni
		}
		if certPath != "" {
			tlsMap["certificate_path"] = certPath
		}
		in["tls"] = tlsMap
	case "anytls":
		in["users"] = []interface{}{map[string]interface{}{"name": "default", "password": strMap(payload, "password", "change-me")}}
		tlsMap := map[string]interface{}{"enabled": true}
		if sni != "" {
			tlsMap["server_name"] = sni
		}
		if certPath != "" {
			tlsMap["certificate_path"] = certPath
		}
		in["tls"] = tlsMap
	case "vmess":
		in["users"] = []interface{}{map[string]interface{}{"uuid": strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000")}}
		netType := strMap(payload, "network", "tcp")
		if netType != "" && netType != "tcp" {
			tr := map[string]interface{}{"type": netType}
			if netType == "grpc" {
				svc := strMap(payload, "service_name", "")
				if svc == "" {
					svc = strings.TrimPrefix(strMap(payload, "path", ""), "/")
				}
				if svc != "" {
					tr["service_name"] = svc
				}
			} else {
				if p := strMap(payload, "path", ""); p != "" {
					tr["path"] = p
				}
				if h := strMap(payload, "host", ""); h != "" {
					tr["headers"] = map[string]interface{}{"Host": h}
				}
			}
			in["transport"] = tr
		}
		tlsOn := strMap(payload, "security", "") == "tls"
		if b, ok := parseBoolAny(payload["tls"]); ok && b {
			tlsOn = true
		}
		if tlsOn {
			tlsMap := map[string]interface{}{"enabled": true}
			if sni != "" {
				tlsMap["server_name"] = sni
			}
			if certPath != "" {
				tlsMap["certificate_path"] = certPath
			}
			in["tls"] = tlsMap
		}
	case "trojan":
		in["users"] = []interface{}{map[string]interface{}{"password": strMap(payload, "password", "change-me")}}
		tlsMap := map[string]interface{}{"enabled": true}
		if sni != "" {
			tlsMap["server_name"] = sni
		}
		if certPath != "" {
			tlsMap["certificate_path"] = certPath
		}
		in["tls"] = tlsMap
	case "shadowsocks":
		method := strMap(payload, "method", "aes-128-gcm")
		pwd := singbox.NormalizeShadowsocksPassword(method, strMap(payload, "password", "change-me"))
		in["method"] = method
		in["password"] = pwd
	case "tuic":
		in["users"] = []interface{}{map[string]interface{}{
			"uuid":     strMap(payload, "uuid", "00000000-0000-0000-0000-000000000000"),
			"password": strMap(payload, "password", "change-me"),
		}}
		in["congestion_control"] = strMap(payload, "congestion_control", "cubic")
		tlsMap := map[string]interface{}{"enabled": true}
		if sni != "" {
			tlsMap["server_name"] = sni
		}
		if certPath != "" {
			tlsMap["certificate_path"] = certPath
		}
		in["tls"] = tlsMap
	case "naive":
		in["users"] = []interface{}{map[string]interface{}{
			"username": strMap(payload, "username", "user"),
			"password": strMap(payload, "password", "change-me"),
		}}
		tlsMap := map[string]interface{}{"enabled": true}
		if sni != "" {
			tlsMap["server_name"] = sni
		}
		if certPath != "" {
			tlsMap["certificate_path"] = certPath
		}
		in["tls"] = tlsMap
	case "shadowtls":
		in["version"] = 3
		if v, ok := parseIntAny(payload["version"]); ok && v > 0 {
			in["version"] = v
		}
		in["users"] = []interface{}{map[string]interface{}{"password": strMap(payload, "password", "change-me")}}
		in["handshake"] = map[string]interface{}{"server": firstNonEmpty(sni, "www.microsoft.com")}
	case "hysteria":
		in["users"] = []interface{}{map[string]interface{}{"auth_str": strMap(payload, "auth_str", strMap(payload, "password", "change-me"))}}
		in["up_mbps"] = 100
		in["down_mbps"] = 100
		if up, ok := parseIntAny(payload["up_mbps"]); ok && up > 0 {
			in["up_mbps"] = up
		}
		if down, ok := parseIntAny(payload["down_mbps"]); ok && down > 0 {
			in["down_mbps"] = down
		}
		if obfs := strMap(payload, "obfs", ""); obfs != "" {
			in["obfs"] = obfs
		}
		if certPath != "" {
			in["tls"] = map[string]interface{}{"enabled": true, "certificate_path": certPath}
		}
	}
	return in, payload
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func resolveNodeSNIAndCert(in map[string]interface{}, configPath, tag string, dbPayload map[string]interface{}) (string, string) {
	var meta map[string]string
	if configPath != "" && tag != "" {
		meta = loadClientInboundMeta(configPath, tag)
	}
	tlsIn, _ := in["tls"].(map[string]interface{})
	certPath := firstNonEmpty(tlsCertPathFromContext(tlsIn), strMap(dbPayload, "certificate_path", ""))
	sniRaw := firstNonEmpty(
		inboundServerName(in),
		strMap(in, "server_name", ""),
		strMap(in, "sni", ""),
		strMap(dbPayload, "server_name", ""),
		strMap(dbPayload, "sni", ""),
		meta["sni"],
		meta["server_name"],
	)
	return sniRaw, certPath
}

func extractHY2Obfs(sources ...map[string]interface{}) (string, string) {
	var obfsType, obfsPass string
	for _, m := range sources {
		if m == nil {
			continue
		}
		if raw, ok := m["obfs"]; ok && raw != nil {
			switch v := raw.(type) {
			case string:
				if obfsType == "" && strings.TrimSpace(v) != "" {
					obfsType = strings.TrimSpace(v)
				}
			case map[string]interface{}:
				if obfsType == "" {
					obfsType = strings.TrimSpace(strMap(v, "type", ""))
				}
				if obfsPass == "" {
					obfsPass = firstNonEmpty(strMap(v, "password", ""), strMap(v, "obfs_password", ""))
				}
			}
		}
		if obfsPass == "" {
			obfsPass = firstNonEmpty(strMap(m, "obfs_password", ""), strMap(m, "obfs-password", ""))
		}
	}
	return obfsType, obfsPass
}

func sanitizeClientTransport(tr map[string]interface{}) map[string]interface{} {
	if tr == nil {
		return nil
	}
	typ := strMap(tr, "type", "")
	if typ == "" || typ == "tcp" {
		return nil
	}
	out := map[string]interface{}{"type": typ}
	switch typ {
	case "grpc":
		if svc := strMap(tr, "service_name", ""); svc != "" {
			out["service_name"] = svc
		}
	case "ws", "httpupgrade", "http":
		if p := strMap(tr, "path", ""); p != "" {
			out["path"] = p
		}
		if h, ok := tr["headers"].(map[string]interface{}); ok {
			if host := strMap(h, "Host", ""); host != "" {
				out["headers"] = map[string]interface{}{"Host": host}
			}
		}
	}
	return out
}

// buildClientOutboundFromConfig builds a clean sing-box 1.13+/1.14 client outbound JSON
// object without server-only fields (listen, users, private_key, certificate_path, key_path).
func buildClientOutboundFromConfig(in map[string]interface{}, host, configPath string, dbPayload map[string]interface{}) (map[string]interface{}, string) {
	typ := strings.ToLower(strMap(in, "type", ""))
	tag := strMap(in, "tag", "S-Matrix")
	port := intMap(in, "listen_port", 0)
	if typ == "" || port <= 0 {
		return nil, ""
	}
	meta := loadClientInboundMeta(configPath, tag)
	tlsIn, _ := in["tls"].(map[string]interface{})
	sniRaw, certPath := resolveNodeSNIAndCert(in, configPath, tag, dbPayload)

	switch typ {
	case "vless":
		uuid := firstUserValue(in, "uuid", strMap(dbPayload, "uuid", "00000000-0000-0000-0000-000000000000"))
		realityIn, _ := tlsIn["reality"].(map[string]interface{})
		pbk := firstNonEmpty(meta["public_key"], strMap(dbPayload, "public_key", ""), strMap(realityIn, "public_key", ""))
		if pbk == "" {
			pbk = singbox.DeriveRealityPublicKey(firstNonEmpty(strMap(realityIn, "private_key", ""), strMap(dbPayload, "private_key", "")))
		}
		sid := firstNonEmpty(meta["short_id"], strMap(dbPayload, "short_id", ""), firstStringValue(realityIn, "short_id", ""))
		sni := firstNonEmpty(sniRaw, "www.apple.com")
		realityOut := map[string]interface{}{
			"enabled":    true,
			"public_key": pbk,
		}
		if sid != "" {
			realityOut["short_id"] = sid
		}
		tlsOut := map[string]interface{}{
			"enabled":     true,
			"server_name": sni,
			"utls": map[string]interface{}{
				"enabled":     true,
				"fingerprint": "chrome",
			},
			"reality": realityOut,
		}
		ob := map[string]interface{}{
			"type":        "vless",
			"server":      host,
			"server_port": port,
			"uuid":        uuid,
			"tls":         tlsOut,
		}
		trMap, _ := in["transport"].(map[string]interface{})
		if cleanTr := sanitizeClientTransport(trMap); cleanTr != nil {
			ob["transport"] = cleanTr
		} else {
			ob["flow"] = "xtls-rprx-vision"
		}
		return ob, "vless"

	case "hysteria2":
		pwd := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
		dialHost := linkHost(host, sniRaw, certPath)
		tlsOut := map[string]interface{}{"enabled": true}
		if sni := sniForLink(sniRaw, dialHost); sni != "" {
			tlsOut["server_name"] = sni
		}
		if linkInsecure(certPath) {
			tlsOut["insecure"] = true
		}
		ob := map[string]interface{}{
			"type":        "hysteria2",
			"server":      dialHost,
			"server_port": port,
			"password":    pwd,
			"tls":         tlsOut,
		}
		if obfsType, obfsPass := extractHY2Obfs(in, dbPayload); obfsType != "" {
			obfsObj := map[string]interface{}{"type": obfsType}
			if obfsPass != "" {
				obfsObj["password"] = obfsPass
			}
			ob["obfs"] = obfsObj
		}
		return ob, "hysteria2"

	case "anytls":
		pwd := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
		dialHost := linkHost(host, sniRaw, certPath)
		tlsOut := map[string]interface{}{"enabled": true}
		if sni := sniForLink(sniRaw, dialHost); sni != "" {
			tlsOut["server_name"] = sni
		}
		if linkInsecure(certPath) {
			tlsOut["insecure"] = true
		}
		ob := map[string]interface{}{
			"type":                        "anytls",
			"server":                      dialHost,
			"server_port":                 port,
			"password":                    pwd,
			"idle_session_check_interval": "30s",
			"idle_session_timeout":        "30s",
			"tls":                         tlsOut,
		}
		return ob, "anytls"

	case "vmess":
		uuid := firstUserValue(in, "uuid", strMap(dbPayload, "uuid", "00000000-0000-0000-0000-000000000000"))
		cipher := firstNonEmpty(strMap(dbPayload, "vmess_security", ""), meta["vmess_security"], firstUserValue(in, "security", "auto"))
		ob := map[string]interface{}{
			"type":        "vmess",
			"server":      host,
			"server_port": port,
			"uuid":        uuid,
			"security":    cipher,
			"alter_id":    0,
		}
		trMap, _ := in["transport"].(map[string]interface{})
		if cleanTr := sanitizeClientTransport(trMap); cleanTr != nil {
			ob["transport"] = cleanTr
		}
		tlsEnabled := false
		if tlsIn != nil {
			if en, _ := tlsIn["enabled"].(bool); en {
				tlsEnabled = true
			}
		}
		if !tlsEnabled && strMap(dbPayload, "security", "") == "tls" {
			tlsEnabled = true
		}
		if tlsEnabled {
			dialHost := linkHost(host, sniRaw, certPath)
			ob["server"] = dialHost
			tlsOut := map[string]interface{}{"enabled": true}
			if sni := sniForLink(sniRaw, dialHost); sni != "" {
				tlsOut["server_name"] = sni
			}
			if linkInsecure(certPath) {
				tlsOut["insecure"] = true
			}
			ob["tls"] = tlsOut
		}
		return ob, "vmess"

	case "trojan":
		pwd := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
		dialHost := linkHost(host, sniRaw, certPath)
		tlsOut := map[string]interface{}{"enabled": true}
		if sni := sniForLink(firstNonEmpty(sniRaw, dialHost), dialHost); sni != "" {
			tlsOut["server_name"] = sni
		}
		if linkInsecure(certPath) {
			tlsOut["insecure"] = true
		}
		return map[string]interface{}{
			"type":        "trojan",
			"server":      dialHost,
			"server_port": port,
			"password":    pwd,
			"tls":         tlsOut,
		}, "trojan"

	case "shadowsocks":
		method := firstNonEmpty(strMap(in, "method", ""), strMap(dbPayload, "method", ""), "aes-128-gcm")
		pwd := firstNonEmpty(strMap(in, "password", ""), firstUserValue(in, "password", ""), strMap(dbPayload, "password", ""), "change-me")
		pwd = singbox.NormalizeShadowsocksPassword(method, pwd)
		return map[string]interface{}{
			"type":        "shadowsocks",
			"server":      host,
			"server_port": port,
			"method":      method,
			"password":    pwd,
		}, "shadowsocks"

	case "tuic":
		uuid := firstUserValue(in, "uuid", strMap(dbPayload, "uuid", "00000000-0000-0000-0000-000000000000"))
		pwd := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
		cc := firstNonEmpty(strMap(in, "congestion_control", ""), strMap(dbPayload, "congestion_control", ""), "cubic")
		dialHost := linkHost(host, sniRaw, certPath)
		tlsOut := map[string]interface{}{"enabled": true}
		if sni := sniForLink(sniRaw, dialHost); sni != "" {
			tlsOut["server_name"] = sni
		}
		if linkInsecure(certPath) {
			tlsOut["insecure"] = true
		}
		return map[string]interface{}{
			"type":               "tuic",
			"server":             dialHost,
			"server_port":        port,
			"uuid":               uuid,
			"password":           pwd,
			"congestion_control": cc,
			"tls":                tlsOut,
		}, "tuic"

	case "naive":
		user := firstUserValue(in, "username", strMap(dbPayload, "username", "user"))
		pwd := firstUserValue(in, "password", strMap(dbPayload, "password", "change-me"))
		dialHost := linkHost(host, sniRaw, certPath)
		tlsOut := map[string]interface{}{"enabled": true}
		if sni := sniForLink(sniRaw, dialHost); sni != "" {
			tlsOut["server_name"] = sni
		}
		if linkInsecure(certPath) {
			tlsOut["insecure"] = true
		}
		return map[string]interface{}{
			"type":        "naive",
			"server":      dialHost,
			"server_port": port,
			"username":    user,
			"password":    pwd,
			"tls":         tlsOut,
		}, "naive"

	case "shadowtls":
		pwd := firstUserValue(in, "password", firstNonEmpty(strMap(in, "password", ""), strMap(dbPayload, "password", ""), "change-me"))
		hs, _ := in["handshake"].(map[string]interface{})
		sni := firstNonEmpty(strMap(hs, "server", ""), sniRaw, "www.microsoft.com")
		ver := intMap(in, "version", 3)
		return map[string]interface{}{
			"type":        "shadowtls",
			"server":      host,
			"server_port": port,
			"version":     ver,
			"password":    pwd,
			"tls": map[string]interface{}{
				"enabled":     true,
				"server_name": sni,
			},
		}, "shadowtls"

	case "hysteria":
		pwd := firstUserValue(in, "auth_str", firstUserValue(in, "password", strMap(dbPayload, "password", "change-me")))
		tlsOut := map[string]interface{}{"enabled": true}
		if linkInsecure(certPath) {
			tlsOut["insecure"] = true
		}
		ob := map[string]interface{}{
			"type":        "hysteria",
			"server":      host,
			"server_port": port,
			"auth_str":    pwd,
			"up_mbps":     intMap(in, "up_mbps", 100),
			"down_mbps":   intMap(in, "down_mbps", 100),
			"tls":         tlsOut,
		}
		if obfs := firstNonEmpty(strMap(in, "obfs", ""), strMap(dbPayload, "obfs", "")); obfs != "" {
			ob["obfs"] = obfs
		}
		return ob, "hysteria"

	case "socks":
		return map[string]interface{}{
			"type":        "socks",
			"server":      host,
			"server_port": port,
			"version":     "5",
		}, "socks"

	case "http":
		return map[string]interface{}{
			"type":        "http",
			"server":      host,
			"server_port": port,
		}, "http"
	}
	return nil, ""
}

func buildClientOutboundFromRecord(ib models.Inbound, host string) (map[string]interface{}, string) {
	in, payload := inboundRecordToConfigMap(ib)
	return buildClientOutboundFromConfig(in, host, "", payload)
}

func buildAeroLinkFromRecord(ib models.Inbound, host string) (string, error) {
	ob, _ := buildClientOutboundFromRecord(ib, host)
	if ob == nil {
		return "", fmt.Errorf("unsupported inbound type: %s", ib.Type)
	}
	meta, _ := extractOrttMetaFromPayloadJSON(ib.Payload)
	return buildAeroV1Link(ib.Tag, ob, meta), nil
}

func resolveExportedOrttMeta(dbPayload map[string]interface{}, clientDoc *clientMetaDocument, tag string) (OrttMeta, bool) {
	if dbPayload != nil {
		if m, ok := extractOrttMetaFromMap(dbPayload); ok {
			return m, true
		}
	}
	return extractOrttMetaFromClientDoc(clientDoc, tag)
}

func cloneMapShallow(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func buildStandardLinkWithExtras(in map[string]interface{}, host, configPath string, dbPayload map[string]interface{}, meta OrttMeta) string {
	typ := strMap(in, "type", "")
	tag := strMap(in, "tag", "S-Matrix")
	port := intMap(in, "listen_port", 0)
	var link string
	switch typ {
	case "vless":
		link = buildVLESSLink(in, host, port, tag, configPath)
	case "hysteria2":
		link = buildHY2LinkWithOptions(in, host, port, tag, configPath, dbPayload)
	case "vmess":
		inCopy := in
		if cipher := strMap(dbPayload, "vmess_security", ""); cipher != "" {
			inCopy = cloneMapShallow(in)
			if users, ok := in["users"].([]interface{}); ok && len(users) > 0 {
				if u0, ok := users[0].(map[string]interface{}); ok {
					uCopy := cloneMapShallow(u0)
					uCopy["security"] = cipher
					inCopy["users"] = []interface{}{uCopy}
				}
			}
		}
		link = buildVMessLink(inCopy, host, port, tag)
	case "trojan":
		link = buildTrojanLinkWithOptions(in, host, port, tag, configPath, dbPayload)
	case "shadowsocks":
		link = buildSSLink(in, host, port, tag)
	case "socks":
		link = fmt.Sprintf("socks5://%s:%d#%s", host, port, url.QueryEscape(tag))
	case "http":
		link = fmt.Sprintf("http://%s:%d#%s", host, port, url.QueryEscape(tag))
	case "tuic":
		link = buildTUICLink(in, host, port, tag)
	case "naive":
		link = buildNaiveLink(in, host, port, tag)
	case "shadowtls":
		link = buildShadowTLSLink(in, host, port, tag)
	case "hysteria":
		link = buildHysteriaLink(in, host, port, tag)
	case "anytls":
		link = buildAnyTLSLink(in, host, port, tag)
	}
	if link == "" {
		return ""
	}
	return appendOrttQueryToLink(link, meta)
}

func toAeroSubOrtt(meta OrttMeta, hasMeta bool) *AeroSubOrtt {
	if !hasMeta || (meta.OrttID == "" && meta.OrttSecret == "" && meta.OrttKeyID == 0) {
		return nil
	}
	return &AeroSubOrtt{
		ID:      meta.OrttID,
		Ver:     2,
		Kid:     meta.OrttKeyID,
		Key:     meta.OrttSecret,
		Port:    resolveOrttPort(meta),
		Enabled: meta.OrttEnabled,
	}
}

func buildExportedNodes(db *gorm.DB, configPath, host string) ([]AeroSubNode, error) {
	dbByTag := make(map[string]models.Inbound)
	var dbRows []models.Inbound
	if db != nil {
		if err := db.Order("id asc").Find(&dbRows).Error; err == nil {
			for _, r := range dbRows {
				dbByTag[r.Tag] = r
			}
		}
	}
	clientDoc := readClientMetaDoc(configPath)

	var cfg runtimeConfig
	hasConfig := false
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := json.Unmarshal(data, &cfg); err != nil {
				return nil, err
			}
			hasConfig = len(cfg.Inbounds) > 0
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	nodes := make([]AeroSubNode, 0)
	if hasConfig {
		for idx, in := range cfg.Inbounds {
			tag := strMap(in, "tag", "S-Matrix")
			var inboundID uint = uint(idx + 1)
			var dbPayload map[string]interface{}
			if dbRow, ok := dbByTag[tag]; ok {
				if !dbRow.Enabled {
					continue
				}
				inboundID = dbRow.ID
				_ = json.Unmarshal([]byte(dbRow.Payload), &dbPayload)
			}
			meta, hasMeta := resolveExportedOrttMeta(dbPayload, clientDoc, tag)
			stdLink := buildStandardLinkWithExtras(in, host, configPath, dbPayload, meta)
			ob, canonType := buildClientOutboundFromConfig(in, host, configPath, dbPayload)
			aeroLink := ""
			var subOutbound map[string]interface{}
			if ob != nil {
				aeroLink = buildAeroV1Link(tag, ob, meta)
				subOutbound = cloneMapShallow(ob)
				subOutbound["tag"] = tag
			}
			if stdLink == "" && aeroLink == "" {
				continue
			}
			nodes = append(nodes, AeroSubNode{
				InboundID: inboundID,
				Tag:       tag,
				Name:      tag,
				Type:      canonType,
				Enabled:   true,
				Outbound:  subOutbound,
				Ortt:      toAeroSubOrtt(meta, hasMeta),
				Links: AeroSubLinks{
					Aero:     aeroLink,
					Standard: stdLink,
				},
			})
		}
		return nodes, nil
	}

	// Fallback when no compiled config.json exists yet but DB holds enabled nodes.
	for _, row := range dbRows {
		if !row.Enabled {
			continue
		}
		in, dbPayload := inboundRecordToConfigMap(row)
		meta, hasMeta := extractOrttMetaFromMap(dbPayload)
		stdLink := buildLinkFromRecord(row, host)
		ob, canonType := buildClientOutboundFromConfig(in, host, configPath, dbPayload)
		aeroLink := ""
		var subOutbound map[string]interface{}
		if ob != nil {
			aeroLink = buildAeroV1Link(row.Tag, ob, meta)
			subOutbound = cloneMapShallow(ob)
			subOutbound["tag"] = row.Tag
		}
		nodes = append(nodes, AeroSubNode{
			InboundID: row.ID,
			Tag:       row.Tag,
			Name:      row.Tag,
			Type:      canonType,
			Enabled:   true,
			Outbound:  subOutbound,
			Ortt:      toAeroSubOrtt(meta, hasMeta),
			Links: AeroSubLinks{
				Aero:     aeroLink,
				Standard: stdLink,
			},
		})
	}
	return nodes, nil
}

func BuildShareLinksWithDB(db *gorm.DB, configPath, host string) ([]string, error) {
	nodes, err := buildExportedNodes(db, configPath, host)
	if err != nil {
		return nil, err
	}
	links := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Links.Standard != "" {
			links = append(links, n.Links.Standard)
		}
	}
	return links, nil
}

func BuildAeroLinksWithDB(db *gorm.DB, configPath, host string) ([]string, error) {
	nodes, err := buildExportedNodes(db, configPath, host)
	if err != nil {
		return nil, err
	}
	links := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Links.Aero != "" {
			links = append(links, n.Links.Aero)
		}
	}
	return links, nil
}

func BuildAeroLinks(configPath, host string) ([]string, error) {
	return BuildAeroLinksWithDB(nil, configPath, host)
}

func BuildAeroSubV2(db *gorm.DB, configPath, host string) (*AeroSubV2Response, error) {
	nodes, err := buildExportedNodes(db, configPath, host)
	if err != nil {
		return nil, err
	}
	return &AeroSubV2Response{
		Schema:      "s-matrix.sub.v2",
		GeneratedAt: time.Now().Unix(),
		ServerHost:  host,
		Nodes:       nodes,
	}, nil
}
