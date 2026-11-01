package singbox

import (
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
