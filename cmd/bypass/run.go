package main

import (
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
)

type envFiles []string

func (f *envFiles) String() string     { return strings.Join(*f, ",") }
func (f *envFiles) Set(s string) error { *f = append(*f, s); return nil }
func runCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("bypass run", flag.ContinueOnError)
	flags.SetOutput(stderr)
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
	var files envFiles
	flags.Var(&files, "env-file", "environment file; repeatable, later files override earlier files")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: bypass run [--env-file FILE] [--timeout 60s] -- COMMAND [ARGS...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	separator := false
	for _, arg := range args {
		if arg == "--" {
			separator = true
			break
		}
	}
	if !separator {
		return fmt.Errorf("separate the command with --")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	command := flags.Args()
	if len(command) == 0 {
		return fmt.Errorf("command is required after --")
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
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		response, err := runpkg.Exchange(ctx, path, runpkg.Request{Command: command, Directory: cwd, References: list})
		if err != nil {
			return err
		}
		for key, value := range environment {
			resolved, err := runpkg.Substitute(value, func(r runpkg.Reference) (string, error) {
				for _, ref := range list {
					candidate, _ := runpkg.ParseReference(ref)
					if equalReference(r, candidate) {
						v, ok := response.Values[ref]
						if !ok {
							return "", fmt.Errorf("incomplete secret response")
						}
						return v, nil
					}
				}
				return "", fmt.Errorf("missing secret response")
			})
			if err != nil {
				return err
			}
			environment[key] = resolved
		}
	}
	env := make([]string, 0, len(environment))
	for key, value := range environment {
		env = append(env, key+"="+value)
	}
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
