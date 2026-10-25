package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type editorReadyMsg struct {
	fields             []vault.Field
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
type resolvedCopyMsg struct {
	id    uint64
	label string
	value []byte
	err   error
}
type resolvedRevealMsg struct {
	id    uint64
	value []byte
	err   error
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
	if s == nil || s.DB == nil {
		return false
	}
	if s.Stale {
		m.showExternal(s.ID)
		m.setNotice("reload required", true)
		return false
	}
	if s.ReadOnly {
		m.setNotice("read-only · press shift+r to enable", true)
		return false
	}
	if m.settings.BackupDir == "" {
		m.setNotice("backup folder unavailable", true)
		return false
	}
	if _, ok := m.backend.(vault.Writer); !ok {
		m.setNotice("backend does not support writes", true)
		return false
	}
	return true
}

func (m *Model) operationKey(k tea.KeyMsg) (tea.Cmd, bool) {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return nil, false
	}
	switch k.String() {
	case "n":
		if !m.writable() {
			return nil, true
		}
		group := s.DB.RootUUID
		if v := m.ws.ActiveView(); v != nil && v.GroupUUID != "" {
			for _, g := range s.DB.Groups {
				if g.UUID == v.GroupUUID && !g.Recycled {
					group = g.UUID
				}
			}
		}
		m.startEditor(nil, group)
		return nil, true
	case "e":
		if !m.writable() {
			return nil, true
		}
		if e := m.selected(); e != nil {
			if s.DB.Interactive() {
				return m.prepareEditor(*e), true
			}
			m.startEditor(e, e.GroupUUID)
		}
		return nil, true
	case "g":
		if !m.writable() {
			return nil, true
		}
		group := s.DB.RootUUID
		if nav := m.currentNav(); nav.kind == "group" {
			group = nav.group
		}
		m.hideReveal()
		m.dialog = &actionDialog{kind: "group", title: "New Group", sessionID: s.ID, groupUUID: group}
		return nil, true
	case "ctrl+d", "delete":
		if reason := m.nativeReason("rm"); reason != "" {
			m.setNotice(reason, true)
			return nil, true
		}
		if !m.writable() {
			return nil, true
		}
		m.confirmDelete()
		return nil, true
	case "m":
		if !m.writable() {
			return nil, true
		}
		e := m.selected()
		if e == nil {
			return nil, true
		}
		m.chooseDestination(e)
		return nil, true
	case "c":
		return m.copyField(""), true
	case "v":
		return m.toggleReveal(), true
	case "ctrl+g":
		m.openGenerator()
		return nil, true
	case "R":
		if s.Stale {
			m.setNotice("reload required", true)
			return nil, true
		}
		s.ReadOnly = !s.ReadOnly
		if s.ReadOnly {
			m.setNotice("read-only enabled", false)
		} else {
			m.setNotice("writes enabled · encrypted backup each change", false)
		}
		return nil, true
	case "ctrl+r":
		return m.requestReload(s.ID), true
	}
	return nil, false
}

func (m *Model) prepareEditor(entry vault.Entry) tea.Cmd {
	session := m.ws.ActiveSession()
	snapshot := session.DB.Clone()
	detailReader := m.backend.(entryDetailsReader)
	resolver := m.backend.(interface {
		ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
	})
	id := m.beginOperation(session.ID)
	ctx := m.operationContext()
	sessionID := session.ID
	return func() tea.Msg {
		defer snapshot.Clear()
		fields, err := detailReader.EntryDetails(ctx, snapshot, entry.UUID)
		if err != nil {
			return editorReadyMsg{id: id, sessionID: sessionID, entryID: entry.UUID, err: err}
		}
		password, err := resolver.ResolveField(ctx, snapshot, vault.Credentials{}, entry.UUID, "Password")
		return editorReadyMsg{id: id, sessionID: sessionID, entryID: entry.UUID, password: password, fields: fields, err: err}
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
	m.editor.compact = strings.EqualFold(m.settings.Appearance, "compact")
	m.editor.ascii = m.settings.ASCII
	m.editor.headerNow = m.now()
	if e != nil {
		m.editor.favorite = m.isFavorite(e)
	}
	m.editorSession = m.ws.ActiveSession().ID
	m.ws.ActiveView().Focus = 2
	m.ws.ActiveView().DetailOffset = 0
}

func favoriteKey(path, entryUUID string) string { return path + "\x00" + entryUUID }

func (m *Model) favoriteOverrides() map[string]bool {
	values := make(map[string]bool)
	if m.settings.FavoriteOverrides != "" {
		_ = json.Unmarshal([]byte(m.settings.FavoriteOverrides), &values)
	}
	if values == nil {
		values = make(map[string]bool)
	}
	return values
}

func (m *Model) isFavorite(entry *vault.Entry) bool {
	if entry == nil {
		return false
	}
	if session := m.ws.ActiveSession(); session != nil {
		if value, ok := m.favoriteOverrides()[favoriteKey(session.Path, entry.UUID)]; ok {
			return value
		}
	}
	for _, tag := range entry.Tags {
		if strings.EqualFold(tag, "Favorite") {
			return true
		}
	}
	return false
}

func (m *Model) toggleEditorFavorite() {
	if m.editor == nil || m.editor.isNew {
		return
	}
	session := m.ws.ActiveSession()
	if session == nil || session.DB == nil {
		return
	}
	values := m.favoriteOverrides()
	want := !m.editor.favorite
	values[favoriteKey(session.Path, m.editor.EntryUUID)] = want
	encoded, err := json.Marshal(values)
	if err != nil {
		m.setNotice("favorite update failed", true)
		return
	}
	previous := m.settings.FavoriteOverrides
	m.settings.FavoriteOverrides = string(encoded)
	if m.saveSettings != nil {
		if err := m.saveSettings(m.settings); err != nil {
			m.settings.FavoriteOverrides = previous
			m.setNotice("favorite save failed", true)
			return
		}
	}
	m.editor.favorite = want
	m.searchCache = nil
	m.setNotice(map[bool]string{true: "added to favorites", false: "removed from favorites"}[want], false)
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
	case "ctrl+l":
		return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(true) })
	case "ctrl+o":
		return m.leaveEditor(m.openPicker)
	case "ctrl+w":
		return m.leaveEditor(func() tea.Cmd { m.ws.CloseView(); m.normalize(); return m.clearClipboard() })
	case "L": // Uppercase text is ordinary editor input; keep the all-vault action global.
	case "ctrl+pgup", "ctrl+pgdown":
		delta := 1
		if k.String() == "ctrl+pgup" {
			delta = -1
		}
		return m.leaveEditor(func() tea.Cmd {
			if m.moveTab(delta) {
				m.normalize()
			}
			return nil
		})
	case "ctrl+g":
		m.openGenerator()
		return nil
	case "ctrl+r":
		if s := m.ws.ActiveSession(); s != nil {
			return m.requestReload(s.ID)
		}
		return nil
	}
	if key := k.String(); strings.HasPrefix(key, "alt+") && len(key) == 5 && key[4] >= '1' && key[4] <= '9' {
		index := int(key[4] - '1')
		return m.leaveEditor(func() tea.Cmd { m.switchTabPosition(index); m.normalize(); return nil })
	}
	switch m.editor.key(k) {
	case editorSave:
		return m.saveEditor()
	case editorCancel:
		return m.leaveEditor(func() tea.Cmd { return nil })
	case editorGenerate:
		m.openGenerator()
	case editorMove:
		return m.moveEditorEntry()
	case editorFavorite:
		m.toggleEditorFavorite()
	}
	return nil
}

func (m *Model) moveEditorEntry() tea.Cmd {
	if m.editor == nil {
		return nil
	}
	return m.leaveEditor(func() tea.Cmd {
		if entry := m.selected(); entry != nil {
			m.chooseDestination(entry)
		}
		return nil
	})
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
	change := vault.Change{Kind: vault.EditEntry, EntryUUID: m.editor.EntryUUID, GroupUUID: m.editor.GroupUUID, Values: m.editor.values(), PasswordChanged: m.editor.passwordChanged()}
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
		clear(change.Password)
		return nil
	}
	s := m.ws.ActiveSession()
	writer := m.backend.(vault.Writer)
	snapshot := s.DB.Clone()
	creds := s.Credentials.Clone()
	id := m.beginOperation(s.ID)
	ctx := m.operationContext()
	backupDir := m.settings.BackupDir
	return func() tea.Msg {
		defer snapshot.Clear()
		defer creds.Clear()
		defer clear(change.Password)
		defer clear(change.AttachmentData)
		db, err := writer.Apply(ctx, snapshot, creds, change, backupDir)
		// The result carries only metadata needed to restore selection, never password bytes.
		change.Password = nil
		change.AttachmentData = nil
		return appliedMsg{id: id, sessionID: s.ID, db: db, err: err, change: change}
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
	if s == nil || s.DB == nil {
		return nil
	}
	request := m.beginOperation(id)
	ctx := m.operationContext()
	creds := s.Credentials.Clone()
	backend := m.backend
	path := s.Path
	return func() tea.Msg {
		defer creds.Clear()
		db, err := backend.Open(ctx, path, creds)
		return reloadedMsg{request, id, db, err}
	}
}

func (m *Model) updateOperation(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case focusedDetailsMsg:
		return m.acceptFocusedDetails(msg), true
	case editorReadyMsg:
		defer clear(msg.password)
		if msg.id != m.operation {
			return nil, true
		}
		m.cancelOperation()
		if msg.err != nil {
			m.setNotice("could not load entry · lock and unlock to retry", true)
			return nil, true
		}
		session := m.ws.Sessions[msg.sessionID]
		if session == nil || session.DB == nil || m.editor != nil {
			return nil, true
		}
		for index := range session.DB.Entries {
			entry := &session.DB.Entries[index]
			if entry.UUID == msg.entryID {
				applyEntryDetails(entry, msg.fields)
				m.startEditor(entry, entry.GroupUUID)
				m.editor.setPassword(msg.password)
				clear(m.editor.originalPassword)
				m.editor.originalPassword = append([]rune(nil), m.editor.inputs[editPassword].text...)
				break
			}
		}
		return nil, true
	case appliedMsg:
		if msg.id != m.operation {
			if msg.db != nil {
				msg.db.Clear()
			}
			return nil, true
		}
		m.cancelOperation()
		if msg.err != nil {
			if msg.db != nil {
				msg.db.Clear()
			}
			if errors.Is(msg.err, vault.ErrInteractiveSessionLost) {
				if session := m.ws.Sessions[msg.sessionID]; session != nil {
					session.ReadOnly = true
				}
			}
			text := writeError(msg.err)
			if m.editor != nil {
				m.editor.err = text
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
		if !m.ws.ReplaceDatabase(msg.sessionID, msg.db) {
			if msg.db != nil {
				msg.db.Clear()
			}
			return nil, true
		}
		m.ws.Sessions[msg.sessionID].LastActivity = m.now()
		if msg.change.Kind == vault.MoveEntry {
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
		m.selectResult(msg.change)
		m.setNotice("saved · backup created", false)
		if after != nil {
			return after(), true
		}
		return nil, true
	case reloadedMsg:
		if msg.id != m.operation {
			if msg.db != nil {
				msg.db.Clear()
			}
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
			if msg.db != nil {
				msg.db.Clear()
			}
			return nil, true
		}
		m.ws.Sessions[msg.sessionID].LastActivity = m.now()
		m.changeDismissed[msg.sessionID] = false
		m.restoreGroup()
		m.normalize()
		m.setNotice("vault reloaded", false)
		return nil, true
	case generatedMsg:
		defer clear(msg.value)
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
		clear(m.dialog.secret)
		m.dialog.secret = append([]byte(nil), msg.value...)
		m.dialog.err = ""
		return nil, true
	case copiedMsg:
		if msg.id != m.clipGeneration {
			return nil, true
		}
		if msg.err != nil {
			m.setNotice("copy failed · clipboard unavailable", true)
			return nil, true
		}
		m.clipboardExpires = msg.expires
		m.setNotice(msg.label+" copied", false)
		id := msg.id
		delay := max(time.Duration(0), msg.expires.Sub(m.now()))
		return tea.Tick(delay, func(now time.Time) tea.Msg { return clipboardExpiryMsg{id, now} }), true
	case resolvedCopyMsg:
		if msg.id != m.clipGeneration {
			clear(msg.value)
			return nil, true
		}
		if msg.err != nil {
			clear(msg.value)
			m.setNotice("field unavailable · reload if vault changed", true)
			return nil, true
		}
		return m.copyValue(msg.value, msg.label), true
	case resolvedRevealMsg:
		defer clear(msg.value)
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
		clear(m.revealValue)
		m.revealValue = append([]byte(nil), msg.value...)
		m.revealUntil = m.now().Add(5 * time.Second)
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
		if s != nil && s.DB != nil && !s.Stale && s.DB.Revision == msg.expected && (msg.err != nil || msg.actual != msg.expected) {
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
	case errors.Is(err, vault.ErrInteractiveSessionLost):
		return "session interrupted · lock and unlock to check saved state"
	case errors.Is(err, vault.ErrExternalChanged):
		return "file changed externally · reload before saving"
	case errors.Is(err, vault.ErrAmbiguous):
		return "can't select item · duplicate path"
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
	if s == nil || s.DB == nil || v == nil {
		return
	}
	if change.Kind == vault.CreateGroup {
		for _, g := range s.DB.Groups {
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
		for _, e := range s.DB.Entries {
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
	names := []string{"UserName", "Password", "URL", "Notes"}
	seen := map[string]bool{"Title": true, "UserName": true, "Password": true, "URL": true, "Notes": true}
	if showNonPasswordVaultSecurity {
		for _, f := range e.Fields {
			if !seen[f.Name] {
				names = append(names, f.Name)
				seen[f.Name] = true
			}
		}
	}
	if v := m.ws.ActiveView(); v != nil && v.DetailFilter != "" {
		names = m.filteredFieldNames(names, v.DetailFilter)
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
	if strings.Contains(e.Get(name), "{") || (m.ws.ActiveSession().DB.Interactive() && (name == "Password" || (name == "Notes" && !e.DetailsLoaded))) {
		resolver, ok := m.backend.(interface {
			ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
		})
		if !ok {
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
		s := m.ws.ActiveSession()
		db := s.DB.Clone()
		creds := s.Credentials.Clone()
		entryID := e.UUID
		return func() tea.Msg {
			defer db.Clear()
			defer creds.Clear()
			value, err := resolver.ResolveField(ctx, db, creds, entryID, name)
			return resolvedCopyMsg{id, label, value, err}
		}
	}
	return m.copyValue([]byte(e.Get(name)), label)
}
func (m *Model) copyValue(value []byte, label string) tea.Cmd {
	if m.clipboard == nil {
		clear(value)
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
	return func() tea.Msg {
		defer clear(value)
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
	m.recordItemUse()
	m.revealEntry = e.UUID
	m.revealField = name
	m.revealSession = s.ID
	if strings.Contains(e.Get(name), "{") || (m.ws.ActiveSession().DB.Interactive() && (name == "Password" || (name == "Notes" && !e.DetailsLoaded))) {
		resolver, ok := m.backend.(interface {
			ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
		})
		if !ok {
			m.hideReveal()
			m.setNotice("backend doesn't support field references", true)
			return nil
		}
		ctx, cancel := context.WithCancel(m.ctx)
		m.revealCancel = cancel
		m.revealGeneration++
		id := m.revealGeneration
		db := s.DB.Clone()
		creds := s.Credentials.Clone()
		entryID := e.UUID
		return func() tea.Msg {
			defer db.Clear()
			defer creds.Clear()
			value, err := resolver.ResolveField(ctx, db, creds, entryID, name)
			return resolvedRevealMsg{id, value, err}
		}
	}
	clear(m.revealValue)
	m.revealValue = []byte(e.Get(name))
	m.revealUntil = m.now().Add(5 * time.Second)
	return nil
}
func (m *Model) hideReveal() {
	m.grand = false
	m.grandOffset = 0
	if m.revealCancel != nil {
		m.revealCancel()
		m.revealCancel = nil
	}
	m.revealGeneration++
	clear(m.revealValue)
	m.revealValue = nil
	m.revealEntry = ""
	m.revealField = ""
	m.revealSession = ""
	m.revealUntil = time.Time{}
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
	if session := m.ws.Sessions[id]; session != nil && session.DB.Interactive() {
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
		m.clearLifecycle()
		m.clearExtras()
		m.cancelOperation()
		m.clearDialog()
		m.closePalette(false)
		m.hideReveal()
		m.jump = false
		for id := range m.ws.Sessions {
			m.lockSession(id)
		}
	} else if s := m.ws.ActiveSession(); s != nil {
		m.lockSession(s.ID)
	}
	return m.clearClipboard()
}
func (m *Model) lockSession(id string) {
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
	configuredPIN := m.gate.Configured()
	allUnlockedIdle := true
	hasUnlocked := false
	locked := false
	for id, s := range m.ws.Sessions {
		if s.DB == nil {
			continue
		}
		hasUnlocked = true
		idleFor := time.Duration(0)
		if !s.LastActivity.IsZero() && !now.Before(s.LastActivity) {
			idleFor = now.Sub(s.LastActivity)
		}
		if configuredPIN {
			if idleFor < m.settings.IdleTimeout {
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
		if !s.DB.Interactive() && m.ticks%2 == 0 && !m.checking[id] && !m.writing {
			m.checking[id] = true
			path := s.Path
			expected := s.DB.Revision
			sessionID := id
			cmds = append(cmds, func() tea.Msg {
				actual, err := vault.Revision(path)
				return revisionMsg{sessionID, expected, actual, err}
			})
		}
		if s.Stale && !m.changeDismissed[id] && m.dialog == nil && !m.writing {
			m.showExternal(id)
		}
	}
	if configuredPIN && hasUnlocked && allUnlockedIdle && !m.gate.Protected() {
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
	generator, ok := m.backend.(interface {
		Generate(context.Context, int) ([]byte, error)
	})
	if !ok {
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
	return func() tea.Msg { value, err := generator.Generate(ctx, length); return generatedMsg{id, value, err} }
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
