package ui

import (
	wipebuf "bypass/internal/secret"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bypass/internal/session"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type editorReadyMsg struct {
	fields             []vault.Field
	secret             *secretValues
	id                 uint64
	sessionID, entryID string
	password           []byte
	err                error
}

type appliedMsg struct {
	id        uint64
	sessionID string
	db        *vault.Database
	err       error
	change    vault.Change
}
type reloadedMsg struct {
	id        uint64
	sessionID string
	db        *vault.Database
	err       error
}
type generatedMsg struct {
	id    uint64
	value []byte
	err   error
}
type copiedMsg struct {
	id      uint64
	label   string
	expires time.Time
	err     error
}
type resolvedRevealMsg struct {
	id     uint64
	value  []byte
	err    error
	secret *secretValues
}

func holdSecret(ctx context.Context, value []byte) *secretValues {
	owner := newSecretValues(ctx)
	owner.Put("value", value)
	return owner
}

func takeSecret(owner *secretValues, fallback []byte) []byte {
	if owner == nil {
		return fallback
	}
	return owner.Take()["value"]
}

type clipboardClearedMsg struct{ err error }
type clipboardExpiryMsg struct {
	id  uint64
	now time.Time
}
type tickMsg time.Time
type revisionMsg struct {
	sessionID        string
	expected, actual [32]byte
	err              error
}

func nextTick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return tickMsg(now) })
}

func (m *Model) writable() bool {
	s := m.ws.ActiveSession()
	if s == nil || s.Database() == nil {
		return false
	}
	if s.Stale {
		m.showExternal(s.ID)
		m.setNotice("reload required", true)
		return false
	}
	if s.ReadOnly {
		if reason := m.writeRestriction(); reason != "" {
			m.setNotice(reason, true)
		} else {
			m.setNotice("read-only · press r to enable", true)
		}
		return false
	}
	if m.settings.BackupDir == "" {
		m.setNotice("backup folder unavailable", true)
		return false
	}
	if !s.Core.CanApply() {
		m.setNotice("backend does not support writes", true)
		return false
	}
	return true
}

func (m *Model) writeRestriction() string {
	if m.readOnly {
		return "read-only · restricted at startup"
	}
	if s := m.ws.ActiveSession(); s != nil {
		if info, err := os.Stat(s.Path); err == nil && info.Mode().Perm()&0222 == 0 {
			return "read-only · vault file is not writable"
		}
	}
	return ""
}

func (m *Model) operationKey(k tea.KeyMsg) (tea.Cmd, bool) {
	s := m.ws.ActiveSession()
	if s == nil || s.Database() == nil {
		return nil, false
	}
	if view := m.ws.ActiveView(); view != nil && view.Focus == 0 {
		nav := m.currentNav()
		var notice string
		switch k.String() {
		case keyEditEntry:
			if nav.kind != "tag" && nav.kind != "group" {
				notice = "can't edit this item"
			}
		case keyMoveEntry:
			if nav.kind != "group" {
				notice = "can't move this item"
			}
		case keyDelete:
			if nav.kind != "group" {
				notice = "can't delete this item"
			}
		}
		if notice != "" {
			m.setNotice(notice, true)
			return nil, true
		}
	}
	switch k.String() {
	case keyNewEntry:
		if !m.writable() {
			return nil, true
		}
		if view := m.ws.ActiveView(); view != nil && view.Focus == 0 {
			if nav := m.currentNav(); nav.kind == "group" {
				m.hideReveal()
				m.dialog = &actionDialog{kind: "rename-group", title: "New Group", sessionID: s.ID, groupUUID: nav.group, createGroup: true, selected: 0}
				return nil, true
			}
		}
		group := s.Database().RootUUID
		if v := m.ws.ActiveView(); v != nil && v.GroupUUID != "" {
			for _, g := range s.Database().Groups {
				if g.UUID == v.GroupUUID && !g.Recycled {
					group = g.UUID
				}
			}
		}
		m.startEditor(nil, group)
		return nil, true
	case keyEditEntry:
		if !m.writable() {
			return nil, true
		}
		if v := m.ws.ActiveView(); v != nil && v.Focus == 0 {
			nav := m.currentNav()
			if nav.kind == "tag" {
				m.hideReveal()
				m.dialog = &actionDialog{kind: "rename-tag", title: "Rename Tag", sessionID: s.ID, originalName: nav.value, selected: 0}
				m.dialog.input.set(nav.value)
				return nil, true
			}
			if nav.kind == "group" {
				if nav.group == s.Database().RecycleBinUUID {
					m.setNotice("the recycle bin group can't be renamed", true)
					return nil, true
				}
				m.hideReveal()
				m.dialog = &actionDialog{kind: "rename-group", title: "Rename Group", sessionID: s.ID, groupUUID: nav.group, originalName: nav.label, selected: 0}
				m.dialog.input.set(nav.label)
				return nil, true
			}
		}
		if e := m.selected(); e != nil {
			if s.Database().Unlocked() {
				return m.prepareEditor(*e), true
			}
			m.startEditor(e, e.GroupUUID)
		}
		return nil, true
	case keyNewGroup:
		if !m.writable() {
			return nil, true
		}
		group := s.Database().RootUUID
		if nav := m.currentNav(); nav.kind == "group" {
			group = nav.group
		}
		m.hideReveal()
		m.dialog = &actionDialog{kind: "group", title: "New Group", sessionID: s.ID, groupUUID: group}
		return nil, true
	case keyDelete:
		if !m.writable() {
			return nil, true
		}
		return m.confirmDelete(), true
	case keyMoveEntry:
		if !m.writable() {
			return nil, true
		}
		if view := m.ws.ActiveView(); view != nil && view.Focus == 0 {
			if nav := m.currentNav(); nav.kind == "group" {
				if nav.group == s.Database().RootUUID || nav.group == s.Database().RecycleBinUUID {
					m.setNotice("the root and recycle bin groups can't be moved", true)
					return nil, true
				}
				for _, group := range s.Database().Groups {
					if group.UUID == nav.group {
						m.chooseGroupDestination(group)
						return nil, true
					}
				}
			}
		}
		if entry := m.selectedRow(); entry != nil && entry.OpaqueGroup {
			for _, group := range s.Database().Groups {
				if group.UUID == entry.UUID {
					m.chooseGroupDestination(group)
					return nil, true
				}
			}
		}
		e := m.selected()
		if e == nil {
			return nil, true
		}
		m.chooseDestination(e)
		return nil, true
	case keyCopy:
		return m.copyField(""), true
	case keyReveal:
		return m.toggleReveal(), true
	case keyReadOnly:
		if s.Stale {
			m.setNotice("reload required", true)
			return nil, true
		}
		if s.ReadOnly {
			if reason := m.writeRestriction(); reason != "" {
				m.setNotice(reason, true)
				return nil, true
			}
		}
		s.ReadOnly = !s.ReadOnly
		if s.ReadOnly {
			m.setNotice("read-only enabled", false)
		} else {
			m.setNotice("writes enabled · encrypted backup each change", false)
		}
		return nil, true
	case keyReload:
		return m.requestReload(s.ID), true
	}
	return nil, false
}

func (m *Model) prepareEditor(entry vault.Entry) tea.Cmd {
	session := m.ws.ActiveSession()
	core := session.Core
	snapshot := core.Snapshot()
	id := m.beginOperation(session.ID)
	ctx := m.operationContext()
	sessionID := session.ID
	resources := newJobResources(ctx, snapshot.Clear)
	return func() tea.Msg {
		if !resources.start() {
			return editorReadyMsg{id: id, sessionID: sessionID, entryID: entry.UUID, err: ctx.Err()}
		}
		defer resources.done()
		fields, err := core.EntryDetails(ctx, snapshot, entry.UUID)
		if err != nil {
			return editorReadyMsg{id: id, sessionID: sessionID, entryID: entry.UUID, err: err}
		}
		var password []byte
		for _, field := range fields {
			if field.Name == "Password" && field.Value != "" {
				password, err = core.ResolveField(ctx, snapshot, entry.UUID, "Password")
				break
			}
		}
		return editorReadyMsg{id: id, sessionID: sessionID, entryID: entry.UUID, password: password, secret: holdSecret(ctx, password), fields: fields, err: err}
	}
}

func (m *Model) startEditor(e *vault.Entry, group string) {
	if e != nil {
		m.recordItemUse()
	}
	m.hideReveal()
	m.clearEditor()
	m.editor = newEntryEditor(e, group)
	m.editor.styles = &m.styles
	m.editor.compact = m.compactAppearance()
	m.editor.ascii = m.settings.ASCII
	m.editor.headerNow = m.now()
	m.editorSession = m.ws.ActiveSession().ID
	m.ws.ActiveView().Focus = 2
	m.ws.ActiveView().DetailOffset = 0
}

func (m *Model) isFavorite(entry *vault.Entry) bool {
	return entry != nil && hasFavoriteTag(entry.Tags)
}

func (m *Model) toggleEditorFavorite() {
	if m.editor == nil || m.editor.isNew {
		return
	}
	m.editor.favorite = !m.editor.favorite
	m.editor.tags = favoriteChangedTags(m.editor.tags, m.editor.favorite)
	verb := "added"
	if !m.editor.favorite {
		verb = "removed"
	}
	m.setNotice("Favorite tag will be "+verb+" when you save", false)
}

func (m *Model) openEditorTags() {
	if m.editor == nil {
		return
	}
	seen := make(map[string]bool)
	tags := make([]string, 0)
	add := func(tag string) {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag != "" && !seen[key] {
			seen[key] = true
			tags = append(tags, tag)
		}
	}
	if session := m.ws.Sessions[m.editorSession]; session != nil && session.Database() != nil {
		for _, entry := range session.Database().Entries {
			for _, tag := range entry.Tags {
				add(tag)
			}
		}
	}
	for _, tag := range m.editor.tags {
		add(tag)
	}
	sort.Slice(tags, func(i, j int) bool { return strings.ToLower(tags[i]) < strings.ToLower(tags[j]) })
	m.hideReveal()
	m.dialog = &actionDialog{kind: "tags", title: "Tags", sessionID: m.editorSession, tags: tags, selectedTags: append([]string(nil), m.editor.tags...)}
}

func (m *Model) clearEditor() {
	if m.editor != nil {
		m.editor.clear()
	}
	m.editor = nil
	m.editorSession = ""
	m.afterSave = nil
}
func (m *Model) leaveEditor(action func() tea.Cmd) tea.Cmd {
	if m.editor == nil {
		return action()
	}
	if !m.editor.dirty() {
		m.clearEditor()
		return action()
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "unsaved", title: "Unsaved entry", options: []string{"Save", "Discard", "Cancel"}, selected: 2, after: action, sessionID: m.editorSession}
	return nil
}
func (m *Model) editorKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+r":
		if s := m.ws.ActiveSession(); s != nil {
			return m.requestReload(s.ID)
		}
		return nil
	}
	switch m.editor.key(k) {
	case editorSave:
		return m.saveEditor()
	case editorCancel:
		return m.leaveEditor(func() tea.Cmd { return nil })
	case editorGenerate:
		m.openGenerator()
	case editorTags:
		m.openEditorTags()
	case editorCustomFields:
		m.editor.openCustomFields()
	case editorFavorite:
		m.toggleEditorFavorite()
	}
	return nil
}

func (m *Model) saveEditor() tea.Cmd {
	if m.editor == nil {
		return nil
	}
	if err := m.editor.validate(); err != nil {
		return nil
	}
	if !m.editor.isNew && !m.editor.dirty() {
		m.clearEditor()
		m.setNotice("no changes", false)
		return nil
	}
	customFields := make([]vault.Field, len(m.editor.customFields))
	copy(customFields, m.editor.customFields)
	change := vault.Change{Kind: vault.EditEntry, EntryUUID: m.editor.EntryUUID, GroupUUID: m.editor.GroupUUID, Values: m.editor.values(), CustomFields: customFields, PasswordChanged: m.editor.passwordChanged()}
	if m.editor.isNew {
		change.Kind = vault.AddEntry
	}
	if change.PasswordChanged {
		change.Password = m.editor.password()
	}
	return m.apply(change)
}

func (m *Model) apply(change vault.Change) tea.Cmd {
	if !m.writable() {
		wipebuf.Wipe(change.Password)
		return nil
	}
	s := m.ws.ActiveSession()
	core := s.Core
	snapshot := core.Snapshot()
	id := m.beginOperation(s.ID)
	ctx := m.operationContext()
	backupDir := m.settings.BackupDir
	password, attachment := change.Password, change.AttachmentData
	resources := newJobResources(ctx, func() {
		snapshot.Clear()
		wipebuf.Wipe(password)
		wipebuf.Wipe(attachment)
	})
	return func() tea.Msg {
		if !resources.start() {
			return appliedMsg{id: id, sessionID: s.ID, err: ctx.Err()}
		}
		defer resources.done()
		db, err := core.Apply(ctx, snapshot, change, backupDir)
		resultChange := change
		resultChange.Password = append([]byte(nil), change.Password...)
		resultChange.AttachmentData = append([]byte(nil), change.AttachmentData...)
		return appliedMsg{id: id, sessionID: s.ID, db: db, err: err, change: resultChange}
	}
}

func (m *Model) beginOperation(session string) uint64 {
	m.cancelOperation()
	m.operation++
	m.operationSession = session
	m.writing = true
	m.operationCancel = func() {} // Replaced by operationContext before the command starts.
	m.hideReveal()
	return m.operation
}
func (m *Model) operationContext() context.Context {
	ctx, cancel := context.WithCancel(m.ctx)
	m.operationCancel = cancel
	return ctx
}
func (m *Model) cancelOperation() {
	if m.operationCancel != nil {
		m.operationCancel()
		m.operationCancel = nil
	}
	m.operation++
	m.operationSession = ""
	m.writing = false
}

func (m *Model) reload(id string) tea.Cmd {
	s := m.ws.Sessions[id]
	if s == nil || s.Database() == nil {
		return nil
	}
	request := m.beginOperation(id)
	ctx := m.operationContext()
	core := s.Core
	snapshot := core.Snapshot()
	resources := newJobResources(ctx, snapshot.Clear)
	return func() tea.Msg {
		if !resources.start() {
			return reloadedMsg{request, id, nil, ctx.Err()}
		}
		defer resources.done()
		db, err := core.Reload(ctx, snapshot)
		return reloadedMsg{request, id, db, err}
	}
}

func (m *Model) updateOperation(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case focusedDetailsMsg:
		return m.acceptFocusedDetails(msg), true
	case editorReadyMsg:
		msg.password = takeSecret(msg.secret, msg.password)
		defer wipebuf.Wipe(msg.password)
		if msg.id != m.operation {
			return nil, true
		}
		m.cancelOperation()
		if msg.err != nil {
			m.setNotice("could not load entry · lock and unlock to retry", true)
			return nil, true
		}
		session := m.ws.Sessions[msg.sessionID]
		if session == nil || session.Database() == nil || m.editor != nil {
			return nil, true
		}
		for index := range session.Database().Entries {
			entry := &session.Database().Entries[index]
			if entry.UUID == msg.entryID {
				applyEntryDetails(entry, msg.fields)
				m.startEditor(entry, entry.GroupUUID)
				m.editor.setPassword(msg.password)
				wipebuf.Wipe(m.editor.originalPassword)
				m.editor.originalPassword = append([]rune(nil), m.editor.inputs[editPassword].text...)
				break
			}
		}
		return nil, true
	case appliedMsg:
		defer wipebuf.Wipe(msg.change.Password)
		defer wipebuf.Wipe(msg.change.AttachmentData)
		if msg.id != m.operation {
			m.discardDatabase(msg.sessionID, msg.db)
			return nil, true
		}
		m.cancelOperation()
		if msg.err != nil {
			m.discardDatabase(msg.sessionID, msg.db)
			text := writeError(msg.err)
			var loss *vault.DataLossError
			if errors.As(msg.err, &loss) {
				if session := m.ws.Sessions[msg.sessionID]; session != nil && session.Database() != nil && session.Database().Path == loss.Path && session.Database().Revision == loss.Revision && !session.ReadOnly && !session.Stale {
					change := msg.change
					change.Password = append([]byte(nil), msg.change.Password...)
					change.AttachmentData = append([]byte(nil), msg.change.AttachmentData...)
					message := MetadataLossMessage(loss, "Saving", "An encrypted backup will be created before saving.")
					m.dialog = &actionDialog{kind: "data-loss", title: "Confirm metadata loss", options: []string{"Cancel", "Save and lose metadata"}, selected: 0, sessionID: msg.sessionID, change: change, loss: loss, message: message}
					m.setNotice("save needs metadata loss confirmation", true)
					return nil, true
				}
			}
			if m.editor != nil {
				m.editor.err = text
			}
			if m.dialog != nil && m.dialog.sessionID == msg.sessionID {
				m.dialog.err = text
			}
			m.afterSave = nil
			m.setNotice(text, true)
			if errors.Is(msg.err, vault.ErrExternalChanged) {
				if s := m.ws.Sessions[msg.sessionID]; s != nil {
					s.Stale = true
					s.ReadOnly = true
				}
				m.showExternal(msg.sessionID)
			}
			return nil, true
		}
		if m.extras != nil && m.extras.sessionID == msg.sessionID {
			m.clearExtras()
		}
		renamed := ""
		if msg.change.Kind == vault.MoveEntry && msg.db != nil {
			if session := m.ws.Sessions[msg.sessionID]; session != nil && session.Database() != nil {
				oldTitle := ""
				for _, entry := range session.Database().Entries {
					if entry.UUID == msg.change.EntryUUID {
						oldTitle = entry.Get("Title")
						break
					}
				}
				for _, entry := range msg.db.Entries {
					if entry.UUID == msg.change.EntryUUID && oldTitle != "" && entry.Get("Title") != oldTitle {
						renamed = entry.Get("Title")
						break
					}
				}
			}
		}
		previousSelection := ""
		if view := m.ws.ActiveView(); view != nil && view.SessionID == msg.sessionID {
			previousSelection = view.SelectedUUID
		}
		if !m.ws.ReplaceDatabase(msg.sessionID, msg.db) {
			m.discardDatabase(msg.sessionID, msg.db)
			return nil, true
		}
		if msg.change.Kind == vault.RenameTag {
			for i := range m.ws.Views {
				view := &m.ws.Views[i]
				if view.SessionID == msg.sessionID && view.NavKey == "tag:"+msg.change.TagName {
					view.NavKey = "tag:" + msg.change.NewName
				}
			}
		}
		m.ws.Sessions[msg.sessionID].LastActivity = m.now()
		if msg.change.Kind == vault.MoveEntry || msg.change.Kind == vault.MoveGroup {
			s := m.ws.Sessions[msg.sessionID]
			destinations := []string{msg.change.GroupUUID}
			for _, id := range s.Destinations {
				if id != msg.change.GroupUUID && len(destinations) < 5 {
					destinations = append(destinations, id)
				}
			}
			s.Destinations = destinations
		}
		after := m.afterSave
		m.clearEditor()
		m.clearDialog()
		m.changeDismissed[msg.sessionID] = false
		m.restoreGroup()
		m.normalize()
		if msg.change.Kind == vault.PrepareRecycleBin {
			change := vault.Change{Kind: vault.DeleteEntry, EntryUUID: msg.change.EntryUUID}
			name, permanent, found := "", false, false
			if msg.change.GroupUUID != "" {
				change.Kind, change.GroupUUID = vault.DeleteGroup, msg.change.GroupUUID
				for _, group := range msg.db.Groups {
					if group.UUID == change.GroupUUID && group.Recycled && group.ParentUUID == msg.db.RecycleBinUUID {
						name, permanent, found = group.Name+"/", true, true
						break
					}
				}
			} else {
				for _, entry := range msg.db.Entries {
					if entry.UUID == change.EntryUUID {
						name, permanent, found = entry.Get("Title"), entry.Recycled, true
						break
					}
				}
			}
			if !found {
				m.setNotice("item unavailable after verifying the recycle bin", true)
				return nil, true
			}
			action, message := "Move to Recycle Bin", "Entry history and attachments move with the item."
			if permanent {
				action, message = "Delete Permanently", "This permanently deletes the item. Recovery requires a backup."
				if change.Kind == vault.DeleteGroup {
					message = "Permanently deletes this group and all its contents. Recovery requires a backup."
				}
			}
			m.dialog = &actionDialog{kind: "delete", title: action, options: []string{"Cancel", action}, selected: 0, sessionID: msg.sessionID, change: change, message: []string{safe(name), message}}
			return nil, true
		}
		if msg.change.Kind == vault.PrepareDeleteGroup && msg.db.GroupDeletePlan != nil {
			plan := msg.db.GroupDeletePlan
			plural := func(n int, singular, plural string) string {
				if n == 1 {
					return singular
				}
				return plural
			}
			message := []string{
				fmt.Sprintf("%s contains %d %s across %d %s.", safe(plan.GroupName), plan.TargetEntries, plural(plan.TargetEntries, "entry", "entries"), plan.TargetGroups, plural(plan.TargetGroups, "group", "groups")),
				"The entire group hierarchy will move to the recycle bin and can be restored later. Nothing is permanently deleted.",
			}
			m.dialog = &actionDialog{kind: "delete", title: "Move Group to Recycle Bin", options: []string{"Cancel", "Move Group to Recycle Bin"}, selected: 0, sessionID: msg.sessionID, change: vault.Change{Kind: vault.DeleteGroup, GroupUUID: plan.GroupUUID}, message: message}
			return nil, true
		}
		m.selectResult(msg.change)
		if previousSelection != "" && (msg.change.Kind == vault.EditEntry || msg.change.Kind == vault.DeleteEntry || msg.change.Kind == vault.DeleteGroup) {
			if view := m.ws.ActiveView(); view != nil && view.SessionID == msg.sessionID && view.SelectedUUID != previousSelection {
				if len(m.entries()) > 0 {
					view.Focus = 1
				} else {
					view.Focus = 0
				}
			}
		}
		m.setNotice("saved · backup created", false)
		if renamed != "" {
			m.setNotice("moved as "+safe(renamed)+" · backup created", false)
		}
		if after != nil {
			return after(), true
		}
		return nil, true
	case reloadedMsg:
		if msg.id != m.operation {
			m.discardDatabase(msg.sessionID, msg.db)
			return nil, true
		}
		m.cancelOperation()
		if msg.err != nil {
			m.setNotice("reload failed · check file access or credentials", true)
			return nil, true
		}
		if m.extras != nil && m.extras.sessionID == msg.sessionID {
			m.clearExtras()
		}
		if !m.ws.ReplaceDatabase(msg.sessionID, msg.db) {
			m.discardDatabase(msg.sessionID, msg.db)
			return nil, true
		}
		m.ws.Sessions[msg.sessionID].LastActivity = m.now()
		m.changeDismissed[msg.sessionID] = false
		m.restoreGroup()
		m.normalize()
		m.setNotice("vault reloaded", false)
		return nil, true
	case generatedMsg:
		defer wipebuf.Wipe(msg.value)
		if msg.id != m.operation {
			return nil, true
		}
		m.cancelOperation()
		if m.dialog == nil || m.dialog.kind != "generator" {
			return nil, true
		}
		if msg.err != nil {
			m.dialog.err = "Could not generate password."
			return nil, true
		}
		wipebuf.Wipe(m.dialog.secret)
		m.dialog.secret = append([]byte(nil), msg.value...)
		m.dialog.err = ""
		return nil, true
	case copiedMsg:
		if msg.id != m.clipGeneration {
			return nil, true
		}
		if msg.err != nil {
			if errors.Is(msg.err, session.ErrFieldUnavailable) {
				m.setNotice("field unavailable · reload if vault changed", true)
			} else {
				m.setNotice("copy failed · clipboard unavailable", true)
			}
			return nil, true
		}
		m.clipboardExpires = msg.expires
		m.setNotice(msg.label+" copied", false)
		id := msg.id
		delay := max(time.Duration(0), msg.expires.Sub(m.now()))
		return tea.Tick(delay, func(now time.Time) tea.Msg { return clipboardExpiryMsg{id, now} }), true
	case resolvedRevealMsg:
		msg.value = takeSecret(msg.secret, msg.value)
		defer wipebuf.Wipe(msg.value)
		if msg.id != m.revealGeneration {
			return nil, true
		}
		if m.revealCancel != nil {
			m.revealCancel()
			m.revealCancel = nil
		}
		if msg.err != nil {
			m.hideReveal()
			m.setNotice("field unavailable · reload if vault changed", true)
			return nil, true
		}
		wipebuf.Wipe(m.revealValue)
		m.revealValue = append([]byte(nil), msg.value...)
		if m.revealField == "TOTP" && !m.revealUntil.IsZero() && !m.now().Before(m.revealUntil) {
			m.hideReveal()
			return nil, true
		}
		if m.revealField != "TOTP" || m.revealUntil.IsZero() {
			m.revealUntil = m.now().Add(5 * time.Second)
		}
		if m.revealField == "TOTP" {
			m.totpRefreshAt = m.now().Add(time.Second)
		}
		return nil, true
	case clipboardExpiryMsg:
		if msg.id != m.clipGeneration {
			return nil, true
		}
		m.clipboardExpires = time.Time{}
		manager := m.clipboard
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
			defer cancel()
			_, err := manager.Expire(ctx, msg.now)
			return clipboardClearedMsg{err}
		}, true
	case clipboardClearedMsg:
		if msg.err != nil {
			m.setNotice("clipboard clear failed · clear it manually", true)
		}
		return nil, true
	case tickMsg:
		return m.onTick(time.Time(msg)), true
	case revisionMsg:
		delete(m.checking, msg.sessionID)
		s := m.ws.Sessions[msg.sessionID]
		if s != nil && s.Database() != nil && !s.Stale && s.Database().Revision == msg.expected && (msg.err != nil || msg.actual != msg.expected) {
			s.Stale = true
			s.ReadOnly = true
			if m.dialog == nil && !m.writing {
				m.showExternal(s.ID)
			}
		}
		return nil, true
	}
	return nil, false
}

func writeError(err error) string {
	switch {
	case errors.Is(err, vault.ErrExternalChanged):
		return "file changed externally · reload before saving"
	case errors.Is(err, vault.ErrAmbiguous):
		return "can't select item · duplicate path"
	case errors.Is(err, vault.ErrAmbiguousAttachment):
		return "can't change attachment · duplicate name"
	case errors.Is(err, vault.ErrUnsupportedData):
		return "vault has fields this version can't preserve · writes disabled"
	case errors.Is(err, vault.ErrUnsupported):
		return "unsupported change · check fields and item names"
	case errors.Is(err, vault.ErrReadOnly):
		return "vault file is read-only"
	case errors.Is(err, vault.ErrBackup):
		return "backup failed · vault unchanged"
	case errors.Is(err, vault.ErrCanceled):
		return "operation canceled · reload to check status"
	default:
		return "save failed · draft kept · check access or credentials"
	}
}

func (m *Model) selectResult(change vault.Change) {
	s := m.ws.ActiveSession()
	v := m.ws.ActiveView()
	if s == nil || s.Database() == nil || v == nil {
		return
	}
	if change.Kind == vault.CreateGroup {
		for _, g := range s.Database().Groups {
			if g.ParentUUID == change.GroupUUID && g.Name == change.NewName {
				v.GroupUUID = g.UUID
				v.NavKey = "group:" + g.UUID
				m.restoreGroup()
				m.normalize()
				return
			}
		}
	}
	if change.Kind == vault.MoveEntry || change.Kind == vault.AddEntry {
		v.NavIndex = 0
		v.NavKey = "all"
		v.GroupUUID = ""
		v.Query = ""
		for _, e := range s.Database().Entries {
			if (change.Kind == vault.MoveEntry && e.UUID == change.EntryUUID) || (change.Kind == vault.AddEntry && e.GroupUUID == change.GroupUUID && e.Get("Title") == change.Values["Title"]) {
				v.SelectedUUID = e.UUID
				break
			}
		}
	}
}

func (m *Model) fieldNames() []string {
	e := m.selected()
	if e == nil {
		return nil
	}
	var names []string
	if e.Get("UserName") != "" {
		names = append(names, "UserName")
	}
	if e.HasPassword || e.Get("Password") != "" {
		names = append(names, "Password")
	}
	if e.HasTOTP {
		names = append(names, "TOTP")
	}
	for _, name := range []string{"URL", "Email", "Notes"} {
		if e.Get(name) != "" {
			names = append(names, name)
		}
	}
	seen := map[string]bool{"TOTP": true, "Title": true, "UserName": true, "Password": true, "URL": true, "Notes": true, "Email": true}
	for _, f := range e.Fields {
		if !seen[f.Name] {
			names = append(names, f.Name)
			seen[f.Name] = true
		}
	}
	return names
}
func (m *Model) focusedField() string {
	v := m.ws.ActiveView()
	names := m.fieldNames()
	if v == nil || len(names) == 0 {
		return "Password"
	}
	if v.Focus != 2 {
		return "Password"
	}
	return names[min(max(v.FieldIndex, 0), len(names)-1)]
}
func (m *Model) copyField(name string) tea.Cmd {
	e := m.selected()
	if e == nil {
		return nil
	}
	if name == "" {
		name = m.focusedField()
	}
	m.recordItemUse()
	label := name
	if label == "UserName" {
		label = "Username"
	}
	if name == "Password" || name == "TOTP" || strings.Contains(e.Get(name), "{") || (m.ws.ActiveSession().Database().Unlocked() && name == "Notes" && !e.DetailsLoaded) {
		core := m.ws.ActiveSession().Core
		if !core.CanResolveField() {
			m.setNotice("backend doesn't support field references", true)
			return nil
		}
		if m.clipCancel != nil {
			m.clipCancel()
		}
		ctx, cancel := context.WithCancel(m.ctx)
		m.clipCancel = cancel
		m.clipGeneration++
		id := m.clipGeneration
		snapshot := core.Snapshot()
		entryID := e.UUID
		now, manager, timeout := m.now(), m.clipboard, m.settings.ClipboardTimeout
		expires := now.Add(timeout)
		resources := newJobResources(ctx, snapshot.Clear)
		return func() tea.Msg {
			if !resources.start() {
				return copiedMsg{id: id, label: label, err: ctx.Err()}
			}
			defer resources.done()
			err := core.CopyField(ctx, snapshot, entryID, name, manager, now, timeout)
			return copiedMsg{id: id, label: label, expires: expires, err: err}
		}
	}
	return m.copyValue([]byte(e.Get(name)), label)
}

func (m *Model) copyValue(value []byte, label string) tea.Cmd {
	if m.clipboard == nil {
		wipebuf.Wipe(value)
		m.setNotice("clipboard unavailable", true)
		return nil
	}
	if m.clipCancel != nil {
		m.clipCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.clipCancel = cancel
	m.clipGeneration++
	id := m.clipGeneration
	now := m.now()
	expires := now.Add(m.settings.ClipboardTimeout)
	manager := m.clipboard
	timeout := m.settings.ClipboardTimeout
	resources := newJobResources(ctx, func() { wipebuf.Wipe(value) })
	return func() tea.Msg {
		if !resources.start() {
			return copiedMsg{id: id, label: label, expires: expires, err: ctx.Err()}
		}
		defer resources.done()
		err := manager.Copy(ctx, value, now, timeout)
		return copiedMsg{id, label, expires, err}
	}
}
func (m *Model) clearClipboard() tea.Cmd {
	if m.clipCancel != nil {
		m.clipCancel()
		m.clipCancel = nil
	}
	m.clipGeneration++
	m.clipboardExpires = time.Time{}
	manager := m.clipboard
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
		defer cancel()
		return clipboardClearedMsg{manager.Clear(ctx)}
	}
}
func (m *Model) toggleReveal() tea.Cmd {
	e := m.selected()
	s := m.ws.ActiveSession()
	if e == nil || s == nil {
		return nil
	}
	name := m.focusedField()
	if v := m.ws.ActiveView(); v != nil {
		v.Focus = 2
		for i, field := range m.fieldNames() {
			if field == name {
				v.FieldIndex = i
				break
			}
		}
		v.DetailManual = false
	}
	if m.revealEntry == e.UUID && m.revealField == name {
		m.hideReveal()
		return nil
	}
	m.hideReveal()
	m.recordItemUse()
	m.revealEntry = e.UUID
	m.revealField = name
	m.revealSession = s.ID
	if name == "TOTP" || strings.Contains(e.Get(name), "{") || (m.ws.ActiveSession().Database().Unlocked() && (name == "Password" || (name == "Notes" && !e.DetailsLoaded))) {
		return m.requestRevealField()
	}
	wipebuf.Wipe(m.revealValue)
	m.revealValue = []byte(e.Get(name))
	m.revealUntil = m.now().Add(5 * time.Second)
	return nil
}

func (m *Model) requestRevealField() tea.Cmd {
	e, s, name := m.selected(), m.ws.ActiveSession(), m.revealField
	if e == nil || s == nil || e.UUID != m.revealEntry || s.ID != m.revealSession {
		m.hideReveal()
		return nil
	}
	core := s.Core
	if !core.CanResolveField() {
		m.hideReveal()
		m.setNotice("backend doesn't support field references", true)
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.revealCancel = cancel
	m.revealGeneration++
	id := m.revealGeneration
	snapshot := core.Snapshot()
	entryID := e.UUID
	resources := newJobResources(ctx, snapshot.Clear)
	return func() tea.Msg {
		if !resources.start() {
			return resolvedRevealMsg{id: id, err: ctx.Err()}
		}
		defer resources.done()
		value, err := core.ResolveField(ctx, snapshot, entryID, name)
		return resolvedRevealMsg{id: id, value: value, err: err, secret: holdSecret(ctx, value)}
	}
}
func (m *Model) hideReveal() {
	m.grand = false
	m.grandOffset = 0
	m.grandSpaced = false
	m.grandFooterHover = ""
	if m.revealCancel != nil {
		m.revealCancel()
		m.revealCancel = nil
	}
	m.revealGeneration++
	wipebuf.Wipe(m.revealValue)
	m.revealValue = nil
	m.revealEntry = ""
	m.revealField = ""
	m.revealSession = ""
	m.revealUntil = time.Time{}
	m.totpRefreshAt = time.Time{}
}
func (m *Model) fieldValue(e vault.Entry, name string) string {
	if m.revealed(e.UUID, name) {
		return safe(string(m.revealValue))
	}
	return visibleField(e, name)
}

func (m *Model) revealed(uuid, name string) bool {
	s := m.ws.ActiveSession()
	return !m.grand && s != nil && s.ID == m.revealSession && m.revealEntry == uuid && m.revealField == name && m.now().Before(m.revealUntil)
}

func (m *Model) requestReload(id string) tea.Cmd {
	if session := m.ws.Sessions[id]; session != nil && session.Database().Unlocked() {
		m.setNotice("lock and unlock to reload", true)
		return nil
	}
	if m.editor != nil && m.editorSession == id && m.editor.dirty() {
		m.dialog = &actionDialog{kind: "discardReload", title: "Discard draft and reload?", options: []string{"Cancel", "Discard and Reload"}, selected: 0, sessionID: id}
		return nil
	}
	m.clearEditor()
	return m.reload(id)
}
func (m *Model) setNotice(text string, persistent bool) {
	m.notice = compactNotice(text)
	m.noticeExpires = time.Time{}
	if !persistent {
		m.noticeExpires = m.now().Add(3 * time.Second)
	}
}

func compactNotice(text string) string {
	text = strings.ToLower(strings.TrimSpace(safe(text)))
	return strings.TrimRight(text, ".! ")
}

func (m *Model) lockCurrent(all bool) tea.Cmd {
	if all {
		m.cancelPendingOpen()
		m.clearTransientUI()
		for id := range m.ws.Sessions {
			m.lockSession(id)
		}
	} else if s := m.ws.ActiveSession(); s != nil {
		m.lockSession(s.ID)
	}
	return m.clearClipboard()
}
func (m *Model) lockSession(id string) {
	if m.runs != nil {
		for secrets, targets := range m.runs.replies {
			for _, target := range targets {
				if target.session == id {
					secrets.Clear()
					delete(m.runs.replies, secrets)
					break
				}
			}
		}
		for grant := range m.runs.grants {
			if grant.session == id {
				delete(m.runs.grants, grant)
			}
		}
	}
	if session := m.ws.Sessions[id]; session != nil && m.searchCache != nil && m.searchCache.db == session.Database() {
		m.searchCache = nil
	}
	if m.detailSession == id {
		m.cancelFocusedDetails()
	}
	if m.lifecycle != nil && m.lifecycle.sessionID == id {
		m.clearLifecycle()
	}
	if m.extras != nil && m.extras.sessionID == id {
		m.clearExtras()
	}
	if m.clipCancel != nil {
		m.clipCancel()
		m.clipCancel = nil
	}
	m.clipGeneration++
	if m.operationSession == id {
		m.cancelOperation()
	}
	if m.editorSession == id {
		m.clearEditor()
	}
	if m.dialog != nil && m.dialog.sessionID == id {
		m.clearDialog()
	}
	if m.revealSession == id {
		m.hideReveal()
	}
	if s := m.ws.ActiveSession(); s != nil && s.ID == id {
		m.searching = false
		m.query.clear()
		m.previousQuery = ""
		m.searchOrigin = nil
		m.help = false
	}
	if m.ws.LockSession(id) && !m.anyUnlocked() {
		m.cancelPendingOpen()
		m.clearLifecycle()
		m.clearExtras()
		m.cancelOperation()
		m.syncPrivacyState()
	}
	delete(m.changeDismissed, id)
	delete(m.checking, id)
	if m.dialog != nil && m.dialog.kind == "run-permissions" {
		m.openRunPermissions()
	}
}

func (m *Model) onTick(now time.Time) tea.Cmd {
	m.ticks++
	cmds := []tea.Cmd{nextTick(), m.extrasTick(now)}
	if !m.noticeExpires.IsZero() && !now.Before(m.noticeExpires) {
		m.notice = ""
		m.noticeExpires = time.Time{}
	}
	if !m.grand && !m.revealUntil.IsZero() && !now.Before(m.revealUntil) {
		m.hideReveal()
	}
	if m.revealField == "TOTP" && m.revealCancel == nil && !m.totpRefreshAt.IsZero() && !now.Before(m.totpRefreshAt) {
		cmds = append(cmds, m.requestRevealField())
	}
	configuredPIN := m.gate.Configured()
	allUnlockedIdle := true
	hasUnlocked := false
	locked := false
	for id, s := range m.ws.Sessions {
		if s.Database() == nil {
			continue
		}
		hasUnlocked = true
		idleFor := time.Duration(0)
		if !s.LastActivity.IsZero() && !now.Before(s.LastActivity) {
			idleFor = now.Sub(s.LastActivity)
		}
		if configuredPIN {
			if idleFor < m.settings.PINIdleTimeout {
				allUnlockedIdle = false
			}
			if m.settings.FullLockTimeout > 0 && idleFor >= m.settings.FullLockTimeout {
				m.lockSession(id)
				locked = true
				continue
			}
		} else if idleFor >= m.settings.IdleTimeout {
			m.lockSession(id)
			locked = true
			continue
		}
		if !s.Database().Unlocked() && m.ticks%2 == 0 && !m.checking[id] && !m.writing {
			m.checking[id] = true
			path := s.Path
			expected := s.Database().Revision
			sessionID := id
			service := m.service
			cmds = append(cmds, func() tea.Msg {
				actual, err := service.ReadRevision(path)
				return revisionMsg{sessionID, expected, actual, err}
			})
		}
		if s.Stale && !m.changeDismissed[id] && m.dialog == nil && !m.writing {
			m.showExternal(id)
		}
	}
	pinInputIdle := m.pinLastActivity.IsZero() || !now.Before(m.pinLastActivity.Add(m.settings.PINIdleTimeout))
	if configuredPIN && hasUnlocked && allUnlockedIdle && pinInputIdle && !m.gate.Protected() {
		m.concealForPIN()
		m.pinInput.clear()
		m.pinError = ""
		m.pinFullLock = false
		m.gate.Protect()
		locked = true // Protecting the screen also clears an owned clipboard value.
	}
	if locked {
		cmds = append(cmds, m.clearClipboard())
	}
	if locked {
		m.syncPrivacyState()
	}
	if !m.clipboardExpires.IsZero() && !now.Before(m.clipboardExpires) {
		m.clipboardExpires = time.Time{}
		manager := m.clipboard
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
			defer cancel()
			_, err := manager.Expire(ctx, now)
			return clipboardClearedMsg{err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) openGenerator() {
	m.hideReveal()
	applyToPassword := m.editor != nil && m.editor.focus == editPassword
	m.dialog = &actionDialog{kind: "generator", title: "Password Generator", applyToPassword: applyToPassword}
	if s := m.ws.ActiveSession(); s != nil {
		m.dialog.sessionID = s.ID
	}
	m.dialog.input.set("24")
}
func (m *Model) generate() tea.Cmd {
	service := m.service
	if !service.CanGeneratePassword() {
		m.dialog.err = "Password generation unavailable."
		return nil
	}
	length, err := strconv.Atoi(m.dialog.input.value())
	if err != nil || length < 8 || length > 64 {
		m.dialog.err = "Choose length from 8 to 64."
		return nil
	}
	id := m.beginOperation("")
	ctx := m.operationContext()
	return func() tea.Msg {
		value, err := service.GeneratePassword(ctx, length)
		return generatedMsg{id, value, err}
	}
}

func vaultLabel(path string) string { return safe(filepath.Base(path)) }
func groupLabel(path string) string {
	if path == "" {
		return "/"
	}
	return safe(path)
}
func (m *Model) clipboardStatus() string {
	if m.clipboardExpires.IsZero() {
		return ""
	}
	remaining := int(m.clipboardExpires.Sub(m.now()).Seconds() + 0.999)
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("Clipboard clears in %ds", remaining)
}

// Invalidate authentication work as well as its cancellation context: a CLI
// result may already be queued when the user locks the workspace.
func (m *Model) cancelPendingOpen() {
	m.request++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.busy = false
	for i := range m.inputs {
		m.inputs[i].clear()
	}
	m.noPassword = false
	m.path = ""
	m.err = ""
	m.screen = browse
}
