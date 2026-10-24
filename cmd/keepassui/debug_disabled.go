//go:build !debug

package main

import (
	"flag"
	tea "github.com/charmbracelet/bubbletea"
	"keepassui/internal/ui"
)

func configureDebug(_ *flag.FlagSet) func(*ui.Model) (tea.Model, error) {
	return func(m *ui.Model) (tea.Model, error) { return m, nil }
}
