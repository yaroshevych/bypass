package ui

import (
	wipebuf "bypass/internal/secret"
	"bytes"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputGrowthWipesOldStorageAndEncodesUTF8(t *testing.T) {
	var in input
	in.text = make([]rune, 1, 1)
	in.text[0] = 'é'
	in.cursor = 1
	old := in.text[:cap(in.text)]
	in.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("密")})
	if old[0] != 0 {
		t.Fatal("old input storage was not wiped")
	}
	encoded := in.bytes()
	defer clear(encoded)
	if !bytes.Equal(encoded, []byte("é密")) {
		t.Fatal("incorrect UTF-8 encoding")
	}
	in.clear()
}

func TestSecretKeystrokesAreConsumedAndWiped(t *testing.T) {
	m := New(Options{InitialPath: "/tmp/test.kdbx"})
	typed := []rune("héllo")
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: typed})
	if got := m.inputs[0].bytes(); !bytes.Equal(got, []byte("héllo")) {
		t.Fatal("password input lost")
	}
	for _, r := range typed {
		if r != 0 {
			t.Fatal("key event retained secret")
		}
	}
	m.key(tea.KeyMsg{Type: tea.KeySpace})
	encoded := m.inputs[0].bytes()
	if !bytes.Equal(encoded, []byte("héllo ")) {
		t.Fatal("space was not added to password input")
	}
	wipebuf.Wipe(encoded)
	m.inputs[0].clear()
}
