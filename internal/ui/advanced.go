package ui

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"keepassui/internal/privacy"
)

type appearanceSavedMsg struct{ err error }

func (m *Model) updateAdvanced(msg tea.Msg) (tea.Cmd, bool) {
	if msg, ok := msg.(appearanceSavedMsg); ok {
		if msg.err != nil {
			m.setNotice("Appearance applied; preferences could not be saved.", true)
		} else {
			m.setNotice("Preferences saved.", false)
		}
		return nil, true
	}
	return nil, false
}
func (m *Model) persistPreferences() tea.Cmd {
	if m.saveSettings == nil {
		m.setNotice("Appearance applied for this run.", false)
		return nil
	}
	settings := m.settings
	save := m.saveSettings
	return func() tea.Msg { return appearanceSavedMsg{save(settings)} }
}
func (m *Model) anyUnlocked() bool {
	for _, s := range m.ws.Sessions {
		if s.DB != nil {
			return true
		}
	}
	return false
}
func (m *Model) syncPrivacyState() {
	if !m.anyUnlocked() {
		m.gate.Clear()
		m.pinInput.clear()
		m.pinError = ""
		m.pinFullLock = false
		if m.dialog != nil && strings.HasPrefix(m.dialog.kind, "pin-") {
			m.clearDialog()
		}
	}
}
func (m *Model) configurePIN(protectAfter bool) {
	if !m.anyUnlocked() {
		m.setNotice("Unlock a vault before setting a session PIN.", true)
		return
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "pin-setup", title: "Set Session PIN"}
	if s := m.ws.ActiveSession(); s != nil {
		m.dialog.sessionID = s.ID
	}
	if protectAfter {
		m.dialog.after = m.protectScreen
	}
}
func (m *Model) protectScreen() tea.Cmd {
	m.clearExtras()
	m.clearLifecycle()
	if !m.anyUnlocked() {
		m.setNotice("Unlock a vault first.", true)
		return nil
	}
	if !m.gate.Configured() {
		m.closePalette(false)
		m.jump = false
		m.configurePIN(true)
		return nil
	}
	m.hideReveal()
	m.dragging = 0
	m.pinInput.clear()
	m.pinError = ""
	m.pinFullLock = false
	m.gate.Protect()
	return m.clearClipboard()
}
func validSessionPIN(value []byte) bool {
	if len(value) < 4 || len(value) > 12 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func (m *Model) pinSetupKey(k tea.KeyMsg) tea.Cmd {
	d := m.dialog
	if k.String() == "esc" {
		m.clearDialog()
		return nil
	}
	if k.String() == "enter" {
		value := []byte(d.input.value())
		defer clear(value)
		if !validSessionPIN(value) {
			d.err = "Use 4–12 digits."
			return nil
		}
		if d.kind == "pin-setup" {
			d.secret = append([]byte(nil), value...)
			d.input.clear()
			d.kind = "pin-confirm"
			d.title = "Confirm Session PIN"
			d.err = ""
			return nil
		}
		if subtle.ConstantTimeCompare(d.secret, value) != 1 {
			d.err = "PINs do not match."
			d.input.clear()
			return nil
		}
		if err := m.gate.Configure(value); err != nil {
			d.err = "Could not set session PIN."
			return nil
		}
		after := d.after
		m.clearDialog()
		m.setNotice("Session PIN set. Ctrl+K protects the screen.", false)
		if after != nil {
			return after()
		}
		return nil
	}
	if k.Type == tea.KeyRunes {
		for _, r := range k.Runes {
			if r < '0' || r > '9' {
				d.err = "Use digits only."
				return nil
			}
		}
	}
	if k.Type == tea.KeySpace {
		return nil
	}
	d.input.update(k)
	d.err = ""
	return nil
}
func (m *Model) pinKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		m.Close()
		return tea.Quit
	}
	if m.pinFullLock {
		if key == "esc" {
			m.pinFullLock = false
			return nil
		}
		if key == "enter" {
			m.pinFullLock = false
			return m.lockCurrent(true)
		}
		return nil
	}
	if key == "ctrl+l" {
		m.pinInput.clear()
		m.pinFullLock = true
		return nil
	}
	if key == "enter" {
		value := []byte(m.pinInput.value())
		defer clear(value)
		m.pinInput.clear()
		err := m.gate.Unlock(value, m.now())
		if err != nil {
			m.pinError = "Incorrect PIN."
			if errors.Is(err, privacy.ErrThrottled) {
				m.pinError = "Wait before trying again."
			}
			return nil
		}
		m.pinError = ""
		for _, s := range m.ws.Sessions {
			if s.DB != nil {
				s.LastActivity = m.now()
			}
		}
		return nil
	}
	if k.Type == tea.KeyRunes {
		for _, r := range k.Runes {
			if r < '0' || r > '9' {
				return nil
			}
		}
		if len(m.pinInput.text)+len(k.Runes) > 12 {
			return nil
		}
	}
	if k.Type == tea.KeySpace {
		return nil
	}
	m.pinInput.update(k)
	return nil
}
func (m *Model) pinLines() []string {
	c := m.colors()
	body := []string{c.accent.Render("◇"), "", c.accent.Render("Screen protected"), c.muted.Render("Vault sessions remain unlocked in memory."), "", c.accent.Render(m.pinInput.display(true) + "▏"), "", c.muted.Render("Enter unlock · Ctrl+L full lock · Ctrl+C quit")}
	if m.pinError != "" {
		body = append(body, c.errorStyle.Render(m.pinError))
	}
	if delay := m.gate.NextAttempt().Sub(m.now()); delay > 0 {
		body = append(body, c.muted.Render(fmt.Sprintf("Try again in %ds", int(delay.Seconds())+1)))
	}
	if m.pinFullLock {
		body = []string{c.accent.Render("Full lock all vaults?"), "", c.muted.Render("Unsaved drafts will be discarded."), c.muted.Render("Vault credentials will be required."), "", c.accent.Render("Enter full lock · Esc cancel")}
	}
	width := min(66, m.width-8)
	left := max(0, (m.width-width)/2)
	top := max(0, (m.height-len(body))/2)
	lines := make([]string, top)
	for _, line := range body {
		lines = append(lines, strings.Repeat(" ", left)+fit(line, width))
	}
	return lines
}

func (m *Model) resizeFocused(delta int) {
	v := m.ws.ActiveView()
	if v == nil || delta == 0 {
		return
	}
	layout := m.layout()
	if layout.Zoomed || layout.Mode == "tiny" || layout.Mode == "narrow" || len(layout.Panes) < 2 {
		return
	}
	navWidth := layout.Panes[0].Width
	if layout.Mode == "medium" {
		// Medium layout has one resizable divider: growing the focused sidebar
		// adds its width, while growing the main pane subtracts from it.
		if v.Focus == 0 {
			v.NavWidth = max(18, navWidth+delta)
		} else {
			v.NavWidth = max(18, navWidth-delta)
		}
		return
	}
	if layout.Mode != "wide" || len(layout.Panes) < 3 {
		return
	}
	detailWidth := layout.Panes[2].Width
	switch v.Focus {
	case 0:
		v.NavWidth = max(18, navWidth+delta)
	case 1:
		// Growing the center pane takes width from the details pane.
		v.DetailWidth = max(24, detailWidth-delta)
	case 2:
		v.DetailWidth = max(24, detailWidth+delta)
	}
}
func (m *Model) resizePointer(msg tea.MouseMsg) bool {
	v := m.ws.ActiveView()
	if v == nil || m.editor != nil || m.screen != browse {
		return false
	}
	l := m.layout()
	if l.Mode == "tiny" || l.Mode == "narrow" || l.Zoomed {
		return false
	}
	if m.dragging != 0 {
		if msg.Action == tea.MouseActionRelease {
			m.dragging = 0
			return true
		}
		if msg.Action == tea.MouseActionMotion {
			if m.dragging == 1 {
				v.NavWidth = max(18, msg.X)
			} else {
				v.DetailWidth = max(24, m.width-msg.X-1)
			}
			return true
		}
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y >= l.BodyY && msg.Y < m.height-2 {
		for i, pane := range l.Panes {
			if i < len(l.Panes)-1 && msg.X == pane.X+pane.Width {
				m.dragging = i + 1
				return true
			}
		}
	}
	return false
}

func (m *Model) jumpKey(k tea.KeyMsg) tea.Cmd {
	if k.String() == "esc" {
		m.jump = false
		return nil
	}
	v := m.ws.ActiveView()
	if v == nil {
		m.jump = false
		return nil
	}
	target := k.String()
	switch target {
	case "g":
		v.Focus = 0
		m.jump = false
		return nil
	case "i":
		v.Focus = 1
		m.jump = false
		return nil
	case "d":
		v.Focus = 2
		m.jump = false
		return nil
	}
	name := map[string]string{"p": "Password", "e": "Email", "u": "UserName", "w": "URL", "n": "Notes"}[target]
	if name == "" {
		return nil
	}
	e := m.selected()
	if e == nil {
		return nil
	}
	found := name == "Password" || name == "UserName" || name == "URL" || name == "Notes"
	for _, f := range e.Fields {
		if f.Name == name {
			found = true
		}
	}
	if !found {
		m.setNotice("That field is not present.", false)
		return nil
	}
	v.DetailFilter = ""
	for i, field := range m.fieldNames() {
		if field == name {
			v.FieldIndex = i
			break
		}
	}
	v.Focus = 2
	v.DetailManual = false
	m.jump = false
	return nil
}

func (m *Model) advancedScreen() ([]string, bool) {
	if m.gate.Protected() {
		return m.pinLines(), true
	}
	if m.lifecycle != nil {
		return m.lifecycleLines(), true
	}
	if m.extras != nil {
		return m.extrasLines(), true
	}
	if m.grand {
		return m.grandLines(), true
	}
	if m.palette != nil {
		return m.paletteLines(), true
	}
	if m.jump {
		return m.overlay("Jump to…", []string{"g Groups   i Entries   d Details", "p Password   u Username   w URL   n Notes", "e Email (when present)"}, "Choose target · Esc cancel", min(66, m.width-4), -1, -1), true
	}
	return nil, false
}

func (m *Model) overlay(title string, body []string, footer string, width, x, y int) []string {
	c := m.colors()
	width = min(max(20, width), max(20, m.width-2))
	maxBody := max(1, m.height-8)
	if len(body) > maxBody {
		body = body[:maxBody]
	}
	border := "│"
	horizontal := "─"
	tl, tr, bl, br := "┌", "┐", "└", "┘"
	if m.settings.ASCII {
		border = "|"
		horizontal = "-"
		tl, tr, bl, br = "+", "+", "+", "+"
	}
	wrap := func(text string) string {
		return c.lineStyle.Render(border) + c.base.Render(" "+fit(text, width-4)+" ") + c.lineStyle.Render(border)
	}
	box := []string{c.lineStyle.Render(tl + strings.Repeat(horizontal, width-2) + tr), wrap(c.accent.Render(title)), wrap(c.muted.Render(strings.Repeat(horizontal, width-4)))}
	for _, line := range body {
		box = append(box, wrap(line))
	}
	box = append(box, wrap(c.muted.Render(footer)), c.lineStyle.Render(bl+strings.Repeat(horizontal, width-2)+br))
	if x < 0 {
		x = (m.width - width) / 2
	}
	x = min(max(0, x), max(0, m.width-width))
	if y < 0 {
		y = (m.height - len(box)) / 2
	}
	y = min(max(0, y), max(0, m.height-len(box)))
	var background []string
	if m.ws.ActiveSession() == nil {
		background = m.welcomeLines()
	} else {
		background = m.workspaceLines()
	}
	background = padLines(background, m.height)
	for i := range background {
		background[i] = c.muted.Render(fit(ansi.Strip(background[i]), m.width))
	}
	for i, line := range box {
		row := y + i
		if row >= m.height {
			break
		}
		original := fit(background[row], m.width)
		background[row] = ansi.Cut(original, 0, x) + line + ansi.Cut(original, x+width, m.width)
	}
	return background
}

func (m *Model) footerText() string {
	v := m.ws.ActiveView()
	text := "  Ctrl+P commands · o open · ? help"
	if v != nil {
		switch v.Focus {
		case 0:
			text = "  ↑↓ views · Enter entries · / filter · Ctrl+P commands"
		case 1:
			text = "  Enter details · n new · e edit · / filter · Ctrl+P commands"
		case 2:
			text = "  Tab fields · c copy · v reveal · e edit · Ctrl+P commands"
		}
		if len(m.ws.Views) > 1 {
			text += " · 1–9 tabs"
		}
		if v.Query != "" {
			text = "  Fuzzy search: " + safe(v.Query) + " · Esc clear · Ctrl+P commands"
		}
		if v.NavFilter != "" {
			text = "  Views filter: " + safe(v.NavFilter) + " · Esc clear"
		}
		if v.DetailFilter != "" {
			text = "  Fields filter: " + safe(v.DetailFilter) + " · Esc clear"
		}
	}
	if m.searching {
		label := []string{"Views", "Entries", "Fields"}[min(max(0, m.searchColumn), 2)]
		text = "  " + label + " search: " + safe(m.query.value()) + "▏ · Enter apply · Esc cancel"
	}
	if m.searchError != "" {
		text = "  " + m.searchError
	}
	return text
}
