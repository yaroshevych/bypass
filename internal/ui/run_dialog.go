package ui

import (
	"errors"
	"fmt"
	"strings"

	runpkg "bypass/internal/run"
	dialogrender "bypass/internal/ui/dialog"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	runSessionNote  = "Session approval retained until vault is locked."
	runIdentityNote = "Agent identity is self-claimed and not verified."
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
	var body []string
	var targets []int
	if len(r.pending) == 0 {
		body = append(body, " No pending requests ")
		targets = append(targets, -1)
	} else {
		contentRows := max(0, m.runBodyCapacity(true)-len(m.runRequestActions())-1)
		requestRows := contentRows
		offset := viewportOffset(r.index, len(r.pending), requestRows, r.index-requestRows/2)
		start, end := viewportRange(len(r.pending), requestRows, offset)
		compact := m.compactAppearance()
		var rows []listComponentRow
		for i := start; i < end; i++ {
			p := r.pending[i]
			if p.Context.Err() != nil {
				continue
			}
			label := runRequestSummary(p)
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

// runRequestSummary uses reference metadata only; it never resolves field values.
func runRequestSummary(p *runpkg.Pending) string {
	labels := make([]string, 0, len(p.Request.References))
	for _, raw := range p.Request.References {
		ref, err := runpkg.ParseReference(raw)
		if err != nil {
			labels = append(labels, "Invalid reference")
			continue
		}
		field := ref.Field
		switch strings.ToLower(field) {
		case "password":
			field = "Password"
		case "username":
			field = "Username"
		case "email":
			field = "Email"
		case "url":
			field = "URL"
		case "notes":
			field = "Notes"
		}
		labels = append(labels, safe(ref.Components[len(ref.Components)-1])+" "+safe(field))
	}
	summary := strings.Join(labels, ", ")
	if summary == "" {
		summary = "No requested secrets"
	}
	if p.Identity.Name != "" {
		return safe(p.Identity.Name) + ": " + summary
	}
	return summary
}

func (m *Model) runReviewActions() []runAction {
	r := m.runs
	back := "Back"
	if r != nil && r.directReview && len(r.pending) == 1 {
		back = "Close"
	}
	if r == nil || m.runReviewed() == nil {
		return []runAction{{back, "esc"}}
	}
	if r.choosing {
		return []runAction{{"Select entry", "enter"}, {back, "esc"}}
	}
	if r.unavailable != "" {
		return []runAction{{"Deny", "d"}, {back, "esc"}}
	}
	actions := []runAction{{"Deny", "d"}, {"Approve once", "a"}}
	actions = append(actions, runAction{"Approve for this session", "s"})
	return append(actions, runAction{back, "esc"})
}

func (m *Model) runRequestActions() []runAction {
	return []runAction{{"Deny all", "d"}, {"Approve all once", "a"}, {"Review session permissions", "r"}, {"Close", "esc"}}
}

func (m *Model) runBulkActions() []runAction {
	return []runAction{{"Cancel", "esc"}, {"Approve all once", "a"}}
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
		body = append(body, " No pending requests ")
	} else {
		if r.unavailable != "" {
			body = append(body, "URI: "+safe(r.unavailable), "Unlock vault or check entry name.")
			if p.Identity.Digest != "" {
				body = append(body, runIdentityLabel(p, r.newIdentity[p]))
			}
		} else if r.choosing {
			body = append(body, "Select entry with up/down:")
			if len(r.choices) > 0 {
				choice := r.choices[r.choiceIndex]
				body = append(body, "Vault: "+safe(choice.database.Path), "Entry: "+safe(runTargetPath(choice)), "ID: "+safe(choice.entry))
			}
		} else if m.height <= 16 {
			agent := "Anonymous"
			if p.Identity.Name != "" {
				agent = p.Identity.Name
			}
			heading := "REQUESTED SECRETS"
			if len(m.runReviewFieldLines()) > 1 {
				heading += " · pgup/pgdn"
			}
			body = append(body, m.runDetailLine("Agent", agent))
			body = append(body, m.runReviewDetailLines("Command", strings.Join(p.Request.Command, " "), m.runReviewDetailWidth())...)
			body = append(body, m.runReviewDetailLines("Directory", p.Request.Directory, m.runReviewDetailWidth())...)
			body = append(body, m.colors().secondary.Render(heading))
			if len(r.targets) == 0 {
				body = append(body, "No requested fields")
			} else {
				lines := m.runReviewFieldLines()
				index := min(r.fieldOffset, len(lines)-1)
				body = append(body, lines[index])
			}
			body = append(body, "", "")
			body = append(body, m.runReviewNoteLines(runSessionNote)...)
			body = append(body, m.runReviewNoteLines(runIdentityNote)...)
		} else {
			prefix, suffix, visible, _ := m.runReviewFieldLayout(p)
			body = append(body, prefix...)
			if len(r.targets) == 0 {
				body = append(body, "No requested fields")
			} else {
				lines := m.runReviewFieldLines()
				start := min(r.fieldOffset, max(0, len(lines)-visible))
				body = append(body, lines[start:min(len(lines), start+visible)]...)
			}
			body = append(body, suffix...)
		}
	}
	return body
}

func (m *Model) runReviewFieldLayout(p *runpkg.Pending) (prefix, suffix []string, visible int, paging bool) {
	agent := "Anonymous"
	if p.Identity.Name != "" {
		agent = p.Identity.Name
	}
	details := append(m.runReviewDetailLines("Command", strings.Join(p.Request.Command, " "), m.runReviewDetailWidth()), m.runReviewDetailLines("Directory", p.Request.Directory, m.runReviewDetailWidth())...)
	prefix = append([]string{m.runDetailLine("Agent", agent)}, details...)
	prefix = append(prefix, "", m.colors().secondary.Render("REQUESTED SECRETS"))
	suffix = []string{"", ""}
	suffix = append(suffix, m.runReviewNoteLines(runSessionNote)...)
	suffix = append(suffix, m.runReviewNoteLines(runIdentityNote)...)
	if p.Identity.Digest == "" && m.runs.err == "" {
		suffix = append(suffix, m.runReviewNoteLines("Identity required for session approval")...)
	}
	capacity := max(0, m.runBodyCapacity(false)-len(m.runReviewActions())-1)
	available := capacity - len(prefix) - len(suffix)
	if available <= 0 {
		prefix = append(details, "", m.colors().secondary.Render("REQUESTED SECRETS"))
		available = capacity - len(prefix) - len(suffix)
	}
	if available <= 0 {
		prefix = append(details, m.colors().secondary.Render("REQUESTED SECRETS"))
		available = capacity - len(prefix) - len(suffix)
	}
	if available <= 0 {
		suffix = nil
		available = capacity - len(prefix)
	}
	visible = max(1, available)
	paging = len(m.runReviewFieldLines()) > visible
	if paging && visible > 1 {
		visible--
		suffix = append(suffix, "pgup/pgdown to inspect requested fields")
	} else if paging && len(prefix) > 0 {
		prefix[len(prefix)-1] = m.colors().secondary.Render("REQUESTED SECRETS · pgup/pgdn")
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

func (m *Model) runDetailLine(label, value string) string {
	c := m.colors()
	return c.secondary.Render(label+":") + strings.Repeat(" ", max(1, 11-len(label))) + c.primary.Render(safe(value))
}

func (m *Model) runReviewDialogWidth() int {
	return min(max(20, m.width-8), max(lipgloss.Width(runSessionNote), lipgloss.Width(runIdentityNote))+6)
}

func (m *Model) runReviewDetailWidth() int {
	if m.smallDialogScreen() {
		return max(1, m.width-2)
	}
	return max(1, m.dialogInnerWidth(m.runReviewDialogWidth())-2)
}

func (m *Model) runReviewNoteLines(note string) []string {
	width := m.runReviewDetailWidth()
	wrapped := ansi.Hardwrap(ansi.Wrap(note, width, ""), width, false)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = m.colors().secondary.Render(lines[i])
	}
	return lines
}

func (m *Model) runReviewDetailLines(label, value string, width int) []string {
	indent := lipgloss.Width(label) + 1 + max(1, 11-len(label))
	valueWidth := max(1, width-indent)
	wrapped := ansi.Hardwrap(ansi.Wrap(safe(value), valueWidth, ""), valueWidth, false)
	parts := strings.Split(wrapped, "\n")
	lines := make([]string, len(parts))
	for i, part := range parts {
		if i == 0 {
			lines[i] = m.runDetailLine(label, part)
		} else {
			lines[i] = strings.Repeat(" ", indent) + m.colors().primary.Render(part)
		}
	}
	return lines
}

func (m *Model) runReviewFieldLines() []string {
	if m.runs == nil {
		return nil
	}
	width := m.width
	if !m.smallDialogScreen() {
		width = min(max(20, m.width-8), max(lipgloss.Width(runSessionNote), lipgloss.Width(runIdentityNote))+6) - 4
	}
	width = max(1, width-2) // One cell of content padding on each side.
	var lines []string
	for _, target := range m.runs.targets {
		wrapped := ansi.Hardwrap(ansi.Wrap(m.runSecretLine(target), width, ""), width, false)
		lines = append(lines, strings.Split(wrapped, "\n")...)
		if p := m.runReviewed(); p != nil {
			if _, selected := m.runs.chosen[p][target.ref]; selected {
				for _, detail := range []string{"Vault: " + target.database.Path, "Entry: " + runTargetPath(target), "ID: " + target.entry} {
					wrapped := ansi.Hardwrap(ansi.Wrap(safe(detail), width, ""), width, false)
					lines = append(lines, strings.Split(wrapped, "\n")...)
				}
			}
		}
	}
	return lines
}

func runTargetPath(target runTarget) string {
	if target.database != nil {
		for _, e := range target.database.Entries {
			if e.UUID == target.entry {
				return strings.TrimRight(e.GroupPath, "/") + "/" + e.Get("Title")
			}
		}
	}
	return "Unknown"
}

func (m *Model) runSecretLine(target runTarget) string {
	name := "Unknown"
	if target.database != nil {
		for _, e := range target.database.Entries {
			if e.UUID == target.entry {
				name = e.Get("Title")
				break
			}
		}
	}
	return m.runDetailLine(name, target.field)
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
	if m.runs == nil || m.runs.review || m.runs.bulkConfirm {
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
		return min(max(20, m.width-8), 60)
	}
	return min(max(20, m.width-6), 60)
}

func (m *Model) runLayout(review bool) runDialogLayout {
	if m.runs != nil && m.runs.bulkConfirm {
		layout := runDialogLayout{width: m.runDialogWidth(true)}
		layout.title, layout.actions = "Approve requests?", m.runBulkActions()
		layout.body = []string{fmt.Sprintf("Approve %d pending request(s) once?", len(m.runs.bulkPending)), "New requests are not included."}
		layout.focus, layout.capacity = m.runs.bulkFocus, m.runBodyCapacity(false)
		return m.positionRunLayout(layout)
	}

	if !review {
		return m.runRequestsLayout()
	}
	width := m.runReviewDialogWidth()
	layout := runDialogLayout{
		title: "Review request", width: width,
		actions: m.runReviewActions(), body: m.runReviewBody(), errorMessage: m.runs.err, focus: m.runs.reviewFocus, capacity: m.runBodyCapacity(false),
	}
	return m.positionRunLayout(layout)
}

func (m *Model) runRequestsLayout() runDialogLayout {
	layout := runDialogLayout{
		title: "Secret Requests", width: m.runDialogWidth(false),
		actions: m.runRequestActions(), focus: -1, capacity: m.runBodyCapacity(true),
	}
	if m.runs.buttonsFocused || len(m.runs.pending) == 0 {
		layout.focus = m.runs.actionFocus
	}
	layout.body, layout.targets = m.runRequestsBody()
	return m.positionRunLayout(layout)
}

func (m *Model) positionRunLayout(layout runDialogLayout) runDialogLayout {
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
	return layout
}

func (m *Model) activateRunAction(index int) tea.Cmd {
	if m.runs == nil {
		return nil
	}
	r := m.runs
	var actions []runAction
	if r.bulkConfirm {
		actions = m.runBulkActions()

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
	if r.bulkConfirm {
		pending := r.bulkPending
		r.bulkConfirm, r.bulkPending = false, nil
		if key != "a" {
			return nil
		}
		return m.approveRunRequests(pending)
	}

	if r.review {
		if r.choosing {
			if key == "esc" {
				m.leaveRunReview()
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
			m.leaveRunReview()
		case "d":
			if p := m.runReviewed(); p != nil {
				p.Reply(runpkg.Response{Error: "secret request denied"})
				m.removeRun(p)
			}
		case "a", "s":
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
		if len(r.pending) > 0 {
			r.bulkConfirm, r.bulkFocus = true, 0
			r.bulkPending = append([]*runpkg.Pending(nil), r.pending...)
		}
	case "esc":
		r.visible = false
	case "r":
		m.openRunPermissions()
	}
	return nil
}

func (m *Model) approveRunRequests(pending []*runpkg.Pending) tea.Cmd {
	r := m.runs
	var jobs []tea.Cmd
	failed := 0
	for _, p := range pending {
		found := false
		for _, current := range r.pending {
			if current == p {
				found = true
				break
			}
		}
		if !found || p.Context.Err() != nil {
			continue
		}
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
	return nil
}

func (m *Model) enterRunReview() {
	r := m.runs
	if r == nil || len(r.pending) == 0 {
		return
	}
	p := r.pending[r.index]
	r.directReview = false
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

func (m *Model) leaveRunReview() {
	r := m.runs
	if r.directReview && len(r.pending) == 1 {
		r.visible = false
	}
	r.review, r.directReview, r.choosing, r.reviewed = false, false, false, nil
	r.reviewFocus = 0
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
	if layout.title == "Review request" || layout.title == "Approve requests?" {
		opts.Body = make([]string, len(layout.body))
		for i, line := range layout.body {
			opts.Body[i] = " " + line + " "
		}
	}
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
	if r != nil && r.review && r.directReview && len(r.pending) == 1 {
		return renderRunLayer(m, m.modalBackdrop(), m.runLayout(true))
	}
	parentLayout := m.runRequestsLayout()
	parent := renderRunLayer(m, m.modalBackdrop(), parentLayout)
	if r == nil || (!r.review && !r.bulkConfirm) {
		return parent
	}
	return renderRunLayer(m, m.dimBackdrop(parent), m.runLayout(r.review))
}
