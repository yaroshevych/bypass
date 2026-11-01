// Package glyphs chooses compact control edges for the terminal color profile.
package glyphs

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func Left() string  { return edge("\ue0b6") }
func Right() string { return edge("\ue0b4") }
func edge(rounded string) string {
	switch lipgloss.ColorProfile() {
	case termenv.ANSI256, termenv.ANSI:
		return "█"
	default:
		return rounded
	}
}
