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
		{"version", []string{"--version"}, "Bypass", false},
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

func TestRunIdentityPrecedence(t *testing.T) {
	lookup := func(string) (string, bool) { return "Env-0123456789abcdef0123456789abcdef", true }
	if got := string(selectedIdentity(identityFlag{}, lookup)); got != "Env-0123456789abcdef0123456789abcdef" {
		t.Fatal("environment identity was not selected")
	}
	if got := string(selectedIdentity(identityFlag{value: "Flag-0123456789abcdef0123456789abcdef", set: true}, lookup)); got != "Flag-0123456789abcdef0123456789abcdef" {
		t.Fatal("flag did not override environment identity")
	}
	if got := selectedIdentity(identityFlag{value: "", set: true}, lookup); len(got) != 0 {
		t.Fatal("explicit empty identity did not select anonymous mode")
	}
}

func TestRunIdentityErrorsAndHelpDoNotEchoCredential(t *testing.T) {
	credential := "Luna-0123456789abcdef0123456789abcdef"
	t.Setenv("BYPASS_IDENTITY", credential)
	for name, args := range map[string][]string{
		"invalid": {"--identity", credential + "g", "--", "/bin/true"},
		"help":    {"--identity", credential, "--help"},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			err := runCommand(args, &output, &output)
			if name == "invalid" && err == nil {
				t.Fatal("accepted malformed identity")
			}
			if name == "help" && err != nil {
				t.Fatal(err)
			}
			if (err != nil && strings.Contains(err.Error(), credential)) || strings.Contains(output.String(), credential) || strings.Contains(output.String(), credential+"g") {
				t.Fatal("identity appeared in CLI output")
			}
		})
	}
}
