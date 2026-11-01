package ui

import (
	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestRunAmbiguityRequiresExplicitSelection(t *testing.T) {
	m := New(Options{})
	for _, path := range []string{"/one", "/two"} {
		m.ws.Open(&vault.Database{Path: path, RootUUID: "root", Entries: []vault.Entry{{UUID: "entry", GroupPath: "Passwords", Fields: []vault.Field{{Name: "Title", Value: "Seed Entry"}, {Name: "Password", Protected: true}}}}})
	}
	p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/bin/sh"}, References: []string{"bypass://Seed Entry"}}}
	m.Update(RunRequestMsg{p})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.runs.choices) != 2 || len(m.runs.targets) != 0 {
		t.Fatalf("ambiguity skipped: choices=%d targets=%d", len(m.runs.choices), len(m.runs.targets))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.runs.targets) != 1 {
		t.Fatal("selection failed")
	}
}
func TestRunSwitchingRequestClearsReviewedTargets(t *testing.T) {
	m := New(Options{})
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{{Context: context.Background()}, {Context: context.Background()}}, targets: []runTarget{{ref: "old"}}}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if len(m.runs.targets) != 0 || m.runs.index != 1 {
		t.Fatal("review survived switching requests")
	}
}

func TestRunApprovalActionsRemainVisibleAcrossLayouts(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}, {120, 35}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/app"}, Directory: "/project"}}
			m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}, targets: []runTarget{{ref: "bypass://entry", field: "Password", database: &vault.Database{}}}}
			view := m.View()
			for _, label := range []string{"Deny", "Allow once", "Always allow"} {
				if !strings.Contains(view, label) {
					t.Errorf("%s %v hides %s", appearance, size, label)
				}
			}
		}
	}
}

func TestRunLockInvalidatesReviewedApproval(t *testing.T) {
	m := New(Options{})
	m.ws.Open(&vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{{UUID: "entry", GroupPath: "Passwords", Fields: []vault.Field{{Name: "Title", Value: "Secret"}}}}})
	p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/bin/sh"}, References: []string{"bypass://Secret"}}}
	m.Update(RunRequestMsg{p})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.runs.targets) != 1 {
		t.Fatal("request was not reviewed")
	}
	m.ws.LockAll()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil || len(m.runs.targets) != 0 {
		t.Fatal("locked vault retained an actionable approval")
	}
}
