package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"s-matrix/models"
)

// Regression: a REALITY node stored without public_key (created through the
// generic compile path) produced "pbk=" links that no client could import.
func TestBuildLinkFromRecordDerivesRealityPublicKey(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"uuid":        "4cbc6790-c8e3-b7c4-0aeb-9182196df735",
		"private_key": "-HXDjuNqJMdBwg7uCKfPq04pGiQo7vwIkbQFDsjqTl4",
		"short_id":    "1c63cf76e9517f49",
		"server_name": "www.microsoft.com",
	})
	link := buildLinkFromRecord(models.Inbound{Tag: "大鸡", Type: "vless", Port: 35393, Payload: string(payload)}, "23.80.81.106")
	if strings.Contains(link, "pbk=&") || strings.Contains(link, "pbk="+""+"&") {
		t.Fatalf("link has empty pbk: %s", link)
	}
	if !strings.Contains(link, "pbk=TufWk5R-0gQe5xoEiWFwMN_RZS7Yn99gbm1ob3nMxGo") {
		t.Fatalf("link missing derived pbk: %s", link)
	}
	if !strings.Contains(link, "flow=xtls-rprx-vision") || !strings.Contains(link, "security=reality") {
		t.Fatalf("link missing reality params: %s", link)
	}
}

// Regression: creating a second quick node used to overwrite config.json.client.json,
// dropping public keys of earlier nodes.
func TestWriteClientMetaMerges(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	first := singleInboundTopology("n1", "inbound-reality", 1111, "one", map[string]interface{}{
		"tag": "one", "port": 1111, "private_key": "-HXDjuNqJMdBwg7uCKfPq04pGiQo7vwIkbQFDsjqTl4", "short_id": "aaaa1111",
	})
	if err := writeClientMeta(path, first); err != nil {
		t.Fatalf("writeClientMeta: %v", err)
	}
	second := singleInboundTopology("n2", "inbound-reality", 2222, "two", map[string]interface{}{
		"tag": "two", "port": 2222, "private_key": "-HXDjuNqJMdBwg7uCKfPq04pGiQo7vwIkbQFDsjqTl4", "short_id": "bbbb2222",
	})
	if err := writeClientMeta(path, second); err != nil {
		t.Fatalf("writeClientMeta: %v", err)
	}

	raw, err := os.ReadFile(path + ".client.json")
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var root struct {
		Inbounds map[string]map[string]string `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	if root.Inbounds["one"]["public_key"] == "" {
		t.Fatalf("first node lost its public key: %s", string(raw))
	}
	if root.Inbounds["two"]["public_key"] == "" {
		t.Fatalf("second node missing public key: %s", string(raw))
	}
}
