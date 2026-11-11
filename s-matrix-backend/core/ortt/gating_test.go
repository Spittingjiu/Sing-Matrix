package ortt

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLoadSecret(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SMATRIX_DATA_DIR", dir)
	t.Setenv("ORTT_SECRET", "")
	t.Setenv("SMATRIX_ACCEL_SECRET", "")

	if got := LoadSecret(); got != "" {
		t.Fatalf("expected no secret, got %q", got)
	}

	// Environment variable wins.
	t.Setenv("ORTT_SECRET", "env-secret-123")
	if got := LoadSecret(); got != "env-secret-123" {
		t.Fatalf("expected env secret, got %q", got)
	}
	t.Setenv("ORTT_SECRET", "")

	// Legacy variable is honored.
	t.Setenv("SMATRIX_ACCEL_SECRET", "legacy-secret-456")
	if got := LoadSecret(); got != "legacy-secret-456" {
		t.Fatalf("expected legacy secret, got %q", got)
	}
	t.Setenv("SMATRIX_ACCEL_SECRET", "")

	// Env file in the data dir is honored.
	envPath := filepath.Join(dir, "env")
	if err := os.WriteFile(envPath, []byte("SMATRIX_JWT_SECRET=x\nORTT_SECRET=file-secret-789\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecret(); got != "file-secret-789" {
		t.Fatalf("expected file secret, got %q", got)
	}

	// An empty ORTT_SECRET line is skipped; a later real one wins.
	if err := os.WriteFile(envPath, []byte("ORTT_SECRET=\nORTT_SECRET=real-secret-000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecret(); got != "real-secret-000" {
		t.Fatalf("expected real secret after empty line, got %q", got)
	}
}

func startTestListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func closedTestPort(t *testing.T) string {
	t.Helper()
	return strconv.Itoa(freePort(t))
}

func TestEffectiveEnabled(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name   string
		ortt   string
		accel  string
		secret string
		listen bool
		want   bool
	}{
		{"enabled with secret", "true", "", "s3cr3t", true, true},
		{"enabled without secret", "true", "", "", true, false},
		{"disabled", "false", "", "s3cr3t", true, false},
		{"accel false suppresses route", "true", "false", "s3cr3t", true, false},
		{"default off", "", "", "s3cr3t", true, false},
		{"default off without secret", "", "", "", true, false},
		{"legacy flag enables with secret", "", "true", "s3cr3t", true, true},
		{"legacy flag alone without secret", "", "true", "", true, false},
		{"engine down with secret", "true", "", "s3cr3t", false, false},
		{"engine down legacy flag", "", "true", "s3cr3t", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SMATRIX_DATA_DIR", dir)
			t.Setenv("ORTT_ENABLED", tc.ortt)
			t.Setenv("SMATRIX_ACCEL_ENABLED", tc.accel)
			t.Setenv("ORTT_SECRET", tc.secret)
			t.Setenv("SMATRIX_ACCEL_SECRET", "")
			if tc.listen {
				t.Setenv("ORTT_FORWARD_PORT", startTestListener(t))
			} else {
				t.Setenv("ORTT_FORWARD_PORT", closedTestPort(t))
			}
			if got := EffectiveEnabled(); got != tc.want {
				t.Fatalf("EffectiveEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestForwarderListening(t *testing.T) {
	t.Setenv("ORTT_FORWARD_PORT", startTestListener(t))
	if !ForwarderListening() {
		t.Fatal("expected listening with a live listener")
	}
	t.Setenv("ORTT_FORWARD_PORT", closedTestPort(t))
	if ForwarderListening() {
		t.Fatal("expected not listening with a closed port")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestServerStartsOnCustomPorts proves the engine starts and the forwarder
// listens when a secret is configured (the state install.sh must produce
// for a fresh install). Custom ports keep the test off production 39998.
func TestServerStartsOnCustomPorts(t *testing.T) {
	fwdPort := freePort(t)
	ctlPort := freePort(t)
	cfg := DefaultConfig()
	cfg.ControlPort = ctlPort
	cfg.ForwardPort = fwdPort
	secret := []byte("test-secret-32-bytes-for-test!!")
	cfg.SecretKey = secret
	cfg.KeyRing[1] = secret
	srv := NewServer(cfg)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", fwdPort), 2*time.Second)
	if err != nil {
		t.Fatalf("forwarder not listening: %v", err)
	}
	conn.Close()
	if !srv.forwarder.IsListening() {
		t.Fatal("forwarder IsListening() false")
	}

	// The stats server starts in a goroutine; poll briefly until it answers.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/ortt/stats", ctlPort))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("stats GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status: %d", resp.StatusCode)
	}
}
