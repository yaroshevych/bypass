package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	base       = lipgloss.NewStyle().Foreground(lipgloss.Color("#cad3f5")).Background(lipgloss.Color("#24273a"))
	accent     = lipgloss.NewStyle().Foreground(lipgloss.Color("#c6a0f6")).Bold(true)
	muted      = lipgloss.NewStyle().Foreground(lipgloss.Color("#a5adcb"))
	lineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e738d"))
	active     = lipgloss.NewStyle().Foreground(lipgloss.Color("#24273a")).Background(lipgloss.Color("#cad3f5")).Bold(true)
	inactive   = lipgloss.NewStyle().Background(lipgloss.Color("#494d64"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ed8796"))
)

// safe prevents vault text or filenames from injecting terminal control sequences.
func safe(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}
func rule(w int) string { return lineStyle.Render(strings.Repeat("─", max(0, w))) }

func (m *Model) View() string {
	w, h := max(1, m.width), max(1, m.height)
	var lines []string
	if w < 65 || h < 16 {
		lines = []string{accent.Render("KeePassUI"), "Terminal too small", muted.Render("Resize to at least 65 × 16"), "Ctrl+C quit"}
	} else if m.help {
		lines = m.helpLines()
	} else if m.screen == chooseFile {
		lines = m.pickerLines()
	} else if m.screen == unlock {
		lines = m.unlockLines()
	} else if m.ws.ActiveSession() == nil {
		lines = m.welcomeLines()
	} else {
		lines = m.workspaceLines()
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = fit(lines[i], w)
	}
	return base.Render(strings.Join(lines, "\n"))
}

func (m *Model) welcomeLines() []string {
	w := m.width
	contentW := min(62, w-8)
	left := max(0, (w-contentW)/2)
	pad := strings.Repeat(" ", left)
	lines := make([]string, max(1, (m.height-18)/2))
	lines = append(lines, pad+accent.Render("        ◇"), "", pad+accent.Render("KeePassUI"), pad+muted.Render("Your vaults. Your keys. Your terminal."), "", pad+active.Render("  Open Vault  ")+muted.Render("  o / Ctrl+O"), "", pad+muted.Render("Open an existing KeePass-compatible .kdbx file."))
	if len(m.recent) > 0 {
		lines = append(lines, "", pad+accent.Render("Recent Vaults"))
		for i, path := range m.recent {
			if i >= min(5, max(1, m.height-len(lines)-5)) {
				break
			}
			text := fit("  "+safe(filepath.Base(path))+"  "+safe(filepath.Dir(path)), contentW)
			if i == m.recentIndex {
				text = inactive.Render(text)
			}
			lines = append(lines, pad+text)
		}
		lines = append(lines, pad+muted.Render("↑↓ select · Enter open · Delete remove recent"))
	}
	if m.notice != "" {
		lines = append(lines, "", pad+muted.Render(m.notice))
	}
	if m.backendMissing {
		lines = append(lines, "", pad+errorStyle.Render("Install KeePassXC or set --backend executable."))
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, rule(w), muted.Render("  o open vault    ? shortcuts    q quit"))
	return lines
}

func (m *Model) paneWidths() (int, int, int) {
	l := min(30, max(18, m.width*23/100))
	r := max(23, m.width*38/100)
	c := m.width - l - r - 2
	return l, c, r
}

func (m *Model) workspaceLines() []string {
	s := m.ws.ActiveSession()
	v := m.ws.ActiveView()
	w := m.width
	name := safe(filepath.Base(s.Path))
	status := "Read only"
	if s.DB == nil {
		status = "Vault locked"
	}
	right := status
	if s.DB != nil {
		right = fmt.Sprintf("Read only  ·  %d entries  ·  %d groups", len(s.DB.Entries), len(s.DB.Groups))
	}
	lines := []string{accent.Render(fit("  "+name, max(0, w-lipgloss.Width(right)-2))) + muted.Render(right), rule(w)}
	var tabs []string
	for i, label := range m.tabLabels() {
		if i == m.ws.Active {
			label = active.Render(label)
		} else {
			label = muted.Render(label)
		}
		tabs = append(tabs, label)
	}
	lines = append(lines, fit(strings.Join(tabs, lineStyle.Render("│")), w), rule(w))
	if s.DB == nil {
		lines = append(lines, "", accent.Render("  Vault locked"), muted.Render("  Enter to unlock with vault credentials."), "", muted.Render("  Alt+1…9 switch vault views · Ctrl+O open another vault"))
		for len(lines) < m.height-2 {
			lines = append(lines, "")
		}
		return append(lines, rule(w), muted.Render("  Enter unlock    Ctrl+W close view    q quit"))
	}
	lw, cw, rw := m.paneWidths()
	bodyH := m.height - 6
	left := m.navLines(lw, bodyH, v.Focus == 0)
	center := m.entryLines(cw, bodyH, v.Focus == 1)
	rightLines := m.detailLines(rw, bodyH, v.Focus == 2)
	for i := 0; i < bodyH; i++ {
		lines = append(lines, fit(left[i], lw)+lineStyle.Render("│")+fit(center[i], cw)+lineStyle.Render("│")+fit(rightLines[i], rw))
	}
	footer := "  Tab panes  / search  Ctrl+O open  Ctrl+T view  Ctrl+W close  ? help"
	if v.Query != "" {
		footer = "  Filter: " + safe(v.Query) + " · Esc clear"
	}
	if m.searching {
		footer = "  Search entries: " + safe(m.query.value()) + "▏  Enter apply · Esc cancel"
	}
	if m.notice != "" {
		footer = "  " + m.notice
	}
	return append(lines, rule(w), muted.Render(fit(footer, w)))
}

func (m *Model) tabLabels() []string {
	var labels []string
	for i, view := range m.ws.Views {
		s := m.ws.Sessions[view.SessionID]
		label := fmt.Sprintf(" %d %s ", i+1, safe(filepath.Base(s.Path)))
		for _, other := range m.ws.Sessions {
			if other.ID != s.ID && filepath.Base(other.Path) == filepath.Base(s.Path) {
				label = fmt.Sprintf(" %d %s (%s) ", i+1, safe(filepath.Base(s.Path)), safe(filepath.Dir(s.Path)))
				break
			}
		}
		if s.DB == nil {
			label += "[locked] "
		}
		labels = append(labels, label)
	}
	return labels
}

func (m *Model) navLines(width, height int, focused bool) []string {
	title := "  Views"
	if focused {
		title = accent.Render(title)
	} else {
		title = muted.Render(title)
	}
	lines := []string{title}
	v := m.ws.ActiveView()
	items := m.navigation()
	visible := max(1, (height-1)/2)
	start := max(0, v.NavIndex-visible+1)
	for i := start; i < len(items) && len(lines) < height; i++ {
		item := items[i]
		label := fit("  "+strings.Repeat("  ", min(item.depth, 5))+safe(item.label), width)
		count := fmt.Sprintf("  %d entries", len(m.entriesFor(item, "")))
		if i == v.NavIndex {
			if focused {
				label = active.Render(label)
				count = active.Render(fit(count, width))
			} else {
				label = inactive.Render(label)
				count = inactive.Render(fit(count, width))
			}
		} else {
			count = muted.Render(count)
		}
		lines = append(lines, label)
		if len(lines) < height {
			lines = append(lines, count)
		}
	}
	return padLines(lines, height)
}

func (m *Model) entryLines(width, height int, focused bool) []string {
	tw := max(7, width*40/100)
	uw := max(5, width*27/100)
	urlw := max(0, width-tw-uw)
	row := func(a, b, c string) string { return fit(a, tw) + fit(b, uw) + fit(c, urlw) }
	heading := row("Title", "Username", "Website")
	if focused {
		heading = accent.Render(heading)
	} else {
		heading = muted.Render(heading)
	}
	lines := []string{heading}
	entries := m.entries()
	v := m.ws.ActiveView()
	if len(entries) == 0 {
		text := "No entries in this view"
		if v.Query != "" {
			text = "No matches"
		}
		lines = append(lines, "", muted.Render(" "+text))
		if v.Query != "" {
			lines = append(lines, muted.Render(" Esc clears filter"))
		} else {
			lines = append(lines, muted.Render(" Read-only foundation"))
		}
		return padLines(lines, height)
	}
	for i := v.EntryOffset; i < len(entries) && len(lines) < height; i++ {
		e := entries[i]
		url := strings.TrimPrefix(strings.TrimPrefix(visibleField(e, "URL"), "https://"), "http://")
		text := row(visibleField(e, "Title"), visibleField(e, "UserName"), url)
		if e.UUID == v.SelectedUUID {
			if focused {
				text = active.Render(text)
			} else {
				text = inactive.Render(text)
			}
		}
		lines = append(lines, text)
	}
	return padLines(lines, height)
}

func (m *Model) detailLines(width, height int, focused bool) []string {
	lines := m.detailContent(width)
	if focused && len(lines) > 0 {
		lines[0] = accent.Render(ansi.Strip(lines[0]))
	}
	v := m.ws.ActiveView()
	offset := 0
	if v != nil {
		offset = min(v.DetailOffset, max(0, len(lines)-height))
	}
	return padLines(lines[offset:], height)
}

func (m *Model) detailContent(width int) []string {
	e := m.selected()
	if e == nil {
		return []string{muted.Render("  Select an entry")}
	}
	title := "  " + visibleField(*e, "Title")
	title = muted.Render(title)
	lines := []string{title}
	if !e.Modified.IsZero() {
		lines = append(lines, muted.Render("  Modified "+e.Modified.Local().Format("2006-01-02")))
	}
	lines = append(lines, muted.Render("  "+safe(e.GroupPath)), rule(width))
	names := []string{"UserName", "Password", "URL", "Notes"}
	seen := map[string]bool{"Title": true}
	for _, f := range e.Fields {
		if f.Name != "Title" && f.Name != "UserName" && f.Name != "Password" && f.Name != "URL" && f.Name != "Notes" {
			names = append(names, f.Name)
		}
	}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		label := name
		if name == "UserName" {
			label = "Username"
		}
		lines = append(lines, "", "  "+safe(label))
		value := visibleField(*e, name)
		if name == "Notes" && !e.IsProtected(name) {
			value = e.Get(name)
		}
		if value == "" {
			value = "—"
		}
		for _, paragraph := range strings.Split(value, "\n") {
			wrapped := ansi.Wrap(safe(paragraph), max(1, width-4), "")
			for _, line := range strings.Split(wrapped, "\n") {
				lines = append(lines, muted.Render("  "+line))
			}
		}
	}
	if len(e.Tags) > 0 {
		lines = append(lines, "", muted.Render("  Tags: "+safe(strings.Join(e.Tags, ", "))))
	}
	return lines
}

func padLines(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m *Model) box(title string, body []string, footer string) []string {
	width := min(m.width-6, 82)
	inner := width - 4
	maxBody := max(1, m.height-8)
	if len(body) > maxBody {
		body = body[:maxBody]
	}
	content := []string{accent.Render("  " + title), rule(width)}
	for _, line := range body {
		content = append(content, "  "+fit(line, inner))
	}
	content = append(content, rule(width), muted.Render("  "+footer))
	left := max(0, (m.width-width)/2)
	top := max(0, (m.height-len(content))/2)
	lines := make([]string, top)
	for _, line := range content {
		lines = append(lines, strings.Repeat(" ", left)+line)
	}
	return lines
}

func (m *Model) unlockLines() []string {
	body := []string{muted.Render(safe(m.path)), ""}
	labels := []string{"Password", "Key file (optional)", "Hardware slot (optional, e.g. 1)", "Password authentication"}
	for i, label := range labels {
		prefix := "  "
		if i == m.inputFocus {
			prefix = "› "
		}
		value := ""
		if i < 3 {
			value = m.inputs[i].display(i == 0)
		} else if m.noPassword {
			value = "[ ] disabled"
		} else {
			value = "[x] enabled"
		}
		if value == "" {
			value = "—"
		}
		line := prefix + label + ": " + value
		if i == m.inputFocus {
			line = accent.Render(line)
		}
		body = append(body, line)
	}
	if m.busy {
		body = append(body, accent.Render("Unlocking… Esc cancels"))
	}
	if m.err != "" {
		body = append(body, errorStyle.Render(m.err))
	}
	return m.box("Unlock Vault", body, "Enter unlock · Tab next · Space toggle · Esc cancel")
}

func (m *Model) pickerLines() []string {
	pathLine := muted.Render(m.inputs[0].display(false))
	if m.pickerEditing {
		pathLine = accent.Render(m.inputs[0].display(false) + "▏")
	}
	body := []string{muted.Render("Path · Tab to edit, or type/paste a new path"), pathLine, ""}
	capacity := max(1, m.height-16)
	start := max(0, m.fileIndex-capacity+1)
	for i := start; i < len(m.files) && i < start+capacity; i++ {
		f := m.files[i]
		name := safe(f.name)
		if f.dir {
			name += "/"
		}
		if i == m.fileIndex {
			name = fit("› "+name, min(m.width-14, 72))
			if m.pickerEditing {
				name = inactive.Render(name)
			} else {
				name = active.Render(name)
			}
		} else {
			name = "  " + name
		}
		body = append(body, name)
	}
	if len(m.files) == 0 {
		body = append(body, muted.Render("No vault files here. Enter another path."))
	}
	if m.err != "" {
		body = append(body, "", errorStyle.Render(m.err))
	}
	return m.box("Open Vault", body, "↑↓ select · Enter open · ← parent · Tab path · Esc cancel")
}

func (m *Model) helpLines() []string {
	body := []string{
		"Ctrl+O        Open another vault", "Ctrl+T        Duplicate current view", "Alt+1…9       Switch view", "Ctrl+W        Close current view", "Ctrl+L        Lock current vault", "", "Tab / Shift+Tab   Next / previous pane", "↑↓ / j k      Select row / scroll details", "←→ / h l      Focus pane", "/ or Ctrl+F   Search entry fields", "Esc           Clear filter / cancel input", "?             Keyboard shortcuts", "q / Ctrl+C    Quit (q types normally in inputs)", "", muted.Render("Foundation · read only · secrets remain masked"),
	}
	if m.backendVersion != "" {
		body = append(body, muted.Render("Backend: "+m.backendVersion))
	}
	return m.box("Keyboard Shortcuts", body, "Esc / Enter close")
}
