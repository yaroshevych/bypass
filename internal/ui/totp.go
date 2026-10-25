package ui

import (
	"bypass/internal/vault"
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
	api, ok := m.backend.(interface {
		TOTP(context.Context, *vault.Database, vault.Credentials, string) (vault.TOTPResult, error)
	})
	if !ok {
		m.setNotice("backend doesn't support totp", true)
		return nil
	}
	m.clearExtras()
	state := &extraState{mode: extraTOTP, sessionID: session.ID, entryUUID: entry.UUID, groupUUID: entry.GroupUUID, loading: true}
	m.extras = state
	return m.requestTOTP(api, state)
}

func (m *Model) requestTOTP(api interface {
	TOTP(context.Context, *vault.Database, vault.Credentials, string) (vault.TOTPResult, error)
}, state *extraState) tea.Cmd {
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
	db := session.DB.Clone()
	creds := session.Credentials.Clone()
	entryID := entry.UUID
	return func() tea.Msg {
		defer db.Clear()
		defer creds.Clear()
		result, err := api.TOTP(ctx, db, creds, entryID)
		return extrasTOTPMsg{state: state, requestID: requestID, result: result, err: err}
	}
}

func (m *Model) totpLines(state *extraState) []string {
	body := []string{m.colors().secondary.Render("TOTP seed stays hidden; only the current one-time code is shown."), ""}
	if state.loading {
		body = append(body, m.colors().accent.Render("Generating code…"))
	} else if state.err != "" {
		body = append(body, m.colors().error.Render(state.err))
	} else if len(state.totpCode) != 0 && m.now().Before(state.totpExpires) {
		remaining := int(state.totpExpires.Sub(m.now()).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		body = append(body, m.colors().accent.Render("Code  "+safe(string(state.totpCode))), m.colors().secondary.Render(fmt.Sprintf("Refreshes in %ds", remaining)))
	} else if len(state.totpCode) != 0 {
		body = append(body, m.colors().accent.Render("Refreshing code…"))
	}
	footer := "c copy code"
	if state.err != "" {
		footer = "r retry"
	}
	return m.overlay("Authenticator Code", body, footer, min(68, m.width-4), -1, -1)
}
