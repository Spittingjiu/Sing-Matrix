package accelerator

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

var Magic = []byte{0x53, 0x4D, 0x54, 0x58} // "SMTX"

const (
	CmdWakeup    byte = 0x01
	CmdHeartbeat byte = 0x02
	CmdSleep     byte = 0x03
)

// Full spectrum of high-value overseas domains for Chinese cross-border users
var TargetDomains = []string{
	// 1. Google Core Ecosystem & YouTube
	"www.google.com", "google.com", "www.youtube.com", "googlevideo.com",
	"gstatic.com", "googleapis.com", "ytimg.com",

	// 2. AI & Large Language Models
	"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com",
	"claude.ai", "anthropic.com", "gemini.google.com", "cursor.com", "perplexity.ai",

	// 3. Social Media & Instant Messaging
	"twitter.com", "x.com", "twimg.com",
	"telegram.org", "t.me",
	"instagram.com", "facebook.com", "discord.com",

	// 4. Global Streaming & Music
	"netflix.com", "nflxvideo.net", "spotify.com", "disneyplus.com", "twitch.tv",

	// 5. Developer & Cloud Knowledge
	"github.com", "githubusercontent.com", "docker.com", "v2ex.com", "wikipedia.org",

	// 6. Global Exchange & Finance
	"binance.com", "okx.com",
}

type ClientSession struct {
	LastActive time.Time
	RemoteAddr string
	LastWakeup time.Time
}

var (
	sessions    sync.Map
	dnsCache    sync.Map
	workerSem   = make(chan struct{}, 8)  // Concurrency guard: max 8 concurrent dials
	activeConns = make(chan struct{}, 15) // Connection pool guard: max 15 active warm sockets
)

// Start starts the Sing-Matrix UDP keep-alive & pre-warming engine
func Start(port int) {
	if port <= 0 {
		port = 39999
	}

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[accelerator] failed to bind UDP :%d: %v", port, err)
		return
	}
	defer conn.Close()

	log.Printf("[accelerator] Sing-Matrix 伴生极速预热引擎已就绪，监听 UDP :%d (覆盖 %d 个主流出海域名)", port, len(TargetDomains))

	// Background ticker for cleanup & periodic warm-up
	go backgroundWorker()

	buf := make([]byte, 256)
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}

		if n >= 6 && string(buf[:4]) == string(Magic) {
			cmd := buf[5]
			handlePacket(cmd, remoteAddr.String())
		}
	}
}

func handlePacket(cmd byte, clientAddr string) {
	now := time.Now()
	switch cmd {
	case CmdWakeup:
		val, loaded := sessions.Load(clientAddr)
		var sess *ClientSession
		if loaded {
			sess = val.(*ClientSession)
		} else {
			sess = &ClientSession{RemoteAddr: clientAddr}
			sessions.Store(clientAddr, sess)
		}

		// Debounce: prevent wakeup flood from spamming VPS network
		if now.Sub(sess.LastWakeup) > 10*time.Second {
			sess.LastWakeup = now
			sess.LastActive = now
			log.Printf("[accelerator] 🚀 收到客户端 [%s] 激活小包，启动主流网站 (Google/YouTube/OpenAI/Telegram) TCP & DNS 预热池", clientAddr)
			go prewarmTargets()
		}

	case CmdHeartbeat:
		val, loaded := sessions.Load(clientAddr)
		if loaded {
			sess := val.(*ClientSession)
			sess.LastActive = now
		} else {
			sessions.Store(clientAddr, &ClientSession{LastActive: now, RemoteAddr: clientAddr})
		}

	case CmdSleep:
		log.Printf("[accelerator] 💤 客户端 [%s] 关闭连接，释放预热连接池", clientAddr)
		sessions.Delete(clientAddr)
	}
}

func backgroundWorker() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		hasActive := false

		sessions.Range(func(key, value interface{}) bool {
			sess := value.(*ClientSession)
			// Idle > 45s => auto expire
			if now.Sub(sess.LastActive) > 45*time.Second {
				log.Printf("[accelerator] ⏳ 客户端 [%s] 心跳超时已解绑", sess.RemoteAddr)
				sessions.Delete(key)
			} else {
				hasActive = true
			}
			return true
		})

		// If at least one active Aero client exists, keep upstream pools warm
		if hasActive {
			go prewarmTargets()
		}
	}
}

func prewarmTargets() {
	for _, domain := range TargetDomains {
		select {
		case workerSem <- struct{}{}:
			go func(d string) {
				defer func() { <-workerSem }()
				prewarmDomain(d)
			}(domain)
		default:
			// Semaphore full, skip to avoid resource hogging
			continue
		}
	}
}

func prewarmDomain(domain string) {
	// 1. DNS Pre-warming (Zero latency resolution)
	var ip string
	if cached, ok := dnsCache.Load(domain); ok {
		ip = cached.(string)
	} else {
		ips, err := net.LookupIP(domain)
		if err != nil || len(ips) == 0 {
			return
		}
		ip = ips[0].String()
		dnsCache.Store(domain, ip)
	}

	// 2. TCP 3-Way Handshake & BBR Pre-warming
	targetAddr := fmt.Sprintf("%s:443", ip)

	select {
	case activeConns <- struct{}{}:
		dialer := &net.Dialer{
			Timeout:   2500 * time.Millisecond,
			KeepAlive: 15 * time.Second,
		}

		conn, err := dialer.Dial("tcp", targetAddr)
		if err != nil {
			<-activeConns
			return
		}

		// Hold socket in warm pool for 10s, then close gracefully before remote Anti-DDoS timeout
		go func(c net.Conn) {
			defer func() {
				_ = c.Close()
				<-activeConns
			}()
			time.Sleep(10 * time.Second)
		}(conn)

	default:
		// Active pool full, skip
		return
	}
}
