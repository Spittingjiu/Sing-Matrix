package ortt

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type warmSocket struct {
	conn      net.Conn
	createdAt time.Time
}

// UpstreamSocketPool manages genuine, pre-connected TCP sockets for accelerated destinations
type UpstreamSocketPool struct {
	mu           sync.Mutex
	pools        map[string][]*warmSocket // Key: "domain:port"
	maxPerDomain int
	maxTotal     int
	currentTotal int
	dialer       *net.Dialer
	dnsMgr       *DNSManager
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewUpstreamSocketPool(maxPerDomain, maxTotal int, dnsMgr *DNSManager) *UpstreamSocketPool {
	if maxPerDomain <= 0 {
		maxPerDomain = 2
	}
	if maxTotal <= 0 {
		maxTotal = 16
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &UpstreamSocketPool{
		pools:        make(map[string][]*warmSocket),
		maxPerDomain: maxPerDomain,
		maxTotal:     maxTotal,
		dialer: &net.Dialer{
			Timeout:   2500 * time.Millisecond,
			KeepAlive: 15 * time.Second,
		},
		dnsMgr: dnsMgr,
		ctx:    ctx,
		cancel: cancel,
	}

	// Background health auditor: evicts dead/stale sockets
	go p.healthAuditor()
	return p
}

// NormalizeKey ensures pool keys are consistently formatted as "domain:port"
func NormalizeKey(target string, defaultPort int) string {
	target = strings.TrimSpace(target)
	if strings.Contains(target, ":") {
		host, portStr, err := net.SplitHostPort(target)
		if err == nil {
			return strings.ToLower(host) + ":" + portStr
		}
	}
	return strings.ToLower(target) + ":" + strconv.Itoa(defaultPort)
}

// Get attempts to pop an established upstream TCP connection from the pool
// This eliminates the VPS -> Destination TCP handshake latency in high-RTT routes
func (p *UpstreamSocketPool) Get(targetKey string) (net.Conn, bool) {
	key := NormalizeKey(targetKey, 443)

	p.mu.Lock()
	defer p.mu.Unlock()

	list := p.pools[key]
	for len(list) > 0 {
		ws := list[len(list)-1]
		p.pools[key] = list[:len(list)-1]
		p.currentTotal--
		atomic.StoreInt64(&GlobalStats.ActiveWarmConns, int64(p.currentTotal))

		// Check staleness (max 15s warm life to prevent remote server idle termination)
		if time.Since(ws.createdAt) > 15*time.Second {
			_ = ws.conn.Close()
			list = p.pools[key]
			continue
		}

		// Quick non-blocking read to verify socket is not broken/RST by remote
		_ = ws.conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
		oneByte := make([]byte, 1)
		n, err := ws.conn.Read(oneByte)
		_ = ws.conn.SetReadDeadline(time.Time{}) // clear deadline

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Socket is clean, healthy, and ready for client payload
				atomic.AddInt64(&GlobalStats.PoolHits, 1)
				// Replenish pool in background asynchronously
				go p.Replenish(key)
				return ws.conn, true
			}
			// Connection closed or reset by peer
			_ = ws.conn.Close()
			list = p.pools[key]
			continue
		}

		if n > 0 {
			// Unexpected data from server before client sent request, discard
			_ = ws.conn.Close()
			list = p.pools[key]
			continue
		}
	}

	// Pool miss or exhausted
	atomic.AddInt64(&GlobalStats.PoolMisses, 1)
	return nil, false
}

// Replenish establishes a new upstream connection for targetKey if quota permits
func (p *UpstreamSocketPool) Replenish(targetKey string) {
	key := NormalizeKey(targetKey, 443)

	p.mu.Lock()
	if p.currentTotal >= p.maxTotal || len(p.pools[key]) >= p.maxPerDomain {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	host, portStr, _ := net.SplitHostPort(key)
	port, _ := strconv.Atoi(portStr)
	if port <= 0 {
		port = 443
	}

	var dialTarget string
	if p.dnsMgr != nil {
		ip, err := p.dnsMgr.Resolve(host)
		if err == nil && ip != "" {
			dialTarget = net.JoinHostPort(ip, strconv.Itoa(port))
		} else {
			dialTarget = key
		}
	} else {
		dialTarget = key
	}

	conn, err := p.dialer.DialContext(p.ctx, "tcp", dialTarget)
	if err != nil {
		return
	}

	// Optimize socket parameters
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(15 * time.Second)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Recheck capacity limits under lock
	if p.currentTotal >= p.maxTotal || len(p.pools[key]) >= p.maxPerDomain {
		_ = conn.Close()
		return
	}

	p.pools[key] = append(p.pools[key], &warmSocket{
		conn:      conn,
		createdAt: time.Now(),
	})
	p.currentTotal++
	atomic.StoreInt64(&GlobalStats.ActiveWarmConns, int64(p.currentTotal))
}

func (p *UpstreamSocketPool) healthAuditor() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.audit()
		}
	}
}

func (p *UpstreamSocketPool) audit() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, list := range p.pools {
		var active []*warmSocket
		for _, ws := range list {
			if now.Sub(ws.createdAt) > 15*time.Second {
				_ = ws.conn.Close()
				p.currentTotal--
				continue
			}
			active = append(active, ws)
		}
		if len(active) == 0 {
			delete(p.pools, key)
		} else {
			p.pools[key] = active
		}
	}
	atomic.StoreInt64(&GlobalStats.ActiveWarmConns, int64(p.currentTotal))
}

func (p *UpstreamSocketPool) Close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, list := range p.pools {
		for _, ws := range list {
			_ = ws.conn.Close()
		}
	}
	p.pools = make(map[string][]*warmSocket)
	p.currentTotal = 0
	atomic.StoreInt64(&GlobalStats.ActiveWarmConns, 0)
}
