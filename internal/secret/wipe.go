// Package secret provides best-effort erasure for owned secret buffers.
package secret

import "runtime"

// Wipe overwrites an owned buffer. It cannot erase copies made elsewhere.
//
//go:noinline
func Wipe[T ~byte | ~rune](value []T) {
	for i := range value {
		value[i] = 0
	}
	runtime.KeepAlive(value)
}
