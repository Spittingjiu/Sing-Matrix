package singbox

import (
	"net"
	"strings"
	"sync"
)

var (
	listenOnce sync.Once
	listenAddr string
)

// DefaultListen returns the wildcard address the core should bind.
//
// "::" is dual-stack on Linux, but on hosts with IPv6 disabled the bind fails
// and sing-box refuses to start - which would take every node offline. Probing
// once and falling back to 0.0.0.0 keeps such hosts working.
func DefaultListen() string {
	listenOnce.Do(func() {
		ln, err := net.Listen("tcp", "[::]:0")
		if err != nil {
			listenAddr = "0.0.0.0"
			return
		}
		_ = ln.Close()
		listenAddr = "::"
	})
	return listenAddr
}

// normalizeListen maps an explicit listen value onto something this host can
// actually bind: empty and IPv6 wildcards become the probed default.
func normalizeListen(v string) string {
	v = strings.TrimSpace(v)
	switch v {
	case "", "::", "[::]", "0.0.0.0":
		return DefaultListen()
	}
	return v
}
