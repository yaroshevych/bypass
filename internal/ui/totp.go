package ui

import (
	"bypass/internal/session"
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openTOTP() tea.Cmd {
	m.recordItemUse()
	m.hideReveal()
	session, entry := m.extraTarget()
	if session == nil || entry == nil {
		m.setNotice("select an entry first", true)
		return nil
	}
	if !entry.HasTOTP {
		m.setNotice("no totp for this entry", false)
		return nil
	}
	if session.Core == nil || !session.Core.CanTOTP() {
		m.setNotice("backend doesn't support totp", true)
		return nil
	}
	m.clearExtras()
	state := &extraState{mode: extraTOTP, sessionID: session.ID, entryUUID: entry.UUID, groupUUID: entry.GroupUUID, loading: true}
	m.extras = state
	return m.requestTOTP(session.Core, state)
}

func (m *Model) requestTOTP(core *session.Session, state *extraState) tea.Cmd {
	return m.requestTOTPAttempt(core, state, 0)
}

func (m *Model) requestTOTPAttempt(core *session.Session, state *extraState, retry int) tea.Cmd {
	session, entry := m.currentExtraEntry(state)
	if session == nil || entry == nil {
		state.loading = false
		state.err = "Entry is no longer available."
		return nil
	}
	if state.cancel != nil {
		state.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	state.cancel = cancel
	state.requestID++
	requestID := state.requestID
	state.loading = true
	state.err = ""
	snapshot := core.ReadSnapshot()
	if snapshot == nil {
		state.loading = false
		state.err = "Entry is no longer available."
		return nil
	}
	entryID := entry.UUID
	resources := newJobResources(ctx, snapshot.Clear)
	return func() tea.Msg {
		if !resources.start() {
			return extrasTOTPMsg{state: state, requestID: requestID, err: ctx.Err(), retry: retry}
		}
		defer resources.done()
		result, err := core.TOTP(ctx, snapshot, entryID)
		return extrasTOTPMsg{state: state, requestID: requestID, retry: retry, result: result, err: err, secret: holdSecret(ctx, result.Code)}
	}
}

func (m *Model) totpLines(state *extraState) []string {
	body := []string{m.colors().secondary.Render("TOTP seed stays hidden; only the current one-time code is shown."), ""}
	if state.loading {
		body = append(body, m.colors().accent.Render("Generating code…"))
	} else if state.err == "" && len(state.totpCode) != 0 && m.now().Before(state.totpExpires) {
		remaining := int(state.totpExpires.Sub(m.now()).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		body = append(body, m.colors().accent.Render("Code  "+safe(string(state.totpCode))), m.colors().secondary.Render(fmt.Sprintf("Refreshes in %ds", remaining)))
	} else if state.err == "" && len(state.totpCode) != 0 {
		body = append(body, m.colors().accent.Render("Refreshing code…"))
	}
	footer := "c copy code"
	if state.err != "" {
		footer = "r retry"
	}
	return m.overlayError("Authenticator Code", body, footer, state.err, min(68, m.width-4), -1, -1)
}
