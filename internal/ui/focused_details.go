package ui

import (
	"bypass/internal/vault"
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"slices"
)

type focusedDetailsMsg struct {
	request            uint64
	sessionID, entryID string
	database           *vault.Database
	fields             []vault.Field
	err                error
}

func (m *Model) cancelFocusedDetails() {
	if m.detailCancel != nil {
		m.detailCancel()
	}
	m.detailCancel = nil
	m.detailRequest++
	m.detailSession, m.detailEntry = "", ""
	m.detailDatabase = nil
}

func (m *Model) loadFocusedDetails() tea.Cmd {
	session := m.ws.ActiveSession()
	if session == nil || session.Core == nil || session.Core.Database() == nil || !session.Core.Database().Unlocked() || m.screen != browse || m.gate.Protected() || m.editor != nil || m.writing {
		m.cancelFocusedDetails()
		return nil
	}
	entry := m.selected()
	if entry == nil || entry.DetailsLoaded {
		m.cancelFocusedDetails()
		return nil
	}
	if m.detailDatabase == session.Database() && m.detailEntry == entry.UUID && m.detailSession == session.ID {
		return nil
	}
	if !session.Core.CanEntryDetails() {
		return nil
	}
	m.cancelFocusedDetails()
	ctx, cancel := context.WithCancel(m.ctx)
	m.detailCancel = cancel
	database := session.Core.Database()
	m.detailDatabase, m.detailSession, m.detailEntry = database, session.ID, entry.UUID
	request, sessionID, entryID := m.detailRequest, session.ID, entry.UUID
	snapshot := session.Core.Snapshot()
	if snapshot == nil {
		m.cancelFocusedDetails()
		return nil
	}
	core := session.Core
	resources := newJobResources(ctx, snapshot.Clear)
	return func() tea.Msg {
		if !resources.start() {
			return focusedDetailsMsg{request: request, sessionID: sessionID, entryID: entryID, database: database, err: ctx.Err()}
		}
		defer resources.done()
		fields, err := core.EntryDetails(ctx, snapshot, entryID)
		return focusedDetailsMsg{request, sessionID, entryID, database, fields, err}
	}
}

func (m *Model) acceptFocusedDetails(msg focusedDetailsMsg) tea.Cmd {
	if msg.request != m.detailRequest {
		return nil
	}
	session := m.ws.Sessions[msg.sessionID]
	if session == nil || session.Database() != msg.database {
		return nil
	}
	if msg.err != nil {
		// Keep the request identity until focus changes, so a failed field read is
		// not retried continuously on each terminal tick.
		m.setNotice("could not load entry details", true)
		return nil
	}
	for index := range session.Database().Entries {
		entry := &session.Database().Entries[index]
		if entry.UUID == msg.entryID {
			focused := ""
			if session == m.ws.ActiveSession() && m.selected() == entry {
				focused = m.focusedField()
			}
			applyEntryDetails(entry, msg.fields)
			if focused != "" {
				if i := slices.Index(m.fieldNames(), focused); i >= 0 {
					m.ws.ActiveView().FieldIndex = i
				}
			}
			break
		}
	}
	m.searchCache = nil
	return nil
}

func applyEntryDetails(entry *vault.Entry, fields []vault.Field) {
	for _, value := range fields {
		if value.Name == "Password" {
			entry.HasPassword = value.Value != ""
			continue
		}
		if value.Name == "TOTP" {
			entry.HasTOTP = true
			if !entry.IsProtected("TOTP") {
				entry.Fields = append(entry.Fields, vault.Field{Name: "TOTP", Protected: true})
			}
			continue
		}
		for index := range entry.Fields {
			if entry.Fields[index].Name == value.Name {
				entry.Fields[index] = value
				goto nextField
			}
		}
		entry.Fields = append(entry.Fields, value)
	nextField:
	}
	entry.DetailsLoaded = true
}
