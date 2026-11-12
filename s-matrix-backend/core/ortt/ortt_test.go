package ortt

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

var testSecret = []byte("super-secret-test-key-32-bytes!!")

func TestAuthentication_Valid(t *testing.T) {
	cache := NewReplayCache()
	pkt := BuildPacket(testSecret, CmdWakeup, 1)
	cmd, _, keyID, err := VerifyPacket(testSecret, pkt, cache)
	if err != nil {
		t.Fatalf("expected valid packet, got error: %v", err)
	}
	if cmd != CmdWakeup {
		t.Fatalf("expected CmdWakeup, got: %d", cmd)
	}
	if keyID != 1 {
		t.Fatalf("expected KeyID 1, got: %d", keyID)
	}
}

func TestAuthentication_ReplayDetected(t *testing.T) {
	cache := NewReplayCache()
	pkt := BuildPacket(testSecret, CmdWakeup, 1)

	// First transmission should succeed
	cmd, _, _, err := VerifyPacket(testSecret, pkt, cache)
	if err != nil || cmd != CmdWakeup {
		t.Fatalf("first verification failed: %v", err)
	}

	// Immediate replay of identical packet within 30s window must be rejected
	_, _, _, err = VerifyPacket(testSecret, pkt, cache)
	if err != ErrReplayDetected {
		t.Fatalf("expected ErrReplayDetected on replayed packet, got: %v", err)
	}
}

func TestAuthentication_Tampered(t *testing.T) {
	cache := NewReplayCache()
	pkt := BuildPacket(testSecret, CmdWakeup, 1)
	pkt[5] = CmdSleep // Tamper with command byte without updating HMAC

	_, _, _, err := VerifyPacket(testSecret, pkt, cache)
	if err != ErrHMACMismatch {
		t.Fatalf("expected ErrHMACMismatch, got: %v", err)
	}
}

func TestAuthentication_BadHMACDoesNotConsumeNonce(t *testing.T) {
	cache := NewReplayCache()
	pkt := BuildPacket(testSecret, CmdWakeup, 1)
	tampered := make([]byte, len(pkt))
	copy(tampered, pkt)
	tampered[5] = CmdSleep // Same nonce, bad HMAC (not re-signed)

	// Tampered packet must fail authentication ...
	if _, _, _, err := VerifyPacket(testSecret, tampered, cache); err != ErrHMACMismatch {
		t.Fatalf("expected ErrHMACMismatch, got: %v", err)
	}
	// ... but must NOT have consumed the nonce: the original packet with
	// the same nonce still verifies.
	if _, _, _, err := VerifyPacket(testSecret, pkt, cache); err != nil {
		t.Fatalf("expected original packet to verify after bad-HMAC attempt, got: %v", err)
	}
	// And a replay of the now-verified packet is still rejected.
	if _, _, _, err := VerifyPacket(testSecret, pkt, cache); err != ErrReplayDetected {
		t.Fatalf("expected ErrReplayDetected on replay, got: %v", err)
	}
}

func TestAuthentication_ClockSkewExpired(t *testing.T) {
	cache := NewReplayCache()
	pkt := make([]byte, PacketWireLength)
	copy(pkt[:4], Magic)
	pkt[4] = ProtocolVersion
	pkt[5] = CmdWakeup

	// Fake expired timestamp 2 minutes in the past
	oldTs := uint32(time.Now().Add(-2 * time.Minute).Unix())
	binary.BigEndian.PutUint32(pkt[6:10], oldTs)
	binary.BigEndian.PutUint32(pkt[10:14], 12345) // Nonce
	binary.BigEndian.PutUint16(pkt[14:16], 1)     // KeyID

	sig := ComputeHMAC(testSecret, pkt[:16])
	copy(pkt[16:32], sig)

	_, _, _, err := VerifyPacket(testSecret, pkt, cache)
	if err != ErrClockSkew {
		t.Fatalf("expected ErrClockSkew, got: %v", err)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10.0, 3.0) // 3 rps per IP
	ip := "1.2.3.4"

	// First bursts should be allowed
	for i := 0; i < 5; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("expected allow for token %d", i)
		}
	}

	// Exceeded burst limit should be throttled
	if rl.Allow(ip) {
		t.Fatalf("expected throttle after burst limit exceeded")
	}
}

func TestRateLimiter_HonorsPerIpAndGlobal(t *testing.T) {
	rl := NewRateLimiterWithBurst(8.0, 2.0, 3.0)
	for i := 0; i < 3; i++ {
		if !rl.Allow("10.0.0.1") {
			t.Fatalf("expected IP1 burst %d allowed", i)
		}
	}
	if rl.Allow("10.0.0.1") {
		t.Fatalf("expected IP1 throttled after burst=3")
	}
	// Different IP still has its own per-IP bucket within global limit
	for i := 0; i < 3; i++ {
		if !rl.Allow("10.0.0.2") {
			t.Fatalf("expected IP2 burst %d allowed", i)
		}
	}
	// Global bucket started at 8, 6 consumed -> 2 remaining for IP3
	for i := 0; i < 2; i++ {
		if !rl.Allow("10.0.0.3") {
			t.Fatalf("expected IP3 %d allowed within global limit", i)
		}
	}
	if rl.Allow("10.0.0.3") {
		t.Fatalf("expected IP3 throttled after global limit reached")
	}
}

func TestConcurrentSessionAccess_DataRaceFree(t *testing.T) {
	sess := &ClientSession{RemoteAddr: "192.168.1.100:12345"}
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				sess.TouchActive(time.Now())
				sess.TouchWakeup(time.Now())
			}
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = sess.GetLastActive()
			}
		}()
	}

	wg.Wait()
}

func TestFirstPacketBuffering_ReplayOnDeadSocket(t *testing.T) {
	// Create mock live destination echo server
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo failed: %v", err)
	}
	defer echoLn.Close()

	var echoReceived []byte
	var echoMu sync.Mutex
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 1024)
				n, _ := conn.Read(buf)
				if n > 0 {
					echoMu.Lock()
					echoReceived = append(echoReceived, buf[:n]...)
					echoMu.Unlock()
					_, _ = conn.Write([]byte("ECHO_ACK"))
				}
			}(c)
		}
	}()

	dnsMgr := NewDNSManager(1 * time.Minute)
	defer dnsMgr.Close()
	pool := NewUpstreamSocketPool(2, 5, dnsMgr)
	defer pool.Close()

	// Put a DEAD/CLOSED socket into the pool
	deadClient, deadServer := net.Pipe()
	_ = deadServer.Close() // Remote closed (dead socket)

	targetKey := echoLn.Addr().String()
	pool.mu.Lock()
	pool.pools[targetKey] = append(pool.pools[targetKey], &warmSocket{
		conn:      deadClient,
		createdAt: time.Now(),
	})
	pool.currentTotal++
	pool.mu.Unlock()

	// Start local forwarder
	fwd := NewLocalForwarder(0, pool)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fwd listen failed: %v", err)
	}
	fwd.listener = ln
	fwd.wg.Add(1)
	go fwd.acceptLoop()
	defer fwd.Close()

	// Connect via SOCKS5
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial forwarder failed: %v", err)
	}
	defer client.Close()

	// SOCKS5 Handshake
	_, _ = client.Write([]byte{0x05, 0x01, 0x00})
	authResp := make([]byte, 2)
	_, _ = io.ReadFull(client, authResp)

	// SOCKS5 CONNECT to echo server
	host, portStr, _ := net.SplitHostPort(echoLn.Addr().String())
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	var portNum uint16
	for _, ch := range portStr {
		portNum = portNum*10 + uint16(ch-'0')
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, portNum)
	req = append(req, portBytes...)
	_, _ = client.Write(req)

	connResp := make([]byte, 10)
	_, _ = io.ReadFull(client, connResp)
	if connResp[1] != 0x00 {
		t.Fatalf("expected SOCKS5 success, got: %x", connResp[1])
	}

	// Send ClientHello first packet
	testPayload := []byte("TLS_CLIENT_HELLO_SIMULATION_PAYLOAD")
	_, err = client.Write(testPayload)
	if err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	// Wait for echo response: verifies transparent fallback & buffer replay succeeded!
	ackBuf := make([]byte, 8)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := client.Read(ackBuf)
	if err != nil || string(ackBuf[:n]) != "ECHO_ACK" {
		t.Fatalf("expected ECHO_ACK after transparent replay, got: %s (err: %v)", string(ackBuf[:n]), err)
	}

	echoMu.Lock()
	defer echoMu.Unlock()
	if string(echoReceived) != string(testPayload) {
		t.Fatalf("payload mismatch in replay buffer, expected %s, got %s", testPayload, echoReceived)
	}
}
