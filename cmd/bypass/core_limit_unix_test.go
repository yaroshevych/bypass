//go:build darwin || linux

package main

import (
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDisableCoreDumps(t *testing.T) {
	if os.Getenv("BYPASS_TEST_CORE_LIMIT") == "1" {
		if err := disableCoreDumps(); err != nil {
			t.Fatal(err)
		}
		var limit unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_CORE, &limit); err != nil {
			t.Fatal(err)
		}
		if limit.Cur != 0 {
			t.Fatalf("core limit = %d", limit.Cur)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDisableCoreDumps$")
	cmd.Env = append(os.Environ(), "BYPASS_TEST_CORE_LIMIT=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
}
