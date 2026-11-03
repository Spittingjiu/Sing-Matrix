package ortt

import (
	"math"
	"net"
	"sort"
	"testing"
	"time"
)

// TestBenchmarkHighRTTDialVsPool demonstrates and measures the physical latency difference
// between cold synchronous TCP dial (1x RTT) vs Upstream Socket Pool handoff in high-RTT cross-region routes.
func TestBenchmarkHighRTTDialVsPool(t *testing.T) {
	// Set up artificial 80ms RTT cross-region delay simulator server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				// Simulate 40ms inbound + 40ms outbound = 80ms RTT
				buf := make([]byte, 1024)
				_, _ = conn.Read(buf)
				time.Sleep(40 * time.Millisecond)
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
			}(c)
		}
	}()

	targetAddr := ln.Addr().String()

	// 1. Measure Native Dial (100 iterations)
	var directLatencies []float64
	for i := 0; i < 100; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", targetAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("native dial failed: %v", err)
		}
		duration := float64(time.Since(start).Microseconds()) / 1000.0
		directLatencies = append(directLatencies, duration)
		conn.Close()
	}

	// 2. Measure Pool Fetch (100 iterations)
	dnsMgr := NewDNSManager(1 * time.Minute)
	defer dnsMgr.Close()
	pool := NewUpstreamSocketPool(5, 20, dnsMgr)
	defer pool.Close()

	var poolLatencies []float64
	for i := 0; i < 100; i++ {
		// Put warm connection
		c, _ := net.Dial("tcp", targetAddr)
		pool.mu.Lock()
		pool.pools[targetAddr] = append(pool.pools[targetAddr], &warmSocket{
			conn:      c,
			createdAt: time.Now(),
		})
		pool.currentTotal++
		pool.mu.Unlock()

		start := time.Now()
		conn, hit := pool.Get(targetAddr)
		if !hit || conn == nil {
			t.Fatalf("expected pool hit")
		}
		duration := float64(time.Since(start).Microseconds()) / 1000.0
		poolLatencies = append(poolLatencies, duration)
		conn.Close()
	}

	// Compute statistics
	calcStats := func(samples []float64) (mean, p50, p90, p99, stdDev float64) {
		sort.Float64s(samples)
		total := 0.0
		for _, v := range samples {
			total += v
		}
		mean = total / float64(len(samples))
		p50 = samples[len(samples)*50/100]
		p90 = samples[len(samples)*90/100]
		p99 = samples[len(samples)*99/100]

		sumSq := 0.0
		for _, v := range samples {
			sumSq += (v - mean) * (v - mean)
		}
		stdDev = math.Sqrt(sumSq / float64(len(samples)))
		return
	}

	dMean, dP50, dP90, dP99, dStd := calcStats(directLatencies)
	pMean, pP50, pP90, pP99, pStd := calcStats(poolLatencies)

	t.Logf("=== High-RTT (Local Simulated) Benchmark Results (100 samples) ===")
	t.Logf("[Direct Dial]    Mean: %.3fms, p50: %.3fms, p90: %.3fms, p99: %.3fms, StdDev: %.3fms", dMean, dP50, dP90, dP99, dStd)
	t.Logf("[Pool Pop]       Mean: %.3fms, p50: %.3fms, p90: %.3fms, p99: %.3fms, StdDev: %.3fms", pMean, pP50, pP90, pP99, pStd)
	t.Logf("[Net Gain]       Saved: %.3fms (Latency Reduction: %.1f%%)", dMean-pMean, (dMean-pMean)/dMean*100)
}
