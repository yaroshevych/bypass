package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type extrasTestBackend struct {
	totpCalls   int
	exportCalls int
	applyCalls  int
	applyKind   vault.ChangeKind
	attachment  string
	attachmentN string
	exportData  []byte
}

func (b *extrasTestBackend) Open(_ context.Context, path string, _ vault.Credentials) (*vault.Database, error) {
	return fixture(path), nil
}

func (b *extrasTestBackend) TOTP(_ context.Context, _ *vault.Database, _ vault.Credentials, _ string) (vault.TOTPResult, error) {
	b.totpCalls++
	return vault.TOTPResult{Code: []byte(fmt.Sprintf("%06d", b.totpCalls)), PeriodSeconds: 30, RemainingSeconds: 5}, nil
}

func (b *extrasTestBackend) ExportAttachment(_ context.Context, _ *vault.Database, _ vault.Credentials, _, _ string) ([]byte, error) {
	b.exportCalls++
	return append([]byte(nil), b.exportData...), nil
}

func (b *extrasTestBackend) Apply(_ context.Context, db *vault.Database, _ vault.Credentials, change vault.Change, _ string) (*vault.Database, error) {
	b.applyCalls++
	b.applyKind = change.Kind
	b.attachmentN = change.AttachmentName
	b.attachment = string(change.AttachmentData)
	clone := db.Clone()
	for i := range clone.Entries {
		if clone.Entries[i].UUID != change.EntryUUID {
			continue
		}
		switch change.Kind {
		case vault.AddAttachment:
			clone.Entries[i].AttachmentNames = append(clone.Entries[i].AttachmentNames, change.AttachmentName)
		case vault.RemoveAttachment:
			var names []string
			for _, name := range clone.Entries[i].AttachmentNames {
				if name != change.AttachmentName {
					names = append(names, name)
				}
			}
			clone.Entries[i].AttachmentNames = names
		}
	}
	return clone, nil
}

func newExtrasTestModel(t *testing.T, db *vault.Database, backend *extrasTestBackend) *Model {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	m := New(Options{Backend: backend, Now: func() time.Time { return now }})
	m.width, m.height = 100, 26
	m.settings.BackupDir = t.TempDir()
	m.ws.Open(db)
	m.ws.ActiveSession().ReadOnly = false
	m.ws.ActiveSession().Credentials = vault.Credentials{NoPassword: true}
	m.normalize()
	return m
}

func extrasEntryDB(path string) *vault.Database {
	db := fixture(path)
	db.Entries[0].HasTOTP = true
	db.Entries[0].TOTPPeriodSeconds = 30
	db.Entries[0].Fields = append(db.Entries[0].Fields, vault.Field{Name: "otp", Value: "JBSWY3DPEHPK3PXP", Protected: true})
	db.Entries[0].AttachmentNames = []string{"document.pdf"}
	return db
}

func TestTOTPOverlayShowsCodeWithoutSeedAndRefreshes(t *testing.T) {
	t.Skip("TOTP UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-totp.kdbx"), backend)
	cmd := m.openTOTP()
	if cmd == nil {
		t.Fatal("opening a configured TOTP entry did not request a code")
	}
	msg := cmd().(extrasTOTPMsg)
	if _, ok := m.updateExtras(msg); !ok {
		t.Fatal("TOTP result was not handled")
	}
	text := ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if !strings.Contains(text, "000001") || strings.Contains(text, "JBSWY3DPEHPK3PXP") {
		t.Fatalf("TOTP overlay did not show only the code: %q", text)
	}
	if backend.totpCalls != 1 {
		t.Fatalf("TOTP calls = %d, want 1", backend.totpCalls)
	}
	cmd = m.extrasTick(m.extras.totpExpires)
	if cmd == nil {
		t.Fatal("expired code did not trigger a refresh")
	}
	if _, ok := m.updateExtras(cmd().(extrasTOTPMsg)); !ok || backend.totpCalls != 2 {
		t.Fatal("refreshed TOTP result was not applied")
	}
}

func TestExpiredTOTPIsNotCopiedWhileRefreshIsPending(t *testing.T) {
	t.Skip("TOTP UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-totp-expired.kdbx"), backend)
	cmd := m.openTOTP()
	m.updateExtras(cmd().(extrasTOTPMsg))
	oldCode := m.extras.totpCode
	m.extras.totpExpires = m.now()
	cmd, _ = m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil || !m.extras.loading || len(m.extras.totpCode) != 0 {
		t.Fatal("expired TOTP code was not cleared before refresh")
	}
	if !bytes.Equal(oldCode, make([]byte, len(oldCode))) {
		t.Fatal("expired TOTP bytes were retained")
	}
	text := ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if strings.Contains(text, "000001") || !strings.Contains(text, "Generating code") {
		t.Fatal("expired code was visible while refresh was pending")
	}
}

func TestCanceledTOTPResultIsCleared(t *testing.T) {
	t.Skip("TOTP UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-totp-cancel.kdbx"), backend)
	cmd := m.openTOTP()
	msg := cmd().(extrasTOTPMsg)
	result := msg.result.Code
	m.clearExtras()
	m.updateExtras(msg)
	if m.extras != nil || !bytes.Equal(result, make([]byte, len(result))) {
		t.Fatal("stale TOTP code was retained after the overlay closed")
	}
}

func TestOlderTOTPRequestCannotReplaceNewerCode(t *testing.T) {
	t.Skip("TOTP UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-totp-generation.kdbx"), backend)
	first := m.openTOTP()
	firstMsg := first().(extrasTOTPMsg)
	second := m.requestTOTP(backend, m.extras)
	secondMsg := second().(extrasTOTPMsg)
	m.updateExtras(firstMsg)
	if !m.extras.loading || len(m.extras.totpCode) != 0 {
		t.Fatal("stale request changed the current loading state")
	}
	m.updateExtras(secondMsg)
	if m.extras.loading || string(m.extras.totpCode) != "000002" {
		t.Fatal("newest TOTP result was not retained")
	}
}

func TestAttachmentReplaceDefaultsToCancelAndRequiresExplicitConfirm(t *testing.T) {
	t.Skip("attachment UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-replace.kdbx"), backend)
	source := filepath.Join(t.TempDir(), "replacement.pdf")
	if err := os.WriteFile(source, []byte("new attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	m.openAttachments()
	m.extras.selected = 1
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m.extras.input.set(source)
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.extras.phase != "confirm-replace" || backend.applyCalls != 0 {
		t.Fatal("replacement was not held at its confirmation step")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	if backend.applyCalls != 0 || m.writing || m.extras.phase != "" {
		t.Fatal("default Cancel selection allowed a replacement")
	}

	m.extras.selected = 1
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m.extras.input.set(source)
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRight})
	cmd, _ := m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.writing {
		t.Fatal("explicit confirmation did not start the replacement")
	}
	m.Update(cmd())
	if backend.applyCalls != 1 || backend.applyKind != vault.ReplaceAttachment || backend.attachmentN != "document.pdf" || backend.attachment != "new attachment" {
		t.Fatalf("replacement change = calls %d kind %q name %q data %q", backend.applyCalls, backend.applyKind, backend.attachmentN, backend.attachment)
	}
}

func TestQClosesOverlayButCanBeTypedInAttachmentPath(t *testing.T) {
	t.Skip("attachment UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-q.kdbx"), backend)
	m.openAttachments()
	m.extras.phase = "path"
	m.extras.action = "add"
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.extras == nil || m.extras.input.value() != "q" {
		t.Fatal("q did not remain ordinary input in a path field")
	}
	m.openTOTP()
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.extras != nil {
		t.Fatal("q did not close the TOTP overlay")
	}
}

func TestPlaintextAttachmentExportRequiresConfirmAndUsesPrivateExclusiveFile(t *testing.T) {
	t.Skip("attachment UI is hidden in beta; retain this test for re-enablement")
	backend := &extrasTestBackend{exportData: []byte("private attachment bytes")}
	m := newExtrasTestModel(t, extrasEntryDB("/tmp/extras-export.kdbx"), backend)
	dest := filepath.Join(t.TempDir(), "document.pdf")
	m.openAttachments()
	m.extras.selected = 1
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m.extras.input.set(dest)
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.extras.phase != "confirm-export" || backend.exportCalls != 0 {
		t.Fatal("export did not wait for explicit confirmation")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	if backend.exportCalls != 0 {
		t.Fatal("default Cancel selection exported plaintext")
	}

	m.extras.selected = 1
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m.extras.input.set(dest)
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRight})
	cmd, _ := m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("explicit export confirmation did not start")
	}
	if _, ok := m.updateExtras(cmd().(extrasExportMsg)); !ok {
		t.Fatal("export result was not handled")
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != string(backend.exportData) {
		t.Fatalf("exported data = %q, err = %v", data, err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("export permissions = %v, err = %v", info.Mode().Perm(), err)
	}
	if err := writePrivateExclusive(context.Background(), dest, []byte("overwrite")); err == nil {
		t.Fatal("exclusive export overwrote an existing file")
	}
	data, _ = os.ReadFile(dest)
	if string(data) != string(backend.exportData) {
		t.Fatal("existing export changed after an overwrite attempt")
	}
}

func TestHistoryMasksSecretsAndAllowsTimedExplicitRevealAndLongTextScroll(t *testing.T) {
	t.Skip("History UI is hidden in beta; retain this test for re-enablement")
	db := extrasEntryDB("/tmp/extras-history.kdbx")
	notes := make([]string, 40)
	for i := range notes {
		notes[i] = fmt.Sprintf("NOTE-%02d full historical paragraph", i)
	}
	db.Entries[0].History = []vault.Entry{{
		UUID:     "a",
		Modified: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		Fields: []vault.Field{
			{Name: "Title", Value: "Old title"},
			{Name: "Password", Value: "old-history-password", Protected: true},
			{Name: "otp", Value: "old-history-totp-seed", Protected: true},
			{Name: "Notes", Value: strings.Join(notes, "\n")},
		},
	}}
	backend := &extrasTestBackend{}
	m := newExtrasTestModel(t, db, backend)
	m.openHistory()
	m.extrasKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.extrasKey(tea.KeyMsg{Type: tea.KeyDown})
	text := ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if strings.Contains(text, "old-history-password") || strings.Contains(text, "old-history-totp-seed") || !strings.Contains(text, "••••••••") {
		t.Fatal("history viewer exposed a protected value by default")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyDown})
	if text := ansi.Strip(strings.Join(m.extrasLines(), "\n")); strings.Contains(text, "old-history-totp-seed") {
		t.Fatal("history viewer exposed a TOTP seed")
	}
	firstPage := ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if !strings.Contains(firstPage, "NOTE-00") || strings.Contains(firstPage, "NOTE-39") {
		t.Fatal("long history value did not show a partial first page")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyPgDown})
	secondPage := ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if strings.Contains(secondPage, "NOTE-39") {
		t.Fatal("long history value unexpectedly skipped its middle page")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyPgDown})
	lastPage := ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if !strings.Contains(lastPage, "NOTE-39") {
		t.Fatal("long history value cannot be scrolled to the end")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyDown})
	if text := ansi.Strip(strings.Join(m.extrasLines(), "\n")); strings.Contains(text, "old-history-totp-seed") {
		t.Fatal("selected TOTP seed was exposed")
	}
	m.extrasKey(tea.KeyMsg{Type: tea.KeyUp})
	m.extrasKey(tea.KeyMsg{Type: tea.KeyUp})
	m.extrasKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	text = ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if !strings.Contains(text, "old-history-password") {
		t.Fatal("explicit history reveal did not show the selected protected field")
	}
	m.extrasTick(m.now().Add(6 * time.Second))
	text = ansi.Strip(strings.Join(m.extrasLines(), "\n"))
	if strings.Contains(text, "old-history-password") {
		t.Fatal("history reveal remained visible after its timeout")
	}
	if strings.Contains(text, "Restore") || !strings.Contains(text, "no restore action") {
		t.Fatal("history viewer must be read-only and omit restore actions")
	}
}
