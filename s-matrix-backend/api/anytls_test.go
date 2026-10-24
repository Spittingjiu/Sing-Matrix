package api

import (
	"strings"
	"testing"

	"s-matrix/models"
)

func TestBuildAnyTLSLinkIncludesSNIAndInsecure(t *testing.T) {
	in := map[string]interface{}{
		"type":        "anytls",
		"tag":         "AnyTLS :45112",
		"listen_port": float64(45112),
		"users": []interface{}{
			map[string]interface{}{"name": "default", "password": "p@ss word"},
		},
		"tls": map[string]interface{}{
			"enabled":     true,
			"server_name": "www.bing.com",
		},
	}
	link := buildAnyTLSLink(in, "1.2.3.4", 45112, "AnyTLS :45112")
	if !strings.HasPrefix(link, "anytls://") {
		t.Fatalf("unexpected scheme: %s", link)
	}
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("missing insecure flag: %s", link)
	}
	if !strings.Contains(link, "sni=www.bing.com") {
		t.Fatalf("missing sni: %s", link)
	}
	if !strings.Contains(link, "1.2.3.4:45112") {
		t.Fatalf("missing endpoint: %s", link)
	}
	if strings.Contains(link, "p@ss word") {
		t.Fatalf("password must be url escaped: %s", link)
	}
}

func TestBuildAnyTLSLinkFromDBPayload(t *testing.T) {
	ib := models.Inbound{
		Tag:     "AnyTLS :45112",
		Type:    "anytls",
		Port:    45112,
		Payload: `{"password":"secretpass","server_name":"cdn.example.com"}`,
	}
	link := buildLinkFromRecord(ib, "5.6.7.8")
	if !strings.HasPrefix(link, "anytls://secretpass@5.6.7.8:45112?") {
		t.Fatalf("unexpected link: %s", link)
	}
	if !strings.Contains(link, "sni=cdn.example.com") {
		t.Fatalf("missing sni from payload: %s", link)
	}
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("missing insecure: %s", link)
	}
}
