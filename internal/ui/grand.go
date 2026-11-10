package ui

import (
	wipebuf "bypass/internal/secret"
	dialogrender "bypass/internal/ui/dialog"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Large View shares reveal's session binding, reference resolution and buffer
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
	case "L":
		return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(true) })
	case "ctrl+p":
		m.openPalette("actions")
	case "s":
		m.grandSpaced = !m.grandSpaced
		m.grandOffset = 0
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
	action := ""
	if m.revealCancel == nil {
		lines := m.grandLines()
		if msg.Y >= 0 && msg.Y < len(lines) {
			line := ansi.Strip(lines[msg.Y])
			if strings.Contains(line, "up/down scroll") {
				for _, label := range []string{"s space", "q close"} {
					if start := strings.Index(line, label); start >= 0 {
						x := lipgloss.Width(line[:start])
						if msg.X >= x && msg.X < x+lipgloss.Width(label) {
							action = label
						}
					}
				}
			}
		}
	}
	if msg.Action == tea.MouseActionMotion {
		m.grandFooterHover = action
		return nil
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		switch action {
		case "s space":
			return m.grandKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		case "q close":
			return m.grandKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		}
	}
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
		return max(1, m.width-8), m.grandTextCapacity(), len(m.grandTextRows())
	}
	runes := []rune(string(m.revealValue))
	defer wipebuf.Wipe(runes)
	cell := grandCellWidth(runes, m.settings.ASCII, m.grandSpaced)
	columns = max(1, (m.width-9)/cell)
	visible = max(1, m.grandContentCapacity()/3)
	total = max(1, (len(runes)+columns-1)/columns)
	return
}

func (m *Model) grandTextCapacity() int {
	return max(1, m.grandContentCapacity())
}

func (m *Model) grandContentCapacity() int {
	opts := m.dialogOptions()
	opts.Mode, opts.Width, opts.StatusBar = dialogrender.Overlay, min(m.width-4, 78), "scroll"
	return max(1, dialogrender.ContentCapacity(opts)-2)
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

func grandCellWidth(runes []rune, ascii, spaced bool) int {
	width := 1
	if spaced {
		width = max(4, len(strconv.Itoa(len(runes)))+2)
	}
	for _, r := range runes {
		characterWidth := lipgloss.Width(grandCharacter(r, ascii))
		if spaced {
			characterWidth += 2
		}
		width = max(width, characterWidth)
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
	defer wipebuf.Wipe(runes)
	columns, visible, total := m.grandGeometry()
	start := min(m.grandOffset, max(0, total-visible))
	cell := grandCellWidth(runes, m.settings.ASCII, m.grandSpaced)
	body := []string{c.secondary.Render(" " + fmt.Sprintf("%s · %d characters · positions start at 1", safe(m.revealField), len(runes))), ""}
	if len(runes) == 0 {
		body = append(body, c.secondary.Render("This field is empty."))
	}
	neutralValue := c.accent.Background(c.primary.GetBackground())
	neutralIndex := c.secondary.Background(c.primary.GetBackground())
	stripeValue := c.accent.Background(c.inactive.GetBackground())
	stripeIndex := c.secondary.Background(c.inactive.GetBackground())
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
			number := strconv.Itoa((index + 1) % 10)
			if m.grandSpaced {
				number = strconv.Itoa(index + 1)
			}
			indexes.WriteString(indexStyle.Render(fit(strings.Repeat(" ", (cell-len(number))/2)+number, cell)))
		}
		body = append(body, " "+values.String(), " "+indexes.String(), "")
	}
	footer := "s space · q close · up/down scroll"
	if total > visible {
		footer = fmt.Sprintf("rows %d–%d/%d · s space · q close · up/down scroll", start+1, min(total, start+visible), total)
	}
	if m.grandFooterHover != "" {
		footer = strings.Replace(footer, m.grandFooterHover, m.colors().inactive.Render(m.grandFooterHover), 1)
	}
	return m.grandOverlay(body, footer)
}

func (m *Model) grandOverlay(body []string, footer string) []string {
	width := max(lipgloss.Width("Large View"), lipgloss.Width(footer))
	for _, line := range body {
		width = max(width, lipgloss.Width(line))
	}
	// Include the borders and one padding cell on each side. The shared
	// overlay centers the result and keeps it within the terminal.
	return m.overlay("Large View", body, footer, min(m.width-4, width+4), -1, -1)
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
	visible := m.grandTextCapacity()
	start := min(m.grandOffset, max(0, len(rows)-visible))
	body := []string{c.secondary.Render(safe(m.revealField)), ""}
	if len(m.revealValue) == 0 {
		body = append(body, c.secondary.Render("This field is empty."))
	} else {
		for _, row := range rows[start:min(len(rows), start+visible)] {
			body = append(body, c.primary.Render(row))
		}
	}
	footer := "q close · up/down scroll"
	if len(rows) > visible {
		footer = fmt.Sprintf("lines %d–%d/%d · q close · up/down scroll", start+1, min(len(rows), start+visible), len(rows))
	}
	if m.grandFooterHover != "" {
		footer = strings.Replace(footer, m.grandFooterHover, m.colors().inactive.Render(m.grandFooterHover), 1)
	}
	return m.grandOverlay(body, footer)
}
