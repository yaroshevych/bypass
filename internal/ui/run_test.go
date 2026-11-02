package ui

import (
	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	"bytes"
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
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

func TestRunReviewDefaultsToSafeBackAction(t *testing.T) {
	m := New(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &runpkg.Pending{Context: ctx}
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}}
	m.updateRun(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.runs.review || m.runReviewActions()[m.runs.reviewFocus].key != "esc" {
		t.Fatal("review did not focus Back by default")
	}
	_, handled := m.updateRun(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || m.runs.review || m.runs.busy || len(m.runs.pending) != 1 {
		t.Fatal("Enter did not activate the safe Back default")
	}
}

func TestRunReviewFocusDoesNotChangeRequestsFocusBehindIt(t *testing.T) {
	p := &runpkg.Pending{Context: context.Background()}
	m := New(Options{})
	m.width, m.height = 80, 24
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}, actionFocus: 2}
	m.enterRunReview()
	if m.runs.actionFocus != 2 || m.runs.reviewFocus != len(m.runReviewActions())-1 {
		t.Fatalf("opening review changed request focus: requests=%d review=%d", m.runs.actionFocus, m.runs.reviewFocus)
	}
	m.updateRun(tea.KeyMsg{Type: tea.KeyUp})
	if m.runs.actionFocus != 2 || m.runs.reviewFocus != len(m.runReviewActions())-2 {
		t.Fatalf("review navigation changed parent focus: requests=%d review=%d", m.runs.actionFocus, m.runs.reviewFocus)
	}
	if got := m.runRequestActions()[m.runs.actionFocus].key; got != "r" {
		t.Fatalf("parent focus moved to %q, want reset action", got)
	}
}

func TestRunReviewTracksPendingAcrossDisconnectPruning(t *testing.T) {
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	first := &runpkg.Pending{Context: firstCtx}
	second := &runpkg.Pending{Context: secondCtx}
	m := New(Options{})
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	m.runs = &runState{
		visible: true, review: true, reviewed: second, index: 1,
		pending: []*runpkg.Pending{first, second}, targets: []runTarget{{ref: "bypass://second", session: m.ws.ActiveSession().ID, database: db}},
	}
	cancelFirst()
	m.updateRun(RunDisconnectedMsg{})
	if !m.runs.review || m.runs.reviewed != second || m.runs.index != 0 || len(m.runs.targets) != 1 {
		t.Fatal("pruning another request changed the reviewed target")
	}
	cancelSecond()
	m.updateRun(RunDisconnectedMsg{})
	if m.runs.review || m.runs.reviewed != nil || len(m.runs.targets) != 0 {
		t.Fatal("disconnect left a canceled request actionable")
	}
}

func TestRunReviewSurvivesOtherRequestCompletion(t *testing.T) {
	first := &runpkg.Pending{Context: context.Background()}
	second := &runpkg.Pending{Context: context.Background()}
	m := New(Options{})
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	m.runs = &runState{
		visible: true, review: true, reviewed: second, index: 1,
		pending: []*runpkg.Pending{first, second},
		targets: []runTarget{{ref: "bypass://second", session: m.ws.ActiveSession().ID, database: db}},
	}
	m.removeRun(first)
	if !m.runs.review || m.runs.reviewed != second || m.runs.index != 0 || len(m.runs.targets) != 1 {
		t.Fatal("another request completing changed the open review")
	}
}

func TestRunActionMouseHitUsesRenderedGeometry(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := New(Options{})
		m.settings.Appearance = "classic"
		if compact {
			m.settings.Appearance = "compact"
		}
		m.width, m.height = 80, 24
		p := &runpkg.Pending{Context: context.Background()}
		m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{ref: "bypass://entry"}}}
		rows := strings.Split(m.View(), "\n")
		actionY := -1
		for y, row := range rows {
			if strings.Contains(ansi.Strip(row), "Deny") {
				actionY = y
				break
			}
		}
		layout := m.runLayout(true)
		if action, ok := m.runActionAt(layout.leftEdge+layout.width/2, actionY); !ok || action != 0 {
			t.Fatalf("%s action geometry missed first button: action=%d ok=%v", m.settings.Appearance, action, ok)
		}
	}
}

func TestRunResetConfirmationDefaultsToCancel(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 80, 24
	p := &runpkg.Pending{Context: context.Background()}
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}}
	m.updateRun(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !m.runs.resetConfirm || m.runResetActions()[m.runs.resetFocus].key != "esc" {
		t.Fatal("permission reset did not open with Cancel selected")
	}
	view := m.View()
	for _, text := range []string{"RESET SAVED PERMISSIONS", "Reset affects unlocked vaults", "Locked vaults stay unchanged", "Pending requests stay queued", "Cancel", "Reset"} {
		if !strings.Contains(view, text) {
			t.Errorf("reset confirmation omitted %q", text)
		}
	}
	m.updateRun(tea.KeyMsg{Type: tea.KeyEnter})
	if m.runs.resetConfirm || m.runs.resetBusy || len(m.runs.pending) != 1 {
		t.Fatal("Enter did not safely cancel the reset confirmation")
	}
}

func TestRunRequestsSmallScreenShowsSelectedQueueItem(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 48, 14
		pending := make([]*runpkg.Pending, 4)
		for i := range pending {
			pending[i] = &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{fmt.Sprintf("/tools/app%d", i+1)}}}
		}
		m.runs = &runState{visible: true, pending: pending, index: 3}
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "Pending secret requests (4)") || !strings.Contains(view, "app4") {
			t.Errorf("%s 48x14 view hid the selected queue item: %q", appearance, view)
		}
	}
}

func TestRunRequestsNavigationIncludesListAndButtons(t *testing.T) {
	m := New(Options{})
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{{Context: context.Background()}, {Context: context.Background()}}}
	key := func(k tea.KeyType) { m.updateRun(tea.KeyMsg{Type: k}) }
	key(tea.KeyDown)
	if m.runs.index != 1 || m.runs.buttonsFocused {
		t.Fatal("Down did not select the next request")
	}
	key(tea.KeyTab)
	if !m.runs.buttonsFocused || m.runs.actionFocus != 0 {
		t.Fatal("Tab did not move from the list to Deny all")
	}
	key(tea.KeyDown)
	if m.runs.actionFocus != 1 {
		t.Fatal("Down did not select Approve all")
	}
	key(tea.KeyUp)
	key(tea.KeyShiftTab)
	if m.runs.buttonsFocused || m.runs.index != 1 {
		t.Fatal("Shift+Tab did not return to the last request")
	}
	key(tea.KeyUp)
	key(tea.KeyShiftTab)
	if !m.runs.buttonsFocused || m.runRequestActions()[m.runs.actionFocus].key != "esc" {
		t.Fatal("Shift+Tab did not wrap to Close")
	}
	key(tea.KeyTab)
	if m.runs.buttonsFocused || m.runs.index != 0 {
		t.Fatal("Tab did not wrap to the first request")
	}
	key(tea.KeyEnter)
	if !m.runs.review {
		t.Fatal("Enter on a request did not open review")
	}
}

func TestRunFocusMovesToButtonsWhenLastRequestRemoved(t *testing.T) {
	m := New(Options{})
	p := &runpkg.Pending{Context: context.Background()}
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}}
	m.removeRun(p)
	m.updateRun(tea.KeyMsg{Type: tea.KeyDown})
	if !m.runs.buttonsFocused || m.runs.actionFocus != 1 {
		t.Fatal("first Down after removing the last request did not move to Approve all")
	}
}

func TestRunReviewPagesFieldsWithoutSkipping(t *testing.T) {
	m := New(Options{})
	m.settings.Appearance = "classic"
	m.width, m.height = 80, 24
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "agent", Digest: "digest"}, Request: runpkg.Request{Command: []string{"/app"}, Directory: "/project"}, Caller: []string{"runner"}}
	targets := make([]runTarget, 20)
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	for i := range targets {
		targets[i] = runTarget{ref: fmt.Sprintf("bypass://field%d", i+1), field: fmt.Sprintf("field%d", i+1), session: m.ws.ActiveSession().ID, database: db}
	}
	m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: targets}
	first := ansi.Strip(m.View())
	if !strings.Contains(first, "1/20: field1") || !strings.Contains(first, "5/20: field5") || strings.Contains(first, "6/20: field6") {
		t.Fatalf("first field page did not match its visible rows: %q", first)
	}
	size := m.runFieldPageSize()
	if size != 5 {
		t.Fatalf("page size ignored visible metadata: %d", size)
	}
	m.updateRun(tea.KeyMsg{Type: tea.KeyPgDown})
	second := ansi.Strip(m.View())
	if !strings.Contains(second, "6/20: field6") || !strings.Contains(second, "10/20: field10") || strings.Contains(second, "11/20: field11") {
		t.Fatalf("second field page skipped or hid rows: %q", second)
	}
}

func TestRunIdentitySaveIsBlockedForReadOnlyVault(t *testing.T) {
	m := New(Options{})
	db := &vault.Database{Path: "/vault", RootUUID: "root"}
	m.ws.Open(db)
	session := m.ws.ActiveSession()
	session.ReadOnly = true
	p := &runpkg.Pending{
		Context: context.Background(), Identity: runpkg.Identity{Name: "dev", Digest: "digest"},
	}
	m.runs = &runState{
		visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p},
		targets: []runTarget{{ref: "bypass://entry", session: session.ID, database: db}},
	}
	cmd, handled := m.updateRun(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !handled || cmd != nil || m.runs.busy || m.runs.err != "vault is read-only; use Allow" {
		t.Fatal("identity permission save started for a read-only vault")
	}
}

func TestRunApprovalActionsRemainVisibleAcrossLayouts(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}, {120, 35}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/app"}, Directory: "/project"}}
			m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{ref: "bypass://entry", field: "Password", database: &vault.Database{}}}}
			view := m.View()
			for _, label := range []string{"Deny", "Allow", "Allow for this identity", "Back"} {
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
				visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p},
				targets:     []runTarget{{ref: "bypass://Safe", field: "Password", database: db}},
				newIdentity: map[*runpkg.Pending]bool{p: true}, err: "approval unavailable",
			}
			view := m.View()
			labels := []string{"Luna", "Deny", "Allow", "Allow for this identity", "Back"}
			if size[1] > 16 {
				labels = append(labels, "new agent")
			}
			for _, label := range labels {
				if !strings.Contains(view, label) {
					t.Errorf("%s %v layout omitted %q", appearance, size, label)
				}
			}
			if strings.Contains(view, bearer) || strings.Contains(view, principal.Digest) {
				t.Errorf("%s %v layout exposed bearer material", appearance, size)
			}
			lines := strings.Join(m.runLines(), "\n")
			if !strings.Contains(lines, "Luna") || strings.Contains(lines, bearer) {
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

func TestUnavailableRunReferenceExplainsFailureAndCanRetry(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 14}, {80, 24}, {120, 35}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev-rw", Digest: "scope"}, Request: runpkg.Request{Command: []string{"/bin/sh"}, References: []string{"bypass://Missing%20Entry#password"}}}
			m.updateRun(RunRequestMsg{Pending: p})
			m.updateRun(tea.KeyMsg{Type: tea.KeyCtrlB})
			m.updateRun(tea.KeyMsg{Type: tea.KeyEnter})
			view := m.View()
			for _, text := range []string{"REVIEW REQUEST", "URI: bypass://Missing%20Entry#password", "Unlock vault or check entry name", "Deny", "Back"} {
				if !strings.Contains(view, text) {
					t.Errorf("%s %v missing reference feedback %q", appearance, size, text)
				}
			}
			if strings.Contains(view, "Allow") {
				t.Fatal("unresolved reference offered approval")
			}
			m.updateRun(tea.KeyMsg{Type: tea.KeyEsc})
			m.ws.Open(&vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{{UUID: "entry", Fields: []vault.Field{{Name: "Title", Value: "Missing Entry"}, {Name: "Password", Protected: true}}}}})
			m.updateRun(tea.KeyMsg{Type: tea.KeyEnter})
			if m.runs.unavailable != "" || m.runs.err != "" || len(m.runs.targets) != 1 || !strings.Contains(m.View(), "Allow") {
				t.Errorf("%s %v retry did not enable approval after vault became available", appearance, size)
			}
		}
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
