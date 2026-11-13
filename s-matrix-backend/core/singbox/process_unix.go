//go:build !windows

package singbox

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func isProcessAlive(p *os.Process) bool {
	if p == nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func configureDaemonCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	pid := p.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	if p.Signal(syscall.Signal(0)) == nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		time.Sleep(200 * time.Millisecond)
	}
}

func terminateSingleProcess(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Signal(syscall.SIGTERM)
	time.Sleep(100 * time.Millisecond)
	_ = p.Signal(syscall.SIGKILL)
}
