package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
		{"version", []string{"--version"}, "bypass dev", false},
		{"help", []string{"--help"}, "Usage:", false},
		{"unknown", []string{"--bad-option"}, "", true},
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

func TestCLIExitAndHelp(t *testing.T) {
	if os.Getenv("BYPASS_CLI_TEST_HELPER") == "1" {
		if err := run(os.Args[3:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
		fail bool
	}{
		{"root help", []string{"--help"}, "Commands:\n  run", false},
		{"help command", []string{"help"}, "bypass vault.kdbx", false},
		{"run help", []string{"help", "run"}, "--identity-token", false},
		{"single dash help", []string{"run", "-help"}, "Usage: bypass run", false},
		{"version", []string{"--version"}, "bypass ", false},
		{"single dash version", []string{"-version"}, "bypass ", false},
		{"missing command", []string{"run"}, "missing command; usage: bypass run -- COMMAND [ARGS...]", true},
		{"empty command", []string{"run", "--"}, "missing command", true},
		{"missing separator", []string{"run", "npm", "test"}, "missing '--' before command; try 'bypass run --help'", true},
		{"late separator", []string{"run", "npm", "--", "test"}, "missing '--'", true},
		{"unknown root flag", []string{"--bad-option"}, "bypass --help", true},
		{"unknown run flag", []string{"run", "--bad-option"}, "bypass run --help", true},
		{"single dash option", []string{"run", "-timeout", "0s", "--", "x"}, "--timeout must be positive", true},
		{"no references", []string{"run", "--", "/usr/bin/true"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-test.run=TestCLIExitAndHelp", "--"}, tc.args...)
			cmd := exec.Command(os.Args[0], args...)
			cmd.Env = []string{"BYPASS_CLI_TEST_HELPER=1", "HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.fail || !strings.Contains(string(out), tc.want) {
				t.Fatalf("output %q, error %v; want %q, failure %v", out, err, tc.want, tc.fail)
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
