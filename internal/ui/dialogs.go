package ui

import (
	"fmt"
	"strings"

	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type actionDialog struct {
	duplicates                      *vault.DuplicateEntries
	kind, title                     string
	message                         []string
	options                         []string
	selected, offset                int
	destinationFocus                int
	input                           input
	err                             string
	applyToPassword                 bool
	sessionID, entryUUID, groupUUID string
	groups                          []vault.Group
	change                          vault.Change
	after                           func() tea.Cmd
	secret                          []byte
}

func (m *Model) clearDialog() {
	if m.dialog != nil {
		m.dialog.duplicates.Close()
		m.dialog.input.clear()
		clear(m.dialog.secret)
		m.dialog.secret = nil
		m.dialog.after = nil
	}
	m.dialog = nil
}
func (m *Model) showExternal(id string) {
	s := m.ws.Sessions[id]
	if s == nil {
		return
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "external", title: "Vault changed externally", sessionID: id, options: []string{"Reload", "Keep Current View"}, selected: 1, message: []string{vaultLabel(s.Path), "Another application changed this vault.", "Reload to use the file on disk; writes blocked until reload."}}
	if m.editor != nil && m.editorSession == id {
		m.dialog.message = append(m.dialog.message, "Reload will discard your unsaved entry draft.")
	}
}
func (m *Model) confirmDelete() {
	s := m.ws.ActiveSession()
	v := m.ws.ActiveView()
	if s == nil || s.DB == nil || v == nil {
		return
	}
	if v.Focus == 0 && m.currentNav().kind != "group" {
		m.setNotice("select an entry or group to delete", true)
		return
	}
	change := vault.Change{Kind: vault.DeleteEntry}
	name := ""
	permanent := !s.DB.RecycleBinEnabled
	if v.Focus == 0 && m.currentNav().kind == "group" {
		groupID := m.currentNav().group
		if groupID == s.DB.RootUUID || groupID == s.DB.RecycleBinUUID {
			m.setNotice("can't delete root or bin groups here", true)
			return
		}
		change.Kind = vault.DeleteGroup
		change.GroupUUID = groupID
		descendants := map[string]bool{groupID: true}
		for changed := true; changed; {
			changed = false
			for _, g := range s.DB.Groups {
				if descendants[g.ParentUUID] && !descendants[g.UUID] {
					descendants[g.UUID] = true
					changed = true
				}
				if g.UUID == groupID {
					name = g.Name
					permanent = permanent || g.Recycled
				}
			}
		}
		count := 0
		for _, e := range s.DB.Entries {
			if descendants[e.GroupUUID] {
				count++
			}
		}
		name = fmt.Sprintf("%s (%d entries, including descendants)", safe(name), count)
	} else {
		e := m.selected()
		if e == nil {
			return
		}
		change.EntryUUID = e.UUID
		name = visibleField(*e, "Title")
		permanent = permanent || e.Recycled
	}
	action := "Move to Recycle Bin"
	message := "Entry history and attachments move with the item."
	if permanent {
		action = "Delete Permanently"
		message = "This permanently deletes the item. Recovery requires a backup."
	}
	if s.DB.Interactive() {
		action = "Delete"
		message = "KeePassXC moves the item to its recycle bin when enabled. Items already in the bin are deleted permanently."
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "delete", title: action, options: []string{"Cancel", action}, selected: 0, sessionID: s.ID, change: change, message: []string{safe(name), message}}
}
func (m *Model) chooseDestination(e *vault.Entry) {
	s := m.ws.ActiveSession()
	itemTitle := visibleField(*e, "Title")
	if itemTitle == "" {
		itemTitle = "Untitled"
	}
	title := "Move " + safe(itemTitle)
	if e.Recycled {
		title = "Restore " + safe(itemTitle)
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "destination", title: title, sessionID: s.ID, entryUUID: e.UUID}
	m.dialog.groups = destinationGroups(s.DB, s.Destinations)
}
func (d *actionDialog) filteredGroups() []vault.Group {
	return d.groups
}

func (d *actionDialog) destinationViewport(height int, groups []vault.Group) (start, capacity int) {
	capacity = max(1, height-17)
	start = viewportOffset(d.selected, len(groups), capacity, d.offset)
	return start, capacity
}

func (m *Model) dialogKey(k tea.KeyMsg) tea.Cmd {
	d := m.dialog
	key := k.String()
	if key == "ctrl+c" {
		m.clearDialog()
		return m.leaveEditor(func() tea.Cmd { m.Close(); return tea.Quit })
	}
	if key == "ctrl+l" {
		m.clearDialog()
		return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(true) })
	}
	if strings.HasPrefix(d.kind, "pin-") {
		return m.pinSetupKey(k)
	}
	if m.writing {
		if key == "esc" && d.kind == "generator" {
			m.cancelOperation()
			m.clearDialog()
		}
		return nil
	}
	if key == "esc" {
		if d.kind == "external" {
			return nil
		}
		m.clearDialog()
		return nil
	}
	if d.kind == "duplicates" {
		page := max(1, min(8, m.height-14))
		if key == "pgdown" {
			d.offset = min(max(0, len(d.duplicates.Renames)-page), d.offset+page)
			return nil
		}
		if key == "pgup" {
			d.offset = max(0, d.offset-page)
			return nil
		}
	}
	if d.kind == "group" {
		switch key {
		case "up", "down", "tab", "shift+tab":
			dialogActionFocus(key, 3, &d.selected)
			return nil
		case "enter":
			if d.selected == 2 {
				m.clearDialog()
				return nil
			}
			if strings.TrimSpace(d.input.value()) == "" {
				d.err = "Group name is required."
				return nil
			}
			return m.apply(vault.Change{Kind: vault.CreateGroup, GroupUUID: d.groupUUID, NewName: d.input.value()})
		}
		if d.selected != 0 {
			return nil
		}
		d.input.update(k)
		d.err = ""
		return nil
	}
	if d.kind == "generator" {
		maxFocus := 2
		if len(d.secret) > 0 {
			maxFocus = 2
			if d.applyToPassword {
				maxFocus++
			}
		}
		if key == "up" {
			d.selected = max(0, d.selected-1)
			return nil
		}
		if key == "down" {
			d.selected = min(maxFocus, d.selected+1)
			return nil
		}
		if key == "tab" || key == "shift+tab" {
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			d.selected = (d.selected + maxFocus + 1 + delta) % (maxFocus + 1)
			return nil
		}
		if len(d.secret) == 0 {
			if key == "enter" {
				if d.selected == 2 {
					m.clearDialog()
					return nil
				}
				return m.generate()
			}
			if d.selected != 0 {
				return nil
			}
			if k.Type == tea.KeyRunes {
				digits := make([]rune, 0, len(k.Runes))
				for _, r := range k.Runes {
					if r >= '0' && r <= '9' {
						digits = append(digits, r)
					}
				}
				if len(digits) > 0 {
					d.input.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: digits})
				}
			} else if k.Type != tea.KeySpace {
				d.input.update(k)
			}
			d.err = ""
			return nil
		}
		switch key {
		case "enter":
			if d.applyToPassword {
				switch d.selected {
				case 0:
					if m.editor != nil {
						m.editor.setPassword(d.secret)
						m.clearDialog()
						m.setNotice("password added to draft", false)
					}
					return nil
				case 1:
					return m.copyValue(append([]byte(nil), d.secret...), "Generated password")
				case 2:
					return m.generate()
				case 3:
					m.clearDialog()
					return nil
				}
			} else {
				switch d.selected {
				case 0:
					return m.copyValue(append([]byte(nil), d.secret...), "Generated password")
				case 1:
					return m.generate()
				case 2:
					m.clearDialog()
					return nil
				}
			}
			return nil
		case "g":
			return m.generate()
		case "c":
			return m.copyValue(append([]byte(nil), d.secret...), "Generated password")
		}
		return nil
	}
	if d.kind == "destination" {
		groups := d.filteredGroups()
		switch key {
		case "tab", "shift+tab", "up", "down":
			actionsFocused := d.destinationFocus != 0
			action := max(0, d.destinationFocus-1)
			dialogFocus(key, len(groups), 2, &d.selected, &action, &actionsFocused)
			d.destinationFocus = 0
			if actionsFocused {
				d.destinationFocus = action + 1
			}
		case "enter":
			if d.destinationFocus == 2 {
				m.clearDialog()
				return nil
			}
			if len(groups) == 0 {
				d.err = "No available destinations."
				return nil
			}
			group := groups[min(d.selected, len(groups)-1)]
			return m.apply(vault.Change{Kind: vault.MoveEntry, EntryUUID: d.entryUUID, GroupUUID: group.UUID})
		default:
			return nil
		}
		return nil
	}
	switch key {
	case "left", "up", "shift+tab":
		dialogLinearFocus("up", len(d.options), &d.selected, true)
	case "right", "down", "tab":
		dialogLinearFocus("down", len(d.options), &d.selected, true)
	case "enter":
		choice := d.selected
		switch d.kind {
		case "duplicates":
			if choice == 1 {
				return m.renameDuplicates()
			}
			m.clearDialog()
		case "delete":
			if choice == 1 {
				return m.apply(d.change)
			}
			m.clearDialog()
		case "external":
			id := d.sessionID
			if choice == 0 {
				m.clearDialog()
				return m.requestReload(id)
			}
			m.changeDismissed[id] = true
			m.clearDialog()
		case "discardReload":
			id := d.sessionID
			m.clearDialog()
			if choice == 1 {
				m.clearEditor()
				return m.reload(id)
			}
		case "unsaved":
			action := d.after
			m.clearDialog()
			switch choice {
			case 0:
				m.afterSave = action
				return m.saveEditor()
			case 1:
				m.clearEditor()
				if action != nil {
					return action()
				}
			}
		}
	}
	return nil
}

func (m *Model) dialogContent() (body []string, footer string, width int, targets []int) {
	c := m.colors()
	accent, secondary := c.accent, c.secondary
	d := m.dialog
	compact := m.compactAppearance()
	body = append([]string(nil), d.message...)
	fieldStart, fieldEnd, actionStart, actionBase, actionCount := -1, -1, -1, 0, 0
	if d.kind == "duplicates" {
		end := min(len(d.duplicates.Renames), d.offset+max(1, min(8, m.height-14)))
		body = []string{fmt.Sprintf("Rename %d entries (%d–%d shown)", len(d.duplicates.Renames), d.offset+1, end), "Encrypted backup first. pgup/pgdn to view names."}
		for _, rename := range d.duplicates.Renames[d.offset:end] {
			body = append(body, safe(rename.Path)+" -> "+safe(rename.Title))
		}
	}
	if d.kind == "external" {
		for i, line := range body {
			if i == 0 {
				body[i] = accent.Bold(true).Render(" " + line)
			} else {
				body[i] = " " + line
			}
		}
	}
	if d.kind == "delete" {
		wrapped := make([]string, 0, len(body))
		textWidth := max(1, min(m.width-6, 60)-5)
		for i, line := range body {
			for _, part := range strings.Split(ansi.Wrap(safe(line), textWidth, ""), "\n") {
				part = " " + part
				if i == 0 {
					part = c.accent.Bold(true).Render(part)
				}
				wrapped = append(wrapped, part)
			}
		}
		body = wrapped
	}
	footer = ""
	switch d.kind {
	case "pin-setup", "pin-confirm":
		dialogWidth := min(m.width-6, 52)
		actionWidth := max(1, dialogWidth-4)
		body = append(body, secondary.Render(" 4–12 digits · current run only"), "")
		fieldStart = len(body)
		body = append(body, m.dialogFields([]inputFieldView{{label: "Session PIN", value: &d.input, secret: true, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
		fieldEnd = len(body)
		body = m.dialogActions(body, []dialogAction{{"Save", "enter"}, {"Cancel", "esc"}}, d.selected-1, actionWidth+4, len(body)+3)
		actionStart, actionBase, actionCount = len(body)-2, 1, 2
		footer = ""
	case "group":
		actionWidth := dialogBoxInputWidth(m.width)
		fieldStart = len(body)
		body = append(body, m.dialogFields([]inputFieldView{{label: "Group name", value: &d.input, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
		fieldEnd = len(body)
		body = m.dialogActions(body, []dialogAction{{"Create", "enter"}, {"Cancel", "esc"}}, d.selected-1, actionWidth+4, len(body)+3)
		actionStart, actionBase, actionCount = len(body)-2, 1, 2
		footer = ""
	case "generator":
		if len(d.secret) == 0 {
			body = append(body, "")
			dialogWidth := min(m.width-6, 72)
			actionWidth := max(1, dialogWidth-4)
			fieldStart = len(body)
			body = append(body, m.dialogFields([]inputFieldView{{label: "Length (8–64)", value: &d.input, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
			fieldEnd = len(body)
			body = m.dialogActions(body, []dialogAction{{"Generate", "enter"}, {"Cancel", "esc"}}, d.selected-1, actionWidth+4, len(body)+3)
			actionStart, actionBase, actionCount = len(body)-2, 1, 2
			footer = ""
		} else {
			body = append(body, " Generated password", accent.Render(" "+safe(string(d.secret))))
			dialogWidth := min(m.width-6, 72)
			actionWidth := max(1, dialogWidth-4)
			actions := []dialogAction{{"Copy", "c"}, {"Regenerate", "g"}, {"Close", "esc"}}
			if d.applyToPassword {
				actions = append([]dialogAction{{"Apply", "enter"}}, actions...)
			}
			body = m.dialogActions(body, actions, d.selected, actionWidth+4, len(body)+len(actions)+1)
			actionStart, actionCount = len(body)-len(actions), len(actions)
			footer = ""
		}
	case "destination":
		body, _, _ = m.destinationDialogBody()
		footer = ""
	default:
		body = append(body, "")
		if d.kind == "duplicates" || d.kind == "delete" || d.kind == "unsaved" || d.kind == "external" || d.kind == "discardReload" {
			dialogWidth := min(m.width-6, 60)
			if d.kind == "unsaved" {
				dialogWidth = min(m.width-6, 42)
			} else if d.kind == "external" {
				dialogWidth = min(m.width-6, 72)
			}
			actionWidth := max(1, dialogWidth-4)
			var actions []dialogAction
			for i, option := range d.options {
				shortcut := ""
				if d.kind == "duplicates" && i == 1 {
					shortcut = "enter"
				}
				if d.kind != "external" && strings.EqualFold(option, "Cancel") {
					shortcut = "esc"
				}
				actions = append(actions, dialogAction{option, shortcut})
			}
			body = append(body, m.dialogActionRows(actions, d.selected, actionWidth)...)
			actionStart, actionCount = len(body)-len(actions), len(actions)
		} else {
			body = append(body, renderDialogChoices(d.options, d.selected, compact, m.settings.ASCII, c))
		}
	}
	if m.writing && d.kind != "generator" {
		body = append(body, accent.Render("Working…"))
	}
	width = min(m.width-6, 82)
	switch d.kind {
	case "unsaved":
		width = min(m.width-6, 42)
	case "pin-setup", "pin-confirm":
		width = min(m.width-6, 52)
	case "external", "generator":
		width = min(m.width-6, 72)
	case "delete", "duplicates", "destination":
		width = min(m.width-6, 60)
	}
	targets = make([]int, len(body))
	for i := range targets {
		targets[i] = -1
	}
	for i := fieldStart; i >= 0 && i < fieldEnd; i++ {
		targets[i] = 0
	}
	for i := 0; i < actionCount; i++ {
		targets[actionStart+i] = actionBase + i
	}
	return body, footer, width, targets
}

func (m *Model) dialogLines() []string {
	body, footer, width, _ := m.dialogContent()
	return m.boxWithWidthError(m.dialog.title, body, footer, m.dialog.err, width, false)
}

func (m *Model) dialogPointer(msg tea.MouseMsg) tea.Cmd {
	if m.dialog.kind == "destination" {
		return m.destinationPointer(msg)
	}
	body, footer, width, targets := m.dialogContent()
	opts := m.dialogOptions()
	opts.Body, opts.StatusBar, opts.Width, opts.Error = body, footer, width, safe(m.dialog.err)
	target := dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target
	if target < 0 {
		return nil
	}
	d := m.dialog
	d.selected = target
	if target == 0 && (d.kind == "group" || d.kind == "pin-setup" || d.kind == "pin-confirm" || d.kind == "generator" && len(d.secret) == 0) {
		return nil
	}
	return m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
}

func (m *Model) destinationDialogBody() (body []string, start, count int) {
	d := m.dialog
	body = append(body, d.message...)
	groups := d.filteredGroups()
	start, capacity := d.destinationViewport(m.height, groups)
	start, end := viewportRange(len(groups), capacity, start)
	body = append(body, m.colors().secondary.Render(" Move item to:"), "")
	rows := make([]listComponentRow, 0, capacity)
	for i := start; i < end; i++ {
		rows = append(rows, listComponentRow{kind: listComponentItem, item: i, text: groupLabel(groups[i].Path), selected: d.destinationFocus == 0 && i == d.selected, activeFill: true, pillSelected: true, classicInset: true})
	}
	if len(groups) == 0 {
		body = append(body, "No matching groups")
	} else {
		width := max(1, min(m.width-6, 60)-4)
		for _, row := range m.renderListComponent(rows, listComponentOptions{width: width, compact: m.compactAppearance(), ascii: m.settings.ASCII, focused: true, sidebar: true}) {
			body = append(body, row.text)
		}
	}
	width := max(1, min(m.width-6, 60)-4)
	body = m.dialogActions(body, []dialogAction{{"Save", "enter"}, {"Cancel", "esc"}}, d.destinationFocus-1, width+4, len(body)+3)
	return body, start, end - start
}
