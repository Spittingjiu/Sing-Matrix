package ortt

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type DNSEntry struct {
	IPs       []string
	ExpiresAt time.Time
}

type DNSManager struct {
	mu      sync.RWMutex
	cache   map[string]*DNSEntry
	ttl     time.Duration
	dialer  *net.Dialer
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewDNSManager(ttl time.Duration) *DNSManager {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &DNSManager{
		cache:  make(map[string]*DNSEntry),
		ttl:    ttl,
		dialer: &net.Dialer{Timeout: 2 * time.Second},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Resolve returns a cached or freshly resolved IP for domain
func (dm *DNSManager) Resolve(domain string) (string, error) {
	dm.mu.RLock()
	entry, found := dm.cache[domain]
	if found && time.Now().Before(entry.ExpiresAt) && len(entry.IPs) > 0 {
		ip := entry.IPs[0]
		dm.mu.RUnlock()
		return ip, nil
	}
	dm.mu.RUnlock()

	// Cache miss or expired: resolve synchronously
	return dm.refresh(domain)
}

func (dm *DNSManager) refresh(domain string) (string, error) {
	ips, err := net.LookupIP(domain)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("dns resolve %s failed: %w", domain, err)
	}

	validIPs := make([]string, 0)
	for _, ip := range ips {
		// Prefer IPv4 for proxy edge performance
		if ip4 := ip.To4(); ip4 != nil {
			validIPs = append(validIPs, ip4.String())
		}
	}
	if len(validIPs) == 0 {
		for _, ip := range ips {
			validIPs = append(validIPs, ip.String())
		}
	}

	dm.mu.Lock()
	dm.cache[domain] = &DNSEntry{
		IPs:       validIPs,
		ExpiresAt: time.Now().Add(dm.ttl),
	}
	dm.mu.Unlock()

	return validIPs[0], nil
}

// PrefetchDomains warms DNS cache for target domains
func (dm *DNSManager) PrefetchDomains(domains []string) {
	for _, d := range domains {
		go func(domain string) {
			_, _ = dm.refresh(domain)
		}(d)
	}
}

func (dm *DNSManager) Close() {
	dm.cancel()
}
