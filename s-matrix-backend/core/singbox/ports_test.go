package singbox

import (
	"net"
	"sync"
	"testing"
)

func TestPickAvailablePort_Concurrency(t *testing.T) {
	used := make(map[int]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			port := PickAvailablePort(0, used)
			mu.Unlock()

			if port == 0 {
				t.Errorf("expected available port, got 0")
			}
			if ReservedPorts[port] {
				t.Errorf("allocated reserved port: %d", port)
			}
		}()
	}

	wg.Wait()
}

func TestPickAvailablePortOwnedKeepsHeldPort(t *testing.T) {
	// Simulate the running sing-box holding a port while a rebuild is
	// compiled: the plain picker must reject it, the owned picker must
	// keep it (otherwise every rebuild silently remaps existing nodes).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if port < 10000 || ReservedPorts[port] {
		t.Skip("held port outside test range")
	}
	if got := PickAvailablePort(port, map[int]bool{}); got == port {
		t.Fatalf("plain picker must reject a held port, got %d", got)
	}
	if got := PickAvailablePortOwned(port, map[int]bool{}, map[int]bool{port: true}); got != port {
		t.Fatalf("owned picker must keep the held port, got %d", got)
	}
}
