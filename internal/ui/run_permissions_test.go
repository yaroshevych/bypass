package ui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
)

func openPermissionsFromRequests(t *testing.T, m *Model) {
	t.Helper()
	if m.runs == nil {
		m.runs = &runState{}
	}
	m.runs.visible = true
	m.key(key("r"))
	if m.dialog == nil || m.dialog.kind != "run-permissions" {
		t.Fatal("session permissions dialog did not open")
	}
}

func TestSavedPermissionsDialogEmptyAndLocked(t *testing.T) {
	m := New(Options{})
	openPermissionsFromRequests(t, m)
	if !strings.Contains(strings.Join(m.dialog.message, " "), "No session permissions") {
		t.Fatal("missing locked state")
	}
	m.key(key("esc"))
	db := fixture("/tmp/fixture.kdbx")
	m.ws.Open(db)
	m.normalize()
	openPermissionsFromRequests(t, m)
	if !strings.Contains(strings.Join(m.dialog.message, " "), "No session permissions") {
		t.Fatal("missing empty state")
	}
	if len(m.dialog.options) != 2 || m.dialog.options[1] != "Close" {
		t.Fatal("empty dialog has unexpected actions")
	}
	m.key(key("enter"))
	if m.dialog != nil {
		t.Fatal("close did not dismiss dialog")
	}
}

func TestSavedPermissionsDialogListsAndCloses(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/fixture.kdbx")
	db.Groups = append(db.Groups, vault.Group{UUID: "grants", ParentUUID: db.RootUUID, Name: runpkg.GrantGroup, SearchEnabled: true})
	db.Entries = append(db.Entries, vault.Entry{UUID: "permission", GroupUUID: "grants", Fields: []vault.Field{{Name: "Title", Value: "deploy access"}}})
	m.ws.Open(db)
	m.normalize()
	m.runs = &runState{grants: map[runSessionGrant]string{{m.ws.ActiveSession().ID, "digest", "permission", "Password"}: "agent"}}
	openPermissionsFromRequests(t, m)
	if len(m.dialog.message) != 1 || !strings.Contains(m.dialog.message[0], "deploy access") {
		t.Fatal("saved permission not listed")
	}
	m.key(key("enter"))
	if m.dialog != nil {
		t.Fatal("close did not dismiss dialog")
	}
}

func TestSavedPermissionsDialogPagesInSmallWindow(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 16
	db := fixture("/tmp/fixture.kdbx")
	db.Groups = append(db.Groups, vault.Group{UUID: "grants", ParentUUID: db.RootUUID, Name: runpkg.GrantGroup, SearchEnabled: true})
	for i := 0; i < 30; i++ {
		db.Entries = append(db.Entries, vault.Entry{UUID: fmt.Sprint(i), GroupUUID: "grants", Fields: []vault.Field{{Name: "Title", Value: fmt.Sprintf("permission %02d", i)}}})
	}
	m.ws.Open(db)
	m.normalize()
	m.runs = &runState{grants: make(map[runSessionGrant]string)}
	for i := 0; i < 30; i++ {
		m.runs.grants[runSessionGrant{m.ws.ActiveSession().ID, "digest", fmt.Sprint(i), "Password"}] = "agent"
	}
	openPermissionsFromRequests(t, m)
	if !strings.Contains(m.View(), "permission 00") {
		t.Fatal("first page not visible")
	}
	m.key(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.dialog.offset == 0 || strings.Contains(m.View(), "permission 00") {
		t.Fatal("paging did not show later permissions")
	}
	if !strings.Contains(m.View(), "Close") {
		t.Fatal("dialog actions missing in small window")
	}
}

func TestSessionPermissionsShowsAndClearsAllVaults(t *testing.T) {
	m := New(Options{})
	a := fixture("/tmp/first.kdbx")
	m.ws.Open(a)
	first := m.ws.ActiveSession().ID
	b := fixture("/tmp/second.kdbx")
	m.ws.Open(b)
	second := m.ws.ActiveSession().ID
	m.runs = &runState{visible: true, grants: map[runSessionGrant]string{{first, "a", "a", "Password"}: "dev", {second, "b", "b", "Username"}: "prod"}}
	openPermissionsFromRequests(t, m)
	text := strings.Join(m.dialog.message, " ")
	for _, want := range []string{"first.kdbx", "second.kdbx", "dev", "prod"} {
		if !strings.Contains(text, want) {
			t.Fatalf("permissions missing %q", want)
		}
	}
	m.lockSession(first)
	text = strings.Join(m.dialog.message, " ")
	if strings.Contains(text, "first.kdbx") || !strings.Contains(text, "second.kdbx") {
		t.Fatal("locking a vault did not refresh the permissions dialog")
	}
	m.key(key("c"))
	if len(m.runs.grants) != 0 || !strings.Contains(strings.Join(m.dialog.message, " "), "No session permissions") {
		t.Fatal("clear all did not update the list")
	}
}

func TestSecretRequestsIsOnlyPermissionsCommand(t *testing.T) {
	m := New(Options{})
	found := false
	for _, command := range m.commands() {
		if command.id == "run-permissions" {
			t.Fatal("saved permissions command still available")
		}
		if command.id == "run-requests" {
			found = command.label == "Secret Requests…"
		}
	}
	if !found {
		t.Fatal("Secret Requests command missing")
	}
}
