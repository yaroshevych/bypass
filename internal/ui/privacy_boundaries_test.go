package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"keepassui/internal/clipboard"
	"keepassui/internal/platform"
	"keepassui/internal/vault"
)

func updateKey(m *Model, k tea.KeyMsg) tea.Cmd {
	_, cmd := m.Update(k)
	return cmd
}

func TestPublicCancelOfPendingOpenDropsLateDatabase(t *testing.T) {
	backend := &boundaryOpenBackend{started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := New(Options{Backend: backend, InitialPath: "/tmp/pending-open.kdbx"})
	t.Cleanup(m.Close)
	m.inputs[0].set("pending open password")
	cmd := updateKey(m, key("enter"))
	if cmd == nil {
		t.Fatal("open key did not start backend work")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, key("esc"))
	close(backend.release)
	msg := awaitBoundary(t, result)
	db := msg.(openedMsg).db
	m.Update(msg)
	if m.ws.ActiveSession() != nil || len(db.Entries) != 0 || len(db.Groups) != 0 {
		t.Fatal("late open result resurrected or retained a canceled database")
	}
}

func TestPublicFullLockDropsPendingSecondaryOpen(t *testing.T) {
	backend := &boundaryOpenBackend{started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := New(Options{Backend: backend, Settings: platform.Settings{BackupDir: t.TempDir()}})
	m.ws.Open(fixture("/tmp/first-open-session.kdbx"))
	first := m.ws.ActiveSession()
	m.ws.SetCredentials(first.ID, vault.Credentials{Password: []byte("first session password")})
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.beginUnlock("/tmp/secondary-pending-open.kdbx")
	m.inputs[0].set("secondary password")
	cmd := updateKey(m, key("enter"))
	if cmd == nil {
		t.Fatal("secondary unlock did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlK})
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	updateKey(m, key("enter"))
	if m.gate.Protected() || first.DB != nil {
		t.Fatal("full lock did not leave all existing sessions locked")
	}
	close(backend.release)
	msg := awaitBoundary(t, result)
	db := msg.(openedMsg).db
	m.Update(msg)
	if len(m.ws.Sessions) != 1 || len(db.Entries) != 0 {
		t.Fatal("late secondary open resurrected a vault after full lock")
	}
}

func TestPublicFullLockDiscardsPendingVaultCreation(t *testing.T) {
	backend := &boundaryCreateBackend{started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := New(Options{Backend: backend, Settings: platform.Settings{BackupDir: t.TempDir()}})
	m.ws.Open(fixture("/tmp/create-lock-existing.kdbx"))
	first := m.ws.ActiveSession()
	m.ws.SetCredentials(first.ID, vault.Credentials{Password: []byte("existing password")})
	m.normalize()
	t.Cleanup(m.Close)
	updateKey(m, key("N"))
	if m.lifecycle == nil || m.lifecycle.kind != "create" {
		t.Fatal("public create shortcut did not open the new vault form")
	}
	m.lifecycle.inputs[0].set(filepath.Join(t.TempDir(), "pending-create.kdbx"))
	m.lifecycle.inputs[1].set("created vault password")
	m.lifecycle.inputs[2].set("created vault password")
	cmd := updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("vault create did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if first.DB != nil {
		t.Fatal("Ctrl+L did not lock the active existing vault during creation")
	}
	close(backend.release)
	msg := awaitBoundary(t, result)
	created := msg.(lifecycleMsg)
	db, creds := created.db, created.credentials.Password
	m.Update(msg)
	if len(m.ws.Sessions) != 1 || len(db.Groups) != 0 {
		t.Fatal("late vault creation resurrected a database after full lock")
	}
	for _, b := range creds {
		if b != 0 {
			t.Fatal("late create credentials remained in memory after full lock")
		}
	}
}

func TestPublicFullLockDropsLateSaveResultAndCredentials(t *testing.T) {
	backend := &boundaryBackend{operationBackend: &operationBackend{}}
	m := New(Options{Backend: backend, Settings: platform.Settings{BackupDir: t.TempDir()}})
	t.Cleanup(m.Close)
	m.ws.Open(fixture("/tmp/pending-save.kdbx"))
	session := m.ws.ActiveSession()
	session.ReadOnly = false
	m.ws.SetCredentials(session.ID, vault.Credentials{Password: []byte("session master password")})
	credentialBytes := session.Credentials.Password
	m.normalize()
	updateKey(m, key("g"))
	updateKey(m, key("New Group"))
	cmd := updateKey(m, key("enter"))
	if cmd == nil || !m.writing {
		t.Fatal("group save was not started through Update")
	}
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if session.DB != nil || len(session.Credentials.Password) != 0 {
		t.Fatal("Ctrl+L did not full-lock the pending session")
	}
	for _, b := range credentialBytes {
		if b != 0 {
			t.Fatal("full lock retained prior credential bytes")
		}
	}
	msg := cmd().(appliedMsg)
	lateDB := msg.db
	m.Update(msg)
	if session.DB != nil || len(lateDB.Groups) != 0 {
		t.Fatal("late save result resurrected the locked session")
	}
}

func TestPublicPINProtectionDropsLateReferenceReveal(t *testing.T) {
	backend := &boundaryBackend{operationBackend: &operationBackend{}, resolveStarted: make(chan struct{}), resolveRelease: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.resolveRelease)
	m := newBoundaryModel(t, backend, fixture("/tmp/pending-reveal.kdbx"))
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	entry := &m.ws.ActiveSession().DB.Entries[0]
	entry.Fields[2].Value = "{REF:P@I:01010101010101010101010101010101}"
	view := m.ws.ActiveView()
	view.Focus = 2
	view.FieldIndex = 1
	cmd := updateKey(m, key("v"))
	if cmd == nil {
		t.Fatal("reference reveal did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.resolveStarted)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m.gate.Protected() || m.revealEntry != "" {
		t.Fatal("PIN protection did not clear reveal state")
	}
	close(backend.resolveRelease)
	msg := awaitBoundary(t, result)
	reveal := msg.(resolvedRevealMsg).value
	m.Update(msg)
	if strings.Contains(m.View(), "LATE-RESOLVED-SECRET") || len(m.revealValue) != 0 {
		t.Fatal("late reference result appeared after PIN protection")
	}
	for _, b := range reveal {
		if b != 0 {
			t.Fatal("late reveal result bytes were not cleared")
		}
	}
}

func TestPublicPINProtectionPreservesCredentialFormDraft(t *testing.T) {
	backend := &boundaryCredentialBackend{operationBackend: &operationBackend{}}
	m := newBoundaryModel(t, backend, fixture("/tmp/pending-credential-form.kdbx"))
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlP})
	updateKey(m, key("credentials"))
	updateKey(m, key("enter"))
	if m.lifecycle == nil || m.lifecycle.kind != "credentials" {
		t.Fatal("public actions palette did not open the credential form")
	}
	lifecycle := m.lifecycle
	lifecycle.inputs[0].set("sensitive credential draft")
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m.gate.Protected() || m.lifecycle != lifecycle || lifecycle.inputs[0].value() != "sensitive credential draft" {
		t.Fatal("PIN protection discarded or changed the credential form draft")
	}
	if strings.Contains(m.View(), "sensitive credential draft") {
		t.Fatal("PIN screen exposed the pending credential draft")
	}
	updateKey(m, key("2468"))
	updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.gate.Protected() || m.lifecycle != lifecycle || lifecycle.inputs[0].value() != "sensitive credential draft" {
		t.Fatal("PIN unlock did not restore the credential form draft")
	}
}

func TestPublicFullLockDropsLateReferenceCopy(t *testing.T) {
	backend := &boundaryBackend{operationBackend: &operationBackend{}, resolveStarted: make(chan struct{}), resolveRelease: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.resolveRelease)
	m := newBoundaryModel(t, backend, fixture("/tmp/pending-copy.kdbx"))
	driver := &memoryClipboard{}
	m.clipboard = clipboard.New(driver)
	m.ws.ActiveSession().DB.Entries[0].Fields[2].Value = "{REF:P@I:01010101010101010101010101010101}"
	view := m.ws.ActiveView()
	view.Focus = 2
	view.FieldIndex = 1
	cmd := updateKey(m, key("c"))
	if cmd == nil {
		t.Fatal("reference copy did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.resolveStarted)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	close(backend.resolveRelease)
	msg := awaitBoundary(t, result)
	value := msg.(resolvedCopyMsg).value
	m.Update(msg)
	if len(driver.value) != 0 {
		t.Fatal("late copy wrote resolved secret after full lock")
	}
	for _, b := range value {
		if b != 0 {
			t.Fatal("late copy result bytes were not cleared")
		}
	}
}

func TestPublicPINProtectionDropsLateTOTPResult(t *testing.T) {
	backend := &boundaryExtrasBackend{extrasTestBackend: &extrasTestBackend{}, started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := newBoundaryModel(t, backend, extrasEntryDB("/tmp/pending-totp.kdbx"))
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	cmd := m.openTOTP()
	if cmd == nil {
		t.Fatal("TOTP request did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m.gate.Protected() || m.extras != nil {
		t.Fatal("PIN protection left TOTP state visible")
	}
	close(backend.release)
	msg := awaitBoundary(t, result)
	code := msg.(extrasTOTPMsg).result.Code
	m.Update(msg)
	for _, b := range code {
		if b != 0 {
			t.Fatal("late TOTP code remained in memory after protection")
		}
	}
	if strings.Contains(m.View(), "731904") {
		t.Fatal("late TOTP code appeared after PIN protection")
	}
}

func TestPublicFullLockCancelsPendingAttachmentExport(t *testing.T) {
	backend := &boundaryExtrasBackend{extrasTestBackend: &extrasTestBackend{exportData: []byte("PLAINTEXT-ATTACHMENT-SECRET")}, started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := newBoundaryModel(t, backend, extrasEntryDB("/tmp/pending-export.kdbx"))
	e := extrasEntryDB("/tmp/pending-export.kdbx").Entries[0]
	dest := filepath.Join(t.TempDir(), "document.pdf")
	m.extras = &extraState{mode: extraAttachments, sessionID: m.ws.ActiveSession().ID, entryUUID: e.UUID, groupUUID: e.GroupUUID, phase: "confirm-export", action: "export", targetName: "document.pdf", path: dest, confirmProceed: true}
	cmd := updateKey(m, key("enter"))
	if cmd == nil {
		t.Fatal("confirmed export did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	close(backend.release)
	msg := awaitBoundary(t, result)
	m.Update(msg)
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("canceled export left a plaintext file")
	}
}

func TestPublicPINProtectionAllowsAuthorizedAttachmentExport(t *testing.T) {
	backend := &boundaryExtrasBackend{extrasTestBackend: &extrasTestBackend{exportData: []byte("PIN-MASKED-ATTACHMENT")}, started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := newBoundaryModel(t, backend, extrasEntryDB("/tmp/pin-masked-export.kdbx"))
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	e := m.ws.ActiveSession().DB.Entries[0]
	dest := filepath.Join(t.TempDir(), "authorized-export.pdf")
	m.extras = &extraState{mode: extraAttachments, sessionID: m.ws.ActiveSession().ID, entryUUID: e.UUID, groupUUID: e.GroupUUID, phase: "confirm-export", action: "export", targetName: "document.pdf", path: dest, confirmProceed: true}
	cmd := updateKey(m, key("enter"))
	if cmd == nil {
		t.Fatal("confirmed export did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m.gate.Protected() {
		t.Fatal("Ctrl+K did not mask the pending export")
	}
	close(backend.release)
	msg := awaitBoundary(t, result)
	m.Update(msg)
	if !m.gate.Protected() || strings.Contains(m.View(), "PIN-MASKED-ATTACHMENT") {
		t.Fatal("completed export exposed data or removed the PIN mask")
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "PIN-MASKED-ATTACHMENT" {
		t.Fatalf("authorized export did not finish behind PIN mask: %v", err)
	}
	clear(data)
	updateKey(m, key("2468"))
	updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.gate.Protected() {
		t.Fatal("test could not unlock PIN screen")
	}
	if m.extras != nil && m.extras.loading {
		t.Fatal("completed attachment export remained stuck in a loading state after PIN unlock")
	}
}

type boundaryOpenBackend struct {
	started chan struct{}
	release chan struct{}
}

type boundaryCreateBackend struct {
	started chan struct{}
	release chan struct{}
}

func (b *boundaryCreateBackend) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	return nil, errors.New("unexpected Open")
}

func (b *boundaryCreateBackend) Apply(context.Context, *vault.Database, vault.Credentials, vault.Change, string) (*vault.Database, error) {
	return nil, errors.New("unexpected Apply")
}

func (b *boundaryCreateBackend) CreateWithOptions(_ context.Context, path string, creds vault.Credentials, _ vault.CreateOptions) (*vault.Database, error) {
	close(b.started)
	<-b.release
	return fixture(path), nil
}

func (b *boundaryOpenBackend) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	close(b.started)
	<-b.release
	return fixture("/tmp/late-open-result.kdbx"), nil
}

type boundaryBackend struct {
	*operationBackend
	resolveStarted chan struct{}
	resolveRelease chan struct{}
}

type boundaryCredentialBackend struct {
	*operationBackend
}

func (b *boundaryCredentialBackend) ChangeCredentials(context.Context, *vault.Database, vault.Credentials, vault.Credentials, string) (*vault.Database, error) {
	return nil, errors.New("unexpected credential change")
}

func (b *boundaryBackend) ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error) {
	close(b.resolveStarted)
	<-b.resolveRelease
	return []byte("LATE-RESOLVED-SECRET"), nil
}

func (b *boundaryBackend) Apply(_ context.Context, db *vault.Database, _ vault.Credentials, _ vault.Change, _ string) (*vault.Database, error) {
	return db.Clone(), nil
}

type boundaryExtrasBackend struct {
	*extrasTestBackend
	started chan struct{}
	release chan struct{}
}

func (b *boundaryExtrasBackend) TOTP(context.Context, *vault.Database, vault.Credentials, string) (vault.TOTPResult, error) {
	close(b.started)
	<-b.release
	return vault.TOTPResult{Code: []byte("731904"), PeriodSeconds: 30, RemainingSeconds: 20}, nil
}

func (b *boundaryExtrasBackend) ExportAttachment(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error) {
	close(b.started)
	<-b.release
	return append([]byte(nil), b.exportData...), nil
}

func newBoundaryModel(t *testing.T, backend vault.Backend, db *vault.Database) *Model {
	t.Helper()
	m := New(Options{Backend: backend, Settings: platform.Settings{BackupDir: t.TempDir()}})
	m.width, m.height = 100, 26
	m.ws.Open(db)
	session := m.ws.ActiveSession()
	session.ReadOnly = false
	m.ws.SetCredentials(session.ID, vault.Credentials{Password: []byte("boundary master password")})
	m.normalize()
	t.Cleanup(m.Close)
	return m
}

func awaitBoundary[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(10 * time.Second):
		var zero T
		t.Fatal("timed out waiting for deterministic privacy test signal")
		return zero
	}
}

func cleanupBoundaryRelease(t *testing.T, ch chan struct{}) {
	t.Helper()
	t.Cleanup(func() {
		select {
		case <-ch:
		default:
			close(ch)
		}
	})
}

func TestIdleFullLockDropsPendingVaultCreation(t *testing.T) {
	backend := &boundaryCreateBackend{started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := newBoundaryModel(t, backend, fixture("/tmp/idle-create-existing.kdbx"))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }
	m.ws.ActiveSession().LastActivity = base
	updateKey(m, key("N"))
	m.lifecycle.inputs[0].set(filepath.Join(t.TempDir(), "idle-create.kdbx"))
	m.lifecycle.inputs[1].set("new vault password")
	m.lifecycle.inputs[2].set("new vault password")
	cmd := updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("create was not started")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	m.Update(tickMsg(base.Add(16 * time.Minute)))
	if m.lifecycle != nil || m.writing {
		t.Fatal("idle full lock retained pending creation or its credential form")
	}
	close(backend.release)
	msg := awaitBoundary(t, result)
	db := msg.(lifecycleMsg).db
	m.Update(msg)
	if len(m.ws.Sessions) != 1 || m.ws.ActiveSession().DB != nil || len(db.Groups) != 0 {
		t.Fatal("late creation resurrected a vault after idle full lock")
	}
}

func TestCurrentLockCancelsPendingUnlockAndPreservesOtherVault(t *testing.T) {
	backend := &boundaryOpenBackend{started: make(chan struct{}), release: make(chan struct{})}
	cleanupBoundaryRelease(t, backend.release)
	m := newBoundaryModel(t, backend, fixture("/tmp/other-authenticated.kdbx"))
	other := m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/current-authenticated.kdbx"))
	current := m.ws.ActiveSession()
	m.beginUnlock("/tmp/pending-unlock.kdbx")
	m.inputs[0].set("pending password")
	cmd := updateKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("pending unlock did not start")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	awaitBoundary(t, backend.started)
	updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	if current.DB != nil || other.DB == nil || m.busy {
		t.Fatal("current lock must cancel pending authentication and preserve the other vault")
	}
	close(backend.release)
	msg := awaitBoundary(t, result)
	db := msg.(openedMsg).db
	m.Update(msg)
	if len(m.ws.Sessions) != 2 || len(db.Groups) != 0 || current.DB != nil {
		t.Fatal("canceled unlock resurrected a vault after current lock")
	}
}
