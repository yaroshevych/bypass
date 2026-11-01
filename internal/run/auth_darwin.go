//go:build darwin

package run

import (
	"errors"
	"net"
	"os"
	"os/exec"
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
	output, err := exec.Command("/bin/ps", "-p", fmtPID(pid), "-o", "comm=").Output()
	if err != nil {
		return errors.New("cannot identify IPC peer")
	}
	return sameExecutable(strings.TrimSpace(string(output)))
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
