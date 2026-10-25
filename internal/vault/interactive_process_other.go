//go:build !linux && !darwin

package vault

import "os/exec"

func configureInteractiveProcess(cmd *exec.Cmd) {}
func killInteractiveProcess(cmd *exec.Cmd)      { _ = cmd.Process.Kill() }
