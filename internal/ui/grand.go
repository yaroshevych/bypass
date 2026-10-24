package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Grand View shares reveal's session binding, reference resolution and buffer
// cleanup. It remains visible until dismissed or privacy/lock takes over.
func (m *Model) openGrandView() tea.Cmd {
	if m.selected() == nil || m.editor != nil {
		return nil
	}
	m.hideReveal()
	origin := *m.ws.ActiveView()
	cmd := m.toggleReveal()
	*m.ws.ActiveView() = origin
	if m.revealEntry != "" {
		m.grand = true
	}
	return cmd
}

func (m *Model) grandKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "enter", "G", "q":
		m.hideReveal()
	case "ctrl+c":
		m.Close()
		return tea.Quit
	case "ctrl+k":
		return m.protectScreen()
	case "ctrl+l", "L":
		return m.lockCurrent(k.String() == "L")
	case "ctrl+p":
		m.openPalette("actions", false)
	case "up", "k":
		m.grandOffset = max(0, m.grandOffset-1)
	case "down", "j":
		m.scrollGrand(1)
	case "pgup":
		_, visible, _ := m.grandGeometry()
		m.grandOffset = max(0, m.grandOffset-visible)
	case "pgdown":
		_, visible, _ := m.grandGeometry()
		m.scrollGrand(visible)
	case "home":
		m.grandOffset = 0
	case "end":
		_, visible, total := m.grandGeometry()
		m.grandOffset = max(0, total-visible)
	}
	return nil
}

func (m *Model) grandPointer(msg tea.MouseMsg) tea.Cmd {
	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.grandOffset = max(0, m.grandOffset-1)
		case tea.MouseButtonWheelDown:
			m.scrollGrand(1)
		}
	}
	return nil
}

func (m *Model) scrollGrand(delta int) {
	_, visible, total := m.grandGeometry()
	m.grandOffset = min(max(0, total-visible), m.grandOffset+delta)
}

func (m *Model) grandGeometry() (columns, visible, total int) {
	if m.grandIsText() {
		return max(1, m.width-8), max(1, m.height-10), len(m.grandTextRows())
	}
	runes := []rune(string(m.revealValue))
	defer clear(runes)
	cell := grandCellWidth(runes, m.settings.ASCII)
	columns = max(1, (m.width-8)/cell)
	visible = max(1, (m.height-10)/3)
	total = max(1, (len(runes)+columns-1)/columns)
	return
}

func grandCharacter(r rune, ascii bool) string {
	switch r {
	case ' ':
		if ascii {
			return "[SP]"
		}
		return "␠"
	case '\n':
		return "\\n"
	case '\r':
		return "\\r"
	case '\t':
		return "\\t"
	}
	if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
		return fmt.Sprintf("U+%04X", r)
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return "◌" + string(r)
	}
	return string(r)
}

func grandCellWidth(runes []rune, ascii bool) int {
	width := max(4, len(strconv.Itoa(len(runes)))+2)
	for _, r := range runes {
		width = max(width, lipgloss.Width(grandCharacter(r, ascii))+2)
	}
	return width
}

func (m *Model) grandLines() []string {
	c := m.colors()
	if m.revealCancel != nil {
		return m.grandOverlay([]string{"Resolving field…"}, "Esc close")
	}
	if m.grandIsText() {
		return m.grandTextView()
	}
	runes := []rune(string(m.revealValue))
	defer clear(runes)
	columns, visible, total := m.grandGeometry()
	start := min(m.grandOffset, max(0, total-visible))
	cell := grandCellWidth(runes, m.settings.ASCII)
	body := []string{c.muted.Render(fmt.Sprintf("%s · %d characters · positions start at 1", safe(m.revealField), len(runes))), ""}
	if len(runes) == 0 {
		body = append(body, c.muted.Render("This field is empty."))
	}
	neutralValue := c.accent.Background(c.base.GetBackground())
	neutralIndex := c.muted.Background(c.base.GetBackground())
	stripeValue := c.accent.Background(c.inactive.GetBackground())
	stripeIndex := c.muted.Background(c.inactive.GetBackground())
	groupSize := max(1, m.settings.GrandViewGroupSize)
	for row := start; row < min(total, start+visible) && len(runes) > 0; row++ {
		var values, indexes strings.Builder
		for index := row * columns; index < min(len(runes), (row+1)*columns); index++ {
			value := grandCharacter(runes[index], m.settings.ASCII)
			left := max(0, (cell-lipgloss.Width(value))/2)
			valueStyle, indexStyle := neutralValue, neutralIndex
			if (index/groupSize)%2 == 1 {
				valueStyle, indexStyle = stripeValue, stripeIndex
			}
			values.WriteString(valueStyle.Render(fit(strings.Repeat(" ", left)+value, cell)))
			number := strconv.Itoa(index + 1)
			indexes.WriteString(indexStyle.Render(fit(strings.Repeat(" ", (cell-len(number))/2)+number, cell)))
		}
		body = append(body, values.String(), indexes.String(), "")
	}
	footer := "Esc / Enter close · ↑↓ scroll · PgUp/PgDn"
	if total > visible {
		footer = fmt.Sprintf("rows %d–%d/%d · Esc close · ↑↓ / PgUp/PgDn", start+1, min(total, start+visible), total)
	}
	return m.grandOverlay(body, footer)
}

func (m *Model) grandOverlay(body []string, footer string) []string {
	width := max(lipgloss.Width("Grand View"), lipgloss.Width(footer))
	for _, line := range body {
		width = max(width, lipgloss.Width(line))
	}
	// Include the borders and one padding cell on each side. The shared
	// overlay centers the result and keeps it within the terminal.
	return m.overlay("Grand View", body, footer, min(m.width-4, width+4), -1, -1)
}

func (m *Model) grandIsText() bool {
	return m.revealField == "Notes" || strings.ContainsAny(string(m.revealValue), "\r\n")
}

func (m *Model) grandTextRows() []string {
	text := strings.ReplaceAll(string(m.revealValue), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var rows []string
	for _, paragraph := range strings.Split(text, "\n") {
		paragraph = safe(strings.ReplaceAll(paragraph, "\t", "    "))
		rows = append(rows, strings.Split(ansi.Wrap(paragraph, max(1, m.width-8), ""), "\n")...)
	}
	return rows
}

func (m *Model) grandTextView() []string {
	c := m.colors()
	rows := m.grandTextRows()
	visible := max(1, m.height-10)
	start := min(m.grandOffset, max(0, len(rows)-visible))
	body := []string{c.muted.Render(safe(m.revealField)), ""}
	if len(m.revealValue) == 0 {
		body = append(body, c.muted.Render("This field is empty."))
	} else {
		for _, row := range rows[start:min(len(rows), start+visible)] {
			body = append(body, c.base.Render(row))
		}
	}
	footer := "q / Esc close · ↑↓ scroll · PgUp/PgDn"
	if len(rows) > visible {
		footer = fmt.Sprintf("lines %d–%d/%d · q / Esc close · ↑↓ scroll", start+1, min(len(rows), start+visible), len(rows))
	}
	return m.grandOverlay(body, footer)
}
