//go:build linux || darwin

package vault

import (
	"os/exec"
	"syscall"
)

func configureInteractiveProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killInteractiveProcess(cmd *exec.Cmd) {
	// Include wrapper descendants, even when they retain inherited pipes.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
