package run

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strconv"
)

func fmtPID(pid int) string { return strconv.Itoa(pid) }
func executableHash(path string) ([32]byte, error) {
	var result [32]byte
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return result, err
	}
	copy(result[:], h.Sum(nil))
	return result, nil
}
func sameExecutable(peer string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	a, err := executableHash(self)
	if err != nil {
		return err
	}
	b, err := executableHash(peer)
	if err != nil {
		return errors.New("cannot verify IPC executable")
	}
	if a != b {
		return errors.New("IPC executable mismatch")
	}
	return nil
}
