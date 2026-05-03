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
	if len(links) != 2 || !strings.HasPrefix(links[0], "vless://") || !strings.HasPrefix(links[1], "hy2://") {
		t.Fatalf("bad links: %#v", links)
	}
}
