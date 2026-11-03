package ui

import (
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
