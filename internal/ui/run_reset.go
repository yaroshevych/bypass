package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type runResetVault struct {
	id       string
	original *vault.Database
	db       *vault.Database
	entries  []string
}

type runResetResult struct {
	id        uint64
	originals map[string]*vault.Database
	updated   map[string]*vault.Database
	count     int
	err       error
}

func (m *Model) beginRunPermissionReset() tea.Cmd {
	r := m.runs
	if r == nil || r.resetBusy {
		return nil
	}
	if r.busy || m.writing || m.busy {
		r.resetErr = "wait for the current operation to finish"
		return nil
	}
	writer, writable := m.backend.(vault.Writer)
	resolver, readable := m.backend.(interface {
		ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
	})
	if m.readOnly || !writable || !readable || m.settings.BackupDir == "" {
		r.resetErr = "permission reset requires writes and a backup folder"
		return nil
	}
	var work []runResetVault
	for id, session := range m.ws.Sessions {
		if session.DB == nil {
			continue
		}
		var entries []string
		for _, entry := range session.DB.Entries {
			if !entry.Recycled && strings.HasSuffix(strings.Trim(entry.GroupPath, "/"), runpkg.GrantGroup) {
				entries = append(entries, entry.UUID)
			}
		}
		if len(entries) == 0 {
			continue
		}
		if session.ReadOnly || session.Stale {
			r.resetErr = "enable writes and reload changed vaults before resetting"
			return nil
		}
		work = append(work, runResetVault{id: id, original: session.DB, entries: entries})
	}
	if len(work) == 0 {
		r.resetConfirm, r.resetErr = false, ""
		m.setNotice("no saved permissions in unlocked vaults", false)
		return nil
	}
	sort.Slice(work, func(i, j int) bool { return work[i].original.Path < work[j].original.Path })
	for i := range work {
		work[i].db = work[i].original.Clone()
	}
	id := m.beginOperation("")
	ctx := m.operationContext()
	backup := m.settings.BackupDir
	r.resetBusy, r.resetErr = true, ""
	r.permissionEpoch++ // Results authorized before this reset must not deliver.
	return func() tea.Msg {
		result := runResetResult{id: id, originals: map[string]*vault.Database{}, updated: map[string]*vault.Database{}}
		defer func() {
			for _, item := range work {
				item.db.Clear()
			}
		}()
		for i := range work {
			item := &work[i]
			result.originals[item.id] = item.original
			for _, entry := range item.entries {
				if err := ctx.Err(); err != nil {
					result.err = err
					return result
				}
				value, err := resolver.ResolveField(ctx, item.db, vault.Credentials{}, entry, "Notes")
				if err != nil {
					clear(value)
					result.err = err
					return result
				}
				_, decision := runpkg.DecodeGrant(string(value))
				clear(value)
				if !decision {
					continue
				}
				updated, err := writer.Apply(ctx, item.db, vault.Credentials{}, vault.Change{Kind: vault.DeleteEntry, EntryUUID: entry}, backup)
				if err != nil {
					result.err = err
					return result
				}
				updated.BorrowInteractiveOwnership()
				item.db.Clear()
				item.db = updated
				if previous := result.updated[item.id]; previous != nil {
					previous.Clear()
				}
				result.updated[item.id] = updated.Clone()
				result.count++
			}
		}
		return result
	}
}

func (m *Model) updateRunPermissionReset(msg tea.Msg) (tea.Cmd, bool) {
	result, ok := msg.(runResetResult)
	if !ok {
		return nil, false
	}
	r := m.runs
	r.resetBusy = false
	if result.id != m.operation {
		for id, db := range result.updated {
			db.Clear()
			if session := m.ws.Sessions[id]; session != nil && session.DB == result.originals[id] {
				session.Stale = true
			}
		}
		r.resetErr = "reset interrupted; reopen affected vaults before continuing"
		return nil, true
	}
	m.cancelOperation()
	for id, db := range result.updated {
		session := m.ws.Sessions[id]
		if session == nil || session.DB != result.originals[id] {
			db.Clear()
			result.err = fmt.Errorf("vault locked or changed during permission reset")
			continue
		}
		m.ws.ReplaceDatabase(id, db)
	}
	m.restoreGroup()
	m.normalize()
	r.targets, r.choices = nil, nil
	if result.err != nil {
		r.resetErr = "could not reset permissions: " + result.err.Error()
		if result.count > 0 {
			r.resetErr = fmt.Sprintf("%d permissions reset; retry to reset the rest", result.count)
		}
		return nil, true
	}
	r.resetConfirm, r.resetErr = false, ""
	r.newIdentity = map[*runpkg.Pending]bool{}
	for _, pending := range r.pending {
		r.newIdentity[pending] = pending.Identity.Digest != ""
	}
	m.setNotice("saved permissions reset", false)
	return nil, true
}
