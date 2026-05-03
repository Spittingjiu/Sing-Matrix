package configgen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Spittingjiu/Sing-Matrix/backend/internal/models"
)

func CompileGraph(graph models.Graph) (models.SingBoxConfig, error) {
	cfg := models.SingBoxConfig{
		Log: map[string]interface{}{"level": "info", "timestamp": true},
		DNS: map[string]interface{}{
			"servers": []map[string]interface{}{
				{"tag": "cloudflare", "address": "https://1.1.1.1/dns-query"},
				{"tag": "alidns", "address": "https://dns.alidns.com/dns-query"},
			},
		},
		Inbounds:  []map[string]interface{}{},
		Outbounds: []map[string]interface{}{{"type": "direct", "tag": "direct"}, {"type": "block", "tag": "block"}},
		Route:     map[string]interface{}{"rule_set": []map[string]interface{}{}, "rules": []map[string]interface{}{}, "final": "direct"},
	}

	byID := map[string]models.GraphNode{}
	for _, node := range graph.Nodes {
		byID[node.ID] = node
		switch node.Kind {
		case "inbound-hy2":
			cfg.Inbounds = append(cfg.Inbounds, hysteria2Inbound(node))
		case "inbound-reality":
			cfg.Inbounds = append(cfg.Inbounds, realityInbound(node))
		case "outbound-direct":
			if tag := stringValue(node.Data, "tag", node.ID); tag != "direct" {
				cfg.Outbounds = append(cfg.Outbounds, map[string]interface{}{"type": "direct", "tag": tag})
			}
		case "outbound-selector":
			cfg.Outbounds = append(cfg.Outbounds, map[string]interface{}{"type": "selector", "tag": stringValue(node.Data, "tag", node.ID), "outbounds": []string{"direct", "block"}})
		case "rule-srs":
			ruleSets := cfg.Route["rule_set"].([]map[string]interface{})
			ruleSets = append(ruleSets, map[string]interface{}{
				"type":   "remote",
				"tag":    stringValue(node.Data, "tag", node.ID),
				"format": "binary",
				"url":    stringValue(node.Data, "url", ""),
			})
			cfg.Route["rule_set"] = ruleSets
		}
	}

	rules := make([]map[string]interface{}, 0)
	for _, edge := range graph.Edges {
		source, okS := byID[edge.Source]
		target, okT := byID[edge.Target]
		if !okS || !okT {
			return cfg, fmt.Errorf("edge references unknown node: %s -> %s", edge.Source, edge.Target)
		}
		if source.Kind == "rule-srs" {
			rules = append(rules, map[string]interface{}{
				"rule_set": stringValue(source.Data, "tag", source.ID),
				"outbound": outboundTag(target),
			})
		}
	}
	cfg.Route["rules"] = rules
	return cfg, nil
}

func MarshalPretty(cfg models.SingBoxConfig) ([]byte, error) {
	return json.MarshalIndent(cfg, "", "  ")
}

func hysteria2Inbound(node models.GraphNode) map[string]interface{} {
	return map[string]interface{}{
		"type":                    "hysteria2",
		"tag":                     stringValue(node.Data, "tag", node.ID),
		"listen":                  stringValue(node.Data, "listen", "::"),
		"listen_port":             intValue(node.Data, "port", 44300),
		"users":                   []map[string]interface{}{{"password": stringValue(node.Data, "password", "change-me")}},
		"up_mbps":                 intValue(node.Data, "up_mbps", 100),
		"down_mbps":               intValue(node.Data, "down_mbps", 300),
		"masquerade":              stringValue(node.Data, "masquerade", "https://www.bing.com"),
		"ignore_client_bandwidth": false,
	}
}

func realityInbound(node models.GraphNode) map[string]interface{} {
	return map[string]interface{}{
		"type":        "vless",
		"tag":         stringValue(node.Data, "tag", node.ID),
		"listen":      stringValue(node.Data, "listen", "::"),
		"listen_port": intValue(node.Data, "port", 443),
		"users": []map[string]interface{}{{
			"uuid": stringValue(node.Data, "uuid", "00000000-0000-0000-0000-000000000000"),
			"flow": "xtls-rprx-vision",
		}},
		"tls": map[string]interface{}{
			"enabled":     true,
			"server_name": realityDest(node),
			"reality": map[string]interface{}{
				"enabled":     true,
				"handshake":   map[string]interface{}{"server": realityDest(node), "server_port": 443},
				"private_key": stringValue(node.Data, "private_key", ""),
				"short_id":    shortIDs(node),
			},
		},
	}
}

func outboundTag(node models.GraphNode) string {
	if node.Kind == "outbound-direct" {
		return stringValue(node.Data, "tag", "direct")
	}
	return stringValue(node.Data, "tag", node.ID)
}

func stringValue(data map[string]interface{}, key string, fallback string) string {
	if data == nil {
		return fallback
	}
	if v, ok := data[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intValue(data map[string]interface{}, key string, fallback int) int {
	if data == nil {
		return fallback
	}
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

func realityDest(node models.GraphNode) string {
	if v := stringValue(node.Data, "dest", ""); v != "" {
		return v
	}
	return stringValue(node.Data, "server_name", "www.cloudflare.com")
}

func shortIDs(node models.GraphNode) []string {
	raw := stringValue(node.Data, "short_id", "")
	if raw == "" {
		raw = stringValue(node.Data, "short_ids", "")
	}
	if raw == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
