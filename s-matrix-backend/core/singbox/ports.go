package singbox

import (
	"fmt"
	"math/rand"
	"net"
)

// Reserved system and service ports to avoid during node generation
var ReservedPorts = map[int]bool{
	22:    true, // SSH
	53:    true, // DNS
	80:    true, // HTTP / ACME HTTP-01
	443:   true, // HTTPS / Web
	19088: true, // Sing-Matrix Panel Web UI
	39999: true, // Sing-Matrix Accelerator Keep-Alive UDP
}

func IsPortAvailable(port int) bool {
	if port <= 0 || port > 65535 || ReservedPorts[port] {
		return false
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	udp, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	_ = udp.Close()
	return true
}

func PickAvailablePort(preferred int, used map[int]bool) int {
	// Trust the preferred port only if not reserved and not used
	if preferred > 0 && !ReservedPorts[preferred] && !used[preferred] && IsPortAvailable(preferred) {
		used[preferred] = true
		return preferred
	}
	// Random scan in range 10000-60000, skip occupied and reserved ports
	start := 10000 + rand.Intn(50001)
	for offset := 0; offset <= 50000; offset++ {
		port := 10000 + (start-10000+offset)%50001
		if used[port] || ReservedPorts[port] {
			continue
		}
		if IsPortAvailable(port) {
			used[port] = true
			return port
		}
	}
	return 0
}
