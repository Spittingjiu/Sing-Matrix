package ortt

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
		return fmt.Errorf("ortt local forwarder bind %s failed: %w", addr, err)
	}
	f.listener = l
	log.Printf("[ORTT] 本地连接池转发器已就绪: %s (SOCKS5 预热复用通道)", addr)

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

func (f *LocalForwarder) handleSocks5(clientConn net.Conn) {
	defer clientConn.Close()

	// 1. Negotiation (RFC 1928)
	buf := make([]byte, 258)
	if _, err := io.ReadFull(clientConn, buf[:2]); err != nil || buf[0] != 0x05 {
		return
	}
	numMethods := int(buf[1])
	if _, err := io.ReadFull(clientConn, buf[:numMethods]); err != nil {
		return
	}
	// Reply: NO_AUTH_REQUIRED (0x05, 0x00)
	if _, err := clientConn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. Request Details
	if _, err := io.ReadFull(clientConn, buf[:4]); err != nil || buf[1] != 0x01 { // 0x01 = CONNECT
		return
	}

	var targetHost string
	switch buf[3] { // ATYP
	case 0x01: // IPv4 (4 bytes)
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(clientConn, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	case 0x03: // Domain name
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(clientConn, lenBuf); err != nil {
			return
		}
		dLen := int(lenBuf[0])
		domainBuf := make([]byte, dLen)
		if _, err := io.ReadFull(clientConn, domainBuf); err != nil {
			return
		}
		targetHost = string(domainBuf)
	case 0x04: // IPv6 (16 bytes)
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(clientConn, ipBuf); err != nil {
			return
		}
		targetHost = net.IP(ipBuf).String()
	default:
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(clientConn, portBuf); err != nil {
		return
	}
	targetPort := int(portBuf[0])<<8 | int(portBuf[1])
	targetKey := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))

	// 3. Obtain Upstream Connection (Try warm pool, fallback to on-demand direct dial)
	upstreamConn, hit := f.pool.Get(targetKey)
	if !hit {
		dialer := &net.Dialer{Timeout: 3 * time.Second}
		var err error
		upstreamConn, err = dialer.Dial("tcp", targetKey)
		if err != nil {
			// Reply: Host unreachable (0x04)
			_, _ = clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}
	}
	defer upstreamConn.Close()

	// 4. Reply SUCCESS (0x05, 0x00, 0x00, 0x01, 127.0.0.1:0)
	if _, err := clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}

	// 5. Full duplex transparent pipe
	if hit {
		f.pipeWithTransparentReplay(clientConn, upstreamConn, targetKey)
	} else {
		f.pipeBidirectional(clientConn, upstreamConn)
	}
}

func (f *LocalForwarder) pipeWithTransparentReplay(client, upstream net.Conn, targetKey string) {
	firstChunk := make([]byte, 16384)
	n, err := client.Read(firstChunk)
	if err != nil {
		return
	}

	_ = upstream.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = upstream.Write(firstChunk[:n])
	_ = upstream.SetWriteDeadline(time.Time{})

	if err != nil {
		atomic.AddInt64(&GlobalStats.PoolReplays, 1)
		_ = upstream.Close()

		dialer := &net.Dialer{Timeout: 3 * time.Second}
		newUpstream, dialErr := dialer.Dial("tcp", targetKey)
		if dialErr != nil {
			return
		}
		defer newUpstream.Close()

		if _, rErr := newUpstream.Write(firstChunk[:n]); rErr != nil {
			return
		}
		f.pipeBidirectional(client, newUpstream)
		return
	}

	f.pipeBidirectional(client, upstream)
}

func (f *LocalForwarder) pipeBidirectional(c1, c2 net.Conn) {
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
