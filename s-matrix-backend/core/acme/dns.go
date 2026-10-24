package acme

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Zone is a Cloudflare zone reachable with the configured token.
type Zone struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// DNSRecord is the subset of a Cloudflare record the panel cares about.
type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// DNS talks to the Cloudflare v4 API with a single API token.
type DNS struct {
	Token  string
	Client *http.Client
	zones  []Zone
}

func NewDNS(token string) *DNS {
	return &DNS{Token: strings.TrimSpace(token), Client: &http.Client{Timeout: 20 * time.Second}}
}

func (d *DNS) call(method, path string, body interface{}) (*cfEnvelope, error) {
	if d.Token == "" {
		return nil, errors.New("cloudflare token is not configured")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, "https://api.cloudflare.com/client/v4"+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var env cfEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("cloudflare returned non-json (status %d)", resp.StatusCode)
	}
	if !env.Success {
		msg := "unknown error"
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return &env, fmt.Errorf("cloudflare: %s", msg)
	}
	return &env, nil
}

// Zones lists zones visible to the token (cached for the process lifetime).
func (d *DNS) Zones() ([]Zone, error) {
	if len(d.zones) > 0 {
		return d.zones, nil
	}
	var all []Zone
	for page := 1; page <= 5; page++ {
		env, err := d.call(http.MethodGet, fmt.Sprintf("/zones?per_page=50&page=%d", page), nil)
		if err != nil {
			return nil, err
		}
		var batch []Zone
		if err := json.Unmarshal(env.Result, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 50 {
			break
		}
	}
	if len(all) == 0 {
		return nil, errors.New("token cannot see any zone")
	}
	sort.Slice(all, func(i, j int) bool { return len(all[i].Name) > len(all[j].Name) })
	d.zones = all
	return all, nil
}

// FindZone returns the most specific zone that contains fqdn.
func (d *DNS) FindZone(fqdn string) (Zone, error) {
	zones, err := d.Zones()
	if err != nil {
		return Zone{}, err
	}
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	for _, z := range zones {
		if fqdn == z.Name || strings.HasSuffix(fqdn, "."+z.Name) {
			return z, nil
		}
	}
	return Zone{}, fmt.Errorf("no managed zone matches %s", fqdn)
}

// EnsureARecord makes fqdn resolve to ip as a DNS-only A record. Existing
// records are updated in place so repeated clicks are idempotent.
func (d *DNS) EnsureARecord(fqdn, ip string) (DNSRecord, bool, error) {
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	if net.ParseIP(ip) == nil {
		return DNSRecord{}, false, fmt.Errorf("invalid ip: %s", ip)
	}
	zone, err := d.FindZone(fqdn)
	if err != nil {
		return DNSRecord{}, false, err
	}
	env, err := d.call(http.MethodGet, fmt.Sprintf("/zones/%s/dns_records?name=%s&per_page=50", zone.ID, fqdn), nil)
	if err != nil {
		return DNSRecord{}, false, err
	}
	var existing []DNSRecord
	if err := json.Unmarshal(env.Result, &existing); err != nil {
		return DNSRecord{}, false, err
	}
	payload := map[string]interface{}{"type": "A", "name": fqdn, "content": ip, "ttl": 1, "proxied": false}
	for _, rec := range existing {
		if rec.Type != "A" {
			continue
		}
		if rec.Content == ip && !rec.Proxied {
			return rec, false, nil
		}
		env, err := d.call(http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", zone.ID, rec.ID), payload)
		if err != nil {
			return DNSRecord{}, false, err
		}
		var updated DNSRecord
		_ = json.Unmarshal(env.Result, &updated)
		return updated, true, nil
	}
	env, err = d.call(http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zone.ID), payload)
	if err != nil {
		return DNSRecord{}, false, err
	}
	var created DNSRecord
	if err := json.Unmarshal(env.Result, &created); err != nil {
		return DNSRecord{}, false, err
	}
	return created, true, nil
}

// DetectPublicIP returns the egress IPv4 of this host, falling back to the
// primary interface address.
func DetectPublicIP() string {
	for _, url := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://api.ip.sb/ip"} {
		client := &http.Client{Timeout: 6 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}
