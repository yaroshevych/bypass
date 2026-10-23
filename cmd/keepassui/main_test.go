package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoninteractiveFlags(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		want    string
		failure bool
	}{
		{"version", []string{"--version"}, "KeePassUI", false},
		{"help", []string{"--help"}, "Usage:", false},
		{"unknown", []string{"--bad-option"}, "flag provided but not defined", true},
		{"too many paths", []string{"one", "two"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(tc.args, &out, &out)
			if (err != nil) != tc.failure {
				t.Fatalf("err=%v", err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("missing %q", tc.want)
			}
		})
	}
}
