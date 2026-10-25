package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func (m *Model) View() string {
	// Rendering may ask the browser for filtered entries. Keep that read path
	// from populating its optional cache so View remains side-effect free.
	searchCache := m.searchCache
	defer func() { m.searchCache = searchCache }()
	w, h := max(1, m.width), max(1, m.height)
	styles := m.colors()
	primary, accent := styles.primary, styles.accent
	secondary := styles.secondary
	var lines []string
	modal := false
	if m.layout().Mode == "tiny" {
		minimum := "Resize to at least 48 × 14"
		if m.settings.ASCII {
			minimum = "Resize to at least 48x14"
		}
		lines = []string{accent.Render("bypass"), "Terminal too small", secondary.Render(minimum), "q quit · ctrl+c quit"}
		if m.editor != nil && m.editor.dirty() {
			lines[3] = "Draft kept · resize · ctrl+c quits"
		}
	} else if advanced, ok := m.advancedScreen(); ok {
		lines = advanced
		modal = true
	} else if m.dialog != nil {
		lines = m.dialogLines()
		modal = true
	} else if m.help {
		lines = m.helpLines()
		modal = true
	} else if m.screen == chooseFile {
		lines = m.pickerLines()
		modal = true
	} else if m.screen == unlock {
		lines = m.unlockLines()
		modal = true
	} else if m.ws.ActiveSession() == nil {
		lines = m.welcomeLines()
		modal = true
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
	view := primary.Render(strings.Join(lines, "\n"))
	if !modal {
		colors := colorsForTheme(m.settings.Theme)
		view = restoreThemeResets(view, colors.primary, colors.background)
	}
	return view
}

func restoreThemeResets(text, foreground, background string) string {
	parse := func(hex string) []string {
		value := strings.TrimPrefix(hex, "#")
		if len(value) != 6 {
			return []string{"0", "0", "0"}
		}
		channels := make([]string, 3)
		for i := range channels {
			v, err := strconv.ParseInt(value[i*2:i*2+2], 16, 32)
			if err != nil {
				return []string{"0", "0", "0"}
			}
			channels[i] = strconv.FormatInt(v, 10)
		}
		return channels
	}
	fg, bg := parse(foreground), parse(background)
	restoreFG := []string{"38", "2", fg[0], fg[1], fg[2]}
	restoreBG := []string{"48", "2", bg[0], bg[1], bg[2]}
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '\x1b' || i+1 >= len(text) || text[i+1] != '[' {
			out.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i+2:], 'm')
		if end < 0 {
			out.WriteByte(text[i])
			i++
			continue
		}
		end += i + 2
		params := strings.Split(text[i+2:end], ";")
		if len(params) == 1 && params[0] == "" {
			params[0] = "0"
		}
		var repaired []string
		for i := 0; i < len(params); i++ {
			param := params[i]
			if (param == "38" || param == "48") && i+1 < len(params) {
				componentCount := 0
				switch params[i+1] {
				case "2":
					componentCount = 3
				case "5":
					componentCount = 1
				}
				if componentCount > 0 && i+1+componentCount < len(params) {
					repaired = append(repaired, params[i:i+2+componentCount]...)
					i += 1 + componentCount
					continue
				}
			}
			switch param {
			case "0":
				repaired = append(repaired, "0")
				repaired = append(repaired, restoreFG...)
				repaired = append(repaired, restoreBG...)
			case "39":
				repaired = append(repaired, restoreFG...)
			case "49":
				repaired = append(repaired, restoreBG...)
			default:
				repaired = append(repaired, param)
			}
		}
		out.WriteString("\x1b[")
		out.WriteString(strings.Join(repaired, ";"))
		out.WriteByte('m')
		i = end + 1
	}
	return out.String()
}

func (m *Model) welcomeLines() []string {
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	headerIndent := "  "
	if !compact {
		headerIndent = " "
	}
	rows := make([]listComponentRow, 0, m.landingBodyRows())
	recentCount := m.landingRecentCount()
	if recentCount > 0 {
		rows = append(rows, listComponentRow{kind: listComponentHeader, item: -1, text: headerIndent + "RECENT ", divider: true})
		for i := 0; i < recentCount; i++ {
			name := safe(vaultLabel(m.recent[i])) + " · " + safe(filepath.Dir(m.recent[i]))
			if !compact {
				name = " " + name
			}
			rows = append(rows, listComponentRow{
				kind: listComponentItem, item: i, text: name, selected: m.landingFocus == i,
				activeFill: true, pillSelected: true,
			})
		}
		rows = append(rows, listComponentRow{kind: listComponentSpacer, item: -1})
	}
	rows = append(rows, listComponentRow{kind: listComponentHeader, item: -1, text: headerIndent + "VAULTS ", divider: true})
	openFocus := recentCount
	openLabel, openShortcut := "Open", "ctrl+o"
	createLabel, createShortcut := "Create", "ctrl+n"
	if !compact {
		openLabel, openShortcut = " Open", "ctrl+o "
		createLabel, createShortcut = " Create", "ctrl+n "
	}
	rows = append(rows, listComponentRow{kind: listComponentItem, item: openFocus, text: openLabel, right: openShortcut, selected: m.landingFocus == openFocus, activeFill: true, pillSelected: true})
	if m.canCreateVault() {
		createFocus := openFocus + 1
		rows = append(rows, listComponentRow{kind: listComponentItem, item: createFocus, text: createLabel, right: createShortcut, selected: m.landingFocus == createFocus, activeFill: true, pillSelected: true})
	}
	component := m.renderListComponent(rows, listComponentOptions{
		width: max(1, min(m.width-6, 82)-4), compact: compact, ascii: m.settings.ASCII, focused: true, sidebar: true,
	})
	body := make([]string, 0, len(component))
	for _, row := range component {
		body = append(body, row.text)
	}
	statusBar := ""
	if m.landingFocus < recentCount {
		statusBar = m.recentRemovalStatusBar()
	}
	lines := m.boxWithBodyRowsOnBackdrop("Bypass - Secrets Manager", body, statusBar, m.landingBodyRows(), make([]string, m.height))
	if len(lines) > 0 {
		theme := colorsForTheme(m.settings.Theme)
		lines[len(lines)-1] = restoreThemeResets(m.footerLine("", "q quit"), theme.primary, theme.background)
	}
	return lines
}

func (m *Model) recentRemovalStatusBar() string {
	statusBar := "delete remove recent"
	if strings.EqualFold(m.settings.Appearance, "compact") {
		statusBar += "  "
	} else {
		statusBar += " "
	}
	return statusBar
}

func (m *Model) landingBodyRows() int {
	contentRows := 1 + 1 // VAULTS heading and Open action.
	if m.canCreateVault() {
		contentRows++
	}
	if count := m.landingRecentCount(); count > 0 {
		contentRows += count + 2 // RECENT heading, entries, and section spacing.
	}
	return min(m.landingMaxBodyRows(), contentRows+1) // Keep one status-bar row reserved.
}

func (m *Model) landingRecentCount() int {
	if len(m.recent) == 0 {
		return 0
	}
	actions := 1
	if m.canCreateVault() {
		actions++
	}
	available := m.landingMaxBodyRows() - actions - 4 // Status row, both headings, and section spacing.
	return min(5, min(len(m.recent), max(0, available)))
}

func (m *Model) landingMaxBodyRows() int {
	return min(11, max(1, m.height-4))
}

func (m *Model) landingMaxFocus() int {
	maxFocus := m.landingRecentCount()
	if m.canCreateVault() {
		maxFocus++
	}
	return maxFocus
}

func (m *Model) activateLandingFocus() tea.Cmd {
	recentCount := m.landingRecentCount()
	if m.landingFocus < recentCount {
		m.beginUnlock(m.recent[m.landingFocus])
		return nil
	}
	if m.landingFocus == recentCount {
		return m.openPicker()
	}
	if m.canCreateVault() && m.landingFocus == recentCount+1 {
		return m.openCreateVault()
	}
	return nil
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
	accent, secondary, active := styles.accent, styles.secondary, styles.active
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
	lines := []string{accent.Render(fit("  "+name, max(0, w-lipgloss.Width(right)))) + secondary.Render(right)}
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
				style := styles.primary.Bold(true)
				if m.tabHovered && m.hoverTab == span.Index {
					style = style.Foreground(styles.accent.GetForeground())
				}
				label = style.Render(label)
			}
		}
		tabs = append(tabs, label)
	}
	separator := styles.divider.Render("│")
	if compact {
		separator = styles.inactive.Render(" ")
	}
	tabLine := strings.Join(tabs, separator)
	if !compact && !m.canAddTab() && len(tabs) > 0 {
		tabLine += separator
	}
	if compact && !m.settings.ASCII {
		edge := styles.primary.Foreground(styles.inactive.GetBackground())
		tabLine = edge.Render("\ue0b6") + tabLine + edge.Render("\ue0b4")
	}
	lines = append(lines, fit(tabLine, w))
	if !compact {
		lines = append(lines, m.tabRule(w, false))
	}
	if s.DB == nil {
		home, _ := os.UserHomeDir()
		lines = append(lines, "", accent.Render("  Vault locked"), secondary.Render("  "+displayVaultPath(s.Path, home)), "", secondary.Render("  enter to unlock"), secondary.Render("  ctrl+o open another vault"))
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
	return m.colors().divider.Render(string(line))
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
	return m.colors().divider.Render(string(line))
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
	return m.colors().divider.Render(string(line))
}

func (m *Model) footerBadge() string {
	c := m.colors()
	style := c.active.Foreground(c.primary.GetBackground()).Background(c.accent.GetForeground())
	badge := style.Render(" bypass ")
	if strings.EqualFold(m.settings.Appearance, "compact") {
		if m.settings.ASCII {
			badge = style.Render("[ bypass ]")
		} else {
			badge = renderPill("bypass", 10, style, c.primary)
		}
	}
	return badge
}

func (m *Model) footerBadgeJoinedTo(surface lipgloss.Style) string {
	c := m.colors()
	style := c.active.Foreground(c.primary.GetBackground()).Background(c.accent.GetForeground())
	if m.settings.ASCII {
		return m.footerBadge()
	}
	return renderPillWithRightSurface("bypass", 10, style, c.primary, surface)
}

func (m *Model) footerLine(text string, contextual ...string) string {
	return m.footerLineWithLeftStyle(text, m.colors().secondary, contextual...)
}

func (m *Model) searchFooterLine(text, contextual string, applied bool) string {
	c := m.colors()
	style := c.inactive
	if applied {
		style = c.primary.Bold(true).Background(c.inactive.GetBackground())
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
		edge := c.primary.Foreground(style.GetBackground())
		control := fit(edge.Render("\ue0b6")+content+edge.Render("\ue0b4"), width)
		badge := m.footerBadge() + " "
		if m.settings.ASCII {
			control = fit(style.Render("[")+content+style.Render("]"), width)
		} else {
			badge = m.footerBadgeJoinedTo(c.inactive) + c.inactive.Render(" ")
			control = fit(c.inactive.Render(" ")+content+edge.Render("\ue0b4"), width)
		}
		left := badge + control
		if contextual != "" {
			left += " " + c.secondary.Render(lowercaseShortcutLabels(contextual))
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
		left += " " + c.secondary.Render(lowercaseShortcutLabels(contextual))
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
		right := c.secondary.Render(lowercaseShortcutLabels(contextual[0]))
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
		body[0] = styles.secondary.Render(fit(m.layoutBreadcrumb(layout), m.width))
	}
	if layout.Zoomed && len(body) > 1 {
		rule := "─"
		if m.settings.ASCII {
			rule = "-"
		}
		body[1] = styles.divider.Render(strings.Repeat(rule, m.width))
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
	if layout.Zoomed {
		startRow = 2
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
		sep := styles.divider.Render("│")
		if m.settings.ASCII {
			sep = styles.divider.Render("|")
		}
		var joined strings.Builder
		for i, part := range parts {
			if i > 0 {
				junction := sep
				right := ansi.Strip(part)
				leftRule := strings.HasSuffix(ansi.Strip(parts[i-1]), "─")
				rightRule := strings.TrimSpace(right) != "" && strings.Trim(right, "─") == ""
				if leftRule && rightRule {
					junction = styles.divider.Render("┼")
				} else if leftRule {
					junction = styles.divider.Render("┤")
				} else if rightRule {
					junction = styles.divider.Render("├")
				}
				if m.settings.ASCII && (strings.HasSuffix(ansi.Strip(parts[i-1]), "-") || rightRule) {
					junction = styles.divider.Render("+")
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
		return "  " + location + "  ·  esc restore layout"
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
	v := m.ws.ActiveView()
	items := m.navigation()
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	rows, start := m.sidebarViewport(height)
	componentRows := make([]listComponentRow, 0, min(height, len(rows)-start))
	for r := start; r < len(rows) && len(componentRows) < height; r++ {
		row := rows[r]
		if row.index < 0 {
			if row.label == "" {
				componentRows = append(componentRows, listComponentRow{kind: listComponentSpacer, item: -1})
			} else {
				label := "  " + strings.ToUpper(row.label) + " "
				componentRows = append(componentRows, listComponentRow{kind: listComponentHeader, item: -1, text: label, divider: row.label != "Views" || compact})
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
		if compact {
			selectedText := text
			componentRows = append(componentRows, listComponentRow{kind: listComponentItem, item: i, text: text, selectedText: selectedText, count: fmt.Sprintf("%d", countValue), selected: i == v.NavIndex})
			continue
		}
		if row.count {
			text = "  " + indent + fmt.Sprintf("%d entries", countValue)
			componentRows = append(componentRows, listComponentRow{kind: listComponentItem, item: i, text: text, countLine: true, selected: i == v.NavIndex})
			continue
		}
		plain := styles.primary.Bold(true).Render(fit("  "+text, width))
		selectedText := styles.inactive.Bold(true).Render(fit("  "+text, width))
		if focused {
			selectedText = styles.active.Bold(true).Render(fit("  "+text, width))
		}
		componentRows = append(componentRows, listComponentRow{kind: listComponentItem, item: i, text: plain, selectedText: selectedText, selected: i == v.NavIndex})
	}
	component := m.renderListComponent(componentRows, listComponentOptions{width: width, height: height, compact: compact, ascii: m.settings.ASCII, focused: focused, sidebar: true})
	lines := make([]string, len(component))
	for i, row := range component {
		lines[i] = row.text
	}
	return lines
}

func (m *Model) entryLines(width, height int, focused bool) []string {
	styles := m.colors()
	secondary, active, inactive := styles.secondary, styles.active, styles.inactive
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
	heading := row("TITLE", "USERNAME", "WEBSITE")
	if compact {
		heading = fit("  "+heading, width)
	}
	heading = secondary.Render(heading)
	lines := []string{heading}
	entries := m.entries()
	v := m.ws.ActiveView()
	if len(entries) == 0 {
		text := "No entries in this view"
		if v.Query != "" {
			text = "No matches"
		}
		lines = append(lines, "", secondary.Render(" "+text))
		if v.Query != "" {
			lines = append(lines, secondary.Render(" esc clears filter"))
		} else {
			text := " n creates an entry"
			if s := m.ws.ActiveSession(); s != nil && s.ReadOnly {
				text = " Read only · R enables writes"
			}
			lines = append(lines, secondary.Render(text))
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
	primary, secondary, active := styles.primary, styles.secondary, styles.active
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	e := m.selected()
	if e == nil {
		return []string{secondary.Render("  Select an entry")}, nil
	}
	lines := newItemHeader(e).render(styles, width, m.now(), false, false)
	names := m.fieldNames()
	var positions []int
	for index, name := range names {
		positions = append(positions, len(lines)+2)
		label := name
		if name == "UserName" {
			label = "Username"
		}
		label = strings.ToUpper(safe(label))
		labelLine := secondary.Render("  " + label)
		focused := false
		if v := m.ws.ActiveView(); v != nil && v.Focus == 2 && index == v.FieldIndex {
			focused = true
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
		var valueLines []string
		for _, paragraph := range strings.Split(value, "\n") {
			wrapped := ansi.Wrap(safe(paragraph), max(1, width-4), "")
			valueLines = append(valueLines, strings.Split(wrapped, "\n")...)
		}
		multiline := strings.Contains(value, "\n")
		if focused && compact && multiline {
			for _, line := range valueLines {
				valueLine := "  " + line
				lines = append(lines, active.Bold(true).Render(fit(ansi.Truncate(valueLine, width, "…"), width)))
			}
			continue
		}
		for _, line := range valueLines {
			valueLine := "  " + line
			if compact {
				if focused {
					// The pill supplies its own leading inset.
					valueLine = line
				} else {
					valueLine = "  " + line
				}
			}
			if focused {
				if compact && !m.settings.ASCII {
					lines = append(lines, renderPill(valueLine, width, active.Bold(true), primary))
				} else if compact {
					lines = append(lines, active.Bold(true).Render(fit("[ "+ansi.Truncate(valueLine, max(0, width-4), "…")+" ]", width)))
				} else {
					lines = append(lines, active.Bold(true).Render(fit(valueLine, width)))
				}
			} else {
				lines = append(lines, primary.Render(valueLine))
			}
		}
	}
	if len(e.Tags) > 0 {
		lines = append(lines, "", secondary.Render("  Tags: "+safe(strings.Join(e.Tags, ", "))))
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

func (m *Model) box(title string, body []string, statusBar string, header ...dialogrender.Header) []string {
	return m.boxWithBodyRows(title, body, statusBar, 0, header...)
}

func (m *Model) boxWithBodyRows(title string, body []string, statusBar string, bodyRows int, header ...dialogrender.Header) []string {
	return m.boxWithBodyRowsOnBackdrop(title, body, statusBar, bodyRows, m.modalBackdrop(), header...)
}

func (m *Model) boxWithBodyRowsOnBackdrop(title string, body []string, statusBar string, bodyRows int, backdrop []string, header ...dialogrender.Header) []string {
	c := m.colors()
	theme := colorsForTheme(m.settings.Theme)
	restoreResets := func(text string) string { return restoreThemeResets(text, theme.primary, theme.background) }
	headerContent, headerDivider := "", 0
	if len(header) > 0 {
		headerContent, headerDivider = header[0].Content, header[0].Divider
	}
	return dialogrender.Render(dialogrender.Options{
		Mode: dialogrender.Box, Title: title, HeaderContent: headerContent, HeaderDivider: headerDivider, Body: body, BodyRows: bodyRows, StatusBar: statusBar,
		Backdrop: backdrop, Width: min(m.width-6, 82), ScreenWidth: m.width,
		Height: m.height, Compact: strings.EqualFold(m.settings.Appearance, "compact"), ASCII: m.settings.ASCII,
		Surface: c.primary, Accent: c.accent, Secondary: c.secondary, Pill: c.active, Divider: c.divider,
		RestoreResets: restoreResets,
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
	accent, secondary, error := styles.accent, styles.secondary, styles.error
	path := safe(m.path)
	pathWidth := max(1, min(m.width-6, 82)-4)
	if cells := lipgloss.Width(path); cells > pathWidth {
		path = "…" + ansi.Cut(path, cells-pathWidth+1, cells)
	}
	body := []string{secondary.Render(path)}
	if m.width >= 70 {
		body = append(body, "")
	}
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
		body = append(body, error.Render(m.err))
	}
	return m.box("Unlock Vault", body, footer)
}

func (m *Model) pickerLines() []string {
	styles := m.colors()
	secondary, error := styles.secondary, styles.error
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	listWidth := max(1, min(m.width-6, 82)-4)
	pathIndent := " "
	if compact {
		pathIndent = "  "
	}
	pathText := pathIndent + m.inputs[0].display(false)
	pathLine := secondary.Render(pathText)
	if m.pickerEditing {
		if compact {
			pathLine = renderPillWithCaret(m.inputs[0].display(false), "▏", listWidth, styles.inactive, styles.active, styles.primary)
		} else {
			caret := styles.active.Render("▏")
			visible := ansi.Truncate(pathText, max(0, listWidth-2), "…")
			padding := max(0, listWidth-lipgloss.Width(visible)-2)
			pathLine = styles.inactive.Render(visible) + caret + styles.inactive.Render(strings.Repeat(" ", padding+1))
		}
	}
	headerIndent := "  "
	if !compact {
		headerIndent = " "
	}
	rows := make([]listComponentRow, 0, m.pickerFixedRows())
	recentCount := m.pickerRecentCount()
	if recentCount > 0 {
		rows = append(rows, listComponentRow{kind: listComponentHeader, item: -1, text: headerIndent + "RECENT ", divider: true})
		for i := 0; i < recentCount; i++ {
			path := m.recent[i]
			name := safe(vaultLabel(path)) + " · " + safe(filepath.Dir(path))
			if !compact {
				name = " " + name
			}
			rows = append(rows, listComponentRow{kind: listComponentItem, item: -1, text: name, selected: m.pickerRecent && i == m.pickerRecentIndex, activeFill: true, pillSelected: true})
		}
	}
	rows = append(rows,
		listComponentRow{kind: listComponentHeader, item: -1, text: headerIndent + "PATH ", divider: true},
		listComponentRow{kind: listComponentItem, item: -1, text: pathLine},
		listComponentRow{kind: listComponentSpacer, item: -1},
	)
	rows = append(rows, listComponentRow{kind: listComponentHeader, item: -1, text: headerIndent + "BROWSE ", divider: true})
	start, capacity := m.fileViewport()
	browseRows := 0
	for i := start; i < len(m.files) && i < start+capacity; i++ {
		f := m.files[i]
		name := safe(f.name)
		if f.dir {
			name += "/"
		}
		if !compact {
			name = " " + name
		}
		rows = append(rows, listComponentRow{kind: listComponentItem, item: i, text: name, selected: !m.pickerEditing && !m.pickerRecent && i == m.fileIndex, activeFill: true, pillSelected: true})
		browseRows++
	}
	if len(m.files) == 0 && capacity > 0 {
		rows = append(rows, listComponentRow{kind: listComponentItem, item: -1, text: secondary.Render("No vault files here. Enter another path.")})
		browseRows++
	}
	for browseRows < capacity {
		rows = append(rows, listComponentRow{kind: listComponentSpacer, item: -1})
		browseRows++
	}
	component := m.renderListComponent(rows, listComponentOptions{
		width: listWidth, compact: compact, ascii: m.settings.ASCII, focused: !m.pickerEditing, sidebar: true,
	})
	body := make([]string, 0, len(component)+2)
	for _, row := range component {
		body = append(body, row.text)
	}
	if m.err != "" {
		body = append(body, error.Render(m.err))
	}
	return m.boxWithBodyRows("Open Vault", body, m.pickerStatusBar(), m.pickerBodyRows())
}

func (m *Model) helpLines() []string {
	title := "Keyboard Shortcuts"
	actions := m.helpActionLines()
	count := fmt.Sprintf("%d shortcuts", len(actions))
	body := append([]string{""}, actions...)
	if m.helpQuery.value() == "" {
		body = append(body, "", m.colors().secondary.Render("Encrypted backup before every write"))
		if m.backendVersion != "" {
			body = append(body, m.colors().secondary.Render("Backend: "+m.backendVersion))
		}
	}
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	if compact {
		count = fmt.Sprintf("%d", len(m.helpActionLines()))
	}
	width := min(86, m.width-4)
	if compact {
		width = min(86, m.width-2)
	}
	filterWidth := max(1, width-4)
	header := dialogrender.FilterHeader(title, safe(m.helpQuery.value()), m.helpQuery.cursor, "type to filter", count, filterWidth, compact, m.settings.ASCII, filterDialogStyles(m.colors()))
	capacity := max(1, m.height-8)
	start := min(m.helpOffset, max(0, len(body)-capacity))
	end := min(len(body), start+capacity)
	return m.overlay(title, body[start:end], "", width, -1, -1, header)
}
