//go:build !debug

package main

import (
	"bypass/internal/ui"
	"flag"
	tea "github.com/charmbracelet/bubbletea"
)

func configureDebug(_ *flag.FlagSet) func(*ui.Model) (tea.Model, error) {
	return func(m *ui.Model) (tea.Model, error) { return m, nil }
}
