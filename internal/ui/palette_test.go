package ui

import (
	"reflect"
	"testing"
)

func TestPaletteThemePreviewCancelRestoresStylesAndViewFilters(t *testing.T) {
	m, _ := operationModel(t)
	v := m.ws.ActiveView()
	v.Focus = 2
	v.Query = "Alpha"
	v.NavFilter = "Root"
	v.DetailFilter = "Password"
	beforeView := *v
	beforeSettings := m.settings
	beforeStyles := m.styles

	m.openPalette("actions", false)
	m.palette.query.set("Choose Theme")
	m.refreshPalette()
	if got := m.paletteChoices(); len(got) != 1 || got[0].next != "themes" {
		t.Fatalf("theme command choices = %#v", got)
	}
	m.executePalette()
	m.palette.query.set("Nord")
	m.refreshPalette()
	m.previewChoice()
	if m.settings.Theme != "nord" || reflect.DeepEqual(m.styles, beforeStyles) {
		t.Fatal("theme selection did not preview the requested theme")
	}

	m.key(key("esc"))
	if m.palette == nil || m.palette.scope != "actions" {
		t.Fatal("Escape did not return to the parent palette")
	}
	if m.settings != beforeSettings || !reflect.DeepEqual(m.styles, beforeStyles) {
		t.Fatal("canceling theme preview did not restore original settings and styles")
	}
	m.key(key("esc"))
	if m.palette != nil {
		t.Fatal("Escape did not close the parent palette")
	}
	if *m.ws.ActiveView() != beforeView {
		t.Fatal("palette preview changed active view focus or filters")
	}
}
