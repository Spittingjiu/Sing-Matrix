package singbox

import (
	"errors"
	"os"
	"os/exec"
	"sync"
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

func (m *SingboxManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusLocked() {
		return nil
	}
	if _, err := exec.LookPath(m.Bin); err != nil {
		return err
	}
	logFile, err := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	cmd := exec.Command(m.Bin, "run", "-c", m.ConfigPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	m.cmd = cmd
	m.logFile = logFile
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.cmd == cmd {
			m.cmd = nil
		}
		if m.logFile == logFile {
			_ = logFile.Close()
			m.logFile = nil
		}
	}()
	return nil
}

func (m *SingboxManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	if err := m.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = m.cmd.Process.Kill()
	}
	m.cmd = nil
	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
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
	return m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil
}
