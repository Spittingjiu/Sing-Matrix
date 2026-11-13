//go:build windows

package singbox

import (
	"os"
	"os/exec"
)

func isProcessAlive(p *os.Process) bool {
	return p != nil
}

func configureDaemonCmd(_ *exec.Cmd) {}

func terminateProcessGroup(p *os.Process) {
	if p != nil {
		_ = p.Kill()
	}
}

func terminateSingleProcess(p *os.Process) {
	if p != nil {
		_ = p.Kill()
	}
}
