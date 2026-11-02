package ui

import (
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
	"github.com/charmbracelet/x/ansi"
)

type RunRequestMsg struct{ Pending *runpkg.Pending }
type RunDisconnectedMsg struct{}
type runTarget struct {
	ref      string
	session  string
	entry    string
	field    string
	database *vault.Database
}
type runResult struct {
	pending       *runpkg.Pending
	targets       []runTarget
	secrets       *runSecretValues
	err           error
	updated       map[string]*vault.Database
	needsApproval bool
	newIdentity   bool
}
type runState struct {
	pending     []*runpkg.Pending
	visible     bool
	index       int
	offset      int
	busy        bool
	targets     []runTarget
	choices     []runTarget
	choiceIndex int
	chosen      map[*runpkg.Pending]map[string]runTarget
	err         string
	newIdentity map[*runpkg.Pending]bool
}

func clearRunValues(values map[string][]byte) {
	for _, value := range values {
		clear(value)
	}
}

// runSecretValues owns buffers between an async command and the UI update. The
// context watcher clears values if Bubble Tea drops the result during shutdown.
type runSecretValues struct {
	mu     sync.Mutex
	ctx    context.Context
	values map[string][]byte
	stop   func() bool
	done   chan struct{}
	closed bool
}

func newRunSecretValues(ctx context.Context) *runSecretValues {
	v := &runSecretValues{ctx: ctx, values: map[string][]byte{}, done: make(chan struct{})}
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

func (v *runSecretValues) Put(key string, value []byte) {
	if v == nil {
		clear(value)
		return
	}
	v.mu.Lock()
	if v.closed || v.ctx.Err() != nil {
		clear(value)
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
		clear(previous)
	}
	v.values[key] = value
	v.mu.Unlock()
}

func (v *runSecretValues) Take() map[string][]byte {
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

func (v *runSecretValues) Clear() {
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

func (v *runSecretValues) clearLocked() {
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
				break
			}
		}
	}
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if r.visible && !m.gate.Protected() {
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && !r.busy {
				lines := strings.Split(m.View(), "\n")
				for _, action := range []struct{ label, key string }{{"Deny", "d"}, {"Allow once", "a"}, {"Allow for this path", "p"}, {"Always allow", "w"}} {
					for row := len(lines) - 1; row >= 0; row-- {
						if strings.Contains(ansi.Strip(lines[row]), action.label) {
							if msg.Y == row {
								return m.updateRun(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(action.key)})
							}
							break
						}
					}
				}
			}
			return nil, true
		}

	case RunDisconnectedMsg:
		m.pruneRuns()
		return nil, true
	case RunRequestMsg:
		if msg.Pending != nil && msg.Pending.Context.Err() == nil {
			r.pending = append(r.pending, msg.Pending)
			if r.newIdentity == nil {
				r.newIdentity = map[*runpkg.Pending]bool{}
			}
			r.newIdentity[msg.Pending] = msg.Pending.Identity.Digest != ""
			m.setNotice("secret request pending · ctrl+b to review", false)
			choices, choiceIndex := r.choices, r.choiceIndex
			targets, err := m.resolveRunTargets(msg.Pending)
			r.choices, r.choiceIndex = choices, choiceIndex
			if err == nil {
				return m.runGrantJob(msg.Pending, targets, "check"), true
			}
		}
		return nil, true
	case runResult:
		r.busy = false
		if msg.pending != nil && msg.pending.Identity.Digest != "" {
			if r.newIdentity == nil {
				r.newIdentity = map[*runpkg.Pending]bool{}
			}
			r.newIdentity[msg.pending] = msg.newIdentity
		}
		if msg.needsApproval {
			msg.secrets.Clear()
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
		savedVaults := []string{}
		for id, db := range msg.updated {
			savedVaults = append(savedVaults, db.Path)
			session := m.ws.Sessions[id]
			valid := false
			for _, target := range msg.targets {
				if target.session == id && session != nil && session.DB == target.database {
					valid = true
				}
			}
			if valid {
				m.ws.ReplaceDatabase(id, db)
			} else {
				db.Clear()
			}
		}
		if msg.err != nil {
			message := "could not resolve requested secrets: " + msg.err.Error()
			if len(savedVaults) > 0 {
				sort.Strings(savedVaults)
				message += "; permissions already saved in: " + strings.Join(savedVaults, ", ")
				m.setNotice("request failed; some permissions were saved", true)
			}
			msg.pending.Reply(runpkg.Response{Error: message})
			clearRunValues(values)
		} else {
			msg.pending.Reply(runpkg.Response{Values: values})
			values = nil // Reply owns the byte slices, including rejected replies.
		}
		clearRunValues(values)
		m.removeRun(msg.pending)
		return nil, true
	case tea.KeyMsg:
		if m.gate.Protected() {
			return nil, false
		}
		if msg.String() == "ctrl+b" && m.editor == nil && m.dialog == nil {
			r.visible = !r.visible
			r.targets = nil
			r.choices = nil
			return nil, true
		}
		if !r.visible {
			return nil, false
		}
		if msg.String() == "ctrl+p" {
			r.visible = false
			return nil, false
		}
		m.pruneRuns()
		if msg.String() == "esc" {
			r.visible = false
			return nil, true
		}
		if r.busy {
			return nil, true
		}
		switch msg.String() {
		case "pgdown":
			r.offset = min(max(0, len(r.targets)-4), r.offset+4)
		case "pgup":
			r.offset = max(0, r.offset-4)
		case "up", "shift+tab":
			if len(r.choices) > 0 {
				r.choiceIndex = max(0, r.choiceIndex-1)
				return nil, true
			}
			r.index = max(0, r.index-1)
			r.targets = nil
			r.offset = 0
		case "down", "tab":
			if len(r.choices) > 0 {
				r.choiceIndex = min(len(r.choices)-1, r.choiceIndex+1)
				return nil, true
			}
			r.index = min(max(0, len(r.pending)-1), r.index+1)
			r.targets = nil
			r.offset = 0
		case "enter":
			if len(r.pending) == 0 {
				return nil, true
			}
			p := r.pending[r.index]
			if len(r.choices) > 0 {
				if r.chosen == nil {
					r.chosen = map[*runpkg.Pending]map[string]runTarget{}
				}
				if r.chosen[p] == nil {
					r.chosen[p] = map[string]runTarget{}
				}
				choice := r.choices[r.choiceIndex]
				r.chosen[p][choice.ref] = choice
				r.choices = nil
				r.choiceIndex = 0
			}
			targets, err := m.resolveRunTargets(p)
			if err != nil {
				r.err = err.Error()
				return nil, true
			}
			r.targets = targets
			r.err = ""
		case "g":
			m.openRunPermissions()
		case "d":
			if len(r.pending) > 0 {
				p := r.pending[r.index]
				p.Reply(runpkg.Response{Error: "secret request denied"})
				m.removeRun(p)
			}
		case "a":
			if len(r.targets) == 0 || len(r.pending) == 0 {
				return nil, true
			}
			p := r.pending[r.index]
			targets := append([]runTarget(nil), r.targets...)
			r.busy = true
			return m.runGrantJob(p, targets, "a"), true
		case "p", "w":
			if len(r.targets) == 0 || len(r.pending) == 0 {
				return nil, true
			}
			p := r.pending[r.index]
			if !grantEligible(p.Request.Command) {
				r.err = "this recipient supports allow once only"
				return nil, true
			}
			r.busy = true
			return m.runGrantJob(p, append([]runTarget(nil), r.targets...), msg.String()), true

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
			delete(r.chosen, r.pending[i])
			delete(r.newIdentity, r.pending[i])
			r.choices = nil
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			r.targets = nil
		}
	}
	r.index = min(r.index, max(0, len(r.pending)-1))
}
func (m *Model) removeRun(p *runpkg.Pending) {
	r := m.runs
	for i, v := range r.pending {
		if v == p {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			break
		}
	}
	r.index = min(r.index, max(0, len(r.pending)-1))
	r.targets = nil
	r.choices = nil
	delete(r.chosen, p)
	delete(r.newIdentity, p)
	r.offset = 0
	m.setNotice("secret request completed", false)
	r.err = ""
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
			return nil, fmt.Errorf("reference unavailable; unlock its vault and retry")
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
	r := m.runs
	if m.height < 30 && len(r.targets) > 0 {
		return m.runCompactLines()
	}
	lines := []string{fmt.Sprintf("Pending secret requests (%d)", len(r.pending)), "[g] Review saved permissions in current vault", ""}
	for i, p := range r.pending {
		if i < max(0, r.index-1) || i > r.index+1 {
			continue
		}
		if p.Context.Err() != nil {
			continue
		}
		prefix := "  "
		if i == r.index {
			prefix = "> "
		}
		lines = append(lines, prefix+safe(strings.Join(p.Request.Command, " ")), "  "+safe(p.Request.Directory))
		if p.Identity.Digest != "" {
			lines = append(lines, "  "+runIdentityLabel(p, r.newIdentity[p]))
		}
	}
	if len(r.choices) > 0 {
		lines = append(lines, "", "Select vault: ")
		for i, choice := range r.choices {
			prefix := "  "
			if i == r.choiceIndex {
				prefix = "> "
			}
			lines = append(lines, prefix+safe(choice.database.Path))
		}
		return append(lines, "[enter] Select vault · [esc] Return")
	}
	if len(r.pending) == 0 {
		lines = append(lines, "No pending requests")
	}
	if len(r.targets) > 0 {
		if len(r.pending) > r.index && len(r.pending[r.index].Caller) > 0 {
			lines = append(lines, "", "Caller (informational): "+safe(strings.Join(r.pending[r.index].Caller, " ← ")))
		}
		lines = append(lines, "", "Requested secrets:")
		for i, t := range r.targets {
			if i < r.offset || i >= r.offset+4 {
				continue
			}
			label := t.field
			for _, entry := range t.database.Entries {
				if entry.UUID == t.entry {
					label = entry.Get("Title") + " #" + t.field
				}
			}
			sessions := map[string]bool{}
			for id, session := range m.ws.Sessions {
				if session.DB != nil {
					sessions[id] = true
				}
			}
			if len(sessions) > 1 {
				label = t.database.Path + " / " + label
			}
			lines = append(lines, "  "+safe(label))
		}
		lines = append(lines, "", "pgup/pgdown to inspect all requested fields",
			renderDialogAction("Deny", "d", max(1, min(m.width-10, 86)), m.settings.Appearance == "compact", m.settings.ASCII, true, m.colors()),
			renderDialogAction("Allow once", "a", max(1, min(m.width-10, 86)), m.settings.Appearance == "compact", m.settings.ASCII, false, m.colors()),
			renderDialogAction("Allow for this path", "p", max(1, min(m.width-10, 86)), m.settings.Appearance == "compact", m.settings.ASCII, false, m.colors()),
			renderDialogAction("Always allow", "w", max(1, min(m.width-10, 86)), m.settings.Appearance == "compact", m.settings.ASCII, false, m.colors()))
	} else {
		lines = append(lines, "", "[enter] Review request", "[d] Deny")
	}
	if r.busy {
		lines = append(lines, "Resolving secrets…")
	}
	if r.err != "" {
		lines = append(lines, r.err)
	}
	return append(lines, "", "[esc] Return to vault · [ctrl+b] Pending requests")
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

func (m *Model) runCompactLines() []string {
	r := m.runs
	if r.index >= len(r.pending) {
		return []string{"No pending requests"}
	}
	p := r.pending[r.index]
	lines := []string{fmt.Sprintf("Request %d/%d: %s", r.index+1, len(r.pending), safe(strings.Join(p.Request.Command, " "))), safe(p.Request.Directory)}
	if p.Identity.Digest != "" {
		lines = append(lines, runIdentityLabel(p, r.newIdentity[p]))
	}
	if len(r.targets) > 0 {
		index := min(r.offset, len(r.targets)-1)
		lines = append(lines, fmt.Sprintf("Field %d/%d: %s", index+1, len(r.targets), safe(r.targets[index].ref)))
	}
	if m.height >= 18 {
		lines = append(lines, "pgup/pgdown fields · up/down requests")
	}
	for _, action := range []struct{ label, key string }{{"Deny", "d"}, {"Allow once", "a"}, {"Allow for this path", "p"}, {"Always allow", "w"}} {
		lines = append(lines, renderDialogAction(action.label, action.key, max(1, min(m.width-10, 86)), m.settings.Appearance == "compact", m.settings.ASCII, action.key == "d", m.colors()))
	}
	if r.err != "" {
		lines = append(lines, r.err)
	}
	return append(lines, "esc return")
}

func runIdentityLabel(p *runpkg.Pending, isNew bool) string {
	state := "known agent"
	if isNew {
		state = "new agent"
	}
	return "Agent: " + safe(p.Identity.Name) + " (" + state + ")"
}

func (m *Model) openRunPermissions() {
	if m.runs == nil {
		m.runs = &runState{}
	}
	r := m.runs
	session := m.ws.ActiveSession()
	view := m.ws.ActiveView()
	if session != nil && session.DB != nil && view != nil {
		for _, g := range session.DB.Groups {
			if g.Name == runpkg.GrantGroup && g.ParentUUID == session.DB.RootUUID {
				view.NavFilter = ""
				for index, item := range m.navigation() {
					if item.kind == "group" && item.group == g.UUID {
						m.selectNav(index)
						break
					}
				}
				view.Focus = 1
				r.visible = false
				m.setNotice("delete permission entries to revoke; delete group to revoke all", false)
				break
			}
		}
	}
}
