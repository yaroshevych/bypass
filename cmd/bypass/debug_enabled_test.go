//go:build debug

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugEnvKeepsCredentialsLiteral(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# local debug\nBYPASS_DEBUG_VAULT='../demo.kdbx'\nexport BYPASS_DEBUG_PASSWORD='$(echo nope) $HOME # literal'\nEMPTY=\nQUOTED=\"space and \\\"quote\\\"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readDebugEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["BYPASS_DEBUG_PASSWORD"] != "$(echo nope) $HOME # literal" || got["QUOTED"] != "space and \"quote\"" {
		t.Fatal("dotenv altered literal credential values")
	}
}

func TestDebugEnvErrorsNeverEchoValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	const secret = "private-sentinel"
	if err := os.WriteFile(path, []byte("BYPASS_DEBUG_PASSWORD=\""+secret), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := readDebugEnv(path)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("invalid credentials must fail without echoing their contents")
	}
}
