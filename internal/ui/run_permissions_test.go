package ui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
)

func openPermissionsFromPalette(t *testing.T, m *Model) {
	t.Helper()
	m.openPalette("actions", false)
	for i, choice := range m.paletteChoices() {
		if choice.id == "run-permissions" {
			if choice.reason != "" {
				t.Fatalf("permissions action disabled: %s", choice.reason)
			}
			m.palette.selected = i
			m.executePalette()
			if m.dialog == nil || m.dialog.kind != "run-permissions" {
				t.Fatal("permissions dialog did not open")
			}
			return
		}
	}
	t.Fatal("permissions action missing")
}

func TestSavedPermissionsDialogEmptyAndLocked(t *testing.T) {
	m := New(Options{})
	openPermissionsFromPalette(t, m)
	if !strings.Contains(strings.Join(m.dialog.message, " "), "Unlock a vault") {
		t.Fatal("missing locked state")
	}
	m.key(key("esc"))
	db := fixture("/tmp/fixture.kdbx")
	m.ws.Open(db)
	m.normalize()
	openPermissionsFromPalette(t, m)
	if !strings.Contains(strings.Join(m.dialog.message, " "), "No saved permissions") {
		t.Fatal("missing empty state")
	}
	if len(m.dialog.options) != 1 || m.dialog.options[0] != "Close" {
		t.Fatal("empty dialog has unexpected actions")
	}
	m.key(key("enter"))
	if m.dialog != nil {
		t.Fatal("close did not dismiss dialog")
	}
}

func TestSavedPermissionsDialogListsAndManagesEntries(t *testing.T) {
	m := New(Options{})
	db := fixture("/tmp/fixture.kdbx")
	db.Groups = append(db.Groups, vault.Group{UUID: "grants", ParentUUID: db.RootUUID, Name: runpkg.GrantGroup, SearchEnabled: true})
	db.Entries = append(db.Entries, vault.Entry{UUID: "permission", GroupUUID: "grants", Fields: []vault.Field{{Name: "Title", Value: "deploy access"}}})
	m.ws.Open(db)
	m.normalize()
	openPermissionsFromPalette(t, m)
	if len(m.dialog.message) != 1 || m.dialog.message[0] != "deploy access" {
		t.Fatal("saved permission not listed")
	}
	m.key(key("enter"))
	if m.dialog != nil || m.currentNav().group != "grants" {
		t.Fatal("manage did not open permissions group")
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
	openPermissionsFromPalette(t, m)
	if !strings.Contains(m.View(), "permission 00") {
		t.Fatal("first page not visible")
	}
	m.key(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.dialog.offset == 0 || strings.Contains(m.View(), "permission 00") {
		t.Fatal("paging did not show later permissions")
	}
	if !strings.Contains(m.View(), "Manage permissions") || !strings.Contains(m.View(), "Close") {
		t.Fatal("dialog actions missing in small window")
	}
}
