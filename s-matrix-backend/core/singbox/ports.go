package singbox

import (
	"fmt"
	"net"
)

func IsPortAvailable(port int) bool {
	if port <= 0 || port > 65535 {
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
	// Trust the preferred port — caller knows the existing ports from current config
	if preferred > 0 && preferred != 443 && !used[preferred] {
		used[preferred] = true
		return preferred
	}
	// Scan for a new available port
	for port := 41000; port <= 60999; port++ {
		if used[port] {
			continue
		}
		if IsPortAvailable(port) {
			used[port] = true
			return port
		}
	}
	return 0
}
