//go:build debug

package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// DebugOpen exists only in builds explicitly compiled with -tags debug.
// Use the normal unlock path, including credential cleanup and error handling.
func (m *Model) DebugOpen(path, password, keyFile string) tea.Cmd {
	m.beginUnlock(path)
	m.inputs[0].set(password)
	m.inputs[1].set(keyFile)
	return m.startOpen()
}

func (m *Model) debugAppearanceToggle(key string) bool {
	if key != "D" {
		return false
	}
	if strings.EqualFold(m.settings.Appearance, "compact") {
		m.settings.Appearance = "classic"
	} else {
		m.settings.Appearance = "compact"
	}
	m.normalize()
	return true
}
