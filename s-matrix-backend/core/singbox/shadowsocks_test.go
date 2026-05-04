package singbox

import (
	"encoding/base64"
	"testing"
)

func TestNormalizeShadowsocks2022Password(t *testing.T) {
	cases := []struct {
		method string
		want   int
	}{
		{"2022-blake3-aes-128-gcm", 16},
		{"2022-blake3-aes-256-gcm", 32},
		{"2022-blake3-chacha20-poly1305", 32},
	}
	for _, tc := range cases {
		got := normalizeShadowsocksPassword(tc.method, "human-password")
		decoded, err := base64.StdEncoding.DecodeString(got)
		if err != nil {
			t.Fatalf("%s produced non-base64 key: %v", tc.method, err)
		}
		if len(decoded) != tc.want {
			t.Fatalf("%s decoded length=%d want=%d", tc.method, len(decoded), tc.want)
		}
		if got2 := normalizeShadowsocksPassword(tc.method, got); got2 != got {
			t.Fatalf("%s should keep already-valid psk", tc.method)
		}
	}
}

func TestNormalizeLegacyShadowsocksPasswordUnchanged(t *testing.T) {
	if got := normalizeShadowsocksPassword("aes-128-gcm", "short-pass"); got != "short-pass" {
		t.Fatalf("legacy shadowsocks password changed: %q", got)
	}
}
