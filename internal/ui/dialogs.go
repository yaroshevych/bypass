package ui

import (
	"fmt"
	"strings"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type actionDialog struct {
	kind, title                     string
	message                         []string
	options                         []string
	selected, offset                int
	input                           input
	err                             string
	sessionID, entryUUID, groupUUID string
	groups                          []vault.Group
	change                          vault.Change
	after                           func() tea.Cmd
	secret                          []byte
}

func (m *Model) clearDialog() {
	if m.dialog != nil {
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
		m.setNotice("Focus an entry or a real group before deleting.", true)
		return
	}
	change := vault.Change{Kind: vault.DeleteEntry}
	name := ""
	permanent := !s.DB.RecycleBinEnabled
	if v.Focus == 0 && m.currentNav().kind == "group" {
		groupID := m.currentNav().group
		if groupID == s.DB.RootUUID || groupID == s.DB.RecycleBinUUID {
			m.setNotice("Root and recycle-bin groups cannot be deleted here.", true)
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
	m.hideReveal()
	m.dialog = &actionDialog{kind: "delete", title: action, options: []string{"Cancel", action}, selected: 0, sessionID: s.ID, change: change, message: []string{safe(name), message}}
}
func (m *Model) chooseDestination(e *vault.Entry) {
	s := m.ws.ActiveSession()
	title := "Move Entry"
	if e.Recycled {
		title = "Restore Entry"
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "destination", title: title, sessionID: s.ID, entryUUID: e.UUID, message: []string{visibleField(*e, "Title")}}
	m.dialog.groups = destinationGroups(s.DB, s.Destinations)
}
func (d *actionDialog) filteredGroups() []vault.Group {
	var groups []vault.Group
	query := strings.ToLower(d.input.value())
	for _, g := range d.groups {
		if strings.Contains(strings.ToLower(g.Path), query) {
			groups = append(groups, g)
		}
	}
	return groups
}

func (d *actionDialog) destinationViewport(height int, groups []vault.Group) (start, capacity int) {
	capacity = max(1, height-15)
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
		return m.leaveEditor(func() tea.Cmd { return m.lockCurrent(false) })
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
			m.changeDismissed[d.sessionID] = true
		}
		m.clearDialog()
		return nil
	}
	if d.kind == "group" {
		if key == "enter" {
			if strings.TrimSpace(d.input.value()) == "" {
				d.err = "Group name is required."
				return nil
			}
			return m.apply(vault.Change{Kind: vault.CreateGroup, GroupUUID: d.groupUUID, NewName: d.input.value()})
		}
		d.input.update(k)
		d.err = ""
		return nil
	}
	if d.kind == "generator" {
		if len(d.secret) == 0 {
			if key == "enter" {
				return m.generate()
			}
			d.input.update(k)
			return nil
		}
		switch key {
		case "enter":
			if m.editor != nil {
				m.editor.setPassword(d.secret)
				m.clearDialog()
				m.setNotice("Generated password added to draft.", false)
				return nil
			}
			return m.copyValue(append([]byte(nil), d.secret...), "Generated password")
		case "g":
			clear(d.secret)
			d.secret = nil
			return m.generate()
		case "c":
			return m.copyValue(append([]byte(nil), d.secret...), "Generated password")
		}
		return nil
	}
	if d.kind == "destination" {
		groups := d.filteredGroups()
		switch key {
		case "up":
			d.selected = max(0, d.selected-1)
		case "down":
			d.selected = min(max(0, len(groups)-1), d.selected+1)
		case "enter":
			if len(groups) == 0 {
				d.err = "No matching destination."
				return nil
			}
			group := groups[min(d.selected, len(groups)-1)]
			return m.apply(vault.Change{Kind: vault.MoveEntry, EntryUUID: d.entryUUID, GroupUUID: group.UUID})
		default:
			d.input.update(k)
			d.selected = 0
		}
		return nil
	}
	switch key {
	case "left", "up", "shift+tab":
		d.selected = (d.selected + len(d.options) - 1) % len(d.options)
	case "right", "down", "tab":
		d.selected = (d.selected + 1) % len(d.options)
	case "enter":
		choice := d.selected
		switch d.kind {
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

func (m *Model) dialogLines() []string {
	c := m.colors()
	accent, muted, active, errorStyle := c.accent, c.muted, c.active, c.errorStyle
	d := m.dialog
	body := append([]string(nil), d.message...)
	footer := "←→ choose · Enter confirm · Esc cancel"
	switch d.kind {
	case "pin-setup", "pin-confirm":
		body = append(body, muted.Render("4–12 digits · current run only"), accent.Render(d.input.display(true)+"▏"))
		footer = "Enter continue · Esc cancel"
	case "group":
		body = append(body, muted.Render("Group name"), accent.Render(d.input.display(false)+"▏"))
		footer = "Enter create · Esc cancel"
	case "generator":
		if len(d.secret) == 0 {
			body = append(body, "Length (8–128)", accent.Render(d.input.display(false)+"▏"))
			footer = "Enter generate · Esc cancel"
		} else {
			body = append(body, "Generated password", accent.Render("••••••••"))
			footer = "Enter copy · c copy · g regenerate · Esc close"
			if m.editor != nil {
				footer = "Enter use in draft · c copy · g regenerate · Esc cancel"
			}
		}
	case "destination":
		body = append(body, muted.Render("Destination · type to filter"), accent.Render(d.input.display(false)+"▏"), "")
		groups := d.filteredGroups()
		start, capacity := d.destinationViewport(m.height, groups)
		for i := start; i < len(groups) && i < start+capacity; i++ {
			label := "  " + groupLabel(groups[i].Path)
			if i == d.selected {
				label = active.Render("› " + groupLabel(groups[i].Path))
			}
			body = append(body, label)
		}
		if len(groups) == 0 {
			body = append(body, "No matching groups")
		}
		footer = "↑↓ choose · Enter move · Esc cancel"
	default:
		body = append(body, "")
		var choices []string
		for i, option := range d.options {
			label := " " + option + " "
			if i == d.selected {
				label = active.Render(label)
			} else {
				label = muted.Render(label)
			}
			choices = append(choices, label)
		}
		body = append(body, strings.Join(choices, "  "))
	}
	if d.err != "" {
		body = append(body, errorStyle.Render(d.err))
	}
	if m.writing {
		body = append(body, accent.Render("Working…"))
	}
	if m.notice != "" && d.kind == "generator" {
		body = append(body, muted.Render(m.notice))
	}
	return m.box(d.title, body, footer)
}
