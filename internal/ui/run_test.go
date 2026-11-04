package ui

import (
	runpkg "bypass/internal/run"
	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	"bytes"
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
	secrets := newSecretValues(context.Background())
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
	secrets := newSecretValues(ctx)
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
	secrets := newSecretValues(context.Background())
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
	for _, appearance := range []string{"classic", "compact"} {
		for _, tc := range []struct {
			name          string
			review, reset bool
			err           string
		}{
			{name: "requests"},
			{name: "review", review: true, err: "Review could not complete"},
		} {
			t.Run(appearance+"/"+tc.name, func(t *testing.T) {
				m := New(Options{})
				m.settings.Appearance = appearance
				m.width, m.height = 80, 24
				p := &runpkg.Pending{Context: context.Background()}
				m.runs = &runState{visible: true, review: tc.review, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{ref: "bypass://entry"}}}
				m.runs.err = tc.err
				layout := m.runLayout(tc.review)
				rows := renderRunLayer(m, make([]string, m.height), layout)
				for i, action := range layout.actions {
					y := -1
					for row := len(rows) - 1; row >= 0; row-- {
						text := ansi.Strip(rows[row])
						match := strings.Contains(text, action.label)
						for _, other := range layout.actions {
							if len(other.label) > len(action.label) && strings.Contains(other.label, action.label) && strings.Contains(text, other.label) {
								match = false
							}
						}
						if match {
							y = row
							break
						}
					}
					if y < 0 {
						t.Fatalf("action %q is not visible", action.label)
					}
					if got, ok := m.runActionAt(layout.x+layout.width/2, y); !ok || got != i {
						t.Fatalf("action %q: got %d, %t", action.label, got, ok)
					}
				}
				if _, ok := m.runActionAt(layout.x-1, layout.y); ok {
					t.Fatal("outside click hit an action")
				}
				if _, ok := m.runActionAt(layout.x+1, layout.y); ok {
					t.Fatal("header click hit an action")
				}
			})
		}
	}
}

func TestRunPermissionsDialogKeepsParentFocusAndClearsSessionGrants(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 80, 24
	p := &runpkg.Pending{Context: context.Background()}
	grant := runSessionGrant{session: "vault", identity: "agent", entry: "entry", field: "Password"}
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}, buttonsFocused: true, actionFocus: 2, grants: map[runSessionGrant]string{grant: "agent"}}
	m.key(key("r"))
	if m.dialog == nil || m.activeLayer() != layerDialog || m.dialog.selected != 1 {
		t.Fatal("permissions dialog did not open with Close selected")
	}
	m.key(tea.KeyMsg{Type: tea.KeyUp})
	if m.runs.actionFocus != 2 || m.dialog.selected != 0 {
		t.Fatal("keyboard changed parent focus")
	}
	m.key(key("esc"))
	if m.dialog != nil || !m.runs.visible || len(m.runs.grants) != 1 {
		t.Fatal("back did not preserve parent or permissions")
	}
	m.key(key("r"))
	m.key(key("c"))
	if m.dialog == nil || len(m.runs.grants) != 0 || m.runs.permissionEpoch != 1 {
		t.Fatal("clear all did not reset permissions")
	}
	m.key(key("esc"))
	if !m.runs.visible {
		t.Fatal("closing permissions did not return to requests")
	}
}

func TestRunRequestsSmallScreenShowsSelectedQueueItem(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.settings.Appearance = appearance
		m.width, m.height = 48, 16
		pending := make([]*runpkg.Pending, 4)
		for i := range pending {
			pending[i] = &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{References: []string{fmt.Sprintf("bypass://app%d", i+1)}}}
		}
		m.runs = &runState{visible: true, pending: pending, index: 3}
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "app4") {
			t.Errorf("%s 48x16 view hid the selected queue item: %q", appearance, view)
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

func TestRunRequestWheelMovesSelectionWithoutReachingButtons(t *testing.T) {
	m := New(Options{})
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{{Context: context.Background()}, {Context: context.Background()}}}
	wheel := func(button tea.MouseButton) {
		m.pointer(tea.MouseMsg{Action: tea.MouseActionPress, Button: button})
	}
	wheel(tea.MouseButtonWheelDown)
	wheel(tea.MouseButtonWheelDown)
	if m.runs.index != 1 || m.runs.buttonsFocused {
		t.Fatal("wheel should stop at the last request")
	}
	wheel(tea.MouseButtonWheelUp)
	if m.runs.index != 0 {
		t.Fatal("wheel should select the previous request")
	}
}

func TestRequestKeyboardRoutingToPaletteAndQuit(t *testing.T) {
	m := New(Options{})
	m.runs = &runState{visible: true}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.runs.visible || m.palette == nil {
		t.Fatal("ctrl+p did not leave requests and open the palette")
	}
	m.closePalette(false)
	m.runs.visible = true
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || !m.closed {
		t.Fatal("ctrl+c did not quit from requests")
	}
}

func TestRequestShortcutAndLegacyAlias(t *testing.T) {
	for _, message := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Alt: true, Runes: []rune{'b'}},
		{Type: tea.KeyCtrlB},
	} {
		m := New(Options{})
		m.Update(message)
		if m.runs == nil || !m.runs.visible {
			t.Fatalf("%s did not open requests", message.String())
		}
	}
}

func TestBulkApprovalRequiresConfirmationAndCapturesQueue(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 16
	first := &runpkg.Pending{Context: context.Background()}
	second := &runpkg.Pending{Context: context.Background()}
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{first}}
	cmd, _ := m.updateRun(key("a"))
	if cmd != nil || !m.runs.bulkConfirm || m.runs.bulkFocus != 0 || len(m.runs.bulkPending) != 1 {
		t.Fatal("bulk approval should open confirmation with Cancel focused")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, " Approve 1 pending request(s) once? ") || !strings.Contains(view, " New requests are not included. ") {
		t.Fatalf("confirmation does not show approval scope: %q", view)
	}
	m.runs.pending = append(m.runs.pending, second)
	if len(m.runs.bulkPending) != 1 || m.runs.bulkPending[0] != first {
		t.Fatal("new request was included in existing confirmation")
	}
	m.updateRun(key("enter"))
	if m.runs.bulkConfirm || len(m.runs.pending) != 2 {
		t.Fatal("default confirmation action must cancel without approving")
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
	size := m.runFieldPageSize()
	if size < 1 || !strings.Contains(first, "field1") {
		t.Fatal("first field page missing")
	}
	m.updateRun(tea.KeyMsg{Type: tea.KeyPgDown})
	second := ansi.Strip(m.View())
	if !strings.Contains(second, fmt.Sprintf("field%d", size+1)) {
		t.Fatalf("second field page skipped rows: %q", second)
	}
}

func TestRunApprovalActionsRemainVisibleAcrossLayouts(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}, {120, 35}} {
			m := New(Options{})
			m.settings.Appearance = appearance
			m.width, m.height = size[0], size[1]
			p := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{Command: []string{"/app"}, Directory: "/project"}}
			m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{ref: "bypass://entry", field: "Password", database: &vault.Database{}}}}
			view := m.View()
			for _, label := range []string{"Deny", "Approve once", "Approve for this session", "Back"} {
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
		for _, size := range [][2]int{{48, 16}, {80, 24}, {120, 35}} {
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
			labels := []string{"Luna", "Deny", "Approve once", "Approve for this session", "Back"}

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
		for _, size := range [][2]int{{48, 16}, {80, 24}, {120, 35}} {
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
			if strings.Contains(view, "Approve") {
				t.Fatal("unresolved reference offered approval")
			}
			m.updateRun(tea.KeyMsg{Type: tea.KeyEsc})
			m.ws.Open(&vault.Database{Path: "/vault", RootUUID: "root", Entries: []vault.Entry{{UUID: "entry", Fields: []vault.Field{{Name: "Title", Value: "Missing Entry"}, {Name: "Password", Protected: true}}}}})
			m.updateRun(tea.KeyMsg{Type: tea.KeyEnter})
			if m.runs.unavailable != "" || m.runs.err != "" || len(m.runs.targets) != 1 || !strings.Contains(m.View(), "Approve") {
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

func TestTinyScreenDoesNotSendKeysToHiddenRequests(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 47, 15
	p := &runpkg.Pending{Context: context.Background()}
	m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, reviewFocus: 2}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if !m.runs.review || m.runs.reviewFocus != 2 {
		t.Fatal("hidden request dialog handled input on tiny screen")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.runs.reviewFocus == 2 {
		t.Fatal("visible review did not regain input after resize")
	}
}

func TestRunRequestMouseSelectsVisibleRow(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		for _, size := range [][2]int{{48, 16}, {80, 24}} {
			m := New(Options{})
			m.width, m.height, m.settings.Appearance = size[0], size[1], appearance
			first := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{References: []string{"bypass://first-agent"}}}
			second := &runpkg.Pending{Context: context.Background(), Request: runpkg.Request{References: []string{"bypass://second-agent"}}}
			m.runs = &runState{visible: true, pending: []*runpkg.Pending{first, second}, buttonsFocused: true}
			for y, line := range m.runDialogView() {
				plain := ansi.Strip(line)
				if x := strings.Index(plain, "second-agent"); x >= 0 {
					m.pointer(tea.MouseMsg{X: len([]rune(plain[:x])), Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
					if m.runs.index != 1 || m.runs.buttonsFocused {
						t.Fatalf("%s %v: request click missed", appearance, size)
					}
					break
				}
			}
			if m.runs.index != 1 {
				t.Fatalf("%s %v: second request not visible", appearance, size)
			}
		}
	}
}

func TestRunReviewDetailsAndNote(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 80, 24
	db := &vault.Database{Entries: []vault.Entry{{UUID: "one", Fields: []vault.Field{{Name: "Title", Value: "Gmail"}}}, {UUID: "two", Fields: []vault.Field{{Name: "Title", Value: "Linkedin"}}}}}
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev-rw", Digest: "digest"}, Request: runpkg.Request{Command: []string{"/usr/bin/true"}, Directory: "/project"}}
	m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{entry: "one", field: "Password", database: db}, {entry: "two", field: "Username", database: db}}}
	text := ansi.Strip(m.View())
	for _, expected := range []string{"Agent:", "dev-rw", "Command:", "/usr/bin/true", "Directory:", "/project", "REQUESTED SECRETS", "Gmail:", "Password", "Linkedin:", "Username", "Session approval retained until vault is locked.", "Agent identity is self-claimed and not verified."} {
		if !strings.Contains(text, expected) {
			t.Errorf("review missing %q", expected)
		}
	}
	if strings.Contains(text, "REQUEST DETAILS:") {
		t.Fatal("unexpected details heading")
	}
	body := m.runReviewBody()
	note := -1
	for i, line := range body {
		if strings.Contains(ansi.Strip(line), "Session approval retained") {
			note = i
			break
		}
	}
	if note < 2 || body[note-1] != "" || body[note-2] != "" {
		t.Fatal("review note must follow two empty lines")
	}
	opts := m.runLayerOptions(m.runLayout(true))
	for _, line := range opts.Body {
		if !strings.HasPrefix(line, " ") || !strings.HasSuffix(line, " ") {
			t.Fatal("review content missing horizontal padding")
		}
	}
}

func TestRunReviewNoteInSmallPane(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 16
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev", Digest: "digest"}, Request: runpkg.Request{Command: []string{"/app"}}}
	m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{field: "Password", database: &vault.Database{}}}}
	text := ansi.Strip(m.View())
	for _, expected := range []string{"Session approval retained until vault", "is locked.", "Agent identity is self-claimed and", "not verified.", "Approve for this session"} {
		if !strings.Contains(text, expected) {
			t.Errorf("small review missing %q: %s", expected, text)
		}
	}
}

func TestRunRequestSummaryShowsIdentityAndFields(t *testing.T) {
	refs := []string{"bypass://Gmail", "bypass://Work/Linkedin#username", "bypass://Proton%20Mail#email"}
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev-rw"}, Request: runpkg.Request{References: refs}}
	want := "dev-rw: Gmail Password, Linkedin Username, Proton Mail Email"
	if got := runRequestSummary(p); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	p.Identity = runpkg.Identity{}
	if got := runRequestSummary(p); got != "Gmail Password, Linkedin Username, Proton Mail Email" {
		t.Fatalf("anonymous summary = %q", got)
	}
}

func TestRunRequestSummaryTruncatesAndKeepsEnterHint(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 16
	p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "dev-rw"}, Request: runpkg.Request{References: []string{"bypass://Gmail", "bypass://Linkedin", "bypass://ProtonMail#email"}}}
	m.runs = &runState{visible: true, pending: []*runpkg.Pending{p}}
	view := ansi.Strip(m.View())
	for _, text := range []string{"dev-rw: Gmail Password", "…", "(enter)"} {
		if !strings.Contains(view, text) {
			t.Errorf("missing %q in %q", text, view)
		}
	}
}

func TestRunReviewWidthFitsNotes(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.width, m.height = 120, 35
		m.settings.Appearance = appearance
		p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "agent", Digest: "digest"}}
		m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{field: "Password", database: &vault.Database{}}}}
		width := m.runLayout(true).width
		if width != max(lipgloss.Width(runSessionNote), lipgloss.Width(runIdentityNote))+6 {
			t.Fatalf("%s width = %d", appearance, width)
		}
		view := ansi.Strip(m.View())
		if !strings.Contains(view, runSessionNote) || !strings.Contains(view, runIdentityNote) {
			t.Fatalf("%s note was clipped", appearance)
		}
	}
}

func TestEmptyRequestsMessageHasHorizontalPadding(t *testing.T) {
	m := New(Options{})
	m.runs = &runState{visible: true}
	body, _ := m.runRequestsBody()
	if len(body) != 1 || body[0] != " No pending requests " {
		t.Fatalf("empty request body = %q", body)
	}
}

func TestRunReviewWrapsLongSecretsAndPagesAllLines(t *testing.T) {
	for _, size := range [][2]int{{48, 16}, {80, 24}} {
		m := New(Options{})
		m.width, m.height = size[0], size[1]
		name := strings.Repeat("LongEntryName", 9)
		field := strings.Repeat("LongFieldName", 7)
		db := &vault.Database{Entries: []vault.Entry{{UUID: "entry", Fields: []vault.Field{{Name: "Title", Value: name}}}}}
		m.ws.Open(db)
		p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "agent", Digest: "digest"}}
		m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{session: m.ws.ActiveSession().ID, entry: "entry", field: field, database: db}}}
		lines := m.runReviewFieldLines()
		if len(lines) < 3 {
			t.Fatalf("%v: long entry did not wrap", size)
		}
		var all strings.Builder
		for _, line := range lines {
			all.WriteString(ansi.Strip(line))
		}
		if !strings.Contains(all.String(), name) || !strings.Contains(all.String(), field) {
			t.Fatalf("%v: wrapping lost text", size)
		}
		seen := make(map[string]bool)
		for range len(lines) + 1 {
			view := ansi.Strip(m.View())
			for _, line := range lines {
				if strings.Contains(view, ansi.Strip(line)) {
					seen[ansi.Strip(line)] = true
				}
			}
			previous := m.runs.fieldOffset
			m.updateRun(tea.KeyMsg{Type: tea.KeyPgDown})
			if previous == m.runs.fieldOffset {
				break
			}
		}
		for _, line := range lines {
			if !seen[ansi.Strip(line)] {
				t.Errorf("%v: wrapped line not visible while paging: %q", size, ansi.Strip(line))
			}
		}
	}
}

func TestRunSessionApprovalShortcut(t *testing.T) {
	m := New(Options{})
	p := &runpkg.Pending{Context: context.Background()}
	m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}}
	if m.runActionForKey("s") < 0 || m.runActionForKey("i") >= 0 {
		t.Fatal("session approval shortcut must be s")
	}
}

func TestRunReviewCenteredAndDimsParent(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.width, m.height = 120, 35
		m.settings.Appearance = appearance
		p := &runpkg.Pending{Context: context.Background(), Identity: runpkg.Identity{Name: "parent-agent", Digest: "digest"}, Request: runpkg.Request{References: []string{"bypass://Synthetic"}}}
		m.runs = &runState{visible: true, review: true, reviewed: p, pending: []*runpkg.Pending{p}, targets: []runTarget{{field: "Password", database: &vault.Database{}}}}
		parent := renderRunLayer(m, m.modalBackdrop(), m.runRequestsLayout())
		expected := m.dimBackdrop(append([]string(nil), parent...))
		actual := m.runDialogView()
		shaded := renderRunLayer(m, expected, m.runLayout(true))
		if !reflect.DeepEqual(actual, shaded) {
			t.Fatalf("%s: parent request not shaded", appearance)
		}

		layout := m.runLayout(true)
		opts := m.runLayerOptions(layout)
		body, _ := dialogrender.PrepareBody(opts)
		centered := m.centeredDialogGeometry(layout.width, len(body))
		if layout.x != centered.X || layout.y != centered.Y {
			t.Errorf("%s: review not centered", appearance)
		}
	}
}
