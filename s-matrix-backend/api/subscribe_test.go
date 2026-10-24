package api

import (
	"os"
	"strings"
	"testing"
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
