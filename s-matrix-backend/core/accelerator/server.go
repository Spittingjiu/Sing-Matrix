package accelerator

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
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
	limiter := NewRateLimiter(30.0, 2.0) // 30 global rps, 2 rps per IP
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

// Start launches the accelerator server if enabled
func Start(port int) {
	enabled := os.Getenv("SMATRIX_ACCEL_ENABLED")
	if enabled != "true" && enabled != "1" {
		log.Printf("[accelerator] Sing-Matrix 极速预热引擎未启用 (如需开启请设置 SMATRIX_ACCEL_ENABLED=true)")
		return
	}

	if port <= 0 {
		log.Printf("[accelerator] 控制端口设为 %d，加速引擎已停用", port)
		return
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ControlPort = port

	secret := os.Getenv("SMATRIX_ACCEL_SECRET")
	if secret != "" {
		cfg.SecretKey = []byte(secret)
	}

	globalServer = NewServer(cfg)
	if err := globalServer.Run(); err != nil {
		log.Printf("[accelerator] 启动失败: %v", err)
	}
}

func (s *Server) Run() error {
	// 1. Start Local Forwarder on 127.0.0.1:39998
	if err := s.forwarder.Start(); err != nil {
		return err
	}

	// 2. Start Authenticated UDP Control Listener
	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.ControlPort)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		s.forwarder.Close()
		return fmt.Errorf("control listener bind %s failed: %w", addr, err)
	}
	s.udpConn = conn

	log.Printf("[accelerator] 🛡️ Sing-Matrix 伴生加速引擎 v2.0 已就绪 (UDP 控制: :%d, SOCKS5 复用池: 127.0.0.1:%d)", s.cfg.ControlPort, s.cfg.ForwardPort)
	log.Printf("[accelerator] 🔐 HMAC-SHA256 签名鉴权与防重放机制已激活，保护目标站点: %v", s.cfg.TargetDomains)

	// Background session cleaner & pre-warmer
	s.wg.Add(2)
	go s.udpListenLoop()
	go s.sessionWorker()

	// Initial prefetch
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

		// Cryptographic authentication & replay check
		cmd, _, _, err := VerifyPacket(s.cfg.SecretKey, buf[:n], s.replayCache)
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
			log.Printf("[accelerator] ⚡ 客户端 [%s] 鉴权成功激活，预热目标连接池", clientAddr)
			s.prewarmPool()
		}

	case CmdHeartbeat:
		sess.TouchActive(now)

	case CmdSleep:
		log.Printf("[accelerator] 💤 客户端 [%s] 正常休眠", clientAddr)
		s.sessions.Delete(clientAddr)

	case CmdPing:
		sess.TouchActive(now)
	}
}

func (s *Server) prewarmPool() {
	for _, domain := range s.cfg.TargetDomains {
		go s.pool.Replenish(domain)
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
