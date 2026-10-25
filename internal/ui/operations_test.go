package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type operationBackend struct {
	failure error
	applied int
}

func (b *operationBackend) Open(_ context.Context, path string, _ vault.Credentials) (*vault.Database, error) {
	return fixture(path), nil
}
func (b *operationBackend) Apply(ctx context.Context, db *vault.Database, _ vault.Credentials, c vault.Change, _ string) (*vault.Database, error) {
	if ctx.Err() != nil {
		return nil, vault.ErrCanceled
	}
	b.applied++
	if b.failure != nil {
		return nil, b.failure
	}
	out := db.Clone()
	if c.Kind == vault.EditEntry {
		for i := range out.Entries {
			if out.Entries[i].UUID != c.EntryUUID {
				continue
			}
			for j := range out.Entries[i].Fields {
				f := &out.Entries[i].Fields[j]
				if value, ok := c.Values[f.Name]; ok {
					f.Value = value
				}
				if f.Name == "Password" && c.PasswordChanged {
					f.Value = string(c.Password)
				}
			}
		}
	}
	if c.Kind == vault.AddEntry {
		e := vault.Entry{UUID: "new", GroupUUID: c.GroupUUID}
		for name, value := range c.Values {
			e.Fields = append(e.Fields, vault.Field{Name: name, Value: value})
		}
		e.Fields = append(e.Fields, vault.Field{Name: "Password", Value: string(c.Password), Protected: true})
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}
func (b *operationBackend) Generate(context.Context, int) ([]byte, error) {
	return []byte("GENERATED-SECRET"), nil
}
func (b *operationBackend) ResolveField(ctx context.Context, _ *vault.Database, _ vault.Credentials, _, _ string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, vault.ErrCanceled
	}
	if b.failure != nil {
		return nil, b.failure
	}
	return []byte("RESOLVED-SECRET"), nil
}

func operationModel(t *testing.T) (*Model, *operationBackend) {
	t.Helper()
	b := &operationBackend{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := New(Options{Backend: b, Now: func() time.Time { return now }, Settings: platform.Settings{BackupDir: t.TempDir()}})
	m.ws.Open(fixture("/tmp/ui-operation.kdbx"))
	m.ws.ActiveSession().ReadOnly = false
	m.ws.SetCredentials(m.ws.ActiveSession().ID, vault.Credentials{Password: []byte("SESSION-SECRET")})
	m.ws.ActiveSession().LastActivity = now
	m.normalize()
	t.Cleanup(m.Close)
	return m, b
}
func execute(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected asynchronous operation")
	}
	m.Update(cmd())
}

func TestInlineEditorSavesAndSelectsNewEntry(t *testing.T) {
	m, b := operationModel(t)
	m.key(key("n"))
	m.editor.inputs[editTitle].set("New example")
	m.editor.inputs[editPassword].set("NEW-SECRET")
	if !strings.Contains(m.View(), "NEW-SECRET") {
		t.Fatal("new password is not visible in the edit form")
	}
	execute(t, m, m.key(tea.KeyMsg{Type: tea.KeyCtrlS}))
	if b.applied != 1 || m.editor != nil || m.selected() == nil || m.selected().Get("Title") != "New example" {
		t.Fatal("save did not refresh/select persisted entry")
	}
}
func TestSaveFailureRetainsDraftAndShowsError(t *testing.T) {
	m, b := operationModel(t)
	b.failure = vault.ErrBackup
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Changed")
	execute(t, m, m.saveEditor())
	if m.editor == nil || m.editor.inputs[editTitle].value() != "Changed" || !strings.Contains(m.editor.err, "backup failed") {
		t.Fatal("failed write lost draft or diagnostic")
	}
	if m.ws.ActiveSession().DB.Entries[0].Get("Title") != "Alpha" {
		t.Fatal("failed write mutated view")
	}
}
func TestDirtyCancelDefaultsToKeepDraft(t *testing.T) {
	m, _ := operationModel(t)
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Draft")
	m.key(key("esc"))
	if m.dialog == nil || m.dialog.selected != 2 {
		t.Fatal("dirty exit lacks safe default")
	}
	m.key(key("enter"))
	if m.editor == nil {
		t.Fatal("cancel discarded draft")
	}
	m.key(key("esc"))
	m.key(tea.KeyMsg{Type: tea.KeyLeft})
	m.key(key("enter"))
	if m.editor != nil || m.dialog != nil {
		t.Fatal("discard didn't exit editor")
	}
}
func TestDirtySaveThenPendingAction(t *testing.T) {
	m, _ := operationModel(t)
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Changed")
	actionRan := false
	m.leaveEditor(func() tea.Cmd { actionRan = true; return nil })
	m.dialog.selected = 0
	execute(t, m, m.dialogKey(key("enter")))
	if !actionRan || m.editor != nil {
		t.Fatal("pending action didn't run after successful save")
	}
}
func TestReadOnlyAndStaleWritesBlocked(t *testing.T) {
	m, b := operationModel(t)
	s := m.ws.ActiveSession()
	s.ReadOnly = true
	m.key(key("n"))
	if m.editor != nil || b.applied != 0 {
		t.Fatal("read only allowed editing")
	}
	s.ReadOnly = false
	s.Stale = true
	m.key(key("e"))
	if m.editor != nil || m.dialog == nil || m.dialog.kind != "external" {
		t.Fatal("stale edit wasn't blocked")
	}
}

func TestExternalChangeCannotDisableReadOnly(t *testing.T) {
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	s.Stale = true
	s.ReadOnly = true

	var readOnly *command
	for _, cmd := range m.commands() {
		if cmd.id == "read-only" {
			copy := cmd
			readOnly = &copy
			break
		}
	}
	if readOnly == nil || readOnly.reason == "" {
		t.Fatal("read-only palette command should be disabled after external change")
	}
	readOnly.run()
	m.key(key("R"))
	if !s.ReadOnly {
		t.Fatal("external-change protection allowed writes to be enabled")
	}
	if !s.Stale {
		t.Fatal("read-only toggle cleared external-change state")
	}
}
func TestExternalChangeKeepAndReloadWithDraft(t *testing.T) {
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Draft")
	m.Update(revisionMsg{sessionID: s.ID, actual: [32]byte{1}})
	if !s.Stale || !s.ReadOnly || m.dialog == nil || m.dialog.selected != 1 {
		t.Fatal("external change modal missing safe default")
	}
	m.key(key("enter"))
	if m.editor == nil || m.dialog != nil || !s.Stale {
		t.Fatal("keep current view lost draft")
	}
	m.requestReload(s.ID)
	if m.dialog == nil || m.dialog.kind != "discardReload" {
		t.Fatal("reload didn't confirm discard")
	}
	m.dialog.selected = 1
	execute(t, m, m.dialogKey(key("enter")))
	if m.editor != nil || s.Stale {
		t.Fatal("reload didn't replace stale draft/view")
	}
}
func TestLockCancelsPendingWriteAndDropsCredentials(t *testing.T) {
	m, b := operationModel(t)
	s := m.ws.ActiveSession()
	credsBacking := s.Credentials.Password
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Draft")
	cmd := m.saveEditor()
	m.lockSession(s.ID)
	if m.editor != nil || s.DB != nil || len(s.Credentials.Password) > 0 {
		t.Fatal("full lock retained unlocked state")
	}
	for _, v := range credsBacking {
		if v != 0 {
			t.Fatal("credential bytes not cleared")
		}
	}
	m.Update(cmd())
	if b.applied != 0 || s.DB != nil {
		t.Fatal("late result resurrected locked session")
	}
}
func TestIdleLockDropsDraftAcrossViews(t *testing.T) {
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	m.ws.Duplicate()
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Draft")
	m.ws.ActiveView().Query = "private query"
	m.onTick(m.now().Add(16 * time.Minute))
	if s.DB != nil || m.editor != nil {
		t.Fatal("idle lock retained data")
	}
	for _, v := range m.ws.Views {
		if v.Query != "" {
			t.Fatal("idle lock retained search")
		}
	}
}
func TestTimedRevealIsSessionBoundAndMasksOnOverlay(t *testing.T) {
	m, _ := operationModel(t)
	m.key(key("v"))
	if !strings.Contains(m.View(), "PASSWORD-DO-NOT-RENDER") {
		t.Fatal("explicit reveal didn't show selected password")
	}
	m.key(key("?"))
	if strings.Contains(m.View(), "PASSWORD-DO-NOT-RENDER") || m.revealEntry != "" {
		t.Fatal("overlay didn't mask reveal")
	}
	m.help = false
	m.key(key("v"))
	m.ws.Open(fixture("/tmp/other.kdbx"))
	if strings.Contains(m.View(), "PASSWORD-DO-NOT-RENDER") {
		t.Fatal("reveal crossed vault sessions sharing UUID")
	}
	m.onTick(m.now().Add(6 * time.Second))
	if m.revealEntry != "" {
		t.Fatal("timed reveal did not expire")
	}
}
func TestGeneratorChangesDraftOnlyOnAccept(t *testing.T) {
	m, _ := operationModel(t)
	m.key(key("e"))
	m.editor.focus = editPassword
	original := m.editor.inputs[editPassword].value()
	m.openGenerator()
	execute(t, m, m.dialogKey(key("enter")))
	if !strings.Contains(m.View(), "GENERATED-SECRET") || m.editor.inputs[editPassword].value() != original {
		t.Fatal("generator preview did not reveal the password or changed the draft before accept")
	}
	m.dialogKey(key("esc"))
	if m.editor.inputs[editPassword].value() != original {
		t.Fatal("cancel changed password")
	}
	m.openGenerator()
	execute(t, m, m.dialogKey(key("enter")))
	m.dialogKey(key("enter"))
	if m.editor.inputs[editPassword].value() != "GENERATED-SECRET" || !m.editor.passwordChanged() {
		t.Fatal("accept didn't update draft")
	}
}

type memoryClipboard struct{ value []byte }

func (c *memoryClipboard) Write(ctx context.Context, v []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clear(c.value)
	c.value = append([]byte(nil), v...)
	return nil
}
func (c *memoryClipboard) Read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte(nil), c.value...), nil
}
func TestCopyCountdownDoesNotRenderSecretOrClearExternalClipboard(t *testing.T) {
	m, _ := operationModel(t)
	driver := &memoryClipboard{}
	m.clipboard = clipboard.New(driver)
	execute(t, m, m.copyField("Password"))
	if !strings.Contains(m.View(), "Clipboard clears in 10s") || strings.Contains(m.View(), "PASSWORD-DO-NOT-RENDER") {
		t.Fatal("copy feedback missing or exposed secret")
	}
	driver.value = []byte("new user content")
	m.clearClipboard()()
	if string(driver.value) != "new user content" {
		t.Fatal("cleared external clipboard")
	}
}
func TestFailedSaveDoesNotRunPendingNavigation(t *testing.T) {
	m, b := operationModel(t)
	b.failure = errors.New("backend detail must not render")
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Draft")
	actionRan := false
	m.leaveEditor(func() tea.Cmd { actionRan = true; return nil })
	m.dialog.selected = 0
	execute(t, m, m.dialogKey(key("enter")))
	if actionRan || m.editor == nil || strings.Contains(m.View(), "backend detail must not render") {
		t.Fatal("failed save navigated or exposed diagnostics")
	}
}

func TestClipboardExpiryUsesCleanupContextAfterAppCancellation(t *testing.T) {
	m, _ := operationModel(t)
	driver := &memoryClipboard{}
	m.clipboard = clipboard.New(driver)
	execute(t, m, m.copyField("Password"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	_, cmd := m.Update(clipboardExpiryMsg{id: m.clipGeneration, now: m.now().Add(11 * time.Second)})
	execute(t, m, cmd)
	if len(driver.value) > 0 {
		t.Fatal("expired copy retained after app cancellation")
	}
}

func TestInputAcceptsSpaceKey(t *testing.T) {
	var i input
	i.set("hello")
	i.update(tea.KeyMsg{Type: tea.KeySpace})
	i.update(key("world"))
	if i.value() != "hello world" {
		t.Fatalf("space lost: %q", i.value())
	}
}

func TestLockingInactiveVaultPreservesActiveDraftAndPopover(t *testing.T) {
	m, _ := operationModel(t)
	inactive := m.ws.ActiveSession().ID
	m.ws.Open(fixture("/tmp/other-active.kdbx"))
	m.ws.ActiveSession().ReadOnly = false
	m.normalize()
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Draft")
	m.openGenerator()
	dialog := m.dialog
	m.lockSession(inactive)
	if m.editor == nil || m.dialog != dialog || m.ws.ActiveSession().DB == nil {
		t.Fatal("locking another vault disrupted active draft")
	}
	if m.ws.Sessions[inactive].DB != nil {
		t.Fatal("inactive vault not locked")
	}
}

func TestLongNotesScrollWithoutMovingFieldFocus(t *testing.T) {
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	s.DB.Entries[0].Fields = append(s.DB.Entries[0].Fields, vault.Field{Name: "Notes", Value: strings.Repeat("long note line\n", 60)})
	v := m.ws.ActiveView()
	v.Focus = 2
	v.FieldIndex = 3
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if v.DetailOffset == 0 || v.FieldIndex != 3 {
		t.Fatal("long notes cannot be scrolled independently")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if v.DetailManual {
		t.Fatal("field navigation did not restore field following")
	}
}

func TestCopyAndRevealResolveReferences(t *testing.T) {
	m, _ := operationModel(t)
	driver := &memoryClipboard{}
	m.clipboard = clipboard.New(driver)
	m.ws.ActiveSession().DB.Entries[0].Fields[2].Value = "{REF:P@I:01010101010101010101010101010101}"
	resolveCmd := m.copyField("Password")
	if resolveCmd == nil {
		t.Fatal("reference not resolved")
	}
	_, copyCmd := m.Update(resolveCmd())
	execute(t, m, copyCmd)
	if string(driver.value) != "RESOLVED-SECRET" {
		t.Fatal("copied raw reference instead of resolved value")
	}
	execute(t, m, m.toggleReveal())
	if !strings.Contains(m.View(), "RESOLVED-SECRET") {
		t.Fatal("reveal didn't resolve value")
	}
	backing := m.revealValue
	m.hideReveal()
	for _, b := range backing {
		if b != 0 {
			t.Fatal("resolved secret retained after masking")
		}
	}
}

func TestPendingResolvedCopyCannotSurviveLock(t *testing.T) {
	m, _ := operationModel(t)
	driver := &memoryClipboard{}
	m.clipboard = clipboard.New(driver)
	m.ws.ActiveSession().DB.Entries[0].Fields[2].Value = "{REF:P@I:01010101010101010101010101010101}"
	cmd := m.copyField("Password")
	m.lockSession(m.ws.ActiveSession().ID)
	_, next := m.Update(cmd())
	if next != nil || len(driver.value) > 0 {
		t.Fatal("pending reference copy survived lock")
	}
}

func TestUnrelatedEditPreservesUnsupportedExistingPassword(t *testing.T) {
	m, _ := operationModel(t)
	m.ws.ActiveSession().DB.Entries[0].Fields[2].Value = "existing\npassword"
	m.key(key("e"))
	m.editor.inputs[editTitle].set("Changed title")
	if err := m.editor.validate(); err != nil {
		t.Fatal("unchanged password blocked unrelated edit")
	}
	if m.editor.passwordChanged() {
		t.Fatal("unrelated edit changed password")
	}
}
