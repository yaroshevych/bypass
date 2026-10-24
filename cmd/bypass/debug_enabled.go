//go:build debug

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"bypass/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

type debugModel struct {
	*ui.Model
	open tea.Cmd
}

func (m debugModel) Init() tea.Cmd { return tea.Batch(m.Model.Init(), m.open) }

func configureDebug(flags *flag.FlagSet) func(*ui.Model) (tea.Model, error) {
	envPath := flags.String("debug-env", "", "debug only: unlock using a local dotenv file")
	return func(m *ui.Model) (tea.Model, error) {
		if *envPath == "" {
			return m, nil
		}
		values, err := readDebugEnv(*envPath)
		if err != nil {
			return nil, err
		}
		path := values["BYPASS_DEBUG_VAULT"]
		password, hasPassword := values["BYPASS_DEBUG_PASSWORD"]
		if path == "" || !hasPassword {
			return nil, fmt.Errorf("debug env requires BYPASS_DEBUG_VAULT and BYPASS_DEBUG_PASSWORD")
		}
		resolve := func(value string) string {
			if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "~/") {
				return value
			}
			return filepath.Join(filepath.Dir(*envPath), value)
		}
		return debugModel{Model: m, open: m.DebugOpen(resolve(path), password, resolve(values["BYPASS_DEBUG_KEY_FILE"]))}, nil
	}
}

// Deliberately accepts literal values only: no shell execution or expansion.
func readDebugEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open debug env file")
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("invalid debug env assignment at line %d", line)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			value, err = strconv.Unquote(value)
			if err != nil {
				return nil, fmt.Errorf("invalid debug env quoting at line %d", line)
			}
		} else if strings.HasPrefix(value, "'") {
			if len(value) < 2 || !strings.HasSuffix(value, "'") {
				return nil, fmt.Errorf("invalid debug env quoting at line %d", line)
			}
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("could not read debug env file")
	}
	return values, nil
}
