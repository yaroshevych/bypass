package ui

import (
	wipebuf "bypass/internal/secret"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"bypass/internal/privacy"
	dialogrender "bypass/internal/ui/dialog"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type appearanceSavedMsg struct{ err error }

func (m *Model) updateAdvanced(msg tea.Msg) (tea.Cmd, bool) {
	if msg, ok := msg.(appearanceSavedMsg); ok {
		if msg.err != nil {
			m.setNotice("appearance applied · save failed", true)
		} else {
			m.setNotice("preferences saved", false)
		}
		return nil, true
	}
	return nil, false
}
func (m *Model) persistPreferences() tea.Cmd {
	if m.saveSettings == nil {
		m.setNotice("appearance applied for this run", false)
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
		m.setNotice("unlock a vault to set a pin", true)
		return
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "pin-setup", title: "Set Session PIN"}
	if s := m.ws.ActiveSession(); s != nil {
		m.dialog.sessionID = s.ID
	}
	if protectAfter {
		m.dialog.selected = 0
		m.dialog.after = m.protectScreen
	}
}
func (m *Model) protectScreen() tea.Cmd {
	if !m.anyUnlocked() {
		m.setNotice("unlock a vault first", true)
		return nil
	}
	if !m.gate.Configured() {
		m.closePalette(false)
		m.jump = false
		m.configurePIN(true)
		return nil
	}
	m.concealForPIN()
	m.pinInput.clear()
	m.pinError = ""
	m.pinFullLock = false
	m.pinFocus = 0
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
	if k.String() == "tab" || k.String() == "shift+tab" || k.String() == "up" || k.String() == "down" {
		delta := 1
		if k.String() == "shift+tab" || k.String() == "up" {
			delta = -1
		}
		d.selected = (d.selected + 3 + delta) % 3
		return nil
	}
	if d.selected == 2 {
		if k.String() == "enter" {
			m.clearDialog()
		}
		return nil
	}
	if k.String() == "enter" {
		value := d.input.bytes()
		defer wipebuf.Wipe(value)
		if !validSessionPIN(value) {
			d.err = "Use 4–12 digits."
			return nil
		}
		if d.kind == "pin-setup" {
			d.secret = append([]byte(nil), value...)
			d.input.clear()
			d.kind = "pin-confirm"
			d.title = "Confirm Session PIN"
			d.selected = 0
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
		m.setNotice("session pin set · shift+p locks screen", false)
		if after != nil {
			return after()
		}
		return nil
	}
	if d.selected == 1 {
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
		if dialogLinearFocus(key, 2, &m.pinFocus, true) {
			return nil
		}
		if key == "esc" {
			m.pinFullLock = false
			m.pinFocus = 0
			return nil
		}
		if key == "enter" {
			if m.pinFocus == 1 {
				m.pinFullLock = false
				m.pinFocus = 0
				return nil
			}
			m.pinFullLock = false
			m.pinFocus = 0
			return m.lockCurrent(true)
		}
		return nil
	}
	if key == keyLockAll {
		m.pinInput.clear()
		m.pinFullLock = true
		m.pinFocus = 1
		return nil
	}
	if dialogLinearFocus(key, 4, &m.pinFocus, true) {
		return nil
	}
	if key == "enter" && m.pinFocus != 2 && m.pinFocus != 3 {
		value := m.pinInput.bytes()
		defer wipebuf.Wipe(value)
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
	if key == "enter" && m.pinFocus == 2 {
		m.pinInput.clear()
		m.pinFullLock = true
		m.pinFocus = 1
		return nil
	}
	if key == "enter" && m.pinFocus == 3 {
		m.Close()
		return tea.Quit
	}
	if m.pinFocus != 0 {
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
func (m *Model) pinContent() (opts dialogrender.Options, targets []int) {
	c := m.colors()
	compact := m.compactAppearance()
	width := min(m.width-6, 52)
	actionWidth := m.dialogInnerWidth(width)
	fieldStart, fieldEnd, actionBase, focus := -1, -1, 0, m.pinFocus
	var body []string
	var actions []dialogAction
	if m.pinFullLock {
		body = []string{c.accent.Render(" Full lock all vaults?"), "", c.secondary.Render(" Unsaved drafts will be discarded."), c.secondary.Render(" Vault credentials will be required.")}
		actions = []dialogAction{{"Lock All", "enter"}, {"Cancel", "esc"}}
	} else {
		body = []string{c.secondary.Render(" Screen protected"), c.secondary.Render(" Vault sessions remain unlocked in memory."), ""}
		fieldStart = len(body)
		body = append(body, renderStackedInputField(inputFieldView{label: "Session PIN", value: &m.pinInput, secret: true, focused: m.pinFocus == 0, indent: 1}, actionWidth, compact, c)...)
		fieldEnd = len(body)
		if delay := m.gate.NextAttempt().Sub(m.now()); delay > 0 {
			body = append(body, c.secondary.Render(fmt.Sprintf(" wait %ds", int(delay.Seconds())+1)))
		}
		actions = []dialogAction{{"Unlock", "enter"}, {"Full Lock", "shift+l"}, {"Quit", ""}}
		actionBase, focus = 1, m.pinFocus-1
	}
	message := ""
	if !m.pinFullLock {
		message = strings.ToLower(strings.TrimSuffix(m.pinError, "."))
	}
	targets = make([]int, len(body))
	for i := range targets {
		targets[i] = -1
	}
	for i := fieldStart; i >= 0 && i < fieldEnd; i++ {
		targets[i] = 0
	}
	opts = m.dialogOptions()
	opts.Title, opts.Body, opts.Error, opts.Width = "Bypass - Secrets Manager", body, safe(message), width
	for i, row := range m.dialogActionRows(actions, focus, actionWidth) {
		opts.Actions = append(opts.Actions, dialogrender.Action{Text: row, Target: actionBase + i})
	}
	return opts, targets
}

func (m *Model) pinPointer(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	opts, targets := m.pinContent()
	target := dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target
	if target < 0 {
		return nil
	}
	m.pinFocus = target
	if target == 0 && !m.pinFullLock {
		return nil
	}
	return m.pinKey(tea.KeyMsg{Type: tea.KeyEnter})
}

func (m *Model) pinLines() []string {
	opts, _ := m.pinContent()
	opts.Backdrop = make([]string, m.height)
	lines := dialogrender.Render(opts)
	theme := colorsForTheme(m.settings.Theme)
	for i := range lines {
		lines[i] = restoreThemeResets(lines[i], theme.primary, theme.background)
	}
	return lines
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
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y >= l.BodyY && msg.Y < l.BodyY+l.BodyHeight {
		for i, pane := range l.Panes {
			if i < len(l.Panes)-1 && msg.X == pane.X+pane.Width {
				m.dragging = i + 1
				return true
			}
		}
	}
	return false
}

func (m *Model) beginJump() tea.Cmd {
	if m.selected() == nil {
		m.setNotice("select an entry first", false)
		return nil
	}
	m.jump = true
	m.hideReveal()
	return nil
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
		m.setNotice("field not present", false)
		return nil
	}
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
	if view := m.handlersFor(m.activeLayer()).view; view != nil {
		return view(), true
	}
	return nil, false
}

func (m *Model) overlay(title string, body []string, footer string, width, x, y int, header ...dialogrender.Header) []string {
	return m.overlayError(title, body, footer, "", width, x, y, header...)
}

func (m *Model) overlayError(title string, body []string, footer, message string, width, x, y int, header ...dialogrender.Header) []string {
	opts := m.dialogOptions()
	opts.Mode, opts.Title = dialogrender.Overlay, title
	if len(header) > 0 {
		opts.Header = header[0]
	}
	opts.Body, opts.StatusBar, opts.Backdrop = body, footer, m.modalBackdrop()
	opts.Error = safe(message)
	opts.Width, opts.X, opts.Y = width, x, y
	return dialogrender.Render(opts)
}

func (m *Model) modalBackdrop() []string {
	var background []string
	if m.ws.ActiveSession() == nil {
		background = m.welcomeLines()
	} else {
		background = m.workspaceLines()
	}
	return m.dimBackdrop(background)
}

func (m *Model) dimBackdrop(background []string) []string {
	c := colorsForTheme(m.settings.Theme)
	// Blend both text and surface toward black. Explicit cell backgrounds keep
	// the scrim consistent in light themes and terminal multiplexers.
	shade := func(value string, factor float64) lipgloss.Color {
		var r, g, b int
		fmt.Sscanf(value, "#%02x%02x%02x", &r, &g, &b)
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", int(float64(r)*factor), int(float64(g)*factor), int(float64(b)*factor)))
	}
	scrimForeground := shade(c.secondary, .60)
	scrimBackground := shade(c.background, .78)
	scrim := lipgloss.NewStyle().Foreground(scrimForeground).Background(scrimBackground)
	background = padLines(background, m.height)
	for i := range background {
		line := scrim.Render(dimANSIColors(fit(background[i], m.width), .60, .58, c.background))
		background[i] = restoreThemeResets(line, string(scrimForeground), string(scrimBackground))
	}
	return background
}

func dimANSIColors(text string, foreground, background float64, surfaceColor string) string {
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
		for j := 0; j+4 < len(params); j++ {
			factor := 0.0
			switch params[j] {
			case "38":
				factor = foreground
			case "48":
				factor = background
				if params[j+1] == "2" && surfaceColor != "" {
					var r, g, b int
					fmt.Sscanf(surfaceColor, "#%02x%02x%02x", &r, &g, &b)
					red, redErr := strconv.Atoi(params[j+2])
					green, greenErr := strconv.Atoi(params[j+3])
					blue, blueErr := strconv.Atoi(params[j+4])
					if redErr == nil && greenErr == nil && blueErr == nil && red == r && green == g && blue == b {
						factor = .78 // Match the scrim's surface shade; dim colored pills more.
					}
				}
			}
			if factor == 0 || params[j+1] != "2" {
				continue
			}
			for channel := 0; channel < 3; channel++ {
				value, err := strconv.Atoi(params[j+2+channel])
				if err == nil {
					params[j+2+channel] = strconv.Itoa(int(float64(value) * factor))
				}
			}
			j += 4
		}
		out.WriteString("\x1b[")
		out.WriteString(strings.Join(params, ";"))
		out.WriteByte('m')
		i = end + 1
	}
	return out.String()
}

func (m *Model) footerText() string {
	readOnlyFooter := func(text string) string {
		if s := m.ws.ActiveSession(); s != nil && (s.ReadOnly || s.Stale) {
			return text + " · read only"
		}
		return text
	}
	v := m.ws.ActiveView()
	if m.searching {
		query := strings.TrimSpace(m.query.value())
		if query == "" {
			return readOnlyFooter("/ filter")
		}
		return readOnlyFooter("/ " + safe(query))
	}
	if v != nil && v.Query != "" {
		return readOnlyFooter("/ " + safe(v.Query))
	}
	text := "/ filter · ctrl+p commands"
	if m.searchError != "" {
		text += " · " + m.searchError
	}
	return readOnlyFooter(text)
}

func (m *Model) footerContext() string {
	if m.writing {
		return ""
	}
	if m.editor != nil {
		return "tab focus · ctrl+g generator"
	}
	if m.searching {
		return "enter apply · esc cancel"
	}
	v := m.ws.ActiveView()
	if v == nil {
		return ""
	}
	if v.Query != "" {
		return "esc cancel"
	}
	switch v.Focus {
	case 0:
		return ""
	case 1:
		return "n new · e edit"
	case 2:
		return "c copy · v reveal · e edit"
	}
	return ""
}

// PIN protection conceals secrets without canceling authenticated work or
// discarding drafts. Full vault locking performs cancellation and cleanup.
func (m *Model) concealForPIN() {
	m.hideReveal()
	m.dragging = 0
	if m.extras != nil {
		if m.extras.mode == extraTOTP {
			m.clearExtras()
		} else {
			m.clearHistoryReveal(m.extras)
		}
	}
}
