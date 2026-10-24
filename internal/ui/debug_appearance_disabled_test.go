//go:build !debug

package ui

import "testing"

func TestShiftDDoesNotToggleAppearanceInNormalBuild(t *testing.T) {
	m := New(Options{})
	m.key(key("D"))
	if m.settings.Appearance != "classic" {
		t.Fatalf("normal build changed appearance to %q", m.settings.Appearance)
	}
}
