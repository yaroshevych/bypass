package ui

import (
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
		m.setNotice("session pin set · ctrl+k locks screen", false)
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
		if key == "tab" || key == "shift+tab" || key == "up" || key == "down" {
			delta := 1
			if key == "shift+tab" || key == "up" {
				delta = -1
			}
			m.pinFocus = (m.pinFocus + 2 + delta) % 2
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
	if key == "ctrl+l" {
		m.pinInput.clear()
		m.pinFullLock = true
		m.pinFocus = 0
		return nil
	}
	if key == "tab" || key == "shift+tab" || key == "up" || key == "down" {
		delta := 1
		if key == "shift+tab" || key == "up" {
			delta = -1
		}
		m.pinFocus = (m.pinFocus + 4 + delta) % 4
		return nil
	}
	if key == "enter" && m.pinFocus != 2 && m.pinFocus != 3 {
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
	if key == "enter" && m.pinFocus == 2 {
		m.pinInput.clear()
		m.pinFullLock = true
		m.pinFocus = 0
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
func (m *Model) pinLines() []string {
	c := m.colors()
	compact := strings.EqualFold(m.settings.Appearance, "compact")
	width := min(m.width-6, 52)
	actionWidth := max(1, width-4)
	var body []string
	if m.pinFullLock {
		body = []string{c.accent.Render(" Full lock all vaults?"), "", c.secondary.Render(" Unsaved drafts will be discarded."), c.secondary.Render(" Vault credentials will be required."), dialogrender.BodyRule,
			renderDialogAction("Lock All", "enter", actionWidth, compact, m.settings.ASCII, m.pinFocus == 0, c),
			renderDialogAction("Cancel", "esc", actionWidth, compact, m.settings.ASCII, m.pinFocus == 1, c),
		}
	} else {
		body = []string{c.secondary.Render(" Screen protected"), c.secondary.Render(" Vault sessions remain unlocked in memory."), ""}
		body = append(body, renderStackedInputField(inputFieldView{label: "Session PIN", value: &m.pinInput, secret: true, focused: m.pinFocus == 0, indent: 1}, actionWidth, compact, c)...)
		if m.pinError != "" {
			body = append(body, c.error.Render(" "+strings.ToLower(strings.TrimSuffix(m.pinError, "."))))
		}
		if delay := m.gate.NextAttempt().Sub(m.now()); delay > 0 {
			body = append(body, c.secondary.Render(fmt.Sprintf(" wait %ds", int(delay.Seconds())+1)))
		}
		body = append(body, dialogrender.BodyRule,
			renderDialogAction("Unlock", "enter", actionWidth, compact, m.settings.ASCII, m.pinFocus == 1, c),
			renderDialogAction("Full Lock", "ctrl+l", actionWidth, compact, m.settings.ASCII, m.pinFocus == 2, c),
			renderDialogAction("Quit", "", actionWidth, compact, m.settings.ASCII, m.pinFocus == 3, c),
		)
	}
	lines := m.boxWithBodyRowsWidthOnBackdrop("Bypass - Secrets Manager", body, "", len(body), width, make([]string, m.height))
	theme := colorsForTheme(m.settings.Theme)
	for i := range lines {
		lines[i] = restoreThemeResets(lines[i], theme.primary, theme.background)
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
	if m.dialog != nil {
		return m.dialogLines(), true
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
		body := []string{"p Password   u Username   w URL   n Notes"}
		if e := m.selected(); e != nil {
			for _, field := range e.Fields {
				if field.Name == "Email" {
					body = append(body, "e Email")
					break
				}
			}
		}
		return m.overlay("Jump to Field", body, "", min(66, m.width-4), -1, -1), true
	}
	return nil, false
}

func (m *Model) overlay(title string, body []string, footer string, width, x, y int, header ...dialogrender.Header) []string {
	c := m.colors()
	theme := colorsForTheme(m.settings.Theme)
	restoreResets := func(text string) string { return restoreThemeResets(text, theme.primary, theme.background) }
	headerContent, headerDivider, compactAccessory, compactCount := "", 0, "", ""
	compactAccessoryStyle := lipgloss.NewStyle()
	if len(header) > 0 {
		headerContent, headerDivider = header[0].Content, header[0].Divider
		compactAccessory, compactCount = header[0].Accessory, header[0].Count
		compactAccessoryStyle = header[0].AccessoryStyle
	}
	return dialogrender.Render(dialogrender.Options{
		Mode: dialogrender.Overlay, Title: title, HeaderContent: headerContent, HeaderDivider: headerDivider,
		CompactAccessory: compactAccessory, CompactAccessoryStyle: compactAccessoryStyle, CompactCount: compactCount, Body: body, StatusBar: footer,
		Backdrop: m.modalBackdrop(), Width: width, ScreenWidth: m.width,
		Height: m.height, X: x, Y: y, Compact: strings.EqualFold(m.settings.Appearance, "compact"), ASCII: m.settings.ASCII,
		Surface: c.primary, Accent: c.accent, Secondary: c.secondary, Pill: c.active, Divider: c.divider,
		RestoreResets: restoreResets,
	})
}

func (m *Model) modalBackdrop() []string {
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
	var background []string
	if m.ws.ActiveSession() == nil {
		background = m.welcomeLines()
	} else {
		background = m.workspaceLines()
	}
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
	if v != nil && (v.Query != "" || v.NavFilter != "" || v.DetailFilter != "") {
		query := v.Query
		if query == "" {
			query = v.NavFilter
		}
		if query == "" {
			query = v.DetailFilter
		}
		return readOnlyFooter("/ " + safe(query))
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
	if v.Query != "" || v.NavFilter != "" || v.DetailFilter != "" {
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
