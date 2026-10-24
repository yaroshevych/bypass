package ui

import (
	"bypass/internal/vault"
	"bypass/internal/workspace"
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

type extraMode uint8

const (
	extraTOTP extraMode = iota
	extraAttachments
	extraHistory
	extraHistoryEntry
)

type extraState struct {
	mode           extraMode
	phase          string
	sessionID      string
	entryUUID      string
	groupUUID      string
	selected       int
	offset         int
	fieldIndex     int
	valueOffset    int
	confirmProceed bool

	input      input
	action     string
	targetName string
	path       string
	err        string
	loading    bool
	cancel     context.CancelFunc
	requestID  uint64

	totpCode    []byte
	totpExpires time.Time
	totpPeriod  int

	history       []vault.Entry
	historyIndex  int
	revealedField string
	revealedValue []byte
	revealedUntil time.Time
}

type extrasTOTPMsg struct {
	state     *extraState
	requestID uint64
	result    vault.TOTPResult
	err       error
}

type extrasExportMsg struct {
	state *extraState
	err   error
}

func (m *Model) extraTarget() (*workspace.Session, *vault.Entry) {
	session := m.ws.ActiveSession()
	if session == nil || session.DB == nil {
		return nil, nil
	}
	entry := m.selected()
	if entry == nil {
		return session, nil
	}
	return session, entry
}

func (m *Model) currentExtraEntry(state *extraState) (*workspace.Session, *vault.Entry) {
	if state == nil {
		return nil, nil
	}
	session := m.ws.Sessions[state.sessionID]
	active := m.ws.ActiveSession()
	if session == nil || session.DB == nil || active == nil || active.ID != state.sessionID {
		return nil, nil
	}
	for i := range session.DB.Entries {
		entry := &session.DB.Entries[i]
		if entry.UUID == state.entryUUID && entry.GroupUUID == state.groupUUID {
			return session, entry
		}
	}
	return nil, nil
}

func (m *Model) clearExtras() {
	state := m.extras
	if state == nil {
		return
	}
	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}
	state.input.clear()
	clear(state.totpCode)
	state.totpCode = nil
	clear(state.revealedValue)
	state.revealedValue = nil
	state.history = nil
	m.extras = nil
}

func (m *Model) updateExtras(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case extrasTOTPMsg:
		if m.extras != msg.state || m.gate.Protected() || msg.state.requestID != msg.requestID {
			clear(msg.result.Code)
			return nil, true
		}
		state := m.extras
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		state.loading = false
		if _, entry := m.currentExtraEntry(state); entry == nil {
			clear(msg.result.Code)
			m.clearExtras()
			return nil, true
		}
		if msg.err != nil {
			clear(msg.result.Code)
			state.err = "Could not generate a TOTP code."
			return nil, true
		}
		clear(state.totpCode)
		state.totpCode = msg.result.Code
		msg.result.Code = nil
		state.totpPeriod = msg.result.PeriodSeconds
		if state.totpPeriod <= 0 {
			state.totpPeriod = 30
		}
		remaining := msg.result.RemainingSeconds
		if remaining <= 0 {
			remaining = state.totpPeriod
		}
		state.totpExpires = m.now().Add(time.Duration(remaining) * time.Second)
		return nil, true
	case extrasExportMsg:
		if m.extras != msg.state {
			return nil, true
		}
		state := m.extras
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		state.loading = false
		if msg.err != nil {
			state.err = "Attachment export failed. Existing files were not overwritten."
			return nil, true
		}
		state.phase = ""
		state.err = ""
		m.setNotice("Attachment exported with private file permissions.", false)
		return nil, true
	default:
		return nil, false
	}
}

func (m *Model) extrasTick(now time.Time) tea.Cmd {
	state := m.extras
	if state == nil {
		return nil
	}
	if state.mode == extraAttachments && state.phase == "working" && !m.writing {
		state.phase = ""
		state.loading = false
		if state.err == "" {
			state.err = "Attachment change failed. Review the notice and try again."
		}
	}
	if state.mode == extraHistoryEntry && len(state.revealedValue) != 0 && !now.Before(state.revealedUntil) {
		clear(state.revealedValue)
		state.revealedValue = nil
		state.revealedField = ""
		state.revealedUntil = time.Time{}
	}
	if state.mode != extraTOTP || state.loading || len(state.totpCode) == 0 || now.Before(state.totpExpires) {
		return nil
	}
	if session, entry := m.currentExtraEntry(state); session == nil || entry == nil {
		m.clearExtras()
		return nil
	}
	api, ok := m.backend.(interface {
		TOTP(context.Context, *vault.Database, vault.Credentials, string) (vault.TOTPResult, error)
	})
	if !ok {
		clear(state.totpCode)
		state.totpCode = nil
		state.err = "TOTP is not supported by this backend."
		return nil
	}
	clear(state.totpCode)
	state.totpCode = nil
	return m.requestTOTP(api, state)
}

func (m *Model) extrasKey(k tea.KeyMsg) (tea.Cmd, bool) {
	state := m.extras
	if state == nil {
		return nil, false
	}
	if m.gate.Protected() {
		m.clearExtras()
		return nil, true
	}
	switch k.String() {
	case "ctrl+c":
		m.clearExtras()
		return m.leaveEditor(func() tea.Cmd { m.Close(); return tea.Quit }), true
	case "ctrl+l":
		m.clearExtras()
		return m.lockCurrent(true), true
	case "L":
		m.clearExtras()
		return m.lockCurrent(true), true
	case "ctrl+k":
		m.clearExtras()
		return m.protectScreen(), true
	case "q":
		if state.mode != extraAttachments || state.phase != "path" {
			m.clearExtras()
			return nil, true
		}
	}
	if state.loading {
		if k.String() == "esc" {
			m.clearExtras()
		}
		return nil, true
	}
	if state.mode == extraHistoryEntry && k.String() != "v" {
		m.clearHistoryReveal(state)
	}
	if state.mode == extraAttachments && state.phase == "path" {
		return m.attachmentPathKey(k), true
	}
	if state.mode == extraAttachments && strings.HasPrefix(state.phase, "confirm-") {
		return m.attachmentConfirmKey(k), true
	}
	switch state.mode {
	case extraTOTP:
		switch k.String() {
		case "esc":
			m.clearExtras()
		case "c", "enter":
			if len(state.totpCode) != 0 && m.now().Before(state.totpExpires) {
				return m.copyValue(append([]byte(nil), state.totpCode...), "TOTP code"), true
			}
			return m.extrasTick(m.now()), true
		case "r":
			api, ok := m.backend.(interface {
				TOTP(context.Context, *vault.Database, vault.Credentials, string) (vault.TOTPResult, error)
			})
			if ok {
				clear(state.totpCode)
				state.totpCode = nil
				return m.requestTOTP(api, state), true
			}
		}
		return nil, true
	case extraAttachments:
		return m.attachmentListKey(k), true
	case extraHistory:
		switch k.String() {
		case "esc":
			m.clearExtras()
		case "up", "k":
			state.historyIndex = max(0, state.historyIndex-1)
			state.offset = viewportOffset(state.historyIndex, len(state.history), max(1, m.height-12), state.offset)
		case "down", "j":
			state.historyIndex = min(max(0, len(state.history)-1), state.historyIndex+1)
			state.offset = viewportOffset(state.historyIndex, len(state.history), max(1, m.height-12), state.offset)
		case "enter":
			state.mode = extraHistoryEntry
			state.fieldIndex = 0
			state.offset = 0
			state.valueOffset = 0
		case "c":
			m.setNotice("Open a history snapshot before copying a field.", false)
		case "v":
			m.setNotice("Open a history snapshot before revealing a field.", false)
		}
		return nil, true
	case extraHistoryEntry:
		entry := m.extraHistorySnapshot(state)
		if entry == nil {
			m.clearExtras()
			return nil, true
		}
		switch k.String() {
		case "esc", "left", "h":
			state.mode = extraHistory
			state.offset = viewportOffset(state.historyIndex, len(state.history), max(1, m.height-12), state.offset)
			state.valueOffset = 0
		case "up", "k":
			state.fieldIndex = max(0, state.fieldIndex-1)
			state.valueOffset = 0
		case "down", "j":
			state.fieldIndex = min(max(0, len(entry.Fields)-1), state.fieldIndex+1)
			state.valueOffset = 0
		case "pgup", "pgdown":
			if state.fieldIndex >= 0 && state.fieldIndex < len(entry.Fields) {
				lines := m.historyValueLines(*entry, entry.Fields[state.fieldIndex], state)
				maxOffset := max(0, len(lines)-m.historyValueCapacity())
				delta := m.historyValueCapacity()
				if k.String() == "pgup" {
					state.valueOffset = max(0, state.valueOffset-delta)
				} else {
					state.valueOffset = min(maxOffset, state.valueOffset+delta)
				}
			}
		case "c":
			return m.copyHistoryField(state), true
		case "v":
			return m.revealHistoryField(state), true
		}
		return nil, true
	}
	return nil, true
}

func (m *Model) extrasPointer(msg tea.MouseMsg) (tea.Cmd, bool) {
	state := m.extras
	if state == nil {
		return nil, false
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, true
	}
	switch state.mode {
	case extraHistory:
		capacity := max(1, m.height-12)
		start := viewportOffset(state.historyIndex, len(state.history), capacity, state.offset)
		visible := min(capacity, len(state.history)-start)
		top := max(0, (m.height-(visible+6))/2)
		index := msg.Y - (top + 4) + start
		boxWidth := min(max(20, min(78, m.width-4)), max(20, m.width-2))
		left := max(0, (m.width-boxWidth)/2)
		if msg.X >= left && msg.X < left+boxWidth && index >= start && index < start+visible {
			state.historyIndex = index
			state.offset = viewportOffset(index, len(state.history), capacity, state.offset)
		}
	case extraAttachments:
		if state.phase != "" {
			return nil, true
		}
		_, entry := m.currentExtraEntry(state)
		if entry == nil {
			return nil, true
		}
		capacity := m.attachmentListCapacity()
		items := len(entry.AttachmentNames) + 1
		start := viewportOffset(state.selected, items, capacity, state.offset)
		visible := min(capacity, items-start)
		extraRows := 0
		if !m.extrasWritable(m.ws.Sessions[state.sessionID]) {
			extraRows += 2
		}
		if state.err != "" {
			extraRows += 2
		}
		top := max(0, (m.height-(visible+6+extraRows))/2)
		index := msg.Y - (top + 4) + start
		boxWidth := min(max(20, min(76, m.width-4)), max(20, m.width-2))
		left := max(0, (m.width-boxWidth)/2)
		if msg.X >= left && msg.X < left+boxWidth && index >= start && index < start+visible {
			state.selected = index
			state.offset = viewportOffset(index, items, capacity, state.offset)
		}
	}
	return nil, true
}

func (m *Model) extrasLines() []string {
	state := m.extras
	if state == nil {
		return nil
	}
	switch state.mode {
	case extraTOTP:
		return m.totpLines(state)
	case extraAttachments:
		return m.attachmentLines(state)
	case extraHistory:
		return m.historyLines(state)
	case extraHistoryEntry:
		return m.historyEntryLines(state)
	default:
		return nil
	}
}

func (m *Model) confirmationChoice(state *extraState, body []string) []string {
	cancel := m.colors().active.Render("› Cancel")
	confirm := "  Confirm  "
	if state.confirmProceed {
		cancel = "  Cancel  "
		confirm = m.colors().active.Render("› Confirm")
	}
	return append(body, "", cancel+"   "+confirm)
}
