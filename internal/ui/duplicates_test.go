package ui

import (
	"fmt"
	"strings"
	"testing"

	"bypass/internal/platform"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDuplicateConfirmationPreviewAndCancel(t *testing.T) {
	for _, appearance := range []string{"Classic", "Compact"} {
		t.Run(appearance, func(t *testing.T) {
			m := New(Options{Settings: platform.Settings{Appearance: appearance}})
			defer m.Close()
			m.width, m.height = 80, 24
			plan := &vault.DuplicateEntries{}
			for i := 1; i <= 30; i++ {
				plan.Renames = append(plan.Renames, vault.EntryRename{Path: "/Amazon", Title: fmt.Sprintf("Amazon %d", i)})
			}
			m.Update(openedMsg{request: m.request, err: plan})
			if m.dialog == nil || m.dialog.selected != 0 {
				t.Fatal("repair must default to cancel")
			}
			preview := strings.Join(m.dialogLines(), "\n")
			if !strings.Contains(preview, "Amazon 1") || !strings.Contains(preview, "Cancel") || !strings.Contains(preview, "Rename and Open") {
				t.Fatal("missing preview or actions")
			}
			m.dialogKey(tea.KeyMsg{Type: tea.KeyPgDown})
			if m.dialog.offset == 0 {
				t.Fatal("preview cannot page")
			}
			if cmd := m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || m.dialog != nil {
				t.Fatal("default confirmation did not cancel")
			}
		})
	}
}

func TestDuplicatePreviewUsesSmallPaneSpace(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 16
	plan := &vault.DuplicateEntries{}
	for i := 0; i < 20; i++ {
		plan.Renames = append(plan.Renames, vault.EntryRename{Title: fmt.Sprintf("Entry %d", i)})
	}
	m.dialog = &actionDialog{kind: "duplicates", duplicates: plan, options: []string{"Cancel", "Rename and Open"}}
	if page := m.duplicatePageSize(); page < 4 {
		t.Fatalf("small pane shows only %d duplicate entries", page)
	}
	m.dialogKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.dialog.offset == 0 {
		t.Fatal("duplicate list did not page")
	}
}

func TestLateDuplicateOfferIsDiscarded(t *testing.T) {
	m := New(Options{})
	defer m.Close()
	plan := &vault.DuplicateEntries{}
	m.Update(openedMsg{request: m.request + 1, err: plan})
	if m.dialog != nil {
		t.Fatal("late open restored a repair dialog")
	}
}
