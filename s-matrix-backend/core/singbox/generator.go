package singbox

import (
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
	Type       string                 `json:"type"`
	Tag        string                 `json:"tag"`
	Listen     string                 `json:"listen,omitempty"`
	ListenPort int                    `json:"listen_port"`
	Users      []User                 `json:"users,omitempty"`
	TLS        map[string]interface{} `json:"tls,omitempty"`
	Masquerade string                 `json:"masquerade,omitempty"`
}

type User struct {
	UUID     string `json:"uuid,omitempty"`
	Flow     string `json:"flow,omitempty"`
	Password string `json:"password,omitempty"`
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
