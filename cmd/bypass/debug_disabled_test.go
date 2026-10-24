//go:build !debug

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNormalBuildRejectsDebugCredentials(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--debug-env", ".env"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatal("normal build accepted debug credential loading")
	}
}
