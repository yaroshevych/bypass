package ui

import (
	"testing"

	"bypass/internal/vault"
)

func TestTinyTerminalKeepsDraftWithoutAcceptingHiddenInput(t *testing.T) {
	m, _ := operationModel(t)
	m.key(key("n"))
	m.editor.inputs[editTitle].set("Keep draft")
	m.width, m.height = 20, 5
	m.key(key("x"))
	m.key(key("q"))
	if m.editor == nil || m.editor.inputs[editTitle].value() != "Keep draft" || m.dialog != nil {
		t.Fatal("tiny state changed the hidden draft or opened a hidden confirmation")
	}
	m.width, m.height = 120, 35
	if !m.editor.dirty() {
		t.Fatal("resizing lost the draft")
	}
}

func TestRecentDestinationsOmitInvalidGroupsAndClearOnLock(t *testing.T) {
	db := &vault.Database{Groups: []vault.Group{{UUID: "a"}, {UUID: "b"}, {UUID: "trash", Recycled: true}}}
	groups := destinationGroups(db, []string{"missing", "trash", "b", "b"})
	if len(groups) != 2 || groups[0].UUID != "b" || groups[1].UUID != "a" {
		t.Fatalf("invalid recent destination order: %#v", groups)
	}
	m, _ := operationModel(t)
	s := m.ws.ActiveSession()
	s.Destinations = []string{"private-group"}
	m.lockSession(s.ID)
	if len(s.Destinations) != 0 {
		t.Fatal("full lock kept destination history")
	}
}

func TestTinyTerminalDoesNotAcceptHiddenPINInput(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	m.gate.Protect()
	m.width, m.height = 20, 5
	m.key(key("1234"))
	m.key(key("enter"))
	if !m.gate.Protected() || m.pinInput.value() != "" {
		t.Fatal("hidden PIN form accepted input")
	}
	m.width, m.height = 120, 35
	m.key(key("1234"))
	m.key(key("enter"))
	if m.gate.Protected() {
		t.Fatal("resizing did not restore the PIN form")
	}
}
