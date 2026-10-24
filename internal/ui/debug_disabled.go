//go:build !debug

package ui

func (m *Model) debugAppearanceToggle(string) bool { return false }
