package ui

import (
	wipebuf "bypass/internal/secret"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type RunRequestMsg struct{ Pending *runpkg.Pending }
type RunDisconnectedMsg struct{}

type unavailableRunReference struct{ reference string }

func (*unavailableRunReference) Error() string { return "entry not found in unlocked vaults" }

type runTarget struct {
	ref      string
	session  string
	entry    string
	field    string
	database *vault.Database
}
type runSessionGrant struct {
	session, identity, entry, field string
}

type runResult struct {
	pending         *runpkg.Pending
	targets         []runTarget
	secrets         *secretValues
	err             error
	needsApproval   bool
	newIdentity     bool
	permissionEpoch uint64
	mode            string
	bulk            bool
}
type runState struct {
	pending         []*runpkg.Pending
	visible         bool
	review          bool
	reviewed        *runpkg.Pending
	choosing        bool
	actionFocus     int
	buttonsFocused  bool
	reviewFocus     int
	index           int
	offset          int
	fieldOffset     int
	busy            bool
	busyPending     *runpkg.Pending
	bulkRemaining   int
	bulkConfirm     bool
	bulkFocus       int
	bulkPending     []*runpkg.Pending
	permissionEpoch uint64
	targets         []runTarget
	choices         []runTarget
	choiceIndex     int
	chosen          map[*runpkg.Pending]map[string]runTarget
	err             string
	unavailable     string
	newIdentity     map[*runpkg.Pending]bool
	grants          map[runSessionGrant]string
}

func (m *Model) pruneRunSessionGrants() {
	if m.runs == nil {
		return
	}
	for grant := range m.runs.grants {
		session := m.ws.Sessions[grant.session]
		if session == nil || session.DB == nil {
			delete(m.runs.grants, grant)
		}
	}
}

func clearRunValues(values map[string][]byte) {
	for _, value := range values {
		wipebuf.Wipe(value)
	}
}

// secretValues owns buffers between an async command and the UI update. The
// context watcher clears values if Bubble Tea drops the result during shutdown.
type secretValues struct {
	mu     sync.Mutex
	ctx    context.Context
	values map[string][]byte
	stop   func() bool
	done   chan struct{}
	closed bool
}

func newSecretValues(ctx context.Context) *secretValues {
	v := &secretValues{ctx: ctx, values: map[string][]byte{}, done: make(chan struct{})}
	v.mu.Lock()
	stop := context.AfterFunc(ctx, v.Clear)
	v.stop = stop
	canceled := ctx.Err() != nil
	if canceled {
		v.clearLocked()
		v.stop = nil
	}
	v.mu.Unlock()
	if canceled {
		stop()
	}
	return v
}

func (v *secretValues) Put(key string, value []byte) {
	if v == nil {
		wipebuf.Wipe(value)
		return
	}
	v.mu.Lock()
	if v.closed || v.ctx.Err() != nil {
		wipebuf.Wipe(value)
		if !v.closed {
			v.clearLocked()
		}
		stop := v.stop
		v.stop = nil
		v.mu.Unlock()
		if stop != nil {
			stop()
		}
		return
	}
	if previous := v.values[key]; previous != nil {
		wipebuf.Wipe(previous)
	}
	v.values[key] = value
	v.mu.Unlock()
}

func (v *secretValues) Take() map[string][]byte {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return nil
	}
	stop := v.stop
	v.stop = nil
	if v.ctx.Err() != nil {
		v.clearLocked()
		v.mu.Unlock()
		if stop != nil {
			stop()
		}
		return nil
	}
	values := v.values
	v.values = nil
	v.closed = true
	close(v.done)
	v.mu.Unlock()
	if stop != nil {
		stop()
	}
	return values
}

func (v *secretValues) Clear() {
	if v == nil {
		return
	}
	v.mu.Lock()
	if !v.closed {
		v.clearLocked()
	}
	stop := v.stop
	v.stop = nil
	v.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (v *secretValues) clearLocked() {
	clearRunValues(v.values)
	v.values = nil
	v.closed = true
	close(v.done)
}

func (m *Model) updateRun(msg tea.Msg) (tea.Cmd, bool) {
	if m.runs == nil {
		m.runs = &runState{}
	}
	r := m.runs
	if !r.busy {
		for _, target := range r.targets {
			session := m.ws.Sessions[target.session]
			if session == nil || session.DB != target.database {
				r.targets = nil
				r.choices = nil
				r.err = "vault changed; unlock it and review the request again"
				if r.review {
					m.focusRunAction("esc")
				}
				break
			}
		}
	}
	switch msg := msg.(type) {
	case RunDisconnectedMsg:
		m.pruneRuns()
		return nil, true
	case RunRequestMsg:
		if msg.Pending != nil && msg.Pending.Context.Err() == nil {
			if len(r.pending) == 0 {
				r.buttonsFocused = false
				r.index = 0
			}
			r.pending = append(r.pending, msg.Pending)
			if r.newIdentity == nil {
				r.newIdentity = map[*runpkg.Pending]bool{}
			}
			r.newIdentity[msg.Pending] = msg.Pending.Identity.Digest != ""
			m.setNotice("secret request pending · "+keyRequests+" to review", false)
			choices, choiceIndex := r.choices, r.choiceIndex
			targets, err := m.resolveRunTargets(msg.Pending)
			r.choices, r.choiceIndex = choices, choiceIndex
			if err == nil {
				return m.runGrantJob(msg.Pending, targets, "check"), true
			}
			// Requests that need user intervention should surface immediately.
			r.visible = true
		}
		return nil, true
	case runResult:
		if msg.bulk {
			r.bulkRemaining--
			if r.bulkRemaining == 0 {
				r.busy = false
			}
		}
		if r.busyPending == msg.pending {
			r.busy = false
			r.busyPending = nil
		}
		if msg.permissionEpoch != r.permissionEpoch {
			if msg.secrets != nil {
				msg.secrets.Clear()
			}
			return nil, true
		}
		if msg.pending != nil && msg.pending.Identity.Digest != "" {
			if r.newIdentity == nil {
				r.newIdentity = map[*runpkg.Pending]bool{}
			}
			if !r.review || r.reviewed != msg.pending {
				r.newIdentity[msg.pending] = msg.newIdentity
			}
		}
		if msg.needsApproval {
			msg.secrets.Clear()
			// A check can finish after another request has just been approved
			// for this identity. Recheck against the current grant map so that
			// this request does not remain stuck awaiting manual approval.
			if msg.mode == "check" && msg.pending != nil && msg.pending.Context.Err() == nil && runTargetsHaveSessionGrants(r.grants, runSessionPrincipal(msg.pending.Identity, msg.targets), msg.targets) {
				return m.runGrantJob(msg.pending, append([]runTarget(nil), msg.targets...), "check"), true
			}
			r.visible = true
			return nil, true
		}
		values := msg.secrets.Take()
		for _, target := range msg.targets {
			session := m.ws.Sessions[target.session]
			if session == nil || session.DB != target.database {
				msg.err = fmt.Errorf("vault locked or changed before secrets were delivered")
				break
			}
		}

		if msg.err != nil {
			message := "could not resolve requested secrets: " + msg.err.Error()

			msg.pending.Reply(runpkg.Response{Error: message})
			clearRunValues(values)
		} else {
			accepted := msg.pending.Reply(runpkg.Response{Values: values})
			if accepted && msg.mode == "s" {
				if r.grants == nil {
					r.grants = make(map[runSessionGrant]string)
				}
				principal := runSessionPrincipal(msg.pending.Identity, msg.targets)
				for _, target := range msg.targets {
					r.grants[runSessionGrant{target.session, principal, target.entry, target.field}] = msg.pending.Identity.Name
				}
			}
			values = nil // Reply owns the byte slices, including rejected replies.
		}
		clearRunValues(values)
		m.removeRun(msg.pending)
		return nil, true
	case tea.KeyMsg:
		if r.visible && m.activeLayer() == layerTiny {
			return nil, false
		}
		if msg.String() == keyLockAll || msg.String() == keyLockAllFallback || msg.String() == "ctrl+c" {
			return nil, false
		}
		if m.gate.Protected() {
			return nil, false
		}
		if msg.String() == "ctrl+p" && r.visible {
			if r.busy {
				return nil, true
			}
			r.visible, r.review, r.choosing, r.reviewed, r.bulkConfirm = false, false, false, nil, false
			r.bulkPending = nil
			return nil, false
		}
		if (msg.String() == keyRequests || msg.String() == keyRequestsLegacy) && m.editor == nil && m.dialog == nil {
			r.visible = !r.visible
			r.bulkConfirm, r.bulkPending = false, nil
			r.review, r.choosing, r.reviewed = false, false, nil
			r.targets = nil
			r.choices = nil
			r.err, r.unavailable = "", ""
			return nil, true
		}
		if !r.visible {
			return nil, false
		}
		m.pruneRuns()
		if r.busy {
			return nil, true
		}
		if r.bulkConfirm {
			switch msg.String() {
			case "esc":
				r.bulkConfirm, r.bulkPending = false, nil
			case "tab", "shift+tab", "up", "down":
				dialogActionFocus(msg.String(), len(m.runBulkActions()), &r.bulkFocus)
			case "enter":
				return m.activateRunAction(r.bulkFocus), true
			case "a":
				return m.activateRunAction(1), true
			}
			return nil, true
		}

		if r.review {
			if r.choosing && (msg.String() == "up" || msg.String() == "down") {
				if msg.String() == "up" {
					r.choiceIndex = max(0, r.choiceIndex-1)
				} else {
					r.choiceIndex = min(len(r.choices)-1, r.choiceIndex+1)
				}
				m.focusRunAction("enter")
				return nil, true
			}
			switch msg.String() {
			case "esc":
				r.review = false
				r.choosing = false
				r.reviewed = nil
				r.reviewFocus = 0
			case "tab", "shift+tab", "up", "down":
				dialogActionFocus(msg.String(), len(m.runReviewActions()), &r.reviewFocus)
			case "enter":
				return m.activateRunAction(r.reviewFocus), true
			case "d", "a", "s":
				if action := m.runActionForKey(msg.String()); action >= 0 {
					return m.activateRunAction(action), true
				}
			case "pgdown":
				size := m.runFieldPageSize()
				r.fieldOffset = min(max(0, len(m.runReviewFieldLines())-size), r.fieldOffset+size)
				r.err = ""
			case "pgup":
				r.fieldOffset = max(0, r.fieldOffset-m.runFieldPageSize())
				r.err = ""
			}
			return nil, true
		}
		if msg.String() == "esc" {
			r.visible = false
			r.choosing = false
			r.review = false
			r.reviewed = nil
			return nil, true
		}
		switch msg.String() {
		case "pgdown":
			r.offset = min(max(0, len(r.targets)-4), r.offset+4)
		case "pgup":
			r.offset = max(0, r.offset-4)
		case "up", "down", "tab", "shift+tab":
			dialogFocus(msg.String(), len(r.pending), len(m.runRequestActions()), &r.index, &r.actionFocus, &r.buttonsFocused)
		case "enter":
			if !r.buttonsFocused && len(r.pending) > 0 {
				m.enterRunReview()
				return nil, true
			}
			return m.activateRunAction(r.actionFocus), true
		case "r":
			m.openRunPermissions()
			return nil, true
		case "d", "a":
			return m.activateRunKey(msg.String()), true
		}
		return nil, true
	}
	return nil, false
}
func (m *Model) pruneRuns() {
	if m.runs == nil {
		return
	}
	r := m.runs
	for i := len(r.pending) - 1; i >= 0; i-- {
		if r.pending[i].Context.Err() != nil {
			removed := r.pending[i]
			if r.reviewed == removed {
				r.review = false
				r.choosing = false
				r.reviewed = nil
				r.targets = nil
				r.err, r.unavailable = "", ""
			}
			if i == r.index {
				if !r.review {
					r.choosing = false
				}
			}
			delete(r.chosen, removed)
			delete(r.newIdentity, removed)
			if !r.review {
				r.choices = nil
			}
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			if !r.review {
				r.targets = nil
				r.err, r.unavailable = "", ""
			}
		}
	}
	if r.review {
		for i, p := range r.pending {
			if p == r.reviewed {
				r.index = i
				break
			}
		}
	}
	r.index = min(r.index, max(0, len(r.pending)-1))
}
func (m *Model) removeRun(p *runpkg.Pending) {
	r := m.runs
	removedReview := r.review && r.reviewed == p
	for i, v := range r.pending {
		if v == p {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			break
		}
	}
	if removedReview {
		r.review, r.reviewed, r.choosing = false, nil, false
		r.targets, r.choices = nil, nil
		r.actionFocus = 0
		r.buttonsFocused = false
	} else if r.review {
		for i, pending := range r.pending {
			if pending == r.reviewed {
				r.index = i
				break
			}
		}
	} else {
		r.index = min(r.index, max(0, len(r.pending)-1))
		r.targets, r.choices = nil, nil
		r.actionFocus = 0
		r.buttonsFocused = false
	}
	delete(r.chosen, p)
	delete(r.newIdentity, p)
	if len(r.pending) == 0 {
		r.buttonsFocused = true
	}
	r.offset = 0
	m.setNotice("secret request completed", false)
	r.err = ""
	r.unavailable = ""
}
func (m *Model) resolveRunTargets(p *runpkg.Pending) ([]runTarget, error) {
	var targets []runTarget
	for _, text := range p.Request.References {
		ref, err := runpkg.ParseReference(text)
		if err != nil {
			return nil, err
		}
		var matches []runTarget
		for id, s := range m.ws.Sessions {
			if s.DB == nil {
				continue
			}
			components := ref.Components
			if ref.Absolute {
				// Find the first file on the received absolute path, independent of extension.
				path := "/"
				cut := -1
				for i, c := range components {
					path = filepath.Join(path, c)
					info, err := osStatPlain(path)
					if err != nil {
						break
					}
					if !info {
						cut = i + 1
						break
					}
				}
				if cut < 0 || path != s.Path {
					continue
				}
				components = components[cut:]
			} else if len(components) > 1 && components[0] == filepath.Base(s.Path) {
				components = components[1:]
			}
			if len(components) == 0 {
				continue
			}
			for _, entry := range s.DB.Entries {
				if entry.Recycled || entry.Get("Title") != components[len(components)-1] {
					continue
				}
				if len(components) > 1 && strings.TrimPrefix(strings.Trim(entry.GroupPath, "/"), "Passwords/") != strings.Join(components[:len(components)-1], "/") {
					continue
				}
				field := ref.Field
				switch strings.ToLower(field) {
				case "password":
					field = "Password"
				case "username":
					field = "UserName"
				case "title":
					field = "Title"
				case "url":
					field = "URL"
				case "notes":
					field = "Notes"
				}
				matches = append(matches, runTarget{text, id, entry.UUID, field, s.DB})
			}
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].database.Path < matches[j].database.Path })
		if len(matches) == 0 {
			return nil, &unavailableRunReference{reference: text}
		}
		if len(matches) > 1 {
			selected, ok := m.runs.chosen[p][text]
			if ok {
				for _, candidate := range matches {
					if candidate.session == selected.session && candidate.entry == selected.entry {
						matches = []runTarget{candidate}
						break
					}
				}
			}
			if len(matches) > 1 {
				m.runs.choices = matches
				m.runs.choiceIndex = 0
				return nil, fmt.Errorf("select the vault for this reference")
			}
		}
		targets = append(targets, matches[0])
	}
	return targets, nil
}
func (m *Model) runLines() []string {
	if m.runs == nil {
		return nil
	}
	if m.runs.review {
		return m.runReviewBody()
	}
	body, _ := m.runRequestsBody()
	return body
}

func osStatPlain(path string) (bool, error) {
	plain, err := runpkg.PlainPath(path)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(plain)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func runIdentityLabel(p *runpkg.Pending, isNew bool) string {
	if p.Identity.Name == "" {
		return "Agent: (not provided)"
	}
	state := "known agent"
	if isNew {
		state = "new agent"
	}
	return "Agent: " + safe(p.Identity.Name) + " (" + state + ")"
}

func (m *Model) openRunPermissions() {
	m.hideReveal()
	d := &actionDialog{kind: "run-permissions", title: "Session Secret Permissions", options: []string{"Clear all", "Close"}, selected: 1}
	if m.runs != nil {
		for grant, agent := range m.runs.grants {
			session := m.ws.Sessions[grant.session]
			if session == nil || session.DB == nil {
				continue
			}
			name := grant.entry
			for _, entry := range session.DB.Entries {
				if entry.UUID == grant.entry {
					name = entry.Get("Title")
					break
				}
			}
			if agent == "" {
				agent = "unnamed request"
			}
			line := safe(agent) + " · " + safe(name) + " · " + safe(grant.field)
			if len(m.ws.Sessions) > 1 {
				line = safe(vaultLabel(session.Path)) + " · " + line
			}
			d.message = append(d.message, line)
		}
	}
	sort.Strings(d.message)
	if len(d.message) == 0 {
		d.message = []string{"No session permissions."}
	}
	m.dialog = d
}
