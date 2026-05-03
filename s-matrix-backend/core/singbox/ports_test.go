package singbox

import "testing"

func TestPickAvailablePortAvoids443(t *testing.T) {
	used := map[int]bool{}
	port := PickAvailablePort(443, used)
	if port == 443 || port == 0 {
		t.Fatalf("expected non-443 available port, got %d", port)
	}
	if !used[port] {
		t.Fatalf("picked port not marked used")
	}
}
