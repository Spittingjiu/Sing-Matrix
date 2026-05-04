package singbox

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
)

type Config struct {
	Log       *Log       `json:"log,omitempty"`
	DNS       *DNS       `json:"dns,omitempty"`
	Inbounds  []Inbound  `json:"inbounds"`
	Outbounds []Outbound `json:"outbounds"`
	Route     Route      `json:"route"`
}

type Log struct {
	Level     string `json:"level"`
	Timestamp bool   `json:"timestamp"`
}

type DNS struct {
	Servers []DNSServer `json:"servers"`
}

type DNSServer struct {
	Tag     string `json:"tag"`
	Address string `json:"address"`
}

type Inbound struct {
	Type              string                 `json:"type"`
	Tag               string                 `json:"tag"`
	Listen            string                 `json:"listen,omitempty"`
	ListenPort        int                    `json:"listen_port"`
	Users             []User                 `json:"users,omitempty"`
	TLS               map[string]interface{} `json:"tls,omitempty"`
	Transport         map[string]interface{} `json:"transport,omitempty"`
	Masquerade        string                 `json:"masquerade,omitempty"`
	Method            string                 `json:"method,omitempty"`
	Password          string                 `json:"password,omitempty"`
	Network           string                 `json:"network,omitempty"`
	UpMbps            int                    `json:"up_mbps,omitempty"`
	DownMbps          int                    `json:"down_mbps,omitempty"`
	Obfs              string                 `json:"obfs,omitempty"`
	Version           int                    `json:"version,omitempty"`
	Handshake         map[string]interface{} `json:"handshake,omitempty"`
	StrictMode        bool                   `json:"strict_mode,omitempty"`
	WildcardSNI       string                 `json:"wildcard_sni,omitempty"`
	CongestionControl string                 `json:"congestion_control,omitempty"`
	AuthTimeout       string                 `json:"auth_timeout,omitempty"`
	Heartbeat         string                 `json:"heartbeat,omitempty"`
	ZeroRTTHandshake  bool                   `json:"zero_rtt_handshake,omitempty"`
	PaddingScheme     []string               `json:"padding_scheme,omitempty"`
}

type User struct {
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Flow     string `json:"flow,omitempty"`
	Password string `json:"password,omitempty"`
	Auth     string `json:"auth,omitempty"`
	AuthStr  string `json:"auth_str,omitempty"`
}

type Outbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type Route struct {
	RuleSets []RouteRuleSet `json:"rule_set,omitempty"`
	Rules    []RouteRule    `json:"rules"`
	Final    string         `json:"final"`
}

type RouteRuleSet struct {
	Type   string `json:"type"`
	Tag    string `json:"tag"`
	Format string `json:"format"`
	URL    string `json:"url"`
}

type RouteRule struct {
	Inbound  string `json:"inbound,omitempty"`
	RuleSet  string `json:"rule_set,omitempty"`
	Outbound string `json:"outbound"`
}

func NewRealityInbound(tag string, port int, uuid string, privateKey string, publicKey string, shortID string, serverName string) Inbound {
	return Inbound{
		Type:       "vless",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
		Users:      []User{{UUID: uuid, Flow: "xtls-rprx-vision"}},
		TLS: map[string]interface{}{
			"enabled":     true,
			"server_name": serverName,
			"reality": map[string]interface{}{
				"enabled": true,
				"handshake": map[string]interface{}{
					"server":      serverName,
					"server_port": 443,
				},
				"private_key": privateKey,
				"short_id":    []string{shortID},
			},
		},
	}
}

func NewHysteria2Inbound(tag string, port int, password string, masquerade string) Inbound {
	return Inbound{
		Type:       "hysteria2",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
		Users:      []User{{Password: password}},
		Masquerade: masquerade,
	}
}

func NewVMessInbound(tag string, port int, uuid string, network string, path string, host string, tls bool, serverName string) Inbound {
	in := Inbound{
		Type:       "vmess",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
		Users:      []User{{UUID: uuid}},
	}
	if network != "" && network != "tcp" {
		t := map[string]interface{}{"type": network}
		if path != "" && path != "/" {
			t["path"] = path
		}
		if host != "" {
			t["headers"] = map[string]interface{}{"Host": host}
		}
		in.Transport = t
	}
	// Do not emit inbound TLS here unless certificate/key fields are supported.
	// sing-box rejects bare TLS inbounds with "missing certificate". TLS-capable
	// protocols should either use REALITY or explicit certificate paths.
	_ = tls
	_ = serverName
	return in
}

func NewTrojanInbound(tag string, port int, password string, serverName string) Inbound {
	_ = serverName
	return Inbound{
		Type:       "trojan",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
		Users:      []User{{Password: password}},
	}
}

func NewShadowsocksInbound(tag string, port int, method string, password string) Inbound {
	pwd := normalizeShadowsocksPassword(method, password)
	return Inbound{
		Type:       "shadowsocks",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
		Method:     method,
		Password:   pwd,
	}
}

func normalizeShadowsocksPassword(method string, password string) string {
	// sing-box 2022-blake3 methods require a base64-encoded PSK with an exact
	// decoded length. If the operator enters a human password, derive a stable
	// valid PSK instead of writing a config that sing-box rejects with "bad key".
	keyLen := 0
	switch method {
	case "2022-blake3-aes-128-gcm":
		keyLen = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		keyLen = 32
	}
	if keyLen == 0 {
		return password
	}
	if decoded, err := base64.StdEncoding.DecodeString(password); err == nil && len(decoded) == keyLen {
		return password
	}
	sum := sha256.Sum256([]byte(password))
	return base64.StdEncoding.EncodeToString(sum[:keyLen])
}

func NewHysteriaInbound(tag string, port int, auth string, upMbps int, downMbps int, obfs string, certPath string, keyPath string) Inbound {
	if upMbps <= 0 {
		upMbps = 100
	}
	if downMbps <= 0 {
		downMbps = 100
	}
	in := Inbound{Type: "hysteria", Tag: tag, Listen: "::", ListenPort: port, UpMbps: upMbps, DownMbps: downMbps, Users: []User{{Name: "default", AuthStr: auth}}}
	if obfs != "" {
		in.Obfs = obfs
	}
	if certPath != "" && keyPath != "" {
		in.TLS = map[string]interface{}{"enabled": true, "certificate_path": certPath, "key_path": keyPath}
	}
	return in
}

func NewTUICInbound(tag string, port int, uuid string, password string, congestion string, certPath string, keyPath string) Inbound {
	if congestion == "" {
		congestion = "cubic"
	}
	in := Inbound{Type: "tuic", Tag: tag, Listen: "::", ListenPort: port, Users: []User{{Name: "default", UUID: uuid, Password: password}}, CongestionControl: congestion, AuthTimeout: "3s", Heartbeat: "10s"}
	if certPath != "" && keyPath != "" {
		in.TLS = map[string]interface{}{"enabled": true, "certificate_path": certPath, "key_path": keyPath}
	}
	return in
}

func NewNaiveInbound(tag string, port int, username string, password string, network string, congestion string, certPath string, keyPath string) Inbound {
	if network == "" {
		network = "tcp"
	}
	in := Inbound{Type: "naive", Tag: tag, Listen: "::", ListenPort: port, Network: network, Users: []User{{Username: username, Password: password}}}
	if congestion != "" {
		in.CongestionControl = congestion
	}
	if certPath != "" && keyPath != "" {
		in.TLS = map[string]interface{}{"enabled": true, "certificate_path": certPath, "key_path": keyPath}
	}
	return in
}

func NewShadowTLSInbound(tag string, port int, password string, serverName string, strict bool, wildcard string) Inbound {
	if serverName == "" {
		serverName = "www.microsoft.com"
	}
	return Inbound{Type: "shadowtls", Tag: tag, Listen: "::", ListenPort: port, Version: 3, Users: []User{{Name: "default", Password: password}}, Handshake: map[string]interface{}{"server": serverName, "server_port": 443}, StrictMode: strict, WildcardSNI: wildcard}
}

func NewAnyTLSInbound(tag string, port int, password string, certPath string, keyPath string, padding []string) Inbound {
	in := Inbound{Type: "anytls", Tag: tag, Listen: "::", ListenPort: port, Users: []User{{Name: "default", Password: password}}, PaddingScheme: padding}
	if certPath != "" && keyPath != "" {
		in.TLS = map[string]interface{}{"enabled": true, "certificate_path": certPath, "key_path": keyPath}
	}
	return in
}

func withFinalMaskTransport(in Inbound, finalMask string) Inbound {
	if finalMask == "" {
		return in
	}
	if in.Transport == nil {
		in.Transport = map[string]interface{}{"type": "ws"}
	}
	if _, ok := in.Transport["path"]; !ok {
		in.Transport["path"] = finalMask
	}
	return in
}

func NewSocksInbound(tag string, port int) Inbound {
	return Inbound{
		Type:       "socks",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
	}
}

func NewHTTPInbound(tag string, port int) Inbound {
	return Inbound{
		Type:       "http",
		Tag:        tag,
		Listen:     "::",
		ListenPort: port,
	}
}

func BuildTestConfig() Config {
	return Config{
		Log: &Log{Level: "info", Timestamp: true},
		DNS: &DNS{Servers: []DNSServer{{Tag: "cloudflare", Address: "https://1.1.1.1/dns-query"}, {Tag: "alidns", Address: "https://dns.alidns.com/dns-query"}}},
		Inbounds: []Inbound{
			NewRealityInbound("reality-443", 443, "00000000-0000-0000-0000-000000000000", "CHANGE_ME_PRIVATE_KEY", "CHANGE_ME_PUBLIC_KEY", "0123456789abcdef", "www.cloudflare.com"),
			NewHysteria2Inbound("hy2-44300", 44300, "change-me-password", "https://www.bing.com"),
		},
		Outbounds: []Outbound{{Type: "direct", Tag: "direct"}, {Type: "block", Tag: "block"}},
		Route: Route{
			RuleSets: []RouteRuleSet{{Type: "remote", Tag: "youtube", Format: "binary", URL: "https://example.com/youtube.srs"}},
			Rules:    []RouteRule{{RuleSet: "youtube", Outbound: "direct"}},
			Final:    "direct",
		},
	}
}

func WriteJSON(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func GenerateTestConfigFile(path string) error {
	return WriteJSON(path, BuildTestConfig())
}
