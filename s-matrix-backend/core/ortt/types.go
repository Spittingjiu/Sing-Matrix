package ortt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Wire Protocol Constants (ORTT Protocol v2.0)
var Magic = []byte{0x4F, 0x52, 0x54, 0x54}       // "ORTT"
var LegacyMagic = []byte{0x53, 0x4D, 0x54, 0x58} // "SMTX" (backward compatibility)

const (
	ProtocolVersion byte = 0x02

	CmdWakeup    byte = 0x01
	CmdHeartbeat byte = 0x02
	CmdSleep     byte = 0x03
	CmdPing      byte = 0x04

	// PacketWireLength: Magic(4) + Ver(1) + Cmd(1) + Timestamp(4) + Nonce(4) + KeyID(2) + HMAC(16) = 32 bytes
	PacketWireLength = 32

	// MaxClockSkew: Allow max 30s timestamp drift for replay prevention
	MaxClockSkew = 30 * time.Second

	DefaultKeyID = 1
)

var (
	ErrPacketTooShort = errors.New("packet length invalid")
	ErrInvalidMagic   = errors.New("invalid magic header")
	ErrInvalidVersion = errors.New("unsupported protocol version")
	ErrClockSkew      = errors.New("timestamp expired or clock skew too large (>30s)")
	ErrReplayDetected = errors.New("replay attack detected: nonce already used in window")
	ErrHMACMismatch   = errors.New("hmac signature verification failed")
	ErrRateLimited    = errors.New("rate limited")
)

// Config defines ORTT runtime configuration
type Config struct {
	Enabled          bool              `json:"enabled"`
	ControlPort      int               `json:"control_port"`        // UDP 39999
	ForwardPort      int               `json:"forward_port"`        // Local SOCKS5 39998 (127.0.0.1 only)
	SecretKey        []byte            `json:"-"`                   // Primary HMAC-SHA256 Secret
	KeyRing          map[uint16][]byte `json:"-"`                   // KeyID -> Secret map
	TargetDomains    []string          `json:"target_domains"`      // Whitelisted domains to maintain in warm pool
	MaxWarmPerDomain int               `json:"max_warm_per_domain"` // Warm sockets per domain (default 2)
	MaxTotalSockets  int               `json:"max_total_sockets"`   // Max global warm sockets (default 96)
	ReplenishDelay   time.Duration     `json:"replenish_delay"`     // Delay between parallel dials (default 80ms)
	IdleTimeout      time.Duration     `json:"idle_timeout"`        // Max socket idle life (default 15s)
	SessionExpire    time.Duration     `json:"session_expire"`      // Session timeout (default 45s)
}

func DefaultConfig() *Config {
	return &Config{
		Enabled:     true, // Enabled by default
		ControlPort: 39999,
		ForwardPort: 39998,
		SecretKey:   nil,
		KeyRing:     make(map[uint16][]byte),
		TargetDomains: []string{
			// 全球核心 CDN 节点 (16 个)
			"www.cloudflare.com", "cdnjs.cloudflare.com", "one.one.one.one",
			"www.gstatic.com", "fonts.gstatic.com", "gvt1.com", "gvt2.com", "yt3.ggpht.com", "i.ytimg.com",
			"github.githubassets.com", "raw.githubusercontent.com", "avatars.githubusercontent.com",
			"abs.twimg.com", "pbs.twimg.com",
			"cdn.oaistatic.com", "cdn.jsdelivr.net",

			// AI 与生产力平台 (8 个)
			"chatgpt.com", "api.openai.com", "claude.ai", "api.anthropic.com",
			"gemini.google.com", "aistudio.google.com", "www.perplexity.ai", "www.notion.so",

			// 日常高频音视频、社交与搜索网站 (16 个)
			"www.google.com", "www.youtube.com", "m.youtube.com", "googlevideo.com",
			"github.com", "api.github.com",
			"x.com", "api.x.com",
			"core.telegram.org", "web.telegram.org", "t.me",
			"www.reddit.com", "discord.com", "gateway.discord.gg",
			"www.wikipedia.org", "store.steampowered.com",
		},
		MaxWarmPerDomain: 2,
		MaxTotalSockets:  96,
		ReplenishDelay:   80 * time.Millisecond,
		IdleTimeout:      15 * time.Second,
		SessionExpire:    45 * time.Second,
	}
}

// Stats provides atomic real-time observability telemetry
type Stats struct {
	PoolHits        int64 `json:"pool_hits"`         // Reused pre-connected socket successfully
	PoolMisses      int64 `json:"pool_misses"`       // Fell back to on-demand direct dial
	PoolReplays     int64 `json:"pool_replays"`      // Stale/dead warm sockets recovered by first-byte replay
	ActiveWarmConns int64 `json:"active_warm_conns"` // Idle sockets waiting in pool
	PacketsReceived int64 `json:"packets_received"`  // Control packets
	PacketsDropped  int64 `json:"packets_dropped"`   // Auth failed, replayed or rate limited
	ActiveSessions  int64 `json:"active_sessions"`   // Active authenticated client sessions
	UptimeSeconds   int64 `json:"uptime_seconds"`
}

var GlobalStats Stats
var startTime = time.Now()

// ClientSession records active client state with strict thread safety
type ClientSession struct {
	sync.RWMutex
	RemoteAddr string
	LastActive time.Time
	LastWakeup time.Time
}

func (s *ClientSession) TouchActive(now time.Time) {
	s.Lock()
	defer s.Unlock()
	s.LastActive = now
}

func (s *ClientSession) GetLastActive() time.Time {
	s.RLock()
	defer s.RUnlock()
	return s.LastActive
}

func (s *ClientSession) TouchWakeup(now time.Time) bool {
	s.Lock()
	defer s.Unlock()
	if now.Sub(s.LastWakeup) < 15*time.Second { // 15s debounce
		return false
	}
	s.LastWakeup = now
	s.LastActive = now
	return true
}

func ComputeHMAC(secret []byte, data []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write(data)
	return h.Sum(nil)[:16] // Truncate to first 16 bytes (128-bit security margin)
}

// BuildPacket constructs a signed v2 packet with Nonce and KeyID
func BuildPacket(secret []byte, cmd byte, keyID uint16) []byte {
	buf := make([]byte, PacketWireLength)
	copy(buf[0:4], Magic)
	buf[4] = ProtocolVersion
	buf[5] = cmd

	ts := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(buf[6:10], ts)

	// Random 4-byte nonce
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])
	copy(buf[10:14], nonce[:])

	binary.BigEndian.PutUint16(buf[14:16], keyID)

	sig := ComputeHMAC(secret, buf[:16])
	copy(buf[16:32], sig)
	return buf
}
