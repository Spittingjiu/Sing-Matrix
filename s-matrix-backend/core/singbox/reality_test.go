package singbox

import "testing"

// A REALITY link without pbk is unusable on every client, so the public half
// must always be recoverable from the server private key.
func TestDeriveRealityPublicKey(t *testing.T) {
	cases := []struct{ priv, want string }{
		{"-HXDjuNqJMdBwg7uCKfPq04pGiQo7vwIkbQFDsjqTl4", "TufWk5R-0gQe5xoEiWFwMN_RZS7Yn99gbm1ob3nMxGo"},
	}
	for _, c := range cases {
		if got := DeriveRealityPublicKey(c.priv); got != c.want {
			t.Fatalf("DeriveRealityPublicKey(%q) = %q, want %q", c.priv, got, c.want)
		}
	}
	if got := DeriveRealityPublicKey("not-a-key"); got != "" {
		t.Fatalf("bad key should derive empty, got %q", got)
	}
	if got := DeriveRealityPublicKey(""); got != "" {
		t.Fatalf("empty key should derive empty, got %q", got)
	}
}

func TestGenerateRealityKeypairRoundTrip(t *testing.T) {
	priv, pub, err := GenerateRealityKeypair()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if priv == "" || pub == "" {
		t.Fatalf("empty keypair: %q %q", priv, pub)
	}
	if got := DeriveRealityPublicKey(priv); got != pub {
		t.Fatalf("derived %q != generated %q", got, pub)
	}
	if len(pub) != 43 {
		t.Fatalf("public key length %d, want 43 (raw base64url of 32 bytes)", len(pub))
	}
}
