package ui

import (
	"context"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) renameDuplicates() tea.Cmd {
	plan := m.dialog.duplicates
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.request++
	id := m.request
	dir := m.settings.BackupDir
	m.busy, m.writing = true, true
	return func() tea.Msg {
		db, err := plan.Rename(ctx, dir)
		if ctx.Err() != nil {
			if db != nil {
				db.Clear()
			}
			plan.Close()
			db, err = nil, vault.ErrCanceled
		}
		return openedMsg{request: id, db: db, err: err, repaired: true}
	}
}
