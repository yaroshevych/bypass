package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type styleSet struct {
	base, accent, muted, lineStyle, active, inactive, errorStyle lipgloss.Style
}

type themeColors struct {
	background, foreground, accent, muted, line, activeForeground, activeBackground, inactiveBackground, error string
}

var defaultStyles = stylesForTheme("macchiato")

func themeNames() []string {
	return []string{"Macchiato", "Mocha", "Frappe", "Latte", "Tokyo Night", "Gruvbox", "Dracula", "Nord", "Vesper"}
}

func stylesForTheme(name string) styleSet {
	c := colorsForTheme(name)
	return styleSet{
		base:       lipgloss.NewStyle().Foreground(lipgloss.Color(c.foreground)).Background(lipgloss.Color(c.background)),
		accent:     lipgloss.NewStyle().Foreground(lipgloss.Color(c.accent)).Bold(true),
		muted:      lipgloss.NewStyle().Foreground(lipgloss.Color(c.muted)),
		lineStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color(c.line)),
		active:     lipgloss.NewStyle().Foreground(lipgloss.Color(c.activeForeground)).Background(lipgloss.Color(c.activeBackground)).Bold(true),
		inactive:   lipgloss.NewStyle().Foreground(lipgloss.Color(c.foreground)).Background(lipgloss.Color(c.inactiveBackground)),
		errorStyle: lipgloss.NewStyle().Foreground(lipgloss.Color(c.error)),
	}
}

func colorsForTheme(name string) themeColors {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mocha":
		return themeColors{"#1e1e2e", "#cdd6f4", "#cba6f7", "#a6adc8", "#585b70", "#1e1e2e", "#cdd6f4", "#45475a", "#f38ba8"}
	case "frappe":
		return themeColors{"#303446", "#c6d0f5", "#ca9ee6", "#a5adce", "#626880", "#303446", "#c6d0f5", "#51576d", "#e78284"}
	case "latte":
		return themeColors{"#eff1f5", "#4c4f69", "#8839ef", "#6c6f85", "#9ca0b0", "#eff1f5", "#4c4f69", "#dce0e8", "#d20f39"}
	case "tokyo night", "tokyo-night":
		return themeColors{"#1a1b26", "#c0caf5", "#bb9af7", "#9aa5ce", "#565f89", "#1a1b26", "#7aa2f7", "#292e42", "#f7768e"}
	case "gruvbox":
		return themeColors{"#282828", "#ebdbb2", "#fabd2f", "#a89984", "#665c54", "#282828", "#fabd2f", "#3c3836", "#fb4934"}
	case "dracula":
		return themeColors{"#282a36", "#f8f8f2", "#bd93f9", "#a9acb8", "#6272a4", "#282a36", "#bd93f9", "#44475a", "#ff5555"}
	case "nord":
		return themeColors{"#2e3440", "#eceff4", "#88c0d0", "#a7b2c5", "#4c566a", "#2e3440", "#88c0d0", "#3b4252", "#bf616a"}
	case "vesper":
		return themeColors{"#101010", "#d5d1c9", "#ffc799", "#96918a", "#514d46", "#101010", "#ffc799", "#272521", "#ff8080"}
	default: // Macchiato; preserve the original default palette.
		return themeColors{"#24273a", "#cad3f5", "#c6a0f6", "#a5adcb", "#6e738d", "#24273a", "#cad3f5", "#494d64", "#ed8796"}
	}
}

func (m *Model) colors() *styleSet { return &m.styles }

func (m *Model) themedRule(width int) string {
	return m.colors().rule(width)
}

func (s *styleSet) rule(width int) string {
	return s.lineStyle.Render(strings.Repeat("─", max(0, width)))
}
