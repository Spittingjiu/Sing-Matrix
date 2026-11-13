package ortt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type keyRingSnapshot struct {
	rawKeys map[uint16][]byte
	entries map[uint16]NodeKeyEntry
}

type nodeStatsCounter struct {
	orttID         string
	keyID          atomic.Uint32
	inboundTag     atomic.Value // string
	lastWakeUnix   atomic.Int64
	wakeCount      atomic.Int64
	heartbeatCount atomic.Int64
	pingCount      atomic.Int64
	lastSeenUnix   atomic.Int64
}

type Server struct {
	cfg         *Config
	dnsMgr      *DNSManager
	pool        *UpstreamSocketPool
	forwarder   *LocalForwarder
	limiter     *RateLimiter
	replayCache *ReplayCache
	sessions    sync.Map // string -> *ClientSession
	keyState    atomic.Pointer[keyRingSnapshot]
	nodeStatsMu sync.RWMutex
	nodeStats   map[string]*nodeStatsCounter
	udpConn      net.PacketConn
	listenPacket func(network, address string) (net.PacketConn, error)
	httpServer   *http.Server
	prewarming  atomic.Bool
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

var (
	globalServerMu sync.RWMutex
	globalServer   *Server
)

func GlobalServer() *Server {
	globalServerMu.RLock()
	defer globalServerMu.RUnlock()
	return globalServer
}

func SetGlobalServer(s *Server) {
	globalServerMu.Lock()
	defer globalServerMu.Unlock()
	globalServer = s
}

func NewServer(cfg *Config) *Server {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())
	dnsMgr := NewDNSManager(5 * time.Minute)
	pool := NewUpstreamSocketPool(cfg.MaxWarmPerDomain, cfg.MaxTotalSockets, dnsMgr)
	forwarder := NewLocalForwarder(cfg.ForwardPort, pool)
	limiter := NewRateLimiterWithBurst(50.0, 5.0, 30.0) // 50 global rps, 5 rps per IP, 30 burst
	replayCache := NewReplayCache()

	srv := &Server{
		cfg:         cfg,
		dnsMgr:      dnsMgr,
		pool:        pool,
		forwarder:   forwarder,
		limiter:     limiter,
		replayCache:  replayCache,
		nodeStats:    make(map[string]*nodeStatsCounter),
		listenPacket: net.ListenPacket,
		ctx:          ctx,
		cancel:       cancel,
	}
	srv.initKeyRingFromConfig()
	return srv
}

func (s *Server) initKeyRingFromConfig() {
	snap := &keyRingSnapshot{
		rawKeys: make(map[uint16][]byte),
		entries: make(map[uint16]NodeKeyEntry),
	}
	if len(s.cfg.SecretKey) > 0 {
		sec := append([]byte(nil), s.cfg.SecretKey...)
		snap.rawKeys[DefaultKeyID] = sec
		snap.entries[DefaultKeyID] = NodeKeyEntry{
			OrttID: "ortt_global",
			KeyID:  DefaultKeyID,
			Secret: sec,
		}
	}
	for k, v := range s.cfg.KeyRing {
		if len(v) == 0 {
			continue
		}
		sec := append([]byte(nil), v...)
		snap.rawKeys[k] = sec
		if k <= 1 {
			snap.entries[k] = NodeKeyEntry{OrttID: "ortt_global", KeyID: k, Secret: sec}
		} else {
			snap.entries[k] = NodeKeyEntry{OrttID: fmt.Sprintf("ortt_key_%d", k), KeyID: k, Secret: sec}
		}
	}
	s.keyState.Store(snap)
}

// ReplaceNodeKeys atomically swaps per-node KeyID (>= 2) bindings while preserving KeyID 1 global ORTT_SECRET.
func (s *Server) ReplaceNodeKeys(entries []NodeKeyEntry) {
	if s == nil {
		return
	}
	snap := &keyRingSnapshot{
		rawKeys: make(map[uint16][]byte),
		entries: make(map[uint16]NodeKeyEntry),
	}
	if len(s.cfg.SecretKey) > 0 {
		sec := append([]byte(nil), s.cfg.SecretKey...)
		snap.rawKeys[DefaultKeyID] = sec
		snap.entries[DefaultKeyID] = NodeKeyEntry{
			OrttID: "ortt_global",
			KeyID:  DefaultKeyID,
			Secret: sec,
		}
	} else if s.cfg.KeyRing != nil && len(s.cfg.KeyRing[DefaultKeyID]) > 0 {
		sec := append([]byte(nil), s.cfg.KeyRing[DefaultKeyID]...)
		snap.rawKeys[DefaultKeyID] = sec
		snap.entries[DefaultKeyID] = NodeKeyEntry{
			OrttID: "ortt_global",
			KeyID:  DefaultKeyID,
			Secret: sec,
		}
	}

	activeOrttIDs := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.KeyID < 2 || len(entry.Secret) == 0 {
			continue
		}
		sec := append([]byte(nil), entry.Secret...)
		cp := NodeKeyEntry{
			OrttID:     entry.OrttID,
			KeyID:      entry.KeyID,
			InboundTag: entry.InboundTag,
			Secret:     sec,
		}
		snap.rawKeys[entry.KeyID] = sec
		snap.entries[entry.KeyID] = cp
		if cp.OrttID != "" {
			activeOrttIDs[cp.OrttID] = true
			s.ensureNodeStatsCounter(cp.OrttID, cp.KeyID, cp.InboundTag)
		}
	}
	s.keyState.Store(snap)

	// Prune stats entries that have zero recorded activity and are no longer active.
	s.nodeStatsMu.Lock()
	for id, c := range s.nodeStats {
		if id == "ortt_global" || activeOrttIDs[id] {
			continue
		}
		if c.wakeCount.Load() == 0 && c.heartbeatCount.Load() == 0 && c.pingCount.Load() == 0 {
			delete(s.nodeStats, id)
		}
	}
	s.nodeStatsMu.Unlock()
}

// SetNodeKey adds or updates a single node KeyID (>= 2) without interrupting other keys.
func (s *Server) SetNodeKey(entry NodeKeyEntry) {
	if s == nil || entry.KeyID < 2 || len(entry.Secret) == 0 {
		return
	}
	cur := s.keyState.Load()
	next := &keyRingSnapshot{
		rawKeys: make(map[uint16][]byte),
		entries: make(map[uint16]NodeKeyEntry),
	}
	if cur != nil {
		for k, v := range cur.rawKeys {
			next.rawKeys[k] = v
		}
		for k, v := range cur.entries {
			next.entries[k] = v
		}
	}
	sec := append([]byte(nil), entry.Secret...)
	cp := NodeKeyEntry{
		OrttID:     entry.OrttID,
		KeyID:      entry.KeyID,
		InboundTag: entry.InboundTag,
		Secret:     sec,
	}
	next.rawKeys[entry.KeyID] = sec
	next.entries[entry.KeyID] = cp
	if cp.OrttID != "" {
		s.ensureNodeStatsCounter(cp.OrttID, cp.KeyID, cp.InboundTag)
	}
	s.keyState.Store(next)
}

// RemoveNodeKey removes a single KeyID (>= 2) immediately from the live KeyRing.
func (s *Server) RemoveNodeKey(keyID uint16) {
	if s == nil || keyID < 2 {
		return
	}
	cur := s.keyState.Load()
	if cur == nil {
		return
	}
	next := &keyRingSnapshot{
		rawKeys: make(map[uint16][]byte),
		entries: make(map[uint16]NodeKeyEntry),
	}
	for k, v := range cur.rawKeys {
		if k != keyID {
			next.rawKeys[k] = v
		}
	}
	for k, v := range cur.entries {
		if k != keyID {
			next.entries[k] = v
		}
	}
	s.keyState.Store(next)
}

func ReplaceNodeKeys(entries []NodeKeyEntry) {
	if gs := GlobalServer(); gs != nil {
		gs.ReplaceNodeKeys(entries)
	}
}

// KeyRingCopy returns a shallow clone of the active KeyID -> secret map.
func (s *Server) KeyRingCopy() map[uint16][]byte {
	if s == nil {
		return nil
	}
	snap := s.keyState.Load()
	if snap == nil {
		return nil
	}
	out := make(map[uint16][]byte, len(snap.rawKeys))
	for k, v := range snap.rawKeys {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

func (s *Server) ensureNodeStatsCounter(orttID string, keyID uint16, tag string) *nodeStatsCounter {
	if orttID == "" {
		return nil
	}
	s.nodeStatsMu.RLock()
	c, ok := s.nodeStats[orttID]
	s.nodeStatsMu.RUnlock()
	if ok {
		if keyID > 0 {
			c.keyID.Store(uint32(keyID))
		}
		if tag != "" {
			c.inboundTag.Store(tag)
		}
		return c
	}
	s.nodeStatsMu.Lock()
	defer s.nodeStatsMu.Unlock()
	if c, ok = s.nodeStats[orttID]; ok {
		if keyID > 0 {
			c.keyID.Store(uint32(keyID))
		}
		if tag != "" {
			c.inboundTag.Store(tag)
		}
		return c
	}
	c = &nodeStatsCounter{orttID: orttID}
	if keyID > 0 {
		c.keyID.Store(uint32(keyID))
	}
	if tag != "" {
		c.inboundTag.Store(tag)
	}
	s.nodeStats[orttID] = c
	return c
}

// envFilePath returns the data-dir env file the engine reads its secret from.
func envFilePath() string {
	if dir := strings.TrimSpace(os.Getenv("SMATRIX_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "env")
	}
	return "/opt/sing-matrix/data/env"
}

// LoadSecret returns the ORTT HMAC secret from the environment or the
// data-dir env file, or "" when none is configured. The engine refuses to
// start without a secret, so callers must treat "" as "ORTT unavailable".
func LoadSecret() string {
	if s := strings.TrimSpace(os.Getenv("ORTT_SECRET")); s != "" {
		return s
	}
	if s := strings.TrimSpace(os.Getenv("SMATRIX_ACCEL_SECRET")); s != "" {
		return s
	}
	data, err := os.ReadFile(envFilePath())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ORTT_SECRET=") {
			continue
		}
		if s := strings.TrimSpace(strings.TrimPrefix(line, "ORTT_SECRET=")); s != "" {
			return s
		}
	}
	return ""
}

// SecretConfigured reports whether an ORTT secret is available.
func SecretConfigured() bool { return LoadSecret() != "" }

// accelExplicitlyDisabled reports whether the legacy flag disables ORTT.
func accelExplicitlyDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SMATRIX_ACCEL_ENABLED")))
	return v == "false" || v == "0"
}

// forwardPort returns the loopback port the ORTT forwarder listens on.
// ORTT_FORWARD_PORT overrides it (used by tests); otherwise the engine's
// default forward port applies.
func forwardPort() int {
	if s := strings.TrimSpace(os.Getenv("ORTT_FORWARD_PORT")); s != "" {
		if p, err := strconv.Atoi(s); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return DefaultConfig().ForwardPort
}

// ForwarderListening reports whether the ORTT warm-pool forwarder is
// accepting connections on the loopback forward port. A plain TCP dial
// with a short timeout is enough: it proves something is listening, and
// a refused connection fails fast on loopback.
func ForwarderListening() bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(forwardPort()))
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// EffectiveEnabled reports whether routing traffic through the ORTT warm
// pool is safe: ORTT must be explicitly enabled (default off), the legacy
// flag must not disable it, a secret must be configured, and the forwarder
// must actually be listening. A secret alone is not enough: if the engine
// failed to start, routing to the forwarder would send all TCP 443
// traffic to a dead listener.
func EffectiveEnabled() bool {
	if accelExplicitlyDisabled() {
		return false
	}
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv("ORTT_ENABLED")))
	if enabled == "" {
		enabled = strings.ToLower(strings.TrimSpace(os.Getenv("SMATRIX_ACCEL_ENABLED")))
	}
	if enabled == "" || enabled == "false" || enabled == "0" {
		return false
	}
	if !SecretConfigured() {
		return false
	}
	return ForwarderListening()
}

// ResolveControlPort returns the configured UDP/stats control port (default 39999).
func ResolveControlPort() int {
	portStr := strings.TrimSpace(os.Getenv("ORTT_PORT"))
	if portStr == "" {
		portStr = strings.TrimSpace(os.Getenv("SMATRIX_ACCEL_PORT"))
	}
	if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
		return port
	}
	return DefaultConfig().ControlPort
}

// Start launches the ORTT server if explicitly enabled (default disabled)
func Start(port int) error {
	if accelExplicitlyDisabled() {
		log.Printf("[ORTT] 0-RTT 极速预热与保活引擎已显式停用 (SMATRIX_ACCEL_ENABLED=false)")
		return nil
	}
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv("ORTT_ENABLED")))
	if enabled == "" {
		enabled = strings.ToLower(strings.TrimSpace(os.Getenv("SMATRIX_ACCEL_ENABLED")))
	}
	if enabled == "false" || enabled == "0" {
		log.Printf("[ORTT] 0-RTT 极速预热与保活引擎已显式停用 (ORTT_ENABLED=false)")
		return nil
	}
	if enabled == "" {
		log.Printf("[ORTT] ORTT 未显式启用，默认关闭 (ORTT_ENABLED=true 可开启)")
		return nil
	}

	if port <= 0 {
		port = ResolveControlPort()
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ControlPort = port

	secret := LoadSecret()
	if secret == "" {
		log.Printf("[ORTT] ⚠️ 未配置 ORTT_SECRET 鉴权密钥，ORTT 模块拒绝启动")
		return nil
	}
	cfg.SecretKey = []byte(secret)
	cfg.KeyRing[1] = []byte(secret)

	srv := NewServer(cfg)
	if err := srv.Run(); err != nil {
		log.Printf("[ORTT] 启动失败: %v", err)
		return err
	}
	SetGlobalServer(srv)
	return nil
}

func (s *Server) Run() error {
	// 1. Start Local Forwarder on 127.0.0.1:39998 (synchronously before sing-box launches)
	if err := s.forwarder.Start(); err != nil {
		return err
	}

	// 2. Start Authenticated UDP Control Listener
	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.ControlPort)
	listenFn := s.listenPacket
	if listenFn == nil {
		listenFn = net.ListenPacket
	}
	conn, err := listenFn("udp", addr)
	if err != nil {
		s.forwarder.Close()
		return fmt.Errorf("ortt control listener bind %s failed: %w", addr, err)
	}
	s.udpConn = conn

	log.Printf("[ORTT] 🛡️ ORTT 0-RTT 极速预热与保活引擎 v2.0 已就绪 (UDP 控制: :%d, SOCKS5 复用池: 127.0.0.1:%d)", s.cfg.ControlPort, s.cfg.ForwardPort)
	log.Printf("[ORTT] 🔐 HMAC-SHA256 签名鉴权与防重放机制已激活，保护 %d 个核心站点与全球 CDN 矩阵", len(s.cfg.TargetDomains))

	// 3. Start Local HTTP Stats Listener on 127.0.0.1:39999
	httpMux := http.NewServeMux()
	statsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.GetStats())
	})
	httpMux.Handle("/api/v1/ortt/stats", statsHandler)
	httpMux.Handle("/ortt/stats", statsHandler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.cfg.ControlPort),
		Handler: httpMux,
	}
	s.httpServer = httpServer
	if httpLn, lnErr := net.Listen("tcp", httpServer.Addr); lnErr == nil {
		go func() {
			_ = httpServer.Serve(httpLn)
		}()
	} else {
		log.Printf("[ORTT] ⚠️ HTTP stats listener bind %s failed: %v", httpServer.Addr, lnErr)
	}

	// Background session cleaner & pre-warmer
	s.wg.Add(2)
	go s.udpListenLoop()
	go s.sessionWorker()

	// Initial DNS prefetch
	s.dnsMgr.PrefetchDomains(s.cfg.TargetDomains)

	return nil
}

func (s *Server) udpListenLoop() {
	defer s.wg.Done()
	buf := make([]byte, 256)

	for {
		_ = s.udpConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, remoteAddr, err := s.udpConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
					log.Printf("[ORTT] UDP ReadFrom error: %v", err)
				}
				continue
			}
		}

		atomic.AddInt64(&GlobalStats.PacketsReceived, 1)
		host, _, _ := net.SplitHostPort(remoteAddr.String())

		// Rate limit check
		if !s.limiter.Allow(host) {
			atomic.AddInt64(&GlobalStats.PacketsDropped, 1)
			continue
		}

		// Cryptographic authentication & replay check with strict per-KeyID isolation
		snap := s.keyState.Load()
		var keyMap map[uint16][]byte
		if snap != nil {
			keyMap = snap.rawKeys
		}
		cmd, _, keyID, matchedSecret, err := VerifyPacketWithKeys(keyMap, s.cfg.SecretKey, buf[:n], s.replayCache)
		if err != nil {
			atomic.AddInt64(&GlobalStats.PacketsDropped, 1)
			continue
		}

		s.handleCommand(cmd, keyID, matchedSecret, remoteAddr, buf[:n])
	}
}

func (s *Server) triggerAsyncPrewarm() {
	if s.prewarming.CompareAndSwap(false, true) {
		go func() {
			defer s.prewarming.Store(false)
			s.prewarmPool()
		}()
	}
}

func (s *Server) checkDataPlaneStatus() uint8 {
	var flags uint8 = 0

	// Bit 0 (0x01): 127.0.0.1:39998 SOCKS5 is listening
	if s.forwarder != nil && s.forwarder.IsListening() {
		flags |= 0x01
	}

	// Bit 1 (0x02): Proxy engine (sing-box or xray) is running and configured with 39998
	if isProxyConfiguredAndRunning() {
		flags |= 0x02
	}

	// Bit 2 (0x04): Active warm conns > 0
	if atomic.LoadInt64(&GlobalStats.ActiveWarmConns) > 0 {
		flags |= 0x04
	}

	return flags
}

func isProxyConfiguredAndRunning() bool {
	// 1. Check if sing-box or xray process is running in /proc
	proxyRunning := false
	entries, err := os.ReadDir("/proc")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cmdlineBytes, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
			if err != nil {
				continue
			}
			cmdline := string(cmdlineBytes)
			if strings.Contains(cmdline, "sing-box") || strings.Contains(cmdline, "xray") {
				proxyRunning = true
				break
			}
		}
	}

	if !proxyRunning {
		return false
	}

	// 2. Check if any active proxy config has 39998 or ortt-warm-pool
	cfgPaths := []string{
		"/opt/sing-matrix/data/config.json",
		"/usr/local/etc/xray/config.json",
		"/etc/sui-xray/config.json",
	}
	if envCfg := strings.TrimSpace(os.Getenv("SMATRIX_CONFIG")); envCfg != "" {
		cfgPaths = append([]string{envCfg}, cfgPaths...)
	}

	for _, p := range cfgPaths {
		data, err := os.ReadFile(p)
		if err == nil {
			content := string(data)
			if strings.Contains(content, "39998") || strings.Contains(content, "ortt-warm-pool") {
				return true
			}
		}
	}

	return false
}

func (s *Server) buildPongPacket(nonceBytes []byte, signingSecret []byte) []byte {
	pong := make([]byte, 48)
	copy(pong[0:4], Magic)       // "ORTT"
	pong[4] = ProtocolVersion    // 0x02
	pong[5] = CmdPong            // 0x84
	copy(pong[6:10], nonceBytes) // Echo Nonce (4 bytes)

	hits := uint32(atomic.LoadInt64(&GlobalStats.PoolHits))
	misses := uint32(atomic.LoadInt64(&GlobalStats.PoolMisses))
	warmConns := uint16(atomic.LoadInt64(&GlobalStats.ActiveWarmConns))
	dropped := uint32(atomic.LoadInt64(&GlobalStats.PacketsDropped))

	binary.BigEndian.PutUint32(pong[10:14], hits)
	binary.BigEndian.PutUint32(pong[14:18], misses)
	binary.BigEndian.PutUint16(pong[18:20], warmConns)
	binary.BigEndian.PutUint32(pong[20:24], dropped)

	pong[24] = s.checkDataPlaneStatus()
	// pong[25:32] are reserved zero bytes

	if len(signingSecret) == 0 {
		signingSecret = s.cfg.SecretKey
	}
	sig := ComputeHMAC(signingSecret, pong[:32])
	copy(pong[32:48], sig)
	return pong
}

func (s *Server) recordNodeCommand(cmd byte, keyID uint16, now time.Time) {
	var orttID, tag string
	if snap := s.keyState.Load(); snap != nil {
		if entry, ok := snap.entries[keyID]; ok {
			orttID = entry.OrttID
			tag = entry.InboundTag
		}
	}
	if orttID == "" {
		if keyID <= 1 {
			orttID = "ortt_global"
		} else {
			orttID = fmt.Sprintf("ortt_key_%d", keyID)
		}
	}
	c := s.ensureNodeStatsCounter(orttID, keyID, tag)
	if c == nil {
		return
	}
	nowUnix := now.Unix()
	c.lastSeenUnix.Store(nowUnix)
	switch cmd {
	case CmdWakeup:
		c.wakeCount.Add(1)
		c.lastWakeUnix.Store(nowUnix)
	case CmdHeartbeat:
		c.heartbeatCount.Add(1)
	case CmdPing:
		c.pingCount.Add(1)
	}
}

func (s *Server) handleCommand(cmd byte, keyID uint16, matchedSecret []byte, remoteAddr net.Addr, rawReq []byte) {
	clientAddr := remoteAddr.String()
	now := time.Now()
	s.recordNodeCommand(cmd, keyID, now)

	val, loaded := s.sessions.Load(clientAddr)
	var sess *ClientSession
	if loaded {
		sess = val.(*ClientSession)
	} else {
		sess = &ClientSession{RemoteAddr: clientAddr}
		s.sessions.Store(clientAddr, sess)
	}

	switch cmd {
	case CmdWakeup:
		if sess.TouchWakeup(now) || atomic.LoadInt64(&GlobalStats.ActiveWarmConns) < 20 {
			log.Printf("[ORTT] ⚡ 客户端 [%s] (KeyID=%d) 鉴权成功激活，异步预热核心目标连接池", clientAddr, keyID)
			s.triggerAsyncPrewarm()
		}
		if len(rawReq) >= 14 && s.udpConn != nil {
			nonce := rawReq[10:14]
			pong := s.buildPongPacket(nonce, matchedSecret)
			_, _ = s.udpConn.WriteTo(pong, remoteAddr)
		}

	case CmdHeartbeat:
		sess.TouchActive(now)
		if atomic.LoadInt64(&GlobalStats.ActiveWarmConns) < 20 {
			s.triggerAsyncPrewarm()
		}

	case CmdSleep:
		log.Printf("[ORTT] 💤 客户端 [%s] (KeyID=%d) 正常休眠", clientAddr, keyID)
		s.sessions.Delete(clientAddr)

	case CmdPing:
		sess.TouchActive(now)
		if atomic.LoadInt64(&GlobalStats.ActiveWarmConns) < 20 {
			s.triggerAsyncPrewarm()
		}
		if len(rawReq) >= 14 && s.udpConn != nil {
			nonce := rawReq[10:14]
			pong := s.buildPongPacket(nonce, matchedSecret)
			_, _ = s.udpConn.WriteTo(pong, remoteAddr)
		}
	}
}

func (s *Server) prewarmPool() {
	domains := s.cfg.TargetDomains
	if len(domains) == 0 {
		return
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, domain := range domains {
		select {
		case <-s.ctx.Done():
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(d string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			s.pool.Replenish(d)
		}(domain)
		if s.cfg.ReplenishDelay > 0 {
			time.Sleep(s.cfg.ReplenishDelay)
		}
	}
	wg.Wait()
}

func (s *Server) sessionWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			activeCount := int64(0)

			s.sessions.Range(func(key, value interface{}) bool {
				sess := value.(*ClientSession)
				if now.Sub(sess.GetLastActive()) > s.cfg.SessionExpire {
					s.sessions.Delete(key)
				} else {
					activeCount++
				}
				return true
			})

			atomic.StoreInt64(&GlobalStats.ActiveSessions, activeCount)
			if activeCount > 0 {
				s.prewarmPool()
			}
		}
	}
}

func (s *Server) Close() {
	s.cancel()
	if s.httpServer != nil {
		_ = s.httpServer.Close()
	}
	if s.udpConn != nil {
		_ = s.udpConn.Close()
	}
	s.forwarder.Close()
	s.pool.Close()
	s.dnsMgr.Close()
	s.wg.Wait()
}

func snapshotNodeCounter(c *nodeStatsCounter) NodeOrttStats {
	tag, _ := c.inboundTag.Load().(string)
	return NodeOrttStats{
		OrttID:         c.orttID,
		KeyID:          uint16(c.keyID.Load()),
		InboundTag:     tag,
		LastWakeUnix:   c.lastWakeUnix.Load(),
		WakeCount:      c.wakeCount.Load(),
		HeartbeatCount: c.heartbeatCount.Load(),
		PingCount:      c.pingCount.Load(),
		LastSeenUnix:   c.lastSeenUnix.Load(),
	}
}

func (s *Server) GetAllNodeStats() map[string]NodeOrttStats {
	if s == nil {
		return nil
	}
	s.nodeStatsMu.RLock()
	defer s.nodeStatsMu.RUnlock()
	if len(s.nodeStats) == 0 {
		return nil
	}
	out := make(map[string]NodeOrttStats, len(s.nodeStats))
	for k, c := range s.nodeStats {
		out[k] = snapshotNodeCounter(c)
	}
	return out
}

func (s *Server) GetNodeStats(orttID string) (NodeOrttStats, bool) {
	if s == nil || orttID == "" {
		return NodeOrttStats{}, false
	}
	s.nodeStatsMu.RLock()
	c, ok := s.nodeStats[orttID]
	s.nodeStatsMu.RUnlock()
	if !ok {
		return NodeOrttStats{}, false
	}
	return snapshotNodeCounter(c), true
}

func (s *Server) GetStats() Stats {
	st := loadAtomicGlobalStats()
	if s != nil {
		st.Nodes = s.GetAllNodeStats()
	}
	return st
}

func loadAtomicGlobalStats() Stats {
	return Stats{
		PoolHits:        atomic.LoadInt64(&GlobalStats.PoolHits),
		PoolMisses:      atomic.LoadInt64(&GlobalStats.PoolMisses),
		PoolReplays:     atomic.LoadInt64(&GlobalStats.PoolReplays),
		ActiveWarmConns: atomic.LoadInt64(&GlobalStats.ActiveWarmConns),
		PacketsReceived: atomic.LoadInt64(&GlobalStats.PacketsReceived),
		PacketsDropped:  atomic.LoadInt64(&GlobalStats.PacketsDropped),
		ActiveSessions:  atomic.LoadInt64(&GlobalStats.ActiveSessions),
		UptimeSeconds:   int64(time.Since(startTime).Seconds()),
	}
}

func GetNodeStats(orttID string) (NodeOrttStats, bool) {
	if gs := GlobalServer(); gs != nil {
		return gs.GetNodeStats(orttID)
	}
	return NodeOrttStats{}, false
}

func GetStats() Stats {
	st := loadAtomicGlobalStats()
	if gs := GlobalServer(); gs != nil {
		st.Nodes = gs.GetAllNodeStats()
	}
	return st
}
