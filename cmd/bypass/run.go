package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"bypass/internal/platform"
	runpkg "bypass/internal/run"
	wipebuf "bypass/internal/secret"
)

type envFiles []string

func (f *envFiles) String() string     { return strings.Join(*f, ",") }
func (f *envFiles) Set(s string) error { *f = append(*f, s); return nil }

type identityFlag struct {
	value string
	set   bool
}

func (f *identityFlag) String() string { return "" }
func (f *identityFlag) Set(value string) error {
	f.value = value
	f.set = true
	return nil
}

func selectedIdentity(option identityFlag, lookup func(string) (string, bool)) []byte {
	if option.set {
		return []byte(option.value)
	}
	if value, ok := lookup("BYPASS_IDENTITY_TOKEN"); ok {
		return []byte(value)
	}
	if value, ok := lookup("BYPASS_IDENTITY"); ok {
		return []byte(value)
	}
	return nil
}

func runCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("bypass run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeoutDefault := 60 * time.Second
	if paths, err := platform.Resolve(); err == nil {
		settings, err := platform.LoadSettings(paths)
		if err != nil {
			return err
		}
		if settings.RunTimeout > 0 {
			timeoutDefault = settings.RunTimeout
		}
	}
	timeout := flags.Duration("timeout", timeoutDefault, "IPC approval timeout (positive duration)")
	var identityOption identityFlag
	flags.Var(&identityOption, "identity-token", "agent identity bearer (prefer BYPASS_IDENTITY_TOKEN)")
	flags.Var(&identityOption, "identity", "deprecated alias for --identity-token")
	var files envFiles
	flags.Var(&files, "env-file", "environment file; repeatable, later files override earlier files")
	var help bytes.Buffer
	flags.Usage = func() {
		fmt.Fprintln(&help, `Usage: bypass run [OPTIONS] -- COMMAND [ARGS...]

Run COMMAND with approved secrets from bypass:// references in
environment variables or env files.

Secrets require user approval, either previously granted or requested
interactively in the Bypass app.

Options go before --. COMMAND and its arguments go after --.

Options:
  --env-file FILE          Read environment file; repeatable, later files win
  --identity-token TOKEN   Agent identity credential; identifies an agent
                           across requests. Prefer BYPASS_IDENTITY_TOKEN
  --timeout DURATION       Connection, approval and secret delivery deadline
                           (default: 60s or config; excludes command runtime)
  --help                   Show this help

Examples:
  bypass run --env-file .env.deploy -- ./scripts/deploy.sh --target staging
  bypass run --timeout 2m -- npm test
  API_TOKEN=bypass://GitHub bypass run -- ./scripts/publish.sh`)
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			_, _ = io.Copy(stdout, &help)
			return nil
		}
		return fmt.Errorf("invalid option or value; try 'bypass run --help'")
	}
	command := flags.Args()
	parsed := len(args) - len(command)
	separator := parsed > 0 && args[parsed-1] == "--"
	if len(command) == 0 {
		return fmt.Errorf("missing command; usage: bypass run -- COMMAND [ARGS...]")
	}
	if !separator {
		return fmt.Errorf("missing '--' before command; try 'bypass run --help'")
	}
	if *timeout <= 0 {
		return fmt.Errorf("--timeout must be positive; try 'bypass run --help'")
	}
	identityBytes := selectedIdentity(identityOption, os.LookupEnv)
	defer wipebuf.Wipe(identityBytes)
	if len(identityBytes) > 0 {
		_, err := runpkg.ParseIdentity(identityBytes)
		if err != nil {
			return fmt.Errorf("invalid run identity; use NAME- followed by at least 32 hexadecimal characters")
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cwd, err = runpkg.PlainPath(cwd)
	if err != nil {
		return err
	}
	executable, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("command unavailable: %w", err)
	}
	executable, err = runpkg.PlainPath(executable)
	if err != nil {
		return err
	}
	command[0] = executable
	if len(command) > 1 && !strings.HasPrefix(command[1], "-") {
		name := strings.ToLower(filepath.Base(executable))
		interpreter := false
		for _, prefix := range []string{"sh", "bash", "zsh", "dash", "fish", "python", "node", "ruby", "perl", "php", "deno", "bun", "lua", "pwsh"} {
			if strings.HasPrefix(name, prefix) {
				interpreter = true
			}
		}
		if interpreter {
			script, err := runpkg.PlainPath(command[1])
			if err != nil {
				return err
			}
			command[1] = script
		}
	}

	var readers []io.Reader
	for _, path := range files {
		path, err = runpkg.PlainPath(path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		readers = append(readers, file)
	}
	environment, err := runpkg.Environment(os.Environ(), readers...)
	if err != nil {
		return err
	}
	// BYPASS_IDENTITY configures the runner and must never flow into a child.
	// It is also reserved from env-file substitution, regardless of references.
	delete(environment, "BYPASS_IDENTITY")
	delete(environment, "BYPASS_IDENTITY_TOKEN")
	refs := map[string]bool{}
	for _, value := range environment {
		_, err := runpkg.Substitute(value, func(r runpkg.Reference) (string, error) { return "", nil })
		if err != nil {
			return err
		}
		if strings.HasPrefix(value, "bypass://") {
			refs[value] = true
		} else {
			for {
				start := strings.Index(value, "${bypass://")
				if start < 0 {
					break
				}
				value = value[start+2:]
				end := strings.IndexByte(value, '}')
				if end < 0 {
					break
				}
				refs[value[:end]] = true
				value = value[end+1:]
			}
		}
	}
	if len(refs) > 0 {
		list := make([]string, 0, len(refs))
		for ref := range refs {
			list = append(list, ref)
		}
		sort.Strings(list)
		path, err := runpkg.SocketPath()
		if err != nil {
			return err
		}
		widgetPath, err := runpkg.WidgetSocketPath()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		response, err := runpkg.ExchangeAny(ctx, []string{path, widgetPath}, runpkg.Request{Command: command, Directory: cwd, References: list, Identity: identityBytes})
		if err != nil {
			return err
		}
		defer func() {
			for _, value := range response.Values {
				wipebuf.Wipe(value)
			}
		}()
		resolvedEnvironment := make(map[string][]byte, len(environment))
		defer func() {
			for _, value := range resolvedEnvironment {
				wipebuf.Wipe(value)
			}
		}()
		for key, value := range environment {
			if !strings.HasPrefix(value, "bypass://") && !strings.Contains(value, "${bypass://") {
				continue
			}
			source := []byte(value)
			resolved, err := runpkg.SubstituteBytes(source, func(r runpkg.Reference) ([]byte, error) {
				for _, ref := range list {
					candidate, _ := runpkg.ParseReference(ref)
					if equalReference(r, candidate) {
						v, ok := response.Values[ref]
						if !ok {
							return nil, fmt.Errorf("incomplete secret response")
						}
						return v, nil
					}
				}
				return nil, fmt.Errorf("missing secret response")
			})
			wipebuf.Wipe(source)
			if err != nil {
				return err
			}
			resolvedEnvironment[key] = resolved
		}
		env := make([]string, 0, len(environment))
		for key, value := range environment {
			if resolved, ok := resolvedEnvironment[key]; ok {
				// syscall.Exec requires immutable strings; this is the final Go-level
				// plaintext boundary before the kernel copies the environment.
				assignment := make([]byte, len(key)+1+len(resolved))
				copy(assignment, key)
				assignment[len(key)] = '='
				copy(assignment[len(key)+1:], resolved)
				env = append(env, string(assignment))
				wipebuf.Wipe(assignment)
			} else {
				env = append(env, key+"="+value)
			}
		}
		for _, value := range response.Values {
			wipebuf.Wipe(value)
		}
		for _, value := range resolvedEnvironment {
			wipebuf.Wipe(value)
		}
		wipebuf.Wipe(identityBytes)
		return syscall.Exec(executable, command, env)
	}
	env := make([]string, 0, len(environment))
	for key, value := range environment {
		env = append(env, key+"="+value)
	}
	wipebuf.Wipe(identityBytes)
	// Replacing the client preserves the terminal, process group, signals and exit
	// status. The IPC deadline never limits the launched process.
	return syscall.Exec(executable, command, env)
}
func equalReference(a, b runpkg.Reference) bool {
	if a.Absolute != b.Absolute || a.Field != b.Field || len(a.Components) != len(b.Components) {
		return false
	}
	for i := range a.Components {
		if a.Components[i] != b.Components[i] {
			return false
		}
	}
	return true
}
