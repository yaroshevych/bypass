package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	runpkg "bypass/internal/run"
	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type runAction = dialogAction

type runDialogLayout struct {
	title, errorMessage          string
	body                         []string
	targets                      []int
	actions                      []runAction
	width, x, y, focus, capacity int
}

func (m *Model) runRequestsBody() ([]string, []int) {
	r := m.runs
	body := []string{fmt.Sprintf("Pending secret requests (%d)", len(r.pending))}
	targets := []int{-1}
	if len(r.pending) == 0 {
		body = append(body, "No pending requests")
		targets = append(targets, -1)
	} else {
		contentRows := max(0, m.runBodyCapacity(true)-len(m.runRequestActions())-1)
		requestRows := max(0, contentRows-1)
		offset := viewportOffset(r.index, len(r.pending), requestRows, r.index-requestRows/2)
		start, end := viewportRange(len(r.pending), requestRows, offset)
		compact := m.compactAppearance()
		var rows []listComponentRow
		for i := start; i < end; i++ {
			p := r.pending[i]
			if p.Context.Err() != nil {
				continue
			}
			name := "unknown command"
			if len(p.Request.Command) > 0 {
				name = filepath.Base(p.Request.Command[0])
			}
			label := safe(name)
			if p.Identity.Name != "" {
				label += " · " + safe(p.Identity.Name)
			}
			if !compact {
				label = " " + label
			}
			selected := i == r.index && !r.buttonsFocused
			right := ""
			if selected {
				right = "(enter)"
			}
			rows = append(rows, listComponentRow{kind: listComponentItem, item: i, text: label, right: right, selected: selected, activeFill: true, pillSelected: true})
		}
		for _, row := range m.renderListComponent(rows, listComponentOptions{width: m.dialogInnerWidth(m.runDialogWidth(false)), compact: compact, ascii: m.settings.ASCII, focused: true, sidebar: true}) {
			body = append(body, row.text)
			targets = append(targets, row.item)
		}
	}

	return body, targets
}

func (m *Model) runReviewActions() []runAction {
	r := m.runs
	if r == nil || m.runReviewed() == nil {
		return []runAction{{"Back", "esc"}}
	}
	if r.choosing {
		return []runAction{{"Select vault", "enter"}, {"Back", "esc"}}
	}
	if r.unavailable != "" {
		return []runAction{{"Deny", "d"}, {"Back", "esc"}}
	}
	actions := []runAction{{"Deny", "d"}, {"Allow", "a"}}
	actions = append(actions, runAction{"Allow for this identity", "i"})
	return append(actions, runAction{"Back", "esc"})
}

func (m *Model) runRequestActions() []runAction {
	return []runAction{{"Deny all", "d"}, {"Approve all", "a"}, {"Reset saved permissions", "r"}, {"Close", "esc"}}
}

func (m *Model) runResetActions() []runAction {
	return []runAction{{"Cancel", "esc"}, {"Reset", "ctrl+s"}}
}

func (m *Model) runBodyCapacity(parent bool) int {
	if m.smallDialogScreen() {
		return max(1, m.height-1)
	}
	chrome := 4
	if m.compactAppearance() {
		chrome = 2
	}
	if !parent {
		chrome = 8
	}
	return max(1, m.height-chrome)
}

func (m *Model) runReviewBody() []string {
	r := m.runs
	body := []string{}
	p := m.runReviewed()
	if p == nil {
		body = append(body, "No pending requests")
	} else {
		if r.unavailable != "" {
			body = append(body, "URI: "+safe(r.unavailable), "Unlock vault or check entry name.")
			if p.Identity.Digest != "" {
				body = append(body, runIdentityLabel(p, r.newIdentity[p]))
			}
		} else if r.choosing {
			body = append(body, "Select vault for this reference:")
			if len(r.choices) > 0 {
				body = append(body, "Vault: "+safe(r.choices[r.choiceIndex].database.Path))
			}
		} else if m.height <= 16 {
			if len(r.targets) == 0 {
				body = append(body, "No requested fields")
			} else {
				index := min(r.fieldOffset, len(r.targets)-1)
				label := fmt.Sprintf("%d/%d: %s", index+1, len(r.targets), m.runTargetLabel(r.targets[index]))
				if p.Identity.Digest == "" {
					label = "No identity · " + label
				} else if p.Identity.Name != "" {
					label = safe(p.Identity.Name) + " · " + label
				}
				body = append(body, label)
			}
		} else {
			prefix, suffix, visible, _ := m.runReviewFieldLayout(p)
			body = append(body, prefix...)
			if len(r.targets) == 0 {
				body = append(body, "No requested fields")
			} else {
				end := min(len(r.targets), min(r.fieldOffset, max(0, len(r.targets)-visible))+visible)
				start := max(0, end-visible)
				for i := start; i < end; i++ {
					body = append(body, fmt.Sprintf("%d/%d: %s", i+1, len(r.targets), m.runTargetLabel(r.targets[i])))
				}
			}
			body = append(body, suffix...)
		}
		if r.choosing && m.height > 16 {
			for i, choice := range r.choices {
				prefix := "  "
				if i == r.choiceIndex {
					prefix = "> "
				}
				body = append(body, prefix+safe(choice.database.Path))
			}
		}
	}
	return body
}

func (m *Model) runReviewFieldLayout(p *runpkg.Pending) (prefix, suffix []string, visible int, paging bool) {
	prefix = []string{}
	if p.Identity.Digest != "" {
		prefix = append(prefix, runIdentityLabel(p, m.runs.newIdentity[p]))
	}
	prefix = append(prefix, "Command: "+safe(strings.Join(p.Request.Command, " ")))
	prefix = append(prefix, "Directory: "+safe(p.Request.Directory))
	if len(p.Caller) > 0 {
		prefix = append(prefix, "Caller: "+safe(strings.Join(p.Caller, " ← ")))
	}
	prefix = append(prefix, "Requested secrets:")
	if p.Identity.Digest == "" && m.runs.err == "" {
		suffix = append(suffix, m.colors().secondary.Render("Identity required to save access."))
	}
	capacity := max(0, m.runBodyCapacity(false)-len(m.runReviewActions())-1)
	available := capacity - len(prefix) - len(suffix)
	if available <= 0 {
		name := "unknown command"
		if len(p.Request.Command) > 0 {
			name = filepath.Base(p.Request.Command[0])
		}
		prefix = []string{"Request: " + safe(name), "Requested secrets:"}
		available = capacity - len(prefix) - len(suffix)
	}
	if available <= 0 {
		prefix = []string{"Requested secrets:"}
		available = capacity - len(prefix) - len(suffix)
	}
	if available <= 0 {
		suffix = nil
		available = capacity - len(prefix)
	}
	visible = max(1, available)
	paging = len(m.runs.targets) > visible
	if paging && visible > 1 {
		visible--
		suffix = append(suffix, "pgup/pgdown to inspect requested fields")
	} else if paging && len(prefix) > 0 {
		prefix[len(prefix)-1] = "Fields · pgup/pgdown"
	}
	return prefix, suffix, visible, paging
}

func (m *Model) runFieldPageSize() int {
	if m.height <= 16 {
		return 1
	}
	if p := m.runReviewed(); p != nil {
		_, _, visible, _ := m.runReviewFieldLayout(p)
		return visible
	}
	return 1
}

func (m *Model) runTargetLabel(target runTarget) string {
	label := target.field
	if target.database != nil {
		for _, entry := range target.database.Entries {
			if entry.UUID == target.entry {
				label = entry.Get("Title") + " #" + target.field
				break
			}
		}
	}
	if len(m.ws.Sessions) > 1 {
		label = target.database.Path + " / " + label
	}
	return safe(label)
}

func (m *Model) runActionAt(x, y int) (int, bool) {
	if m.runs == nil {
		return 0, false
	}
	layout := m.runLayout(m.runs.review)
	opts := m.runLayerOptions(layout)
	target := dialogrender.HitTest(opts, x, y, nil).Target
	return target, target >= 0
}

func (m *Model) runRequestAt(x, y int) int {
	if m.runs == nil || m.runs.review || m.runs.resetConfirm {
		return -1
	}
	layout := m.runRequestsLayout()
	targets := make([]int, len(layout.targets))
	for i, target := range layout.targets {
		targets[i] = -1
		if target >= 0 {
			targets[i] = len(layout.actions) + target
		}
	}
	target := dialogrender.HitTest(m.runLayerOptions(layout), x, y, targets).Target
	if target < len(layout.actions) {
		return -1
	}
	return target - len(layout.actions)
}

func (m *Model) runDialogWidth(review bool) int {
	if review {
		return min(max(20, m.width-14), 52)
	}
	return min(max(20, m.width-6), 60)
}

func (m *Model) runLayout(review bool) runDialogLayout {
	if m.runs != nil && m.runs.resetConfirm {
		layout := runDialogLayout{width: m.runDialogWidth(true)}
		layout.title, layout.actions = "Reset saved permissions", m.runResetActions()
		body := []string{"Reset affects unlocked vaults.", "Locked vaults stay unchanged.", "Pending requests stay queued."}
		if m.runs.resetBusy {
			body = []string{"Resetting saved permissions…", "Pending requests stay queued."}
		}
		layout.errorMessage = m.runs.resetErr
		layout.body, layout.focus, layout.capacity = body, m.runs.resetFocus, m.runBodyCapacity(false)
		return m.positionRunLayout(layout, true)
	}
	if !review {
		return m.runRequestsLayout()
	}
	width := m.runDialogWidth(true)
	if m.runs != nil && m.runs.unavailable != "" {
		width = m.runDialogWidth(false)
	}
	layout := runDialogLayout{
		title: "Review request", width: width,
		actions: m.runReviewActions(), body: m.runReviewBody(), errorMessage: m.runs.err, focus: m.runs.reviewFocus, capacity: m.runBodyCapacity(false),
	}
	return m.positionRunLayout(layout, true)
}

func (m *Model) runRequestsLayout() runDialogLayout {
	layout := runDialogLayout{
		title: "Requests", width: m.runDialogWidth(false),
		actions: m.runRequestActions(), focus: -1, capacity: m.runBodyCapacity(true),
	}
	if m.runs.buttonsFocused || len(m.runs.pending) == 0 {
		layout.focus = m.runs.actionFocus
	}
	layout.body, layout.targets = m.runRequestsBody()
	return m.positionRunLayout(layout, false)
}

func (m *Model) positionRunLayout(layout runDialogLayout, review bool) runDialogLayout {
	opts := m.runLayerOptions(layout)
	prepared, _ := dialogrender.PrepareBody(opts)
	if opts.Fullscreen {
		layout.width, layout.x, layout.y = m.width, 0, 0
		return layout
	}
	g := m.centeredDialogGeometry(layout.width, len(prepared))
	layout.width = g.Width
	layout.x = g.X
	layout.y = g.Y
	if review {
		parentY := m.runRequestsLayout().y
		layout.y = min(max(0, parentY+2), max(0, m.height-g.Height))
	}
	return layout
}

func (m *Model) activateRunAction(index int) tea.Cmd {
	if m.runs == nil {
		return nil
	}
	r := m.runs
	var actions []runAction
	if r.resetConfirm {
		actions = m.runResetActions()
	} else if r.review {
		actions = m.runReviewActions()
	} else {
		actions = m.runRequestActions()
	}
	if index < 0 || index >= len(actions) {
		return nil
	}
	return m.activateRunKey(actions[index].key)
}

func (m *Model) runActionForKey(key string) int {
	actions := m.runReviewActions()
	if m.runs != nil && m.runs.resetConfirm {
		actions = m.runResetActions()
	}
	for i, action := range actions {
		if action.key == key {
			return i
		}
	}
	return -1
}

func (m *Model) activateRunKey(key string) tea.Cmd {
	r := m.runs
	if r == nil {
		return nil
	}
	if r.resetConfirm {
		switch key {
		case "esc":
			r.resetConfirm = false
			r.resetErr = ""
		case "ctrl+s", "r":
			r.resetErr = ""
			return m.beginRunPermissionReset()
		}
		return nil
	}
	if r.review {
		if r.choosing {
			if key == "esc" {
				r.review, r.choosing, r.reviewed = false, false, nil
				return nil
			}
			if key == "enter" && len(r.choices) > 0 {
				p := m.runReviewed()
				if p == nil {
					return nil
				}
				if r.chosen == nil {
					r.chosen = map[*runpkg.Pending]map[string]runTarget{}
				}
				if r.chosen[p] == nil {
					r.chosen[p] = map[string]runTarget{}
				}
				choice := r.choices[r.choiceIndex]
				r.chosen[p][choice.ref] = choice
				r.choices, r.choiceIndex, r.choosing = nil, 0, false
				targets, err := m.resolveRunTargets(p)
				if err != nil {
					r.err = err.Error()
					var missing *unavailableRunReference
					if errors.As(err, &missing) {
						r.unavailable = missing.reference
					}
					r.choosing = len(r.choices) > 0
					m.focusRunAction("esc")
					return nil
				}
				r.targets, r.err, r.unavailable = targets, "", ""
				m.focusRunAction("esc")
			}
			return nil
		}
		switch key {
		case "esc":
			r.review = false
			r.reviewed = nil
			r.reviewFocus = 0
		case "d":
			if p := m.runReviewed(); p != nil {
				p.Reply(runpkg.Response{Error: "secret request denied"})
				m.removeRun(p)
			}
		case "a", "i":
			if len(r.targets) == 0 || r.unavailable != "" || !m.runTargetsCurrent(r.targets) {
				r.targets = nil
				r.err = "vault changed; review the request again"
				m.focusRunAction("esc")
				return nil
			}
			p := m.runReviewed()
			if p == nil {
				return nil
			}
			if key == "i" && p.Identity.Digest == "" {
				r.err = "identity required to save access"
				return nil
			}
			if key == "i" && !m.canSaveRunGrant(r.targets) {
				r.err = "vault is read-only; use Allow"
				return nil
			}
			r.busy = true
			r.busyPending = p
			return m.runGrantJob(p, append([]runTarget(nil), r.targets...), key)
		}
		return nil
	}
	switch key {
	case "enter":
		m.enterRunReview()
	case "d":
		for _, p := range append([]*runpkg.Pending(nil), r.pending...) {
			p.Reply(runpkg.Response{Error: "secret request denied"})
			m.removeRun(p)
		}
	case "a":
		var jobs []tea.Cmd
		failed := 0
		for _, p := range r.pending {
			choices, choiceIndex := r.choices, r.choiceIndex
			targets, err := m.resolveRunTargets(p)
			r.choices, r.choiceIndex = choices, choiceIndex
			if err != nil || len(targets) == 0 {
				failed++
				continue
			}
			job := m.runGrantJob(p, targets, "a")
			jobs = append(jobs, func() tea.Msg {
				result := job().(runResult)
				result.bulk = true
				return result
			})
		}
		if failed > 0 {
			m.setNotice(fmt.Sprintf("%d request(s) need individual review", failed), true)
		}
		if len(jobs) > 0 {
			r.busy, r.bulkRemaining = true, len(jobs)
			return tea.Batch(jobs...)
		}
	case "esc":
		r.visible = false
	case "r":
		r.resetConfirm = true
		r.resetFocus = 0
	}
	return nil
}

func (m *Model) canSaveRunGrant(targets []runTarget) bool {
	if m.readOnly {
		return false
	}
	if _, ok := m.backend.(vault.Writer); !ok {
		return false
	}
	for _, target := range targets {
		session := m.ws.Sessions[target.session]
		if session == nil || session.ReadOnly || session.Stale || session.DB != target.database {
			return false
		}
	}
	return true
}

func (m *Model) enterRunReview() {
	r := m.runs
	if r == nil || len(r.pending) == 0 {
		return
	}
	p := r.pending[r.index]
	r.choices, r.choiceIndex = nil, 0
	r.targets, r.err, r.unavailable = nil, "", ""
	r.reviewed = p
	targets, err := m.resolveRunTargets(p)
	if err != nil {
		r.err = err.Error()
		var missing *unavailableRunReference
		if errors.As(err, &missing) {
			r.unavailable = missing.reference
		}
		r.choosing = len(r.choices) > 0
	} else {
		r.targets = targets
		r.choosing = false
	}
	r.review = true
	r.reviewFocus = m.runActionForKey("esc") // Back is the safe default in the child dialog.
	r.fieldOffset = 0
}

func (m *Model) focusRunAction(key string) {
	if m.runs.review {
		m.runs.reviewFocus = m.runActionForKey(key)
		if m.runs.reviewFocus < 0 {
			m.runs.reviewFocus = 0
		}
		return
	}
	m.runs.actionFocus = m.runActionForKey(key)
	if m.runs.actionFocus < 0 {
		m.runs.actionFocus = 0
	}
}

func (m *Model) runReviewed() *runpkg.Pending {
	if m.runs == nil || !m.runs.review {
		return nil
	}
	return m.runs.reviewed
}

func (m *Model) runTargetsCurrent(targets []runTarget) bool {
	for _, target := range targets {
		session := m.ws.Sessions[target.session]
		if session == nil || session.DB != target.database {
			return false
		}
	}
	return true
}

func (m *Model) runLayerOptions(layout runDialogLayout) dialogrender.Options {
	opts := m.dialogOptions()
	opts.Mode, opts.Title, opts.Body, opts.Error = dialogrender.Overlay, layout.title, layout.body, safe(layout.errorMessage)
	opts.MaxBodyRows, opts.Width, opts.X, opts.Y = layout.capacity, layout.width, layout.x, layout.y
	rows := m.dialogActionRows(layout.actions, layout.focus, m.dialogInnerWidth(layout.width))
	for i, row := range rows {
		opts.Actions = append(opts.Actions, dialogrender.Action{Text: row, Target: i})
	}
	return opts
}

func renderRunLayer(m *Model, backdrop []string, layout runDialogLayout) []string {
	opts := m.runLayerOptions(layout)
	opts.Backdrop = backdrop
	return dialogrender.Render(opts)
}

func (m *Model) runDialogView() []string {
	r := m.runs
	parentLayout := m.runRequestsLayout()
	parent := renderRunLayer(m, m.modalBackdrop(), parentLayout)
	if r == nil || (!r.review && !r.resetConfirm) {
		return parent
	}
	return renderRunLayer(m, parent, m.runLayout(r.review))
}
