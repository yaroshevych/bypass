package ui

import (
	"bypass/internal/vault"
	"context"
	tea "github.com/charmbracelet/bubbletea"
)

type entryDetailsReader interface {
	EntryDetails(context.Context, *vault.Database, string) ([]vault.Field, error)
}

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
	if session == nil || session.DB == nil || !session.DB.Interactive() || m.screen != browse || m.gate.Protected() || m.editor != nil || m.writing {
		m.cancelFocusedDetails()
		return nil
	}
	entry := m.selected()
	if entry == nil || entry.DetailsLoaded {
		m.cancelFocusedDetails()
		return nil
	}
	if m.detailDatabase == session.DB && m.detailEntry == entry.UUID && m.detailSession == session.ID {
		return nil
	}
	reader, ok := m.backend.(entryDetailsReader)
	if !ok {
		return nil
	}
	m.cancelFocusedDetails()
	ctx, cancel := context.WithCancel(m.ctx)
	m.detailCancel = cancel
	m.detailDatabase, m.detailSession, m.detailEntry = session.DB, session.ID, entry.UUID
	request, database, sessionID, entryID := m.detailRequest, session.DB, session.ID, entry.UUID
	snapshot := database.Clone()
	return func() tea.Msg {
		defer snapshot.Clear()
		fields, err := reader.EntryDetails(ctx, snapshot, entryID)
		return focusedDetailsMsg{request, sessionID, entryID, database, fields, err}
	}
}

func (m *Model) acceptFocusedDetails(msg focusedDetailsMsg) tea.Cmd {
	if msg.request != m.detailRequest {
		return nil
	}
	session := m.ws.Sessions[msg.sessionID]
	if session == nil || session.DB != msg.database {
		return nil
	}
	if msg.err != nil {
		// Keep the request identity until focus changes, so a failed field read is
		// not retried continuously on each terminal tick.
		m.setNotice("could not load entry details", true)
		return nil
	}
	for index := range session.DB.Entries {
		entry := &session.DB.Entries[index]
		if entry.UUID == msg.entryID {
			applyEntryDetails(entry, msg.fields)
			break
		}
	}
	m.searchCache = nil
	return nil
}

func applyEntryDetails(entry *vault.Entry, fields []vault.Field) {
	for _, value := range fields {
		if value.Name == "Password" {
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
				entry.Fields[index].Protected = false
			}
		}
	}
	entry.DetailsLoaded = true
}
