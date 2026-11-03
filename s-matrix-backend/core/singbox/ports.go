package singbox

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
)

var portMutex sync.Mutex

// Reserved system, sing-box default and ORTT engine ports to avoid collision
var ReservedPorts = map[int]bool{
	22:    true, // SSH
	53:    true, // DNS
	80:    true, // HTTP / ACME HTTP-01
	443:   true, // HTTPS / Web
	1053:  true, // Local DNS Inbound
	10808: true, // SOCKS5 Inbound
	10809: true, // HTTP Inbound
	10810: true, // TProxy Inbound
	19088: true, // Sing-Matrix Panel Web UI
	39998: true, // ORTT Engine SOCKS5 Local Forwarder (127.0.0.1)
	39999: true, // ORTT Engine Control UDP
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
	portMutex.Lock()
	defer portMutex.Unlock()

	// Trust preferred port only if strictly valid and unreserved
	if preferred > 0 && !ReservedPorts[preferred] && !used[preferred] && IsPortAvailable(preferred) {
		used[preferred] = true
		return preferred
	}

	// Random scan in unprivileged range 10000-60000
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
