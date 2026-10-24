//go:build debug

package ui

import "testing"

func TestDebugShiftDTogglesAppearanceWithoutPersistence(t *testing.T) {
	m := New(Options{})
	if m.settings.Appearance != "classic" {
		t.Fatalf("initial appearance = %q", m.settings.Appearance)
	}
	m.key(key("D"))
	if m.settings.Appearance != "compact" {
		t.Fatalf("Shift+D appearance = %q, want compact", m.settings.Appearance)
	}
	m.key(key("D"))
	if m.settings.Appearance != "classic" {
		t.Fatalf("second Shift+D appearance = %q, want classic", m.settings.Appearance)
	}
}
