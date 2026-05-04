package singbox

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SingboxManager struct {
	Bin        string
	ConfigPath string
	LogPath    string
	mu         sync.Mutex
	cmd        *exec.Cmd
}

func NewSingboxManager(bin, configPath, logPath string) *SingboxManager {
	return &SingboxManager{Bin: bin, ConfigPath: configPath, LogPath: logPath}
}

func (m *SingboxManager) writeLog(msg string) {
	if m.LogPath == "" {
		return
	}
	f, _ := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if f != nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("%s [smatrix] %s\n", time.Now().Format(time.RFC3339), msg))
	}
}

func (m *SingboxManager) Status() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *SingboxManager) statusLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	// Check if process is actually alive
	err := m.cmd.Process.Signal(syscall.Signal(0))
	if err != nil {
		// Process is dead, clean up
		m.cmd = nil
		return false
	}
	return true
}

func (m *SingboxManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Kill stray sing-box processes not tracked by us
	m.killStrayLocked()

	// Already running?
	if m.statusLocked() {
		return nil
	}

	if _, err := exec.LookPath(m.Bin); err != nil {
		return err
	}
	// Validate config
	if out, err := exec.Command(m.Bin, "check", "-c", m.ConfigPath).CombinedOutput(); err != nil {
		errMsg := fmt.Sprintf("blocked: %s", out)
		m.writeLog(errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Kill any sing-box process lingering on ports our config needs
	m.killConfigPortsLocked()

	cmd := exec.Command(m.Bin, "run", "-c", m.ConfigPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		m.writeLog(fmt.Sprintf("start failed: %v", err))
		return err
	}
	m.cmd = cmd
	m.writeLog(fmt.Sprintf("started pid=%d", cmd.Process.Pid))
	return nil
}

func (m *SingboxManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	return nil
}

func (m *SingboxManager) stopLocked() {
	if m.cmd == nil || m.cmd.Process == nil {
		m.killStrayLocked()
		return
	}
	pid := m.cmd.Process.Pid
	// Kill entire process group
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	// Check if dead
	if m.cmd.Process.Signal(syscall.Signal(0)) == nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		time.Sleep(200 * time.Millisecond)
	}
	m.cmd = nil
	m.writeLog(fmt.Sprintf("stopped pid=%d", pid))
	m.killStrayLocked()
}

func (m *SingboxManager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	time.Sleep(300 * time.Millisecond)
	return m.startLocked()
}

func (m *SingboxManager) startLocked() error {
	m.killStrayLocked()
	if m.statusLocked() {
		return nil
	}
	if _, err := exec.LookPath(m.Bin); err != nil {
		return err
	}
	if out, err := exec.Command(m.Bin, "check", "-c", m.ConfigPath).CombinedOutput(); err != nil {
		errMsg := fmt.Sprintf("blocked: %s", out)
		m.writeLog(errMsg)
		return fmt.Errorf("%s", errMsg)
	}
	m.killConfigPortsLocked()
	cmd := exec.Command(m.Bin, "run", "-c", m.ConfigPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		m.writeLog(fmt.Sprintf("start failed: %v", err))
		return err
	}
	m.cmd = cmd
	m.writeLog(fmt.Sprintf("started pid=%d", cmd.Process.Pid))
	return nil
}

func (m *SingboxManager) killStrayLocked() {
	out, err := exec.Command("pgrep", "-f", "sing-box.*run.*-c").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		if m.cmd != nil && m.cmd.Process != nil && m.cmd.Process.Pid == pid {
			continue
		}
		p, _ := os.FindProcess(pid)
		if p != nil {
			_ = p.Signal(syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			_ = p.Signal(syscall.SIGKILL)
			m.writeLog(fmt.Sprintf("killed stray pid=%d", pid))
		}
	}
}

func (m *SingboxManager) killConfigPortsLocked() {
	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return
	}
	var cfg struct {
		Inbounds []struct {
			ListenPort int `json:"listen_port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	for _, ib := range cfg.Inbounds {
		if ib.ListenPort == 0 {
			continue
		}
		// Check if port is occupied
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ib.ListenPort))
		if err != nil {
			// Port in use — find and kill the process
			out, _ := exec.Command("ss", "-tlnp", "sport", "=", fmt.Sprintf("%d", ib.ListenPort)).Output()
			for _, line := range strings.Split(string(out), "\n") {
				var pid int
				if _, err := fmt.Sscanf(line, "pid=%d", &pid); err == nil && pid > 0 {
					if m.cmd != nil && m.cmd.Process != nil && m.cmd.Process.Pid == pid {
						continue
					}
					p, _ := os.FindProcess(pid)
					if p != nil {
						_ = p.Signal(syscall.SIGTERM)
						time.Sleep(100 * time.Millisecond)
						_ = p.Signal(syscall.SIGKILL)
						m.writeLog(fmt.Sprintf("killed port-conflict pid=%d", pid))
					}
				}
			}
		} else {
			_ = ln.Close()
		}
	}
}
