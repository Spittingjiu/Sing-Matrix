package ortt

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	cfg         *Config
	dnsMgr      *DNSManager
	pool        *UpstreamSocketPool
	forwarder   *LocalForwarder
	limiter     *RateLimiter
	replayCache *ReplayCache
	sessions    sync.Map // string -> *ClientSession
	udpConn     net.PacketConn
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

var globalServer *Server

func NewServer(cfg *Config) *Server {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())
	dnsMgr := NewDNSManager(5 * time.Minute)
	pool := NewUpstreamSocketPool(cfg.MaxWarmPerDomain, cfg.MaxTotalSockets, dnsMgr)
	forwarder := NewLocalForwarder(cfg.ForwardPort, pool)
	limiter := NewRateLimiter(50.0, 5.0) // 50 global rps, 5 rps per IP
	replayCache := NewReplayCache()

	return &Server{
		cfg:         cfg,
		dnsMgr:      dnsMgr,
		pool:        pool,
		forwarder:   forwarder,
		limiter:     limiter,
		replayCache: replayCache,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start launches the ORTT server if enabled (default enabled)
func Start(port int) error {
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv("ORTT_ENABLED")))
	if enabled == "" {
		enabled = strings.ToLower(strings.TrimSpace(os.Getenv("SMATRIX_ACCEL_ENABLED")))
	}
	if enabled == "false" || enabled == "0" {
		log.Printf("[ORTT] 0-RTT 极速预热与保活引擎已显式停用 (ORTT_ENABLED=false)")
		return nil
	}

	if port <= 0 {
		portStr := os.Getenv("ORTT_PORT")
		if portStr == "" {
			portStr = os.Getenv("SMATRIX_ACCEL_PORT")
		}
		port, _ = strconv.Atoi(portStr)
		if port <= 0 {
			port = 39999
		}
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ControlPort = port

	secret := os.Getenv("ORTT_SECRET")
	if secret == "" {
		secret = os.Getenv("SMATRIX_ACCEL_SECRET")
	}
	if secret == "" {
		if envData, err := os.ReadFile("/opt/sing-matrix/data/env"); err == nil {
			for _, line := range strings.Split(string(envData), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "ORTT_SECRET=") {
					secret = strings.TrimPrefix(line, "ORTT_SECRET=")
					break
				}
			}
		}
	}
	if secret == "" {
		log.Printf("[ORTT] ⚠️ 未配置 ORTT_SECRET 鉴权密钥，ORTT 模块拒绝启动")
		return nil
	}
	cfg.SecretKey = []byte(secret)
	cfg.KeyRing[1] = []byte(secret)

	globalServer = NewServer(cfg)
	if err := globalServer.Run(); err != nil {
		log.Printf("[ORTT] 启动失败: %v", err)
		return err
	}
	return nil
}

func (s *Server) Run() error {
	// 1. Start Local Forwarder on 127.0.0.1:39998 (synchronously before sing-box launches)
	if err := s.forwarder.Start(); err != nil {
		return err
	}

	// 2. Start Authenticated UDP Control Listener
	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.ControlPort)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		s.forwarder.Close()
		return fmt.Errorf("ortt control listener bind %s failed: %w", addr, err)
	}
	s.udpConn = conn

	log.Printf("[ORTT] 🛡️ ORTT 0-RTT 极速预热与保活引擎 v2.0 已就绪 (UDP 控制: :%d, SOCKS5 复用池: 127.0.0.1:%d)", s.cfg.ControlPort, s.cfg.ForwardPort)
	log.Printf("[ORTT] 🔐 HMAC-SHA256 签名鉴权与防重放机制已激活，保护 %d 个核心站点与全球 CDN 矩阵", len(s.cfg.TargetDomains))

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

		// Cryptographic authentication & replay check with multi-key support
		cmd, _, _, err := VerifyPacketWithKeys(s.cfg.KeyRing, s.cfg.SecretKey, buf[:n], s.replayCache)
		if err != nil {
			atomic.AddInt64(&GlobalStats.PacketsDropped, 1)
			continue
		}

		s.handleCommand(cmd, remoteAddr.String())
	}
}

func (s *Server) handleCommand(cmd byte, clientAddr string) {
	now := time.Now()
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
		if sess.TouchWakeup(now) {
			log.Printf("[ORTT] ⚡ 客户端 [%s] 鉴权成功激活，预热核心目标连接池", clientAddr)
			s.prewarmPool()
		}

	case CmdHeartbeat:
		sess.TouchActive(now)

	case CmdSleep:
		log.Printf("[ORTT] 💤 客户端 [%s] 正常休眠", clientAddr)
		s.sessions.Delete(clientAddr)

	case CmdPing:
		sess.TouchActive(now)
	}
}

func (s *Server) prewarmPool() {
	for _, domain := range s.cfg.TargetDomains {
		go func(d string) {
			s.pool.Replenish(d)
		}(domain)
		if s.cfg.ReplenishDelay > 0 {
			time.Sleep(s.cfg.ReplenishDelay)
		}
	}
}

func (s *Server) sessionWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
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
	if s.udpConn != nil {
		_ = s.udpConn.Close()
	}
	s.forwarder.Close()
	s.pool.Close()
	s.dnsMgr.Close()
	s.wg.Wait()
}

func GetStats() Stats {
	st := GlobalStats
	st.UptimeSeconds = int64(time.Since(startTime).Seconds())
	return st
}
