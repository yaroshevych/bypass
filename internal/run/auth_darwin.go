//go:build darwin

package run

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func authenticate(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var pid int
	var peer *unix.Xucred
	var failure error
	err = raw.Control(func(fd uintptr) {
		peer, failure = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if failure == nil {
			pid, failure = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		}
	})
	if err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if peer.Uid != uint32(os.Getuid()) {
		return errors.New("IPC peer UID mismatch")
	}
	path, err := peerExecutable(pid)
	if err != nil {
		return errors.New("cannot identify IPC peer")
	}
	return sameExecutable(path)
}

// ps can report the path used to start a process, which may be relative to
// that process's working directory rather than ours.
func peerExecutable(pid int) (string, error) {
	output, err := exec.Command("/bin/ps", "-p", fmtPID(pid), "-o", "comm=").Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", errors.New("empty executable path")
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	output, err = exec.Command("/usr/sbin/lsof", "-a", "-p", fmtPID(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "n/") {
			candidate := filepath.Join(line[1:], path)
			// Do not trust the peer's working directory alone: it can change
			// after launch. Confirm the candidate is its mapped executable.
			mapped, err := exec.Command("/usr/sbin/lsof", "-a", "-p", fmtPID(pid), "-d", "txt", "-Fn").Output()
			if err != nil {
				return "", err
			}
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", err
			}
			for _, field := range strings.Split(string(mapped), "\n") {
				if strings.HasPrefix(field, "n/") {
					if field[1:] != resolved {
						return "", errors.New("IPC executable path changed")
					}
					return candidate, nil
				}
			}
			return "", errors.New("cannot identify mapped IPC executable")
		}
	}
	return "", errors.New("cannot identify IPC peer working directory")
}

func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var failure error
	err = raw.Control(func(fd uintptr) { pid, failure = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID) })
	if err != nil {
		return 0, err
	}
	return pid, failure
}
