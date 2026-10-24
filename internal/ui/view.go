package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/workspace"
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

func fitRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	return strings.Repeat(" ", max(0, width-lipgloss.Width(s))) + s
}

func displayVaultPath(path, home string) string {
	if path == "" || home == "" || !filepath.IsAbs(path) || !filepath.IsAbs(home) {
		return safe(path)
	}
	rel, err := filepath.Rel(filepath.Clean(home), filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return safe(path)
	}
	if rel == "." {
		return "~"
	}
	return safe(filepath.Join("~", rel))
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
		lines = []string{accent.Render("bypass"), "Terminal too small", muted.Render(minimum), "q quit · ctrl+c quit"}
		if m.editor != nil && m.editor.dirty() {
			lines[3] = "Draft kept · resize · ctrl+c quits"
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
	lines = append(lines, pad+accent.Render("        ◇"), "", pad+accent.Render("bypass secrets manager"), pad+muted.Render("Your vaults. Your keys. Your terminal."), "", pad+active.Render("  Open Vault  ")+muted.Render("  o / ctrl+o"), "", pad+muted.Render("Open an existing KeePass-compatible .kdbx file."))
	if m.canCreateVault() {
		lines = append(lines, pad+accent.Render("New Vault")+muted.Render("  N"))
	}
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
		lines = append(lines, pad+muted.Render("↑↓ select · enter open · delete remove recent"))
	}
	if m.notice != "" {
		lines = append(lines, "", pad+muted.Render(m.notice))
	}
	if m.backendMissing {
		lines = append(lines, "", pad+errorStyle.Render("Install KeePassXC or set --backend executable."))
	}
	footerHeight := 1
	for len(lines) < m.height-footerHeight {
		lines = append(lines, "")
	}
	lines = append(lines, m.footerLine("o open vault    ? shortcuts    q quit"))
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
		right = fmt.Sprintf("%d entries · ctrl+l lock", len(s.DB.Entries))
		if s.Stale {
			right = "Changed externally · ctrl+r reload · ctrl+l lock"
		}
	}
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	lines := []string{accent.Render(fit("  "+name, max(0, w-lipgloss.Width(right)))) + muted.Render(right)}
	if !compact {
		lines = append(lines, m.tabRule(w, true))
	}
	var tabs []string
	viewIndexes := m.visibleTabIndexes()
	activeTabIndex := 0
	for i, viewIndex := range viewIndexes {
		if viewIndex == m.ws.Active {
			activeTabIndex = i
			break
		}
	}
	labels := m.tabLabels()
	if m.canAddTab() {
		labels = append(labels, "+")
	}
	for _, span := range m.tabSpans() {
		label := labels[span.Index]
		if !compact && span.Index == len(viewIndexes) {
			label = " + "
		}
		if compact {
			if span.Index == activeTabIndex {
				if m.settings.ASCII {
					label = "[ " + label + " ]"
				} else {
					label = " " + label + " "
				}
			} else {
				label = " " + label + " "
			}
		} else {
			label = strings.ToUpper(label)
		}
		label = fit(label, span.End-span.Start)
		if span.Index == activeTabIndex {
			if compact {
				style := active
				if m.tabHovered && m.hoverTab == span.Index {
					style = style.Foreground(styles.accent.GetForeground())
				}
				if !m.settings.ASCII {
					label = renderPill(labels[span.Index], span.End-span.Start, style, styles.inactive)
				} else {
					label = style.Render(label)
				}
			} else {
				label = styles.accent.Render(label)
			}
		} else {
			if compact {
				style := styles.inactive
				if m.tabHovered && m.hoverTab == span.Index {
					style = style.Foreground(styles.accent.GetForeground()).Bold(true)
				}
				label = style.Render(label)
			} else {
				style := styles.base.Bold(true)
				if m.tabHovered && m.hoverTab == span.Index {
					style = style.Foreground(styles.accent.GetForeground())
				}
				label = style.Render(label)
			}
		}
		tabs = append(tabs, label)
	}
	separator := styles.lineStyle.Render("│")
	if compact {
		separator = styles.inactive.Render(" ")
	}
	tabLine := strings.Join(tabs, separator)
	if !compact && !m.canAddTab() && len(tabs) > 0 {
		tabLine += separator
	}
	if compact && !m.settings.ASCII {
		edge := styles.base.Foreground(styles.inactive.GetBackground())
		tabLine = edge.Render("\ue0b6") + tabLine + edge.Render("\ue0b4")
	}
	lines = append(lines, fit(tabLine, w))
	if !compact {
		lines = append(lines, m.tabRule(w, false))
	}
	if s.DB == nil {
		home, _ := os.UserHomeDir()
		lines = append(lines, "", accent.Render("  Vault locked"), muted.Render("  "+displayVaultPath(s.Path, home)), "", muted.Render("  enter to unlock"), muted.Render("  ctrl+o open another vault"))
		footerHeight := 1
		for len(lines) < m.height-footerHeight {
			lines = append(lines, "")
		}
		return append(lines, m.footerLine("", "enter unlock · ctrl+w close tab · q quit"))
	}
	l := m.layout()
	if !compact {
		lines[l.BodyY-1] = m.workspaceTopRule(l)
	}
	lines = append(lines, m.layoutBody(l)...)
	footer := m.footerText()
	if status := m.clipboardStatus(); status != "" {
		footer += " · " + status
	}
	if m.notice != "" {
		footer += " · " + m.notice
	}
	if m.writing {
		footer += " · Working…"
	}
	contextual := m.footerContext()
	if m.notice != "" || m.clipboardStatus() != "" {
		available := m.width - lipgloss.Width(m.footerBadge()) - 1 - lipgloss.Width(strings.TrimSpace(footer)) - 2
		if lipgloss.Width(contextual) > available {
			var keys []string
			for _, hint := range strings.Split(contextual, " · ") {
				if words := strings.Fields(hint); len(words) > 0 {
					keys = append(keys, words[0])
				}
			}
			contextual = strings.Join(keys, " · ")
			if lipgloss.Width(contextual) > available {
				contextual = ""
			}
		}
	}
	footerLine := m.footerLine(footer, contextual)
	if m.searching {
		footerLine = m.searchFooterLine(footer, contextual, false)
	} else if v := m.ws.ActiveView(); v != nil && (v.Query != "" || v.NavFilter != "" || v.DetailFilter != "") {
		footerLine = m.searchFooterLine(footer, contextual, true)
	}
	return append(lines, footerLine)
}

func (m *Model) paneRule(layout viewLayout, top bool) string {
	horizontal, junction := '─', '┴'
	if top {
		junction = '┬'
	}
	if m.settings.ASCII {
		horizontal, junction = '-', '+'
	}
	line := []rune(strings.Repeat(string(horizontal), max(0, m.width)))
	for i, pane := range layout.Panes {
		if x := pane.X + pane.Width; i < len(layout.Panes)-1 && x >= 0 && x < len(line) {
			line[x] = junction
		}
	}
	return m.colors().lineStyle.Render(string(line))
}

func (m *Model) tabRule(width int, top bool) string {
	horizontal, junction := '─', '┬'
	if !top {
		junction = '┴'
	}
	if m.settings.ASCII {
		horizontal, junction = '-', '+'
	}
	line := []rune(strings.Repeat(string(horizontal), max(0, width)))
	spans := m.tabSpans()
	lastDivider := len(spans) - 1
	if m.canAddTab() {
		lastDivider-- // The final visible span is the + tab, which has no trailing divider.
	}
	for i := 0; i <= lastDivider; i++ {
		if x := spans[i].End; x >= 0 && x < len(line) {
			line[x] = junction
		}
	}
	return m.colors().lineStyle.Render(string(line))
}

func (m *Model) workspaceTopRule(layout viewLayout) string {
	line := []rune(ansi.Strip(m.paneRule(layout, true)))
	spans := m.tabSpans()
	lastDivider := len(spans) - 1
	if m.canAddTab() {
		lastDivider-- // The final visible span is the + tab, which has no trailing divider.
	}
	for i := 0; i <= lastDivider; i++ {
		span := spans[i]
		if x := span.End; x >= 0 && x < len(line) {
			if line[x] == '┬' {
				line[x] = '┼'
			} else if m.settings.ASCII {
				line[x] = '+'
			} else {
				line[x] = '┴'
			}
		}
	}
	return m.colors().lineStyle.Render(string(line))
}

func (m *Model) footerBadge() string {
	c := m.colors()
	style := c.active.Foreground(c.base.GetBackground()).Background(c.accent.GetForeground())
	badge := style.Render(" bypass ")
	if strings.EqualFold(m.settings.Appearance, "compact") {
		if m.settings.ASCII {
			badge = style.Render("[ bypass ]")
		} else {
			badge = renderPill("bypass", 10, style, c.base)
		}
	}
	return badge
}

func (m *Model) footerLine(text string, contextual ...string) string {
	return m.footerLineWithLeftStyle(text, m.colors().muted, contextual...)
}

func (m *Model) searchFooterLine(text, contextual string, applied bool) string {
	c := m.colors()
	style := c.inactive
	if applied {
		style = c.base.Bold(true).Background(c.inactive.GetBackground())
	}
	if strings.EqualFold(m.settings.Appearance, "compact") {
		label := strings.TrimSpace(text)
		var content string
		switch {
		case m.searching && strings.TrimSpace(m.query.value()) == "":
			content = c.inactive.Render("/ ") + c.active.Render("f") + c.inactive.Render("ilter ")
		case m.searching:
			content = c.inactive.Render(label) + c.active.Render(" ")
		default:
			content = style.Render(label + " ")
		}
		width := lipgloss.Width(content) + 2 // one cell of inner padding per side plus caps
		edge := c.base.Foreground(style.GetBackground())
		control := fit(edge.Render("\ue0b6")+content+edge.Render("\ue0b4"), width)
		if m.settings.ASCII {
			control = fit(style.Render("[")+content+style.Render("]"), width)
		}
		left := m.footerBadge() + " " + control
		if contextual != "" {
			left += " " + c.muted.Render(lowercaseShortcutLabels(contextual))
		}
		return fit(ansi.Truncate(left, m.width, "…"), m.width)
	}
	if m.searching && strings.TrimSpace(m.query.value()) == "" {
		text = c.inactive.Render(" ") + c.inactive.Render("/ ") + c.active.Render("f") + c.inactive.Render("ilter") + c.inactive.Render(" ")
	} else if m.searching {
		text = style.Render(" "+strings.TrimSpace(text)) + c.active.Render(" ")
	} else {
		text = style.Render(" " + strings.TrimSpace(text) + " ")
	}
	left := m.footerBadge()
	left += text
	if contextual != "" {
		left += " " + c.muted.Render(lowercaseShortcutLabels(contextual))
	}
	return fit(ansi.Truncate(left, m.width, "…"), m.width)
}

func (m *Model) footerLineWithLeftStyle(text string, leftStyle lipgloss.Style, contextual ...string) string {
	text = lowercaseShortcutLabels(text)
	c := m.colors()
	badge := m.footerBadge()
	gap := " "
	if strings.HasPrefix(strings.TrimSpace(text), "/ filter · ctrl+p commands") {
		if strings.EqualFold(m.settings.Appearance, "compact") {
			gap = "  "
		}
	}
	left := badge + gap + leftStyle.Render(strings.TrimSpace(text))
	if len(contextual) > 0 && contextual[0] != "" {
		right := c.muted.Render(lowercaseShortcutLabels(contextual[0]))
		minimumLeft := lipgloss.Width(badge) + lipgloss.Width(gap) + lipgloss.Width("/ filter · ctrl+p commands")
		if status := m.clipboardStatus(); status != "" {
			minimumLeft += 3 + lipgloss.Width(status)
		}
		if m.width >= minimumLeft+lipgloss.Width(right)+2 {
			left = ansi.Truncate(left, m.width-lipgloss.Width(right)-2, "…")
		}
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap >= 2 {
			return left + strings.Repeat(" ", gap) + right
		}
	}
	return fit(left, m.width)
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
		var joined strings.Builder
		for i, part := range parts {
			if i > 0 {
				junction := sep
				right := ansi.Strip(part)
				leftRule := strings.HasSuffix(ansi.Strip(parts[i-1]), "─")
				rightRule := strings.TrimSpace(right) != "" && strings.Trim(right, "─") == ""
				if leftRule && rightRule {
					junction = styles.lineStyle.Render("┼")
				} else if leftRule {
					junction = styles.lineStyle.Render("┤")
				} else if rightRule {
					junction = styles.lineStyle.Render("├")
				}
				if m.settings.ASCII && (strings.HasSuffix(ansi.Strip(parts[i-1]), "-") || rightRule) {
					junction = styles.lineStyle.Render("+")
				}
				joined.WriteString(junction)
			}
			joined.WriteString(part)
		}
		body[row] = joined.String()
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
		return "  " + location + "  ·  Zoomed  ·  esc restore layout"
	}
	return "  " + back + " " + location + "  ·  Tab next pane"
}

func (m *Model) visibleTabIndexes() []int {
	if m.ws == nil {
		return nil
	}
	firstBySession := make(map[string]int)
	for i, view := range m.ws.Views {
		if s := m.ws.Sessions[view.SessionID]; s != nil && s.DB == nil {
			if _, ok := firstBySession[view.SessionID]; !ok {
				firstBySession[view.SessionID] = i
			}
		}
	}
	representative := make(map[string]int, len(firstBySession))
	for id, first := range firstBySession {
		representative[id] = first
		if session := m.ws.Sessions[id]; session != nil && session.ActiveViewID != "" {
			for i, view := range m.ws.Views {
				if view.SessionID == id && view.ID == session.ActiveViewID {
					representative[id] = i
					break
				}
			}
		}
	}
	if active := m.ws.ActiveView(); active != nil {
		if s := m.ws.Sessions[active.SessionID]; s != nil && s.DB == nil {
			representative[active.SessionID] = m.ws.Active
		}
	}
	indexes := make([]int, 0, len(m.ws.Views))
	for i, view := range m.ws.Views {
		if s := m.ws.Sessions[view.SessionID]; s != nil && s.DB == nil && firstBySession[view.SessionID] != i {
			continue
		}
		if s := m.ws.Sessions[view.SessionID]; s != nil && s.DB == nil {
			indexes = append(indexes, representative[view.SessionID])
		} else {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (m *Model) tabLabels() []string {
	indexes := m.visibleTabIndexes()
	if len(indexes) == 0 {
		return nil
	}
	var labels []string
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	for position, viewIndex := range indexes {
		view := m.ws.Views[viewIndex]
		s := m.ws.Sessions[view.SessionID]
		if s == nil {
			label := fmt.Sprintf("%d (unavailable)", position+1)
			if !compact {
				label = " " + label + " "
			}
			labels = append(labels, label)
			continue
		}
		label := fmt.Sprintf("%d %s", position+1, safe(filepath.Base(s.Path)))
		for _, other := range m.ws.Sessions {
			if other.ID != s.ID && filepath.Base(other.Path) == filepath.Base(s.Path) {
				label = fmt.Sprintf("%d %s (%s)", position+1, safe(filepath.Base(s.Path)), safe(filepath.Dir(s.Path)))
				break
			}
		}
		if s.DB == nil {
			// A single vault tab represents every preserved view while locked.
		} else {
			for _, entry := range s.DB.Entries {
				if entry.UUID == view.SelectedUUID {
					if title := visibleField(entry, "Title"); title != "" {
						label = fmt.Sprintf("%d %s", position+1, title)
					}
					break
				}
			}
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
	viewIndexes := m.visibleTabIndexes()
	labels := m.tabLabels()
	if len(labels) == 0 || m.width <= 0 {
		return nil
	}
	activeIndex := 0
	for i, viewIndex := range viewIndexes {
		if viewIndex == m.ws.Active {
			activeIndex = i
			break
		}
	}
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	hasAddTab := m.canAddTab()
	capacity := m.width
	if hasAddTab {
		capacity = max(1, m.width-4) // Reserve a gap and the rightmost + tab.
	}
	startX := 0
	if compact && !m.settings.ASCII {
		startX = 1
		if hasAddTab {
			capacity = max(1, m.width-6)
		} else {
			capacity = max(1, m.width-2) // Keep only the outer Powerline caps.
		}
	}
	visible := map[int]string{}
	display := func(i int) string {
		label := labels[i]
		if compact && i == activeIndex {
			if m.settings.ASCII {
				return "[ " + label + " ]"
			}
			return " " + label + " "
		}
		if compact {
			return " " + label + " "
		}
		return label
	}
	visible[activeIndex] = display(activeIndex)
	used := min(capacity, lipgloss.Width(visible[activeIndex]))
	start, end := activeIndex, activeIndex+1
	for start > 0 || end < len(labels) {
		added := false
		if start > 0 {
			candidate := display(start - 1)
			need := lipgloss.Width(candidate) + 1
			if used+need <= capacity {
				start--
				visible[start] = candidate
				used += need
				added = true
			}
		}
		if end < len(labels) {
			candidate := display(end)
			need := lipgloss.Width(candidate) + 1
			if used+need <= capacity {
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
	x := startX
	for i := start; i < end; i++ {
		if i != start {
			x++
		}
		width := min(startX+capacity-x, lipgloss.Width(visible[i]))
		if width <= 0 {
			break
		}
		spans = append(spans, tabSpan{Index: i, Start: x, End: x + width})
		x += width
	}
	if m.canAddTab() {
		spans = append(spans, tabSpan{Index: len(labels), Start: x + 1, End: x + 4})
	}
	return spans
}

func (m *Model) navLines(width, height int, focused bool) []string {
	styles := m.colors()
	muted, active, inactive := styles.muted, styles.active, styles.inactive
	var lines []string
	v := m.ws.ActiveView()
	items := m.navigation()
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	rows, start := m.sidebarViewport(height)
	for r := start; r < len(rows) && len(lines) < height; r++ {
		row := rows[r]
		if row.index < 0 {
			if row.label == "" {
				lines = append(lines, "")
			} else {
				label := "  " + strings.ToUpper(row.label) + " "
				rule := "─"
				if m.settings.ASCII {
					rule = "-"
				}
				endPadding := 0
				if compact {
					endPadding = 2
				}
				line := muted.Render(label)
				if row.label != "Views" || compact {
					line += styles.lineStyle.Render(strings.Repeat(rule, max(0, width-lipgloss.Width(label)-endPadding)))
				}
				lines = append(lines, fit(line, width))
			}
			continue
		}
		i := row.index
		item := items[i]
		countValue := len(m.entriesFor(item, ""))
		itemLabel := safe(item.label)
		if v.NavFilter != "" {
			itemLabel = m.highlightText(item.label, v.NavFilter, "Title")
		}
		indent := strings.Repeat("  ", min(item.depth, 5))
		text := indent + itemLabel
		label := fit("  "+text, width)
		if compact {
			count := fmt.Sprintf("%d", countValue)
			countStyle := muted
			if i == v.NavIndex {
				selection := inactive
				if focused {
					selection = active
				}
				countStyle = muted.Foreground(selection.GetForeground()).Background(selection.GetBackground()).Faint(true).Bold(false)
			}
			inner := max(0, width-4)
			labelWidth := max(0, inner-lipgloss.Width(count)-1)
			body := fit(ansi.Truncate(text, labelWidth, "…"), labelWidth) + " " + countStyle.Render(count)
			if i == v.NavIndex {
				if !m.settings.ASCII {
					label = m.pill(body, width, focused)
				} else {
					style := inactive
					if focused {
						style = active
					}
					label = style.Render("[ " + body + " ]")
				}
			} else {
				label = fit("  "+body+"  ", width)
			}
			lines = append(lines, label)
			continue
		}
		if row.count {
			label = fit("  "+indent+fmt.Sprintf("%d entries", countValue), width)
		}
		style := styles.base
		if i == v.NavIndex {
			if focused {
				style = active
			} else {
				style = inactive
			}
		} else if row.count {
			style = muted
		}
		if !row.count {
			style = style.Bold(true)
		}
		if row.count {
			style = style.Bold(false)
		}
		label = style.Render(label)
		lines = append(lines, label)
	}
	return padLines(lines, height)
}

func (m *Model) entryLines(width, height int, focused bool) []string {
	styles := m.colors()
	accent, muted, active, inactive := styles.accent, styles.muted, styles.active, styles.inactive
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	contentWidth := width
	if compact {
		contentWidth = max(0, width-4)
	}
	available := max(0, contentWidth-2)
	tw := min(available, max(7, available*40/100))
	uw := min(available-tw, max(5, available*27/100))
	urlw := max(0, available-tw-uw)
	cell := func(text string, width int) string {
		return fit(ansi.Truncate(text, width, "…"), width)
	}
	row := func(a, b, c string) string { return fit(cell(a, tw)+" "+cell(b, uw)+" "+cell(c, urlw), contentWidth) }
	heading := row("Title", "Username", "Website")
	if compact {
		heading = fit("  "+heading, width)
	}
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
			lines = append(lines, muted.Render(" esc clears filter"))
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
		selected := e.UUID == v.SelectedUUID
		url := strings.TrimPrefix(strings.TrimPrefix(visibleField(e, "URL"), "https://"), "http://")
		title, username := visibleField(e, "Title"), visibleField(e, "UserName")
		if e.HasTOTP {
			title += " [2FA]"
		}
		if len(e.AttachmentNames) > 0 {
			title += " [A]"
		}
		if e.ExpiryEnabled && !e.Expires.IsZero() && !m.now().Before(e.Expires) {
			title += " [expired]"
		}
		if v.Query != "" && !selected {
			title = m.highlightText(title, v.Query, "Title")
			username = m.highlightText(username, v.Query, "UserName")
			url = m.highlightText(url, v.Query, "URL")
		}
		text := row(title, username, url)
		if compact {
			if selected && !m.settings.ASCII {
				text = m.pill(text, width, focused)
			} else if selected {
				style := inactive
				if focused {
					style = active
				}
				text = style.Render("[ " + text + " ]")
			} else {
				text = fit("  "+text, width)
			}
		} else if selected {
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
	lines = append(lines, muted.Render("  "+safe(e.GroupPath)))
	if e.HasTOTP {
		lines = append(lines, muted.Render("  TOTP configured · commands to view/copy"))
	}
	if len(e.AttachmentNames) > 0 {
		lines = append(lines, muted.Render(fmt.Sprintf("  Attachments: %d", len(e.AttachmentNames))))
	}
	if e.HistoryCount > 0 {
		lines = append(lines, muted.Render(fmt.Sprintf("  History snapshots: %d", e.HistoryCount)))
	}
	if e.ExpiryEnabled && !e.Expires.IsZero() {
		label := "  Expires: "
		if !m.now().Before(e.Expires) {
			label = "  Expired: "
		}
		lines = append(lines, muted.Render(label+e.Expires.Local().Format("2006-01-02 15:04")))
	}
	lines = append(lines, m.themedRule(width))
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
	c := m.colors()
	return dialogrender.Render(dialogrender.Options{
		Mode: dialogrender.Box, Title: title, Body: body, Footer: footer,
		Backdrop: m.modalBackdrop(), Width: min(m.width-6, 82), ScreenWidth: m.width,
		Height: m.height, Compact: strings.EqualFold(m.settings.Appearance, "compact"), ASCII: m.settings.ASCII,
		Base: c.base, Accent: c.accent, Muted: c.muted, Line: c.lineStyle,
	})
}

func (m *Model) dialogTitle(title string) string {
	if !strings.EqualFold(m.settings.Appearance, "compact") {
		return strings.ToUpper(title)
	}
	return title
}

func (m *Model) unlockLines() []string {
	styles := m.colors()
	accent, muted, errorStyle := styles.accent, styles.muted, styles.errorStyle
	path := safe(m.path)
	pathWidth := max(1, min(m.width-6, 82)-4)
	if cells := lipgloss.Width(path); cells > pathWidth {
		path = "…" + ansi.Cut(path, cells-pathWidth+1, cells)
	}
	body := []string{muted.Render(path), ""}
	labels := []string{"Password", "Key file (optional)", "Hardware slot (optional, e.g. 1)", "Password authentication"}
	footer := "tab next · space toggle"
	if m.width < 70 {
		labels = []string{"Password", "Key file", "Hardware slot", "Password auth"}
		footer = "tab next"
	}
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
		body = append(body, accent.Render("Unlocking… esc cancels"))
	}
	if m.err != "" {
		body = append(body, errorStyle.Render(m.err))
	}
	return m.box("Unlock Vault", body, footer)
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
	return m.box("Open Vault", body, "tab path")
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
	return m.box("Keyboard Shortcuts", body[start:end], "")
}
