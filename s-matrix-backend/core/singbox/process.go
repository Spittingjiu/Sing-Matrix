package singbox

import (
	"fmt"
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
	logFile    *os.File
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

func (m *SingboxManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	// Validate before start
	if out, err := exec.Command(m.Bin, "check", "-c", m.ConfigPath).CombinedOutput(); err != nil {
		errMsg := fmt.Sprintf("blocked: config check failed: %s", string(out))
		m.writeLog(errMsg)
		return fmt.Errorf("%s", errMsg)
	}
	logFile, err := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	cmd := exec.Command(m.Bin, "run", "-c", m.ConfigPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	m.cmd = cmd
	m.logFile = logFile
	m.writeLog(fmt.Sprintf("started pid=%d", cmd.Process.Pid))
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
		}
		if m.logFile == logFile {
			_ = logFile.Close()
			m.logFile = nil
		}
		m.mu.Unlock()
		if err != nil {
			m.writeLog(fmt.Sprintf("exited: %v", err))
		}
	}()
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
	pgid := m.cmd.Process.Pid
	// Kill process group
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = m.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
	}
	m.cmd = nil
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	m.writeLog(fmt.Sprintf("stopped pid=%d", pgid))
	// Brief wait for port release
	time.Sleep(300 * time.Millisecond)
	m.killStrayLocked()
}

func (m *SingboxManager) Restart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	return m.startLocked()
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
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil || pid <= 0 {
			continue
		}
		if pid == os.Getpid() {
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

func (m *SingboxManager) Status() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *SingboxManager) statusLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	if m.cmd.ProcessState != nil {
		return false
	}
	return m.cmd.Process.Signal(os.Signal(nil)) == nil
}
