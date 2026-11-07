//go:build linux

package platform

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// WaitForOmarchyThemeChange waits for changes under Omarchy's active theme.
// It watches the stable parent as well because Omarchy replaces the theme dir.
func WaitForOmarchyThemeChange(ctx context.Context) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	current := filepath.Join(home, ".local", "state", "omarchy", "current")
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	mask := uint32(unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_ATTRIB | unix.IN_CLOSE_WRITE)
	_, parentErr := unix.InotifyAddWatch(fd, current, mask)
	_, themeErr := unix.InotifyAddWatch(fd, filepath.Join(current, "theme"), mask)
	if parentErr != nil && themeErr != nil {
		return false
	}
	buffer := make([]byte, 4096)
	var changedAt time.Time
	for {
		if err := ctx.Err(); err != nil {
			return false
		}
		timeout := 100
		if !changedAt.IsZero() {
			remaining := time.Until(changedAt.Add(150 * time.Millisecond))
			if remaining <= 0 {
				return true
			}
			timeout = int(remaining.Milliseconds())
			if timeout < 1 {
				timeout = 1
			}
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err := unix.Poll(fds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false
		}
		if fds[0].Revents&unix.POLLIN != 0 {
			if _, err := unix.Read(fd, buffer); err != nil && err != unix.EAGAIN {
				return false
			}
			changedAt = time.Now()
		}
	}
}
