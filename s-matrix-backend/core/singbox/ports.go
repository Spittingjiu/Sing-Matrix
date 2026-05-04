package singbox

import (
	"fmt"
	"math/rand"
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
	// Random scan in range 10000-60000, skip occupied ports
	start := 10000 + rand.Intn(50001)
	for offset := 0; offset <= 50000; offset++ {
		port := 10000 + (start-10000+offset)%50001
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
