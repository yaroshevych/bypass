package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"keepassui/internal/workspace"
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
	// Rendering may ask the browser for filtered entries. Keep that read path
	// from populating its optional cache so View remains side-effect free.
	searchCache := m.searchCache
	defer func() { m.searchCache = searchCache }()
	w, h := max(1, m.width), max(1, m.height)
	styles := m.colors()
	base, accent := styles.base, styles.accent
	muted := styles.muted
	var lines []string
	if m.layout().Mode == "tiny" {
		minimum := "Resize to at least 48 × 14"
		if m.settings.ASCII {
			minimum = "Resize to at least 48x14"
		}
		lines = []string{accent.Render("KeePassUI"), "Terminal too small", muted.Render(minimum), "q quit · Ctrl+C quit"}
		if m.editor != nil && m.editor.dirty() {
			lines[3] = "Draft kept · resize · Ctrl+C quits"
		}
	} else if advanced, ok := m.advancedScreen(); ok {
		lines = advanced
	} else if m.dialog != nil {
		lines = m.dialogLines()
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
	styles := m.colors()
	accent, muted, active, inactive, errorStyle := styles.accent, styles.muted, styles.active, styles.inactive, styles.errorStyle
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
	lines = append(lines, m.themedRule(w), muted.Render("  o open vault    ? shortcuts    q quit"))
	return lines
}

func (m *Model) paneWidths() (int, int, int) {
	var v *workspace.View
	if m.ws != nil {
		v = m.ws.ActiveView()
	}
	return m.widePaneWidths(max(120, m.width), v)
}

func (m *Model) workspaceLines() []string {
	styles := m.colors()
	accent, muted, active := styles.accent, styles.muted, styles.active
	s := m.ws.ActiveSession()
	w := m.width
	name := safe(filepath.Base(s.Path))
	status := "Read only"
	if !s.ReadOnly {
		status = "Unlocked"
	}
	if s.DB == nil {
		status = "Vault locked"
	}
	right := status
	if s.DB != nil {
		right = fmt.Sprintf("%s  ·  %d entries  ·  %d groups", status, len(s.DB.Entries), len(s.DB.Groups))
		if s.Stale {
			right = "Changed externally · Ctrl+R reload"
		}
	}
	lines := []string{accent.Render(fit("  "+name, max(0, w-lipgloss.Width(right)-2))) + muted.Render(right), m.themedRule(w)}
	var tabs []string
	labels := m.tabLabels()
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	for _, span := range m.tabSpans() {
		label := labels[span.Index]
		if compact {
			if span.Index == m.ws.Active {
				if m.settings.ASCII {
					label = "[ " + label + " ]"
				} else {
					label = "◖ " + label + " ◗"
				}
			} else {
				label = " " + label + " "
			}
		}
		label = fit(label, span.End-span.Start)
		if span.Index == m.ws.Active {
			label = active.Render(label)
		} else {
			label = muted.Render(label)
		}
		tabs = append(tabs, label)
	}
	separator := styles.lineStyle.Render("│")
	if compact && m.settings.ASCII {
		separator = styles.lineStyle.Render("|")
	}
	lines = append(lines, fit(strings.Join(tabs, separator), w), m.themedRule(w))
	if s.DB == nil {
		lines = append(lines, "", accent.Render("  Vault locked"), muted.Render("  Enter to unlock with vault credentials."), "", muted.Render("  1–9 switch vault views · Ctrl+O open another vault"))
		for len(lines) < m.height-2 {
			lines = append(lines, "")
		}
		return append(lines, m.themedRule(w), muted.Render("  Enter unlock    Ctrl+W close view    q quit"))
	}
	l := m.layout()
	lines = append(lines, m.layoutBody(l)...)
	footer := m.footerText()
	if m.notice != "" {
		footer = "  " + m.notice
	}
	if m.editor != nil {
		footer = "  Tab fields · Ctrl+S save · Esc cancel · Ctrl+G generator"
	}
	if m.writing {
		footer = "  Working…"
	}
	if status := m.clipboardStatus(); status != "" {
		footer += " · " + status
	}
	return append(lines, m.themedRule(w), muted.Render(fit(footer, w)))
}

func (m *Model) layoutBody(layout viewLayout) []string {
	body := make([]string, max(0, layout.BodyHeight))
	if len(body) == 0 {
		return body
	}
	if layout.Mode == "tiny" || len(layout.Panes) == 0 {
		return body
	}
	styles := m.colors()
	if layout.Breadcrumb {
		body[0] = styles.muted.Render(fit(m.layoutBreadcrumb(layout), m.width))
	}
	contents := make([][]string, len(layout.Panes))
	for i, pane := range layout.Panes {
		focused := false
		if view := m.ws.ActiveView(); view != nil {
			focused = view.Focus == pane.Focus
		}
		switch pane.Focus {
		case 0:
			contents[i] = m.navLines(pane.Width, pane.Height, focused)
		case 1:
			contents[i] = m.entryLines(pane.Width, pane.Height, focused)
		case 2:
			if m.editor != nil {
				contents[i] = m.editor.render(pane.Width, pane.Height)
			} else {
				contents[i] = m.detailLines(pane.Width, pane.Height, focused)
			}
		}
	}
	startRow := 0
	if layout.Breadcrumb {
		startRow = 1
	}
	for row := startRow; row < len(body); row++ {
		contentRow := row - startRow
		var parts []string
		for i, pane := range layout.Panes {
			line := ""
			if contentRow < len(contents[i]) {
				line = contents[i][contentRow]
			}
			parts = append(parts, fit(line, pane.Width))
		}
		sep := styles.lineStyle.Render("│")
		if m.settings.ASCII {
			sep = styles.lineStyle.Render("|")
		}
		body[row] = strings.Join(parts, sep)
	}
	return body
}

func (m *Model) layoutBreadcrumb(layout viewLayout) string {
	view := m.ws.ActiveView()
	focus := 1
	if view != nil {
		focus = min(2, max(0, view.Focus))
	}
	if m.editor != nil {
		focus = 2
	}
	names := []string{"Views", "Entries", "Details"}
	location := names[focus]
	if view != nil {
		items := m.navigation()
		if view.NavIndex >= 0 && view.NavIndex < len(items) {
			location = safe(items[view.NavIndex].label) + " / " + location
		}
	}
	if focus == 2 {
		if entry := m.selected(); entry != nil {
			location = safe(entry.Get("Title")) + " / " + location
		}
	}
	back := "←"
	if m.settings.ASCII {
		back = "<"
	}
	if layout.Zoomed {
		return "  " + location + "  ·  Zoomed  ·  Esc restore layout"
	}
	return "  " + back + " " + location + "  ·  Tab next pane"
}

func (m *Model) tabLabels() []string {
	if m.ws == nil {
		return nil
	}
	var labels []string
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	for i, view := range m.ws.Views {
		s := m.ws.Sessions[view.SessionID]
		if s == nil {
			label := fmt.Sprintf("%d (unavailable)", i+1)
			if !compact {
				label = " " + label + " "
			}
			labels = append(labels, label)
			continue
		}
		label := fmt.Sprintf("%d %s", i+1, safe(filepath.Base(s.Path)))
		for _, other := range m.ws.Sessions {
			if other.ID != s.ID && filepath.Base(other.Path) == filepath.Base(s.Path) {
				label = fmt.Sprintf("%d %s (%s)", i+1, safe(filepath.Base(s.Path)), safe(filepath.Dir(s.Path)))
				break
			}
		}
		if s.DB == nil {
			label += " [locked]"
		}
		if !compact {
			label = " " + label + " "
		}
		labels = append(labels, label)
	}
	return labels
}

type tabSpan struct{ Index, Start, End int }

func (m *Model) tabSpans() []tabSpan {
	labels := m.tabLabels()
	if len(labels) == 0 || m.width <= 0 {
		return nil
	}
	activeIndex := min(len(labels)-1, max(0, m.ws.Active))
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	visible := map[int]string{}
	display := func(i int) string {
		label := labels[i]
		if compact && i == activeIndex {
			if m.settings.ASCII {
				return "[ " + label + " ]"
			}
			return "◖ " + label + " ◗"
		}
		if compact {
			return " " + label + " "
		}
		return label
	}
	visible[activeIndex] = display(activeIndex)
	used := min(m.width, lipgloss.Width(visible[activeIndex]))
	start, end := activeIndex, activeIndex+1
	for start > 0 || end < len(labels) {
		added := false
		if start > 0 {
			candidate := display(start - 1)
			need := lipgloss.Width(candidate) + 1
			if used+need <= m.width {
				start--
				visible[start] = candidate
				used += need
				added = true
			}
		}
		if end < len(labels) {
			candidate := display(end)
			need := lipgloss.Width(candidate) + 1
			if used+need <= m.width {
				visible[end] = candidate
				used += need
				end++
				added = true
			}
		}
		if !added {
			break
		}
	}
	var spans []tabSpan
	x := 0
	for i := start; i < end; i++ {
		if i != start {
			x++
		}
		width := min(m.width-x, lipgloss.Width(visible[i]))
		if width <= 0 {
			break
		}
		spans = append(spans, tabSpan{Index: i, Start: x, End: x + width})
		x += width
	}
	return spans
}

func (m *Model) navLines(width, height int, focused bool) []string {
	styles := m.colors()
	accent, muted, active, inactive := styles.accent, styles.muted, styles.active, styles.inactive
	title := "  Views"
	if focused {
		title = accent.Render(title)
	} else {
		title = muted.Render(title)
	}
	lines := []string{title}
	v := m.ws.ActiveView()
	items := m.navigation()
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	rowHeight := m.navRowHeight()
	capacity := max(0, (height-1)/rowHeight)
	start := viewportOffset(v.NavIndex, len(items), capacity, v.NavOffset)
	for i := start; i < len(items) && len(lines) < height; i++ {
		item := items[i]
		countValue := len(m.entriesFor(item, ""))
		itemLabel := safe(item.label)
		if v.NavFilter != "" {
			itemLabel = m.highlightText(item.label, v.NavFilter, "Title")
		}
		text := "  " + strings.Repeat("  ", min(item.depth, 5)) + itemLabel
		label := fit(text, width)
		count := fmt.Sprintf("  %d entries", countValue)
		if compact {
			text := strings.Repeat("  ", min(item.depth, 3)) + itemLabel + fmt.Sprintf("  %d", countValue)
			if i == v.NavIndex {
				if m.settings.ASCII {
					text = "[ " + text + " ]"
				} else {
					text = "◖ " + text + " ◗"
				}
			}
			label = fit(text, width)
			if i == v.NavIndex {
				if focused {
					label = active.Render(label)
				} else {
					label = inactive.Render(label)
				}
			} else {
				label = muted.Render(label)
			}
			lines = append(lines, label)
			continue
		}
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
	styles := m.colors()
	accent, muted, active, inactive := styles.accent, styles.muted, styles.active, styles.inactive
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
			text := " n creates an entry"
			if s := m.ws.ActiveSession(); s != nil && s.ReadOnly {
				text = " Read only · R enables writes"
			}
			lines = append(lines, muted.Render(text))
		}
		return padLines(lines, height)
	}
	selectedIndex := -1
	for i := range entries {
		if entries[i].UUID == v.SelectedUUID {
			selectedIndex = i
			break
		}
	}
	capacity := max(1, height-1)
	start := v.EntryOffset
	if selectedIndex >= 0 {
		start = viewportOffset(selectedIndex, len(entries), capacity, v.EntryOffset)
	}
	for i := start; i < len(entries) && len(lines) < height; i++ {
		e := entries[i]
		url := strings.TrimPrefix(strings.TrimPrefix(visibleField(e, "URL"), "https://"), "http://")
		title, username := visibleField(e, "Title"), visibleField(e, "UserName")
		if v.Query != "" {
			title = m.highlightText(title, v.Query, "Title")
			username = m.highlightText(username, v.Query, "UserName")
			url = m.highlightText(url, v.Query, "URL")
		}
		text := row(title, username, url)
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
	accent := m.colors().accent
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
	lines, _ := m.detailLayout(width)
	return lines
}

func (m *Model) detailLayout(width int) ([]string, []int) {
	styles := m.colors()
	muted, active := styles.muted, styles.active
	e := m.selected()
	if e == nil {
		return []string{muted.Render("  Select an entry")}, nil
	}
	title := "  " + visibleField(*e, "Title")
	title = muted.Render(title)
	lines := []string{title}
	if !e.Modified.IsZero() {
		lines = append(lines, muted.Render("  Modified "+e.Modified.Local().Format("2006-01-02")))
	}
	lines = append(lines, muted.Render("  "+safe(e.GroupPath)), m.themedRule(width))
	names := m.fieldNames()
	var positions []int
	for index, name := range names {
		positions = append(positions, len(lines)+1)
		label := name
		if name == "UserName" {
			label = "Username"
		}
		labelLine := "  " + safe(label)
		if v := m.ws.ActiveView(); v != nil && v.Focus == 2 && index == v.FieldIndex {
			labelLine = active.Render(fit("› "+safe(label), width))
		}
		lines = append(lines, "", labelLine)
		value := m.fieldValue(*e, name)
		if name == "Notes" && (!e.IsProtected(name) || m.revealed(e.UUID, name)) {
			value = e.Get(name)
			if m.revealed(e.UUID, name) {
				value = string(m.revealValue)
			}
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
	return lines, positions
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
	styles := m.colors()
	accent, muted := styles.accent, styles.muted
	width := min(m.width-6, 82)
	inner := width - 4
	maxBody := max(1, m.height-8)
	if len(body) > maxBody {
		body = body[:maxBody]
	}
	content := []string{accent.Render("  " + title), m.themedRule(width)}
	for _, line := range body {
		content = append(content, "  "+fit(line, inner))
	}
	content = append(content, m.themedRule(width), muted.Render("  "+footer))
	left := max(0, (m.width-width)/2)
	top := max(0, (m.height-len(content))/2)
	lines := make([]string, top)
	for _, line := range content {
		lines = append(lines, strings.Repeat(" ", left)+line)
	}
	return lines
}

func (m *Model) unlockLines() []string {
	styles := m.colors()
	accent, muted, errorStyle := styles.accent, styles.muted, styles.errorStyle
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
	styles := m.colors()
	accent, muted, active, inactive, errorStyle := styles.accent, styles.muted, styles.active, styles.inactive, styles.errorStyle
	pathLine := muted.Render(m.inputs[0].display(false))
	if m.pickerEditing {
		pathLine = accent.Render(m.inputs[0].display(false) + "▏")
	}
	body := []string{muted.Render("Path · Tab to edit, or type/paste a new path"), pathLine, ""}
	start, capacity := m.fileViewport()
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
	muted := m.colors().muted
	body := m.helpActionLines()
	body = append(body, "", muted.Render("Encrypted backup before every write"))
	if m.backendVersion != "" {
		body = append(body, muted.Render("Backend: "+m.backendVersion))
	}
	capacity := max(1, m.height-8)
	start := min(m.helpOffset, max(0, len(body)-capacity))
	end := min(len(body), start+capacity)
	return m.box("Keyboard Shortcuts", body[start:end], "↑↓ scroll · Esc / Enter close")
}
