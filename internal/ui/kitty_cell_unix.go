//go:build darwin || linux

package ui

import "golang.org/x/sys/unix"

func previewCellSize(fd uintptr) (float64, float64) {
	size, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err == nil && size.Col > 0 && size.Row > 0 && size.Xpixel > 0 && size.Ypixel > 0 {
		return float64(size.Xpixel) / float64(size.Col), float64(size.Ypixel) / float64(size.Row)
	}
	return 1, 2
}
