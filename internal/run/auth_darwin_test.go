//go:build darwin

package run

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPeerExecutableWithRelativeLaunchPath(t *testing.T) {
	if os.Getenv("BYPASS_TEST_PEER_HELPER") == "1" {
		_, _ = os.Stdout.WriteString("ready\n")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dir, "peer")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("./peer", "-test.run=^TestPeerExecutableWithRelativeLaunchPath$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BYPASS_TEST_PEER_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("peer did not start: %q: %v", line, err)
	}
	reported, err := exec.Command("/bin/ps", "-p", fmtPID(cmd.Process.Pid), "-o", "comm=").Output()
	if err != nil || filepath.IsAbs(strings.TrimSpace(string(reported))) {
		t.Fatalf("fixture did not launch with a relative path: %q: %v", reported, err)
	}
	path, err := peerExecutable(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameExecutable(path); err != nil {
		t.Fatalf("could not authenticate peer launched from another directory: %v", err)
	}
}
