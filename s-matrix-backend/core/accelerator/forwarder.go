package accelerator

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// LocalForwarder implements a loopback SOCKS5 server (127.0.0.1:39998)
// that consumes pre-connected sockets directly from the UpstreamSocketPool
type LocalForwarder struct {
	port     int
	pool     *UpstreamSocketPool
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewLocalForwarder(port int, pool *UpstreamSocketPool) *LocalForwarder {
	if port <= 0 {
		port = 39998
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &LocalForwarder{
		port:   port,
		pool:   pool,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (f *LocalForwarder) Start() error {
	addr := fmt.Sprintf("127.0.0.1:%d", f.port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("local forwarder bind %s failed: %w", addr, err)
	}
	f.listener = l
	log.Printf("[accelerator] 本地连接池转发器已就绪: %s (SOCKS5 预热复用通道)", addr)

	f.wg.Add(1)
	go f.acceptLoop()
	return nil
}

func (f *LocalForwarder) acceptLoop() {
	defer f.wg.Done()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			select {
			case <-f.ctx.Done():
				return
			default:
				continue
			}
		}

		go f.handleSocks5(conn)
	}
}

// handleSocks5 implements RFC 1928 SOCKS5 CONNECT with First-Packet Buffering & Transparent Replay
func (f *LocalForwarder) handleSocks5(client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))

	// 1. Version identifier / method selection
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil || header[0] != 0x05 {
		return
	}

	numMethods := int(header[1])
	methods := make([]byte, numMethods)
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}

	// Reply: No authentication required (0x05, 0x00)
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. SOCKS5 Request
	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(client, reqHeader); err != nil || reqHeader[0] != 0x05 || reqHeader[1] != 0x01 {
		// Only support CONNECT (0x01)
		return
	}

	var targetHost string
	switch reqHeader[3] {
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(client, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	case 0x03: // Domain name
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(client, lenBuf); err != nil {
			return
		}
		domainBuf := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(client, domainBuf); err != nil {
			return
		}
		targetHost = string(domainBuf)
	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(client, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	default:
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(client, portBuf); err != nil {
		return
	}
	targetPort := int(portBuf[0])<<8 | int(portBuf[1])
	targetKey := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))

	_ = client.SetDeadline(time.Time{}) // clear handshake deadline

	// 3. Acquire connection: check Upstream Pre-Connection Pool first
	var upstream net.Conn
	var isPoolHit bool

	if targetPort == 443 && f.pool != nil {
		upstream, isPoolHit = f.pool.Get(targetKey)
	}

	// Case A: Pool Hit with First-Packet Buffering & Transparent Replay
	if isPoolHit && upstream != nil {
		// Reply SOCKS5 Success
		if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			upstream.Close()
			return
		}

		// Buffer client's first application packet (typically TLS ClientHello, 200~600 bytes)
		initialBuf := make([]byte, 4096)
		_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := client.Read(initialBuf)
		_ = client.SetReadDeadline(time.Time{})
		if err != nil || n == 0 {
			upstream.Close()
			return
		}

		// Attempt writing buffered payload to the warm socket
		_ = upstream.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, writeErr := upstream.Write(initialBuf[:n])
		_ = upstream.SetWriteDeadline(time.Time{})

		if writeErr != nil {
			// Remote server silently closed / sent RST during idle (Half-Open TCP)
			upstream.Close()
			atomic.AddInt64(&GlobalStats.PoolReplays, 1)

			// Transparent Replay: Immediately dial fresh socket and replay buffered payload
			d := net.Dialer{Timeout: 5 * time.Second}
			freshUpstream, dialErr := d.Dial("tcp", targetKey)
			if dialErr != nil {
				return
			}
			defer freshUpstream.Close()

			if _, err := freshUpstream.Write(initialBuf[:n]); err != nil {
				return
			}
			pipeConns(client, freshUpstream)
			return
		}

		// Warm socket is live and write succeeded!
		defer upstream.Close()
		pipeConns(client, upstream)
		return
	}

	// Case B: Pool Miss: Synchronous dial fallback
	d := net.Dialer{Timeout: 5 * time.Second}
	var err error
	upstream, err = d.Dial("tcp", targetKey)
	if err != nil {
		_, _ = client.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	pipeConns(client, upstream)
}

// pipeConns relays bidirectional TCP streams using 32KB user-space buffers (io.Copy)
func pipeConns(c1, c2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(c1, c2)
		_ = c1.SetDeadline(time.Now())
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(c2, c1)
		_ = c2.SetDeadline(time.Now())
	}()

	wg.Wait()
}

func (f *LocalForwarder) Close() {
	f.cancel()
	if f.listener != nil {
		_ = f.listener.Close()
	}
	f.wg.Wait()
}
