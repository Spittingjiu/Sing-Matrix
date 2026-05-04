package singbox

import (
	"fmt"
	"os"
	"os/exec"
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

func (m *SingboxManager) checkConfig() error {
	cmd := exec.Command(m.Bin, "check", "-c", m.ConfigPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sing-box config invalid: %s", string(out))
	}
	return nil
}

func (m *SingboxManager) writeLog(msg string) {
	if m.LogPath == "" {
		return
	}
	f, err := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(fmt.Sprintf("%s [smatrix] %s\n", time.Now().Format(time.RFC3339), msg))
}

func (m *SingboxManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusLocked() {
		return nil
	}
	if _, err := exec.LookPath(m.Bin); err != nil {
		return err
	}
	// Validate config before starting
	if err := m.checkConfig(); err != nil {
		m.writeLog(fmt.Sprintf("start blocked: %v", err))
		return err
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
		m.writeLog(fmt.Sprintf("start failed: %v", err))
		return err
	}
	m.cmd = cmd
	m.logFile = logFile
	m.writeLog("sing-box started")
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.cmd == cmd {
			m.cmd = nil
		}
		if m.logFile == logFile {
			_ = logFile.Close()
			m.logFile = nil
		}
		var msg string
		if err != nil {
			msg = fmt.Sprintf("sing-box exited with error: %v", err)
		} else {
			msg = "sing-box exited normally"
		}
		m.writeLog(msg)
	}()
	return nil
}

func (m *SingboxManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	// Try graceful shutdown first
	_ = m.cmd.Process.Signal(os.Interrupt)
	// Wait up to 2s for graceful exit
	done := make(chan struct{})
	go func() {
		_ = m.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = m.cmd.Process.Kill()
	}
	m.cmd = nil
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	m.writeLog("sing-box stopped")
	return nil
}

func (m *SingboxManager) Restart() error {
	_ = m.Stop()
	return m.Start()
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
		return false // already exited
	}
	// Verify process is still alive
	err := m.cmd.Process.Signal(os.Signal(nil))
	return err == nil
}
