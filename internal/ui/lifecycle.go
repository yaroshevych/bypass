package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	"bypass/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type lifecycleState struct {
	kind, sessionID, err string
	inputs               []input
	focus, offset        int
	confirm, proceed     bool
	busy                 bool
	request              uint64
	lines                []string
}

type lifecycleMsg struct {
	id              uint64
	kind, sessionID string
	db              *vault.Database
	credentials     vault.Credentials
	info            vault.DatabaseInfo
	err             error
}

type vaultCreator interface {
	CreateWithOptions(context.Context, string, vault.Credentials, vault.CreateOptions) (*vault.Database, error)
}
type credentialChanger interface {
	ChangeCredentials(context.Context, *vault.Database, vault.Credentials, vault.Credentials, string) (*vault.Database, error)
}
type sessionDatabaseInspector interface {
	InfoDatabase(context.Context, *vault.Database) (vault.DatabaseInfo, error)
}

func (m *Model) clearLifecycle() {
	if m.lifecycle == nil {
		return
	}
	if m.lifecycle.busy && m.lifecycle.request == m.operation {
		m.cancelOperation()
	}
	for i := range m.lifecycle.inputs {
		m.lifecycle.inputs[i].clear()
	}
	m.lifecycle.lines = nil
	m.lifecycle = nil
}

func (m *Model) openCreateVault() tea.Cmd {
	if len(m.ws.Views) >= workspace.MaxTabs {
		m.setNotice("max 9 tabs · close one to create a vault", false)
		return nil
	}
	if reason := m.nativeReason("db-create"); reason != "" {
		m.setNotice(reason, true)
		return nil
	}
	if m.readOnly {
		m.setNotice("restart without --read-only to create a vault", true)
		return nil
	}
	if _, ok := m.backend.(vaultCreator); !ok {
		m.setNotice("backend can't create vaults", true)
		return nil
	}
	m.hideReveal()
	m.clearExtras()
	m.clearLifecycle()
	m.lifecycle = &lifecycleState{kind: "create", inputs: make([]input, 5)}
	return nil
}

func (m *Model) openCredentials() tea.Cmd {
	if reason := m.nativeReason("db-edit"); reason != "" {
		m.setNotice(reason, true)
		return nil
	}
	if !m.writable() {
		return nil
	}
	s := m.ws.ActiveSession()
	if s.Credentials.YubiKey != "" {
		m.setNotice("backend can't change hardware-factor credentials", true)
		return nil
	}
	if _, ok := m.backend.(credentialChanger); !ok {
		m.setNotice("backend can't change vault credentials", true)
		return nil
	}
	m.hideReveal()
	m.clearExtras()
	m.clearLifecycle()
	m.lifecycle = &lifecycleState{kind: "credentials", sessionID: s.ID, inputs: make([]input, 3)}
	m.lifecycle.inputs[2].set(s.Credentials.KeyFile)
	return nil
}

func (m *Model) openDatabaseInfo() tea.Cmd {
	if reason := m.nativeReason("db-info"); reason != "" {
		m.setNotice(reason, true)
		return nil
	}
	s := m.ws.ActiveSession()
	backend, ok := m.backend.(sessionDatabaseInspector)
	if s == nil || s.DB == nil {
		return nil
	}
	if !ok {
		m.setNotice("database info unavailable", true)
		return nil
	}
	m.hideReveal()
	m.clearExtras()
	m.clearLifecycle()
	m.lifecycle = &lifecycleState{kind: "info", sessionID: s.ID, busy: true}
	id := m.beginOperation(s.ID)
	m.lifecycle.request = id
	ctx := m.operationContext()
	snapshot := s.DB.Clone()
	return func() tea.Msg {
		defer snapshot.Clear()
		msg := lifecycleMsg{id: id, kind: "info", sessionID: s.ID}
		msg.info, msg.err = backend.InfoDatabase(ctx, snapshot)
		return msg
	}
}

func (m *Model) lifecycleCredentials() (vault.Credentials, error) {
	l := m.lifecycle
	passwordIndex := 0
	if l.kind == "create" {
		passwordIndex = 1
	}
	password, confirmation := l.inputs[passwordIndex].value(), l.inputs[passwordIndex+1].value()
	if password != confirmation {
		return vault.Credentials{}, fmt.Errorf("Passwords do not match.")
	}
	keyFile := strings.TrimSpace(l.inputs[passwordIndex+2].value())
	if password == "" && keyFile == "" {
		return vault.Credentials{}, fmt.Errorf("Set a password or select an existing key file.")
	}
	if strings.ContainsAny(password, "\r\n\x00") {
		return vault.Credentials{}, fmt.Errorf("Password cannot contain line breaks.")
	}
	return vault.Credentials{Password: []byte(password), KeyFile: keyFile, NoPassword: password == ""}, nil
}

func (m *Model) submitLifecycle() tea.Cmd {
	l := m.lifecycle
	creds, err := m.lifecycleCredentials()
	if err != nil {
		l.err = err.Error()
		return nil
	}
	if l.kind == "create" {
		path := strings.TrimSpace(l.inputs[0].value())
		if path == "" || !strings.EqualFold(filepath.Ext(path), ".kdbx") {
			creds.Clear()
			l.err = "Choose a new .kdbx file path."
			return nil
		}
		options := vault.CreateOptions{}
		if raw := strings.TrimSpace(l.inputs[4].value()); raw != "" {
			options.DecryptionTimeMS, err = strconv.Atoi(raw)
			if err != nil || options.DecryptionTimeMS < 100 || options.DecryptionTimeMS > 5000 {
				creds.Clear()
				l.err = "Unlock time must be between 100 and 5000 milliseconds."
				return nil
			}
		}
		backend, ok := m.backend.(vaultCreator)
		if !ok {
			creds.Clear()
			l.err = "Vault creation is unavailable."
			return nil
		}
		id := m.beginOperation("")
		ctx := m.operationContext()
		l.busy, l.request, l.err = true, id, ""
		return func() tea.Msg {
			db, err := backend.CreateWithOptions(ctx, path, creds, options)
			return lifecycleMsg{id: id, kind: "create", db: db, credentials: creds, err: err}
		}
	}
	if !l.confirm {
		creds.Clear()
		l.confirm, l.proceed = true, false
		return nil
	}
	s := m.ws.Sessions[l.sessionID]
	if s == nil || s.DB == nil || s.ReadOnly || s.Stale {
		creds.Clear()
		l.err = "Unlock and reload a writable vault before changing credentials."
		return nil
	}
	backend, ok := m.backend.(credentialChanger)
	if !ok {
		creds.Clear()
		l.err = "Credential changes are unavailable."
		return nil
	}
	db, old := s.DB.Clone(), s.Credentials.Clone()
	id := m.beginOperation(s.ID)
	ctx := m.operationContext()
	l.busy, l.request, l.err = true, id, ""
	sessionID, backupDir := s.ID, m.settings.BackupDir
	return func() tea.Msg {
		defer db.Clear()
		defer old.Clear()
		updated, err := backend.ChangeCredentials(ctx, db, old, creds, backupDir)
		return lifecycleMsg{id: id, kind: "credentials", sessionID: sessionID, db: updated, credentials: creds, err: err}
	}
}

func (m *Model) updateLifecycle(msg tea.Msg) (tea.Cmd, bool) {
	result, ok := msg.(lifecycleMsg)
	if !ok {
		return nil, false
	}
	defer result.credentials.Clear()
	l := m.lifecycle
	if l == nil || l.request != result.id || result.id != m.operation {
		if result.db != nil {
			result.db.Clear()
		}
		return nil, true
	}
	m.cancelOperation()
	l.busy = false
	if result.err != nil {
		if result.db != nil {
			result.db.Clear()
		}
		l.confirm = false
		l.err = "Operation failed. Check file access, credentials and backend support."
		switch {
		case errors.Is(result.err, vault.ErrAlreadyExists):
			l.err = "That file already exists. Choose a new path."
		case errors.Is(result.err, vault.ErrExternalChanged):
			l.err = "Vault changed externally. Cancel and reload before retrying."
			if session := m.ws.Sessions[result.sessionID]; session != nil {
				session.Stale = true
			}
		case errors.Is(result.err, vault.ErrBackup):
			l.err = "Backup failed. Check the backup directory before retrying."
		case errors.Is(result.err, vault.ErrUnsupported):
			l.err = "This operation is not supported by the installed CLI."
		case errors.Is(result.err, vault.ErrInvalidPath):
			l.err = "Check the parent folder and existing key-file path."
		}
		return nil, true
	}
	if result.kind == "info" {
		i := result.info
		l.lines = []string{
			"Name: " + safe(i.Name), "UUID: " + safe(i.UUID), "Cipher: " + safe(i.Cipher),
			"KDF: " + safe(i.KDF), fmt.Sprintf("Recycle bin: %t", i.RecycleBinEnabled),
			fmt.Sprintf("Groups: %d", i.GroupCount), fmt.Sprintf("Entries: %d", i.EntryCount),
			fmt.Sprintf("Expired: %d", i.ExpiredEntryCount),
		}
		if m.backendVersion != "" {
			version := strings.TrimSpace(m.backendVersion)
			version = strings.TrimPrefix(version, "KeePassXC ")
			version = strings.TrimPrefix(version, "keepassxc-cli ")
			l.lines = append(l.lines, "Backend: keepassxc-cli "+version)
		}
		return nil, true
	}
	if result.db == nil {
		l.err = "Backend returned no database."
		return nil, true
	}
	if result.kind == "create" {
		if !m.ws.CanOpen(result.db.Path) {
			result.db.Clear()
			l.err = "Vault created. Close a tab before opening it (maximum 9 tabs)."
			return nil, true
		}
		m.ws.Open(result.db)
		s := m.ws.ActiveSession()
		m.ws.SetCredentials(s.ID, result.credentials)
		s.ReadOnly, s.LastActivity = false, m.now()
		m.screen = browse
	} else {
		if !m.ws.ReplaceDatabase(result.sessionID, result.db) {
			result.db.Clear()
			m.clearLifecycle()
			return nil, true
		}
		m.ws.SetCredentials(result.sessionID, result.credentials)
		m.ws.Sessions[result.sessionID].LastActivity = m.now()
	}
	path := result.db.Path
	m.clearLifecycle()
	m.restoreGroup()
	m.normalize()
	if result.kind == "credentials" {
		m.setNotice("credentials changed · backup created", false)
		return m.clearClipboard(), true
	}
	m.setNotice("vault created · unlocked", false)
	if m.store != nil {
		store := m.store
		return func() tea.Msg {
			err := store.Remember(path)
			paths, loadErr := store.LoadRecent()
			if err != nil {
				return rememberedMsg{err}
			}
			return recentMsg{paths, loadErr}
		}, true
	}
	return nil, true
}

func (m *Model) lifecycleKey(k tea.KeyMsg) tea.Cmd {
	l := m.lifecycle
	key := k.String()
	if key == "ctrl+c" {
		m.Close()
		return tea.Quit
	}
	if key == "ctrl+l" || key == "L" && l.kind == "info" {
		m.clearLifecycle()
		return m.lockCurrent(true)
	}
	if l.busy {
		return nil
	}
	if key == "esc" {
		if l.confirm {
			l.confirm = false
		} else {
			m.clearLifecycle()
		}
		return nil
	}
	if l.kind == "info" {
		switch key {
		case "q", "enter":
			m.clearLifecycle()
		case "up", "k":
			l.offset = max(0, l.offset-1)
		case "down", "j":
			l.offset++
		case "pgdown":
			l.offset += max(1, m.height-10)
		case "pgup":
			l.offset = max(0, l.offset-max(1, m.height-10))
		}
		return nil
	}
	if l.confirm {
		switch key {
		case "left", "right", "tab":
			l.proceed = !l.proceed
		case "enter":
			if l.proceed {
				return m.submitLifecycle()
			}
			l.confirm = false
		}
		return nil
	}
	inputs := lifecycleFocusOrder(l)
	actionFocus := len(lifecycleVisibleInputs(l))
	position := 0
	for i, index := range inputs {
		if index == l.focus {
			position = i
			break
		}
	}
	switch key {
	case "tab", "down":
		l.focus = inputs[min(len(inputs)-1, position+1)]
	case "shift+tab", "up":
		l.focus = inputs[max(0, position-1)]
	case "enter":
		if l.focus == actionFocus {
			return m.submitLifecycle()
		}
		if l.focus == actionFocus+1 {
			m.clearLifecycle()
			return nil
		}
		if position < actionFocus {
			l.focus = inputs[position+1]
		}
	case "ctrl+s":
		return m.submitLifecycle()
	default:
		if l.focus < actionFocus {
			l.inputs[l.focus].update(k)
			l.err = ""
		}
	}
	return nil
}

func lifecycleVisibleInputs(l *lifecycleState) []int {
	if l != nil && l.kind == "create" {
		return []int{0, 1, 2}
	}
	return []int{0, 1}
}

func lifecycleFocusOrder(l *lifecycleState) []int {
	fields := lifecycleVisibleInputs(l)
	return append(fields, len(fields), len(fields)+1)
}

func (m *Model) lifecycleLines() []string {
	l, c := m.lifecycle, m.colors()
	title := map[string]string{"create": "New Vault", "credentials": "Change Password", "info": "Database Info"}[l.kind]
	dialogWidth := min(84, m.width-4)
	if l.kind == "create" || l.kind == "credentials" {
		dialogWidth = min(60, m.width-4)
	} else if l.kind == "info" {
		dialogWidth = min(64, m.width-4)
	}
	var body []string
	if s := m.ws.Sessions[l.sessionID]; s != nil {
		home, _ := os.UserHomeDir()
		path := renderVaultPath(s.Path, home, c.accent)
		if l.kind == "credentials" || l.kind == "info" {
			path = " " + path
			body = append(body, path)
		} else {
			body = append(body, path, "")
		}
	}
	if l.kind == "create" {
		body = append(body, c.secondary.Render(" Create KeePass-compatible KDBX vault"))
	}
	footer := "tab focus "
	if l.kind == "create" || l.kind == "credentials" {
		footer = ""
	}
	if l.kind == "info" {
		var wrapped []string
		for _, line := range l.lines {
			for _, part := range strings.Split(ansi.Wrap(safe(line), max(1, dialogWidth-5), ""), "\n") {
				wrapped = append(wrapped, " "+part)
			}
		}
		capacity := max(1, m.height-12)
		start := min(l.offset, max(0, len(wrapped)-capacity))
		body = append(body, wrapped[start:min(len(wrapped), start+capacity)]...)
		footer = ""
	} else if l.confirm {
		body = append(body, "Change the credentials for this vault?", "An encrypted backup will retain the old credentials.", "")
		compact := strings.EqualFold(m.settings.Appearance, "compact")
		selected := 0
		if l.proceed {
			selected = 1
		}
		body = append(body, renderDialogChoices([]string{"Cancel", "Change Credentials"}, selected, compact, m.settings.ASCII, c))
		footer = "←→ choose"
	} else {
		labels := []string{"New password", "Confirm password"}
		indices := []int{0, 1}
		secretStart := 0
		if l.kind == "create" {
			labels = []string{"New .kdbx path", "New password", "Confirm password"}
			indices = []int{0, 1, 2}
			secretStart = 1
		}
		var fields []string
		compact := strings.EqualFold(m.settings.Appearance, "compact")
		// Overlay rows have a two-cell border and one cell of body padding
		// on each side.
		actionWidth := max(1, dialogWidth-4)
		for position, index := range indices {
			field := inputFieldView{
				label: labels[position], value: &l.inputs[index],
				secret:  index == secretStart || index == secretStart+1,
				focused: index == l.focus, indent: 1,
			}
			fields = append(fields, "")
			fields = append(fields, renderStackedInputField(field, actionWidth, compact, &m.styles)...)
		}
		if l.kind == "create" || l.kind == "credentials" {
			fields = append(fields, dialogrender.BodyRule)
		} else {
			fields = append(fields, c.rule(actionWidth))
		}
		actionFocus := len(indices)
		for _, action := range []struct {
			label, shortcut string
			index           int
		}{{"Save", "ctrl+s", actionFocus}, {"Cancel", "esc", actionFocus + 1}} {
			line := renderDialogAction(action.label, action.shortcut, actionWidth, compact, m.settings.ASCII, l.focus == action.index, &m.styles)
			fields = append(fields, line)
		}
		capacity := max(2, m.height-12)
		focusLine := l.focus
		if l.focus < actionFocus {
			focusLine = l.focus*3 + 2
		} else if l.focus >= actionFocus {
			fieldRows := len(indices) * 3
			focusLine = fieldRows + 1 + l.focus - actionFocus
		}
		start := viewportOffset(focusLine, len(fields), capacity, l.offset)
		body = append(body, fields[start:min(len(fields), start+capacity)]...)
	}
	if l.err != "" {
		message := l.err
		if l.kind == "info" {
			message = " " + message
		}
		body = append(body, c.error.Render(message))
	}
	if l.busy {
		message := "Working…"
		if l.kind == "info" {
			message = " " + message
		}
		body = append(body, c.accent.Render(message))
	}
	return m.overlay(title, body, footer, dialogWidth, -1, -1)
}

func renderVaultPath(path, home string, accent lipgloss.Style) string {
	return accent.Bold(true).Render(displayVaultPath(path, home))
}

func (m *Model) lifecyclePointer(msg tea.MouseMsg) tea.Cmd {
	if msg.Action == tea.MouseActionPress && m.lifecycle.kind == "info" {
		if msg.Button == tea.MouseButtonWheelDown {
			m.lifecycle.offset++
		}
		if msg.Button == tea.MouseButtonWheelUp {
			m.lifecycle.offset = max(0, m.lifecycle.offset-1)
		}
		return nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft || m.lifecycle.busy || m.lifecycle.confirm {
		return nil
	}
	labels := []string{"New password", "Confirm password"}
	if m.lifecycle.kind == "create" {
		labels = []string{"New .kdbx path", "New password", "Confirm password"}
	}
	lines := m.lifecycleLines()
	for row, line := range lines {
		plain := ansi.Strip(line)
		for index, label := range labels {
			if (msg.Y == row || msg.Y == row+1) && strings.Contains(plain, label) {
				m.lifecycle.focus = index
				return nil
			}
		}
	}
	return nil
}

func (m *Model) canCreateVault() bool {
	_, ok := m.backend.(vaultCreator)
	return ok && !m.readOnly && m.nativeReason("db-create") == ""
}

func (m *Model) nativeReason(commands ...string) string {
	if m.backendCapabilities == nil {
		return ""
	}
	for _, command := range commands {
		if !m.backendCapabilities[command] {
			return "Backend does not support " + command
		}
	}
	return ""
}
