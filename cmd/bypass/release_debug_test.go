//go:build !debug

package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseRejectsDebugStartup(t *testing.T) {
	var output bytes.Buffer
	err := run([]string{"--debug-env", "unused"}, &output, &output)
	if err == nil || !strings.Contains(output.String(), "flag provided but not defined") {
		t.Fatalf("release accepted debug startup: %v %s", err, output.String())
	}
}

func TestReleaseBinaryRejectsDebugStartup(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "bypass")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	output, err := exec.Command(binary, "--debug-env", "unused").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "flag provided but not defined") {
		t.Fatalf("release accepted debug startup: %v %s", err, output)
	}
}
