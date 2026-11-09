package ui

import (
	wipebuf "bypass/internal/secret"
	"fmt"
	"sort"
	"strings"

	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type actionDialog struct {
	kind, title                     string
	originalName                    string
	message                         []string
	options                         []string
	selected, offset                int
	destinationFocus                int
	input                           input
	err                             string
	applyToPassword                 bool
	createGroup                     bool
	restoringGroup                  bool
	sessionID, entryUUID, groupUUID string
	sourceGroupUUID                 string
	groups                          []vault.Group
	tags                            []string
	selectedTags                    []string
	tagIndex                        int
	tagActionsFocused               bool
	change                          vault.Change
	loss                            *vault.DataLossError
	credentials                     *vault.Credentials
	after                           func() tea.Cmd
	secret                          []byte
}

// ShowApprovalsUnavailable warns before input starts when this session cannot
// receive secret requests. Closing the other session does not attach this one.
func (m *Model) ShowApprovalsUnavailable(inUse bool) {
	message := "Secret requests cannot be received in this session. Restart Bypass to try again."
	if inUse {
		message = "Another Bypass session receives secret requests. To approve requests here, close that session and restart this one."
	}
	m.dialog = &actionDialog{kind: "startup-warning", title: "Approvals unavailable", message: []string{message}, options: []string{"Close"}}
}

func (m *Model) clearDialog() {
	if m.dialog != nil {
		m.dialog.input.clear()
		wipebuf.Wipe(m.dialog.secret)
		m.dialog.secret = nil
		wipebuf.Wipe(m.dialog.change.Password)
		wipebuf.Wipe(m.dialog.change.AttachmentData)
		m.dialog.change.Values = nil
		if m.dialog.credentials != nil {
			m.dialog.credentials.Clear()
			m.dialog.credentials = nil
		}
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
func (m *Model) confirmDelete() tea.Cmd {
	s := m.ws.ActiveSession()
	v := m.ws.ActiveView()
	if s == nil || s.DB == nil || v == nil {
		return nil
	}
	if v.Focus == 0 && m.currentNav().kind != "group" {
		m.setNotice("select an entry or group to delete", true)
		return nil
	}
	change := vault.Change{Kind: vault.DeleteEntry}
	name := ""
	permanent := !s.DB.RecycleBinEnabled
	if v.Focus == 0 && m.currentNav().kind == "group" {
		groupID := m.currentNav().group
		if groupID == s.DB.RootUUID || groupID == s.DB.RecycleBinUUID {
			m.setNotice("can't delete root or bin groups here", true)
			return nil
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
		e := m.selectedRow()
		if e == nil {
			return nil
		}
		if e.OpaqueGroup {
			change.Kind = vault.DeleteGroup
			change.GroupUUID = e.UUID
		} else {
			change.EntryUUID = e.UUID
		}
		name = visibleField(*e, "Title")
		permanent = permanent || e.Recycled
	}
	action := "Move to Recycle Bin"
	message := "Entry history and attachments move with the item."
	if permanent {
		action = "Delete Permanently"
		message = "This permanently deletes the item. Recovery requires a backup."
	}
	if s.DB.Unlocked() && !permanent {
		action = "Delete"
		message = "The vault moves the item to its recycle bin when enabled. Items already in the bin are deleted permanently."
	}
	if change.Kind == vault.DeleteGroup && permanent {
		action = "Delete Permanently"
		message = "Permanently deletes this group and all its contents. Recovery requires a backup."
	}
	if change.Kind == vault.DeleteGroup && s.DB.Unlocked() && s.DB.RecycleBinEnabled && !permanent {
		change.Kind = vault.PrepareDeleteGroup
		return m.apply(change)
	}
	m.hideReveal()
	m.dialog = &actionDialog{kind: "delete", title: action, options: []string{"Cancel", action}, selected: 0, sessionID: s.ID, change: change, message: []string{safe(name), message}}
	return nil
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

func (m *Model) chooseGroupDestination(group vault.Group) {
	s := m.ws.ActiveSession()
	if s == nil || s.DB == nil {
		return
	}
	excluded := map[string]bool{group.UUID: true}
	for changed := true; changed; {
		changed = false
		for _, candidate := range s.DB.Groups {
			if excluded[candidate.ParentUUID] && !excluded[candidate.UUID] {
				excluded[candidate.UUID] = true
				changed = true
			}
		}
	}
	if group.ParentUUID != "" {
		excluded[group.ParentUUID] = true
	}
	groups := destinationGroups(s.DB, s.Destinations)
	filtered := groups[:0]
	for _, candidate := range groups {
		if !excluded[candidate.UUID] {
			filtered = append(filtered, candidate)
		}
	}
	m.hideReveal()
	title := "Move Group · " + safe(group.Name)
	if group.Recycled {
		title = "Restore Group · " + safe(group.Name)
	}
	m.dialog = &actionDialog{kind: "destination", title: title, sessionID: s.ID, sourceGroupUUID: group.UUID, restoringGroup: group.Recycled, groups: filtered}
}
func (d *actionDialog) filteredGroups() []vault.Group {
	return d.groups
}

func (m *Model) destinationViewport(groups []vault.Group) (start, capacity int) {
	d := m.dialog
	opts := m.dialogOptions()
	opts.Width, opts.Error = min(m.width-6, 60), safe(d.err)
	opts.Actions = []dialogrender.Action{{}, {}}
	capacity = dialogrender.ContentCapacity(opts) - len(d.message) - 2
	if m.writing {
		capacity--
	}
	capacity = max(1, capacity)
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
	if d.kind == "tag-new" {
		return m.newTagDialogKey(k)
	}
	if d.kind == "tags" {
		return m.tagsDialogKey(k)
	}
	if key == "esc" {
		if d.kind == "external" {
			return nil
		}
		m.clearDialog()
		return nil
	}
	if d.kind == "run-permissions" {
		if key == "c" {
			return m.clearRunPermissionsDialog()
		}
		page := m.runPermissionsPageSize()
		switch key {
		case "pgdown":
			d.offset = min(max(0, len(d.message)-page), d.offset+page)
			return nil
		case "pgup":
			d.offset = max(0, d.offset-page)
			return nil
		}
	}
	if d.kind == "rename-tag" || d.kind == "rename-group" {
		switch key {
		case "up", "down", "tab", "shift+tab":
			dialogActionFocus(key, 3, &d.selected)
			return nil
		case "enter":
			if d.selected == 2 {
				m.clearDialog()
				return nil
			}
			name := strings.TrimSpace(d.input.value())
			if name == "" {
				if d.kind == "rename-tag" {
					d.err = "Tag name is required."
				} else {
					d.err = "Group name is required."
				}
				return nil
			}
			if name == d.originalName {
				m.clearDialog()
				return nil
			}
			if d.kind == "rename-tag" {
				return m.apply(vault.Change{Kind: vault.RenameTag, TagName: d.originalName, NewName: name})
			}
			if d.createGroup {
				return m.apply(vault.Change{Kind: vault.CreateGroup, GroupUUID: d.groupUUID, NewName: name})
			}
			return m.apply(vault.Change{Kind: vault.RenameGroup, GroupUUID: d.groupUUID, NewName: name})
		}
		if d.selected == 0 {
			d.input.update(k)
			d.err = ""
		}
		return nil
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
			if d.sourceGroupUUID != "" {
				return m.apply(vault.Change{Kind: vault.MoveGroup, SourceGroupUUID: d.sourceGroupUUID, GroupUUID: group.UUID})
			}
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
		case "startup-warning":
			m.clearDialog()
		case "run-permissions":
			if choice == 0 {
				return m.clearRunPermissionsDialog()
			}
			m.clearDialog()
		case "delete":
			if choice == 1 {
				return m.apply(d.change)
			}
			m.clearDialog()
		case "data-loss":
			if choice == 1 {
				s := m.ws.Sessions[d.sessionID]
				if s == nil || s.DB == nil || s.ReadOnly || s.Stale || d.loss == nil || s.DB.Path != d.loss.Path || s.DB.Revision != d.loss.Revision {
					m.clearDialog()
					m.setNotice("vault changed · reload before saving", true)
					return nil
				}
				if d.credentials != nil {
					creds, loss := d.credentials.Clone(), d.loss
					m.clearDialog()
					return m.rotateCredentialsWithLossConsent(d.sessionID, creds, loss)
				}
				change := d.change
				change.Password = append([]byte(nil), d.change.Password...)
				change.AttachmentData = append([]byte(nil), d.change.AttachmentData...)
				change.AllowDataLoss = true
				change.DataLossConsent = d.loss.Operation
				m.clearDialog()
				return m.apply(change)
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

func (m *Model) tagsDialogKey(k tea.KeyMsg) tea.Cmd {
	d := m.dialog
	key := k.String()
	if key == "esc" {
		m.clearDialog()
		return nil
	}
	if key == " " || key == "space" {
		if !d.tagActionsFocused && d.tagIndex >= 0 && d.tagIndex < len(d.tags) {
			tag := d.tags[d.tagIndex]
			for i, selected := range d.selectedTags {
				if strings.EqualFold(strings.TrimSpace(selected), strings.TrimSpace(tag)) {
					d.selectedTags = append(d.selectedTags[:i], d.selectedTags[i+1:]...)
					return nil
				}
			}
			d.selectedTags = append(d.selectedTags, tag)
		}
		return nil
	}
	if key == "up" || key == "down" || key == "tab" || key == "shift+tab" {
		dialogFocus(key, len(d.tags), 3, &d.tagIndex, &d.selected, &d.tagActionsFocused)
		return nil
	}
	if key != "enter" {
		return nil
	}
	if !d.tagActionsFocused || d.selected == 0 {
		if m.editor != nil {
			m.editor.tags = append([]string(nil), d.selectedTags...)
			m.editor.favorite = hasFavoriteTag(m.editor.tags)
		}
		m.clearDialog()
		return nil
	}
	switch d.selected {
	case 1:
		d.kind, d.title, d.selected = "tag-new", "New Tag", 0
		d.input.clear()
		d.err = ""
	case 2:
		m.clearDialog()
	}
	return nil
}

func (m *Model) newTagDialogKey(k tea.KeyMsg) tea.Cmd {
	d := m.dialog
	key := k.String()
	if key == "esc" {
		d.kind, d.title, d.selected = "tags", "Tags", 0
		d.input.clear()
		d.err = ""
		return nil
	}
	if key == "up" || key == "down" || key == "tab" || key == "shift+tab" {
		dialogActionFocus(key, 3, &d.selected)
		return nil
	}
	if key == "enter" {
		if d.selected == 2 {
			d.kind, d.title, d.selected = "tags", "Tags", 0
			d.input.clear()
			d.err = ""
			return nil
		}
		name := strings.TrimSpace(d.input.value())
		if name == "" {
			d.err = "Tag name is required."
			return nil
		}
		index := -1
		for i, tag := range d.tags {
			if strings.EqualFold(strings.TrimSpace(tag), name) {
				index = i
				break
			}
		}
		if index < 0 {
			d.tags = append(d.tags, name)
			sort.Slice(d.tags, func(i, j int) bool { return strings.ToLower(d.tags[i]) < strings.ToLower(d.tags[j]) })
			for i, tag := range d.tags {
				if strings.EqualFold(tag, name) {
					index = i
					break
				}
			}
		}
		found := false
		for _, tag := range d.selectedTags {
			if strings.EqualFold(strings.TrimSpace(tag), name) {
				found = true
				break
			}
		}
		if !found {
			d.selectedTags = append(d.selectedTags, d.tags[index])
		}
		d.kind, d.title, d.selected = "tags", "Tags", 0
		d.tagIndex = index
		d.tagActionsFocused = false
		d.input.clear()
		d.err = ""
		return nil
	}
	if d.selected == 0 {
		d.input.update(k)
		d.err = ""
	}
	return nil
}

func (m *Model) dialogContent() (opts dialogrender.Options, targets []int) {
	c := m.colors()
	accent, secondary := c.accent, c.secondary
	d := m.dialog
	compact := m.compactAppearance()
	body := append([]string(nil), d.message...)
	fieldStart, fieldEnd, actionBase, actionFocus := -1, -1, 0, d.selected
	var actions []dialogAction
	var contentTargets []int
	footer := ""
	if d.kind == "external" {
		for i, line := range body {
			if i == 0 {
				body[i] = accent.Bold(true).Render(" " + line)
			} else {
				body[i] = " " + line
			}
		}
	}
	if d.kind == "delete" || d.kind == "startup-warning" || d.kind == "data-loss" {
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
	switch d.kind {
	case "run-permissions":
		page := m.runPermissionsPageSize()
		start := min(d.offset, max(0, len(d.message)-page))
		end := min(len(d.message), start+page)
		body = make([]string, 0, end-start+1)
		for _, line := range d.message[start:end] {
			body = append(body, " "+safe(line))
		}
		if len(d.message) > page {
			body = append(body, secondary.Render(" pgup/pgdn to scroll"))
		}
		for _, option := range d.options {
			shortcut := ""
			if option == "Close" {
				shortcut = "esc"
			} else {
				shortcut = "c"
			}
			actions = append(actions, dialogAction{option, shortcut})
		}
	case "pin-setup", "pin-confirm":
		dialogWidth := min(m.width-6, 52)
		actionWidth := m.dialogInnerWidth(dialogWidth)
		body = append(body, secondary.Render(" 4–12 digits · current run only"), "")
		fieldStart = len(body)
		body = append(body, m.dialogFields([]inputFieldView{{label: "Session PIN", value: &d.input, secret: true, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
		fieldEnd = len(body)
		actions, actionBase, actionFocus = []dialogAction{{"Save", "enter"}, {"Cancel", "esc"}}, 1, d.selected-1
		footer = ""
	case "group", "tag-new":
		actionWidth := m.dialogInnerWidth(min(m.width-6, 82))
		label, submit := "Group name", "Create"
		if d.kind == "tag-new" {
			label, submit = "Tag name", "Add"
		}
		fieldStart = len(body)
		body = append(body, m.dialogFields([]inputFieldView{{label: label, value: &d.input, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
		fieldEnd = len(body)
		actions, actionBase, actionFocus = []dialogAction{{submit, "enter"}, {"Cancel", "esc"}}, 1, d.selected-1
		footer = ""
	case "tags":
		body = append(body, secondary.Render(" Choose tags"))
		capacity := max(1, min(8, m.height-12))
		start, end := viewportRange(len(d.tags), capacity, viewportOffset(d.tagIndex, len(d.tags), capacity, d.offset))
		rows := make([]listComponentRow, 0, max(1, end-start))
		if len(d.tags) == 0 {
			body = append(body, secondary.Render(" No tags available"))
		} else {
			for i := start; i < end; i++ {
				checked := false
				for _, tag := range d.selectedTags {
					if strings.EqualFold(strings.TrimSpace(tag), strings.TrimSpace(d.tags[i])) {
						checked = true
						break
					}
				}
				mark := "  "
				if checked {
					mark = "✓ "
					if m.settings.ASCII {
						mark = "x "
					}
				}
				rows = append(rows, listComponentRow{kind: listComponentItem, item: i, text: mark + safe(d.tags[i]), right: "space", selected: !d.tagActionsFocused && i == d.tagIndex, activeFill: true, pillSelected: true, classicInset: true})
			}
			listRows := m.renderListComponent(rows, listComponentOptions{width: m.dialogInnerWidth(min(m.width-6, 46)), compact: compact, ascii: m.settings.ASCII, focused: !d.tagActionsFocused, sidebar: true})
			listBody, listTargets := listContent(listRows)
			body = append(body, listBody...)
			contentTargets = append(contentTargets, -1)
			for _, listTarget := range listTargets {
				if listTarget >= 0 {
					contentTargets = append(contentTargets, 100000+listTarget)
				} else {
					contentTargets = append(contentTargets, listTarget)
				}
			}
		}
		actionBase = len(body)
		actions = []dialogAction{{"Save", "enter"}, {"Add new", ""}, {"Cancel", "esc"}}
		actionFocus = -1
		if d.tagActionsFocused {
			actionFocus = d.selected
		}
		footer = ""
	case "rename-tag", "rename-group":
		actionWidth := m.dialogInnerWidth(min(m.width-6, 48))
		fieldStart = len(body)
		label := "Tag name"
		if d.kind == "rename-group" {
			label = "Group name"
		}
		body = append(body, m.dialogFields([]inputFieldView{{label: label, value: &d.input, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
		fieldEnd = len(body)
		actions, actionBase, actionFocus = []dialogAction{{"Save", "enter"}, {"Cancel", "esc"}}, 1, d.selected-1
		footer = ""
	case "generator":
		if len(d.secret) == 0 {
			body = append(body, "")
			dialogWidth := min(m.width-6, 72)
			actionWidth := m.dialogInnerWidth(dialogWidth)
			fieldStart = len(body)
			body = append(body, m.dialogFields([]inputFieldView{{label: "Length (8–64)", value: &d.input, focused: d.selected == 0, indent: 1}}, actionWidth, false)...)
			fieldEnd = len(body)
			actions, actionBase, actionFocus = []dialogAction{{"Generate", "enter"}, {"Cancel", "esc"}}, 1, d.selected-1
			footer = ""
		} else {
			body = append(body, " Generated password", accent.Render(" "+safe(string(d.secret))))
			actions = []dialogAction{{"Copy", "c"}, {"Regenerate", "g"}, {"Close", "esc"}}
			if d.applyToPassword {
				actions = append([]dialogAction{{"Apply", "enter"}}, actions...)
			}

			footer = ""
		}
	case "destination":
		body, contentTargets = m.destinationDialogBody()
		actions, actionBase, actionFocus = []dialogAction{{"Save", "enter"}, {"Cancel", "esc"}}, len(d.filteredGroups()), d.destinationFocus-1
		footer = ""
	default:
		body = append(body, "")
		if d.kind == "delete" || d.kind == "data-loss" || d.kind == "startup-warning" || d.kind == "unsaved" || d.kind == "external" || d.kind == "discardReload" {
			for _, option := range d.options {
				shortcut := ""
				if d.kind != "external" && strings.EqualFold(option, "Cancel") {
					shortcut = "esc"
				}
				if d.kind == "startup-warning" {
					shortcut = "enter"
				}
				actions = append(actions, dialogAction{option, shortcut})
			}
		} else {
			body = append(body, renderDialogChoices(d.options, d.selected, compact, m.settings.ASCII, c))
		}
	}
	if m.writing && d.kind != "generator" {
		body = append(body, accent.Render("Working…"))
	}
	width := min(m.width-6, 82)
	switch d.kind {
	case "unsaved":
		width = min(m.width-6, 42)
	case "pin-setup", "pin-confirm":
		width = min(m.width-6, 52)
	case "rename-tag", "rename-group", "tag-new":
		width = min(m.width-6, 48)
	case "external", "generator":
		width = min(m.width-6, 72)
	case "tags":
		width = min(m.width-6, 46)
	case "delete", "data-loss", "destination", "startup-warning":
		width = min(m.width-6, 60)
	}
	targets = make([]int, len(body))
	for i := range targets {
		targets[i] = -1
	}
	for i := fieldStart; i >= 0 && i < fieldEnd; i++ {
		targets[i] = 0
	}
	if contentTargets != nil {
		copy(targets, contentTargets)
	}
	opts = m.dialogOptions()
	opts.Title, opts.Body, opts.StatusBar, opts.Width, opts.Error = d.title, body, footer, width, safe(d.err)
	for i, row := range m.dialogActionRows(actions, actionFocus, m.dialogInnerWidth(width)) {
		opts.Actions = append(opts.Actions, dialogrender.Action{Text: row, Target: actionBase + i})
	}
	return opts, targets
}

func (m *Model) clearRunPermissionsDialog() tea.Cmd {
	if m.runs == nil {
		return nil
	}
	if m.runs.busy || m.writing || m.busy {
		m.dialog.err = "Wait for the current operation to finish."
		return nil
	}
	m.beginRunPermissionReset()
	m.openRunPermissions()
	return nil
}

func (m *Model) runPermissionsPageSize() int {
	opts := m.dialogOptions()
	opts.Width = min(m.width-6, 82)
	opts.Actions = make([]dialogrender.Action, len(m.dialog.options))
	return max(1, dialogrender.ContentCapacity(opts)-1)
}

func (m *Model) dialogLines() []string {
	opts, _ := m.dialogContent()
	opts.Backdrop = m.modalBackdrop()
	if m.dialog.kind == "run-permissions" && m.runs != nil && m.runs.visible {
		opts.Backdrop = m.dimBackdrop(m.runDialogView())
	}
	return dialogrender.Render(opts)
}

func (m *Model) dialogPointer(msg tea.MouseMsg) tea.Cmd {
	if m.dialog.kind == "destination" {
		return m.destinationPointer(msg)
	}
	opts, targets := m.dialogContent()
	target := dialogrender.HitTest(opts, msg.X, msg.Y, targets).Target
	if target < 0 {
		return nil
	}
	if m.dialog.kind == "tags" {
		if target >= 100000 {
			m.dialog.tagIndex = target - 100000
			m.dialog.tagActionsFocused = false
			return m.tagsDialogKey(tea.KeyMsg{Type: tea.KeySpace})
		}
		if target >= len(opts.Body) {
			m.dialog.selected = target - len(opts.Body)
			m.dialog.tagActionsFocused = true
			return m.tagsDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
		return nil
	}
	d := m.dialog
	d.selected = target
	if target == 0 && (d.kind == "group" || d.kind == "tag-new" || d.kind == "rename-tag" || d.kind == "rename-group" || d.kind == "pin-setup" || d.kind == "pin-confirm" || d.kind == "generator" && len(d.secret) == 0) {
		return nil
	}
	return m.dialogKey(tea.KeyMsg{Type: tea.KeyEnter})
}

func (m *Model) destinationDialogBody() ([]string, []int) {
	d := m.dialog
	rows := make([]listComponentOutput, 0, len(d.message)+2)
	for _, line := range d.message {
		rows = append(rows, listComponentOutput{text: line, item: -1})
	}
	targetLabel := " Move item to:"
	if d.sourceGroupUUID != "" {
		targetLabel = " Move group to:"
		if d.restoringGroup {
			targetLabel = " Restore group to:"
		}
	}
	rows = append(rows, listComponentOutput{text: m.colors().secondary.Render(targetLabel), item: -1}, listComponentOutput{item: -1})
	groups := d.filteredGroups()
	start, capacity := m.destinationViewport(groups)
	start, end := viewportRange(len(groups), capacity, start)
	items := make([]listComponentRow, 0, capacity)
	for i := start; i < end; i++ {
		items = append(items, listComponentRow{kind: listComponentItem, item: i, text: groupLabel(groups[i].Path), selected: d.destinationFocus == 0 && i == d.selected, activeFill: true, pillSelected: true, classicInset: true})
	}
	if len(groups) == 0 {
		rows = append(rows, listComponentOutput{text: "No matching groups", item: -1})
	} else {
		width := m.dialogInnerWidth(min(m.width-6, 60))
		rows = append(rows, m.renderListComponent(items, listComponentOptions{width: width, compact: m.compactAppearance(), ascii: m.settings.ASCII, focused: true, sidebar: true})...)
	}
	return listContent(rows)
}
