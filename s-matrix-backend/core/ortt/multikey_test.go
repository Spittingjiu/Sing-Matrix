package ortt

import (
	"crypto/hmac"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

type memDatagram struct {
	data []byte
	from net.Addr
}

type memPacketConn struct {
	localAddr    net.Addr
	inbox        chan memDatagram
	peer         *memPacketConn
	mu           sync.Mutex
	closed       bool
	closedCh     chan struct{}
	readDeadline time.Time
}

func newMemPacketConnPair(serverAddr, clientAddr net.Addr) (*memPacketConn, *memPacketConn) {
	srv := &memPacketConn{
		localAddr: serverAddr,
		inbox:     make(chan memDatagram, 64),
		closedCh:  make(chan struct{}),
	}
	cli := &memPacketConn{
		localAddr: clientAddr,
		inbox:     make(chan memDatagram, 64),
		closedCh:  make(chan struct{}),
	}
	srv.peer = cli
	cli.peer = srv
	return srv, cli
}

func (m *memPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	m.mu.Lock()
	dl := m.readDeadline
	m.mu.Unlock()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			return 0, nil, os.ErrDeadlineExceeded
		}
		timer = time.NewTimer(d)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-m.closedCh:
		return 0, nil, net.ErrClosed
	case pkt := <-m.inbox:
		n := copy(p, pkt.data)
		return n, pkt.from, nil
	case <-timeoutCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (m *memPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	m.mu.Lock()
	closed := m.closed
	peer := m.peer
	m.mu.Unlock()
	if closed || peer == nil {
		return 0, net.ErrClosed
	}
	cp := append([]byte(nil), p...)
	select {
	case peer.inbox <- memDatagram{data: cp, from: m.localAddr}:
		return len(p), nil
	case <-peer.closedCh:
		return 0, io.ErrClosedPipe
	}
}

func (m *memPacketConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.closedCh)
	}
	return nil
}

func (m *memPacketConn) LocalAddr() net.Addr { return m.localAddr }

func (m *memPacketConn) SetDeadline(t time.Time) error {
	return m.SetReadDeadline(t)
}

func (m *memPacketConn) SetReadDeadline(t time.Time) error {
	m.mu.Lock()
	m.readDeadline = t
	m.mu.Unlock()
	return nil
}

func (m *memPacketConn) SetWriteDeadline(_ time.Time) error { return nil }

func buildV2Packet(t *testing.T, cmd byte, keyID uint16, nonce []byte, secret []byte) []byte {
	t.Helper()
	pkt := make([]byte, PacketWireLength)
	copy(pkt[0:4], Magic)
	pkt[4] = ProtocolVersion
	pkt[5] = cmd
	binary.BigEndian.PutUint32(pkt[6:10], uint32(time.Now().Unix()))
	copy(pkt[10:14], nonce)
	binary.BigEndian.PutUint16(pkt[14:16], keyID)
	copy(pkt[16:32], ComputeHMAC(secret, pkt[0:16]))
	return pkt
}

func freeUDPAndTCPPort(t *testing.T) int {
	t.Helper()
	for i := 0; i < 16; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = pc.Close()
			return port
		}
	}
	t.Fatal("failed to allocate free TCP+UDP port")
	return 0
}

func TestVerifyPacketWithKeys_StrictPerNodeIsolation(t *testing.T) {
	globalSecret := []byte("global-secret-key-32-bytes-0001!")
	node2Secret := []byte("node-2-secret-key-32-bytes-0002!")
	node3Secret := []byte("node-3-secret-key-32-bytes-0003!")

	keyRing := map[uint16][]byte{
		1: globalSecret,
		2: node2Secret,
		3: node3Secret,
	}
	cache := NewReplayCache()

	// 1. KeyID >= 2 succeeds only with its exact KeyID secret and returns that secret.
	p2 := buildV2Packet(t, CmdPing, 2, []byte{1, 2, 3, 4}, node2Secret)
	cmd, _, kid, matched, err := VerifyPacketWithKeys(keyRing, globalSecret, p2, cache)
	if err != nil || cmd != CmdPing || kid != 2 || !hmac.Equal(matched, node2Secret) {
		t.Fatalf("expected KeyID=2 match with node2Secret, got cmd=%d kid=%d err=%v", cmd, kid, err)
	}

	// 2. Packet signed with globalSecret but claiming KeyID=2 MUST FAIL (no fallback).
	pFake2 := buildV2Packet(t, CmdPing, 2, []byte{2, 2, 3, 4}, globalSecret)
	if _, _, _, _, err := VerifyPacketWithKeys(keyRing, globalSecret, pFake2, cache); err == nil {
		t.Fatal("expected KeyID=2 signed by globalSecret to be rejected")
	}

	// 3. Unknown KeyID >= 2 (e.g. deleted/rotated KeyID=99) signed with globalSecret or node2Secret MUST FAIL.
	for idx, sec := range [][]byte{globalSecret, node2Secret, node3Secret} {
		pUnknown := buildV2Packet(t, CmdPing, 99, []byte{byte(10 + idx), 2, 3, 4}, sec)
		if _, _, _, _, err := VerifyPacketWithKeys(keyRing, globalSecret, pUnknown, cache); err == nil {
			t.Fatalf("expected unknown KeyID=99 (attempt %d) to be rejected without fallback", idx)
		}
	}

	// 4. KeyID <= 1 still resolves against defaultSecret / keyRing[1].
	pGlobal := buildV2Packet(t, CmdPing, 1, []byte{3, 2, 3, 4}, globalSecret)
	_, _, kid1, matched1, err := VerifyPacketWithKeys(keyRing, globalSecret, pGlobal, cache)
	if err != nil || kid1 != 1 || !hmac.Equal(matched1, globalSecret) {
		t.Fatalf("expected KeyID=1 match, got kid=%d err=%v", kid1, err)
	}
	pZero := buildV2Packet(t, CmdPing, 0, []byte{4, 2, 3, 4}, globalSecret)
	_, _, _, matched0, err := VerifyPacketWithKeys(keyRing, globalSecret, pZero, cache)
	if err != nil || !hmac.Equal(matched0, globalSecret) {
		t.Fatalf("expected KeyID=0 fallback to defaultSecret, got err=%v", err)
	}
}

func TestServerRunRealListenersAndStatsEndpoint(t *testing.T) {
	ctlPort := freeUDPAndTCPPort(t)
	fwdPort := freePort(t)
	cfg := DefaultConfig()
	cfg.ControlPort = ctlPort
	cfg.ForwardPort = fwdPort
	cfg.SecretKey = []byte("real-listener-secret-32-chars-!!")
	cfg.TargetDomains = nil

	srv := NewServer(cfg)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run real listeners: %v", err)
	}
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ortt/stats", ctlPort))
	if err != nil {
		t.Fatalf("GET /ortt/stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestServerMultiKeyPongSigningStatsAndLiveReload(t *testing.T) {
	ctlPort := freeUDPAndTCPPort(t)
	fwdPort := freePort(t)
	globalSecret := []byte("global-secret-32-chars-minimum!!")
	node2Secret := []byte("node2-secret-32-chars-minimum!!!")
	node3Secret := []byte("node3-secret-32-chars-minimum!!!")
	node4Secret := []byte("node4-secret-32-chars-minimum!!!")

	cfg := DefaultConfig()
	cfg.ControlPort = ctlPort
	cfg.ForwardPort = fwdPort
	cfg.SecretKey = globalSecret
	cfg.KeyRing[1] = globalSecret
	cfg.TargetDomains = nil

	srvConn, cliConn := newMemPacketConnPair(
		&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: ctlPort},
		&net.UDPAddr{IP: net.ParseIP("198.51.100.25"), Port: 54321},
	)
	defer cliConn.Close()

	srv := NewServer(cfg)
	srv.listenPacket = func(_, _ string) (net.PacketConn, error) {
		return srvConn, nil
	}
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer srv.Close()

	srv.ReplaceNodeKeys([]NodeKeyEntry{
		{OrttID: "ortt_aa11bb22cc33", KeyID: 2, InboundTag: "node-2", Secret: node2Secret},
		{OrttID: "ortt_dd44ee55ff66", KeyID: 3, InboundTag: "node-3", Secret: node3Secret},
	})

	sendPingAndVerifyPong := func(keyID uint16, secret []byte, nonce []byte) error {
		pkt := buildV2Packet(t, CmdPing, keyID, nonce, secret)
		if _, err := cliConn.WriteTo(pkt, srvConn.LocalAddr()); err != nil {
			return err
		}
		_ = cliConn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		buf := make([]byte, 128)
		n, _, err := cliConn.ReadFrom(buf)
		if err != nil {
			return err
		}
		if n != PongPacketWireLength {
			return fmt.Errorf("pong length %d != %d", n, PongPacketWireLength)
		}
		pong := buf[:PongPacketWireLength]
		if string(pong[0:4]) != "ORTT" || pong[4] != ProtocolVersion || pong[5] != CmdPong {
			return fmt.Errorf("unexpected pong header: %x", pong[:6])
		}
		if string(pong[6:10]) != string(nonce) {
			return fmt.Errorf("nonce mismatch")
		}
		expectedHMAC := ComputeHMAC(secret, pong[0:32])
		if !hmac.Equal(pong[32:48], expectedHMAC) {
			return fmt.Errorf("pong HMAC verification failed for keyID=%d", keyID)
		}
		// Must NOT verify under a different key's secret.
		for _, other := range [][]byte{globalSecret, node2Secret, node3Secret, node4Secret} {
			if hmac.Equal(other, secret) {
				continue
			}
			if hmac.Equal(pong[32:48], ComputeHMAC(other, pong[0:32])) {
				return fmt.Errorf("pong HMAC accidentally matched wrong secret for keyID=%d", keyID)
			}
		}
		return nil
	}

	if err := sendPingAndVerifyPong(1, globalSecret, []byte{1, 1, 1, 1}); err != nil {
		t.Logf("stats: %+v", srv.GetStats())
		t.Fatalf("KeyID=1 ping/pong failed: %v", err)
	}
	if err := sendPingAndVerifyPong(2, node2Secret, []byte{2, 2, 2, 2}); err != nil {
		t.Fatalf("KeyID=2 ping/pong failed: %v", err)
	}
	if err := sendPingAndVerifyPong(3, node3Secret, []byte{3, 3, 3, 3}); err != nil {
		t.Fatalf("KeyID=3 ping/pong failed: %v", err)
	}

	// Send Wakeup + Heartbeat for KeyID=2 and check per-node counters.
	if _, err := cliConn.WriteTo(buildV2Packet(t, CmdWakeup, 2, []byte{9, 9, 9, 1}, node2Secret), srvConn.LocalAddr()); err != nil {
		t.Fatalf("udp write wakeup: %v", err)
	}
	_ = cliConn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	pongBuf := make([]byte, 128)
	n, _, err := cliConn.ReadFrom(pongBuf)
	if err != nil || n != PongPacketWireLength {
		t.Fatalf("expected Wakeup Pong, got n=%d err=%v", n, err)
	}
	if !hmac.Equal(pongBuf[32:48], ComputeHMAC(node2Secret, pongBuf[:32])) {
		t.Fatal("Wakeup Pong was not signed with KeyID=2 secret")
	}

	if _, err := cliConn.WriteTo(buildV2Packet(t, CmdHeartbeat, 2, []byte{9, 9, 9, 2}, node2Secret), srvConn.LocalAddr()); err != nil {
		t.Fatalf("udp write heartbeat: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := srv.GetNodeStats("ortt_aa11bb22cc33"); ok && st.WakeCount >= 1 && st.PingCount >= 1 && st.HeartbeatCount >= 1 && st.LastWakeUnix > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	st2, ok := srv.GetNodeStats("ortt_aa11bb22cc33")
	if !ok || st2.WakeCount < 1 || st2.PingCount < 1 || st2.HeartbeatCount < 1 || st2.LastWakeUnix == 0 {
		t.Fatalf("expected node-2 wake/ping/heartbeat stats recorded, got ok=%v stats=%+v", ok, st2)
	}

	// Verify :controlPort/ortt/stats exposes per-node counters.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ortt/stats", ctlPort))
	if err != nil {
		t.Fatalf("GET /ortt/stats: %v", err)
	}
	defer resp.Body.Close()
	var statsBody Stats
	if err := json.NewDecoder(resp.Body).Decode(&statsBody); err != nil {
		t.Fatalf("decode /ortt/stats: %v", err)
	}
	if ns, exists := statsBody.Nodes["ortt_aa11bb22cc33"]; !exists || ns.WakeCount < 1 || ns.PingCount < 1 {
		t.Fatalf("expected ortt_aa11bb22cc33 in /ortt/stats nodes, got: %+v", statsBody.Nodes)
	}

	// Dynamically rotate KeyID=2 -> KeyID=4 without restarting server.
	srv.ReplaceNodeKeys([]NodeKeyEntry{
		{OrttID: "ortt_aa11bb22cc33", KeyID: 4, InboundTag: "node-2", Secret: node4Secret},
		{OrttID: "ortt_dd44ee55ff66", KeyID: 3, InboundTag: "node-3", Secret: node3Secret},
	})

	// Old KeyID=2 MUST fail immediately (no Pong returned).
	if err := sendPingAndVerifyPong(2, node2Secret, []byte{2, 2, 2, 9}); err == nil {
		t.Fatal("expected rotated/removed KeyID=2 to stop responding immediately")
	}
	// New KeyID=4 and existing KeyID=3 MUST both succeed without restart.
	if err := sendPingAndVerifyPong(4, node4Secret, []byte{4, 4, 4, 4}); err != nil {
		t.Fatalf("rotated KeyID=4 ping/pong failed: %v", err)
	}
	if err := sendPingAndVerifyPong(3, node3Secret, []byte{3, 3, 3, 9}); err != nil {
		t.Fatalf("unaffected KeyID=3 ping/pong failed after rotation: %v", err)
	}
}
