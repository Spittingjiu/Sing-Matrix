package api

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"s-matrix/models"
)

func TestBuildShareLinks(t *testing.T) {
	path := "test-config-sub.json"
	defer os.Remove(path)
	if err := os.WriteFile(path, []byte(`{"inbounds":[{"type":"vless","tag":"reality","listen_port":443,"users":[{"uuid":"00000000-0000-0000-0000-000000000000"}],"tls":{"server_name":"www.cloudflare.com","reality":{"public_key":"PUB"}}},{"type":"hysteria2","tag":"hy2","listen_port":44300,"users":[{"password":"pw"}]}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	links, err := BuildShareLinks(path, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || !strings.HasPrefix(links[0], "vless://") || !strings.HasPrefix(links[1], "hysteria2://") {
		t.Fatalf("bad links: %#v", links)
	}
	// The hysteria2 inbound has no TLS block at all, so the link must not invent
	// an SNI from the connect address (RFC 6066 forbids IP literals in SNI).
	if strings.Contains(links[1], "sni=") {
		t.Fatalf("hy2 link must omit sni when the inbound has none: %s", links[1])
	}
}

func decodeVMessLink(t *testing.T, link string) map[string]interface{} {
	t.Helper()
	if !strings.HasPrefix(link, "vmess://") {
		t.Fatalf("bad link: %s", link)
	}
	rawJSON, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decoded := map[string]interface{}{}
	if err := json.Unmarshal(rawJSON, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return decoded
}

func TestBuildVMessLinkCarriesSecurityAndHost(t *testing.T) {
	// The compiled server config carries no per-user cipher (sing-box
	// rejects an unknown "security" field in vmess users), so the
	// config-based link defaults scy to auto and still connects.
	in := map[string]interface{}{
		"type": "vmess", "tag": "VMess :50123", "listen_port": 50123,
		"users":     []interface{}{map[string]interface{}{"uuid": "11111111-2222-4333-8444-555555555555"}},
		"transport": map[string]interface{}{"type": "ws", "path": "/chat", "headers": map[string]interface{}{"Host": "www.bing.com"}},
		"tls":       map[string]interface{}{"enabled": true, "server_name": "www.bing.com"},
	}
	decoded := decodeVMessLink(t, buildVMessLink(in, "127.0.0.1", 50123, "VMess :50123"))
	if decoded["scy"] != "auto" || decoded["net"] != "ws" || decoded["path"] != "/chat" || decoded["host"] != "www.bing.com" || decoded["tls"] != "tls" || decoded["sni"] != "www.bing.com" {
		t.Fatalf("bad vmess json: %+v", decoded)
	}
}

func TestBuildLinkFromRecordVMessCarriesCipher(t *testing.T) {
	// The DB record carries the cipher in vmess_security; the record-based
	// link must emit it as scy, with the TLS flag from "security".
	ib := models.Inbound{Tag: "VMess :50123", Type: "vmess", Port: 50123,
		Payload: `{"uuid":"11111111-2222-4333-8444-555555555555","network":"ws","path":"/chat","host":"www.bing.com","vmess_security":"aes-128-gcm","security":"tls","server_name":"www.bing.com"}`}
	decoded := decodeVMessLink(t, buildLinkFromRecord(ib, "127.0.0.1"))
	if decoded["scy"] != "aes-128-gcm" || decoded["net"] != "ws" || decoded["path"] != "/chat" || decoded["host"] != "www.bing.com" || decoded["tls"] != "tls" || decoded["sni"] != "www.bing.com" {
		t.Fatalf("bad vmess json: %+v", decoded)
	}
}
