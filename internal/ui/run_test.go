package ui

import (
	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	"bytes"
	"context"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

type partialRunResolver struct {
	calls int
	first []byte
}

func (*partialRunResolver) Open(context.Context, string, vault.Credentials) (*vault.Database, error) {
	return nil, errors.New("unexpected open")
}

func (b *partialRunResolver) ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error) {
	b.calls++
	if b.calls == 1 {
		b.first = []byte("partial-secret")
		return b.first, nil
	}
	return nil, errors.New("second field failed")
}

func TestRunGrantJobClearsEarlierValuesAfterPartialFailure(t *testing.T) {
	db := &vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{{UUID: "one"}, {UUID: "two"}}}
	backend := &partialRunResolver{}
	m := New(Options{Backend: backend})
	m.ws.Open(db)
	session := m.ws.ActiveSession()
	p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/app"}}}
	cmd := m.runGrantJob(p, []runTarget{
		{ref: "one", session: session.ID, entry: "one", field: "Password", database: db},
		{ref: "two", session: session.ID, entry: "two", field: "Password", database: db},
	}, "a")
	result := cmd().(runResult)
	if result.err == nil || result.secrets == nil {
		t.Fatal("expected partial resolution failure")
	}
	if !bytes.Equal(backend.first, make([]byte, len(backend.first))) {
		t.Fatal("partial result retained secret bytes")
	}
}

func TestRunUpdateClearsValuesForInvalidatedVault(t *testing.T) {
	m := New(Options{})
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	session := m.ws.ActiveSession()
	stale := &vault.Database{Path: "/vault", RootUUID: "root"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &runpkg.Pending{Context: ctx, Request: runpkg.Request{Command: []string{"/app"}}}
	secret := []byte("secret")
	secrets := newRunSecretValues(context.Background())
	secrets.Put("ref", secret)
	m.runs = &runState{busy: true, pending: []*runpkg.Pending{p}}
	m.updateRun(runResult{
		pending: p,
		targets: []runTarget{{session: session.ID, database: stale}},
		secrets: secrets,
	})
	if !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatal("invalidated result retained secret bytes")
	}
}

func TestRunSecretValuesClearOnCancellationAfterCommandReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secrets := newRunSecretValues(ctx)
	value := []byte("secret")
	secrets.Put("ref", value)
	cancel()
	<-secrets.done
	if !bytes.Equal(value, make([]byte, len(value))) {
		t.Fatal("dropped command result retained secret bytes after cancellation")
	}
	late := []byte("late")
	secrets.Put("late", late)
	if !bytes.Equal(late, make([]byte, len(late))) {
		t.Fatal("put after cancellation retained secret bytes")
	}
}

func TestRunSecretValuesTakeAndClearAreSafeConcurrently(t *testing.T) {
	secrets := newRunSecretValues(context.Background())
	value := []byte("secret")
	secrets.Put("ref", value)
	start := make(chan struct{})
	results := make(chan map[string][]byte, 1)
	go func() {
		<-start
		results <- secrets.Take()
	}()
	go func() {
		<-start
		secrets.Clear()
	}()
	close(start)
	values := <-results
	clearRunValues(values)
	secrets.Clear()
	if !bytes.Equal(value, make([]byte, len(value))) {
		t.Fatal("concurrent take/clear left secret bytes")
	}
}

func TestRunAmbiguityRequiresExplicitSelection(t *testing.T) {
	m := New(Options{})
	for _, path := range []string{"/one", "/two"} {
		m.ws.Open(&vault.Database{Path: path, RootUUID: "root", Entries: []vault.Entry{{UUID: "entry", GroupPath: "Passwords", Fields: []vault.Field{{Name: "Title", Value: "Seed Entry"}, {Name: "Password", Protected: true}}}}})
	}
	p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/bin/sh"}, References: []string{"bypass://Seed Entry"}}}
	m.Update(RunRequestMsg{p})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
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

func TestRunIdentityLabelIsSafeAcrossCompactLayouts(t *testing.T) {
	const bearer = "Luna-0123456789abcdef0123456789abcdef"
	principal, err := runpkg.ParseIdentity([]byte(bearer))
	if err != nil {
		t.Fatal(err)
	}
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}, {120, 35}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			p := &runpkg.Pending{Context: context.Background(), Identity: principal, Request: runpkg.Request{Command: []string{"/app"}, Directory: "/project"}}
			db := &vault.Database{}
			m.runs = &runState{
				visible: true, pending: []*runpkg.Pending{p},
				targets:     []runTarget{{ref: "bypass://Safe", field: "Password", database: db}},
				newIdentity: map[*runpkg.Pending]bool{p: true}, err: "approval unavailable",
			}
			view := m.View()
			for _, label := range []string{"Luna", "new agent", "Deny", "Allow once", "Always allow"} {
				if !strings.Contains(view, label) {
					t.Errorf("%s %v layout omitted %q", appearance, size, label)
				}
			}
			if strings.Contains(view, bearer) || strings.Contains(view, principal.Digest) {
				t.Errorf("%s %v layout exposed bearer material", appearance, size)
			}
			lines := strings.Join(m.runLines(), "\n")
			if !strings.Contains(lines, "Agent: Luna (new agent)") || strings.Contains(lines, bearer) {
				t.Errorf("%s %v request lines omitted readable identity or exposed bearer", appearance, size)
			}
		}
	}
}

func TestRunLockInvalidatesReviewedApproval(t *testing.T) {
	m := New(Options{})
	m.ws.Open(&vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{{UUID: "entry", GroupPath: "Passwords", Fields: []vault.Field{{Name: "Title", Value: "Secret"}}}}})
	p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/bin/sh"}, References: []string{"bypass://Secret"}}}
	m.Update(RunRequestMsg{p})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
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

func TestRunPaletteEntryAndReloadShortcut(t *testing.T) {
	m := New(Options{})
	found := false
	for _, command := range m.commands() {
		if command.id == "run-requests" {
			found = true
			if command.reason != "" {
				t.Fatal("pending requests require an unlocked vault")
			}
			command.run()
		}
	}
	if !found || m.runs == nil || !m.runs.visible {
		t.Fatal("palette did not open pending requests")
	}
	m.runs.visible = false
	if _, handled := m.updateRun(tea.KeyMsg{Type: tea.KeyCtrlR}); handled {
		t.Fatal("run requests stole vault reload shortcut")
	}
}
