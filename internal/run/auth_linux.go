//go:build linux

package run

import (
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
)

func authenticate(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peer *unix.Ucred
	var failure error
	err = raw.Control(func(fd uintptr) { peer, failure = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if peer.Uid != uint32(os.Getuid()) {
		return errors.New("IPC peer UID mismatch")
	}
	return nil
}

func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var peer *unix.Ucred
	var failure error
	err = raw.Control(func(fd uintptr) { peer, failure = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return 0, err
	}
	if failure != nil {
		return 0, failure
	}
	return int(peer.Pid), nil
}
