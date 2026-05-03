package singbox

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type UIData struct {
	Nodes []UINode `json:"nodes" binding:"required"`
	Edges []UIEdge `json:"edges"`
}

type UINode struct {
	ID       string                 `json:"id" binding:"required"`
	Kind     string                 `json:"kind"`
	Label    string                 `json:"label"`
	Position UIPosition             `json:"position"`
	Data     map[string]interface{} `json:"data"`
}

type UIPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type UIEdge struct {
	ID     string `json:"id"`
	Source string `json:"source" binding:"required"`
	Target string `json:"target" binding:"required"`
}

func CompileToSingbox(uiData UIData) (Config, error) {
	cfg := Config{
		Log:       &Log{Level: "info", Timestamp: true},
		DNS:       &DNS{Servers: []DNSServer{{Tag: "cloudflare", Address: "https://1.1.1.1/dns-query"}, {Tag: "alidns", Address: "https://dns.alidns.com/dns-query"}}},
		Inbounds:  []Inbound{},
		Outbounds: []Outbound{{Type: "direct", Tag: "direct"}, {Type: "block", Tag: "block"}},
		Route:     Route{RuleSets: []RouteRuleSet{}, Rules: []RouteRule{}, Final: "direct"},
	}

	byID := map[string]UINode{}
	inboundTags := map[string]string{}
	usedPorts := map[int]bool{}
	for _, node := range uiData.Nodes {
		if node.ID == "" {
			return cfg, fmt.Errorf("node id is required")
		}
		kind := nodeKind(node)
		node.Kind = kind
		byID[node.ID] = node
		switch kind {
		case "inbound-hy2":
			port := PickAvailablePort(num(node.Data, "port", 0), usedPorts)
			if port == 0 {
				return cfg, fmt.Errorf("no available port for %s", node.ID)
			}
			in := NewHysteria2Inbound(str(node.Data, "tag", node.ID), port, str(node.Data, "password", "change-me"), str(node.Data, "masquerade", "https://www.bing.com"))
			cfg.Inbounds = append(cfg.Inbounds, in)
			inboundTags[node.ID] = in.Tag
		case "inbound-reality":
			dest := str(node.Data, "dest", str(node.Data, "server_name", "www.cloudflare.com"))
			port := PickAvailablePort(num(node.Data, "port", 0), usedPorts)
			if port == 0 {
				return cfg, fmt.Errorf("no available port for %s", node.ID)
			}
			in := NewRealityInbound(str(node.Data, "tag", node.ID), port, str(node.Data, "uuid", "00000000-0000-0000-0000-000000000000"), str(node.Data, "private_key", ""), firstShortID(node.Data), dest)
			cfg.Inbounds = append(cfg.Inbounds, in)
			inboundTags[node.ID] = in.Tag
		case "outbound-direct":
			tag := str(node.Data, "tag", "direct")
			if tag != "direct" {
				cfg.Outbounds = append(cfg.Outbounds, Outbound{Type: "direct", Tag: tag})
			}
		case "outbound-selector":
			cfg.Outbounds = append(cfg.Outbounds, Outbound{Type: "selector", Tag: str(node.Data, "tag", node.ID)})
		case "rule-srs":
			cfg.Route.RuleSets = append(cfg.Route.RuleSets, RouteRuleSet{Type: "remote", Tag: str(node.Data, "tag", node.ID), Format: "binary", URL: str(node.Data, "url", "")})
		}
	}

	inboundToRule := map[string][]string{}
	ruleToOutbound := map[string][]string{}
	for _, edge := range uiData.Edges {
		src, okS := byID[edge.Source]
		dst, okT := byID[edge.Target]
		if !okS || !okT {
			return cfg, fmt.Errorf("edge references unknown node: %s -> %s", edge.Source, edge.Target)
		}
		srcKind, dstKind := nodeKind(src), nodeKind(dst)
		if strings.HasPrefix(srcKind, "inbound-") && dstKind == "rule-srs" {
			inboundToRule[src.ID] = append(inboundToRule[src.ID], dst.ID)
		}
		if srcKind == "rule-srs" && strings.HasPrefix(dstKind, "outbound-") {
			ruleToOutbound[src.ID] = append(ruleToOutbound[src.ID], outboundTag(dst))
		}
	}

	for inboundID, ruleIDs := range inboundToRule {
		for _, ruleID := range ruleIDs {
			outs := ruleToOutbound[ruleID]
			if len(outs) == 0 {
				outs = []string{"direct"}
			}
			for _, out := range outs {
				cfg.Route.Rules = append(cfg.Route.Rules, RouteRule{Inbound: inboundTags[inboundID], RuleSet: str(byID[ruleID].Data, "tag", ruleID), Outbound: out})
			}
		}
	}
	return cfg, nil
}

func CompileAndWrite(uiData UIData, path string) (Config, error) {
	cfg, err := CompileToSingbox(uiData)
	if err != nil {
		return cfg, err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return cfg, err
	}
	return cfg, os.WriteFile(path, data, 0644)
}

func nodeKind(node UINode) string {
	if node.Kind != "" {
		return node.Kind
	}
	return str(node.Data, "kind", "")
}

func outboundTag(node UINode) string { return str(node.Data, "tag", node.ID) }

func str(data map[string]interface{}, key, fallback string) string {
	if data == nil {
		return fallback
	}
	if v, ok := data[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func num(data map[string]interface{}, key string, fallback int) int {
	if data == nil {
		return fallback
	}
	switch v := data[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return fallback
}

func firstShortID(data map[string]interface{}) string {
	raw := str(data, "short_id", str(data, "short_ids", ""))
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' })
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
