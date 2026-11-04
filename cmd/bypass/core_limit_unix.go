//go:build darwin || linux

package main

import "golang.org/x/sys/unix"

func disableCoreDumps() error {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &limit); err != nil {
		return err
	}
	limit.Cur = 0
	limit.Max = 0
	return unix.Setrlimit(unix.RLIMIT_CORE, &limit)
}
