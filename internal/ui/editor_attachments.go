package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	dialogrender "bypass/internal/ui/dialog"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

type editorAttachment struct {
	Name, OriginalName, Path string
	OriginalIndex            int   // -1 for a new attachment
	Size                     int64 // -1 while size is unavailable
}

func (e *entryEditor) attachmentsChanged() bool {
	if len(e.attachments) != len(e.originalAttachments) {
		return true
	}
	for i, item := range e.attachments {
		original := e.originalAttachments[i]
		if item.Name != original.Name || item.Path != original.Path || item.OriginalIndex != original.OriginalIndex || item.OriginalName != original.OriginalName {
			return true
		}
	}
	return false
}

// attachmentEdits returns ordered desired attachments and corresponding import
// paths. A nil changes slice means that the vault's attachments are unchanged.
func (e *entryEditor) attachmentEdits() ([]vault.AttachmentEdit, []string) {
	if !e.attachmentsChanged() {
		return nil, nil
	}
	edits := make([]vault.AttachmentEdit, len(e.attachments))
	paths := make([]string, len(e.attachments))
	for i, item := range e.attachments {
		edits[i].Name = item.Name
		if item.OriginalIndex >= 0 {
			index := item.OriginalIndex
			edits[i].OriginalIndex = &index
			edits[i].OriginalName = item.OriginalName
		} else {
			paths[i] = item.Path
		}
	}
	return edits, paths
}

// setAttachmentSizes receives sizes indexed by the source vault entry, without
// affecting whether the editor is dirty or any staged deletions.
func (e *entryEditor) setAttachmentSizes(sizes []int64) {
	set := func(items []editorAttachment) {
		for i := range items {
			if n := items[i].OriginalIndex; n >= 0 && n < len(sizes) {
				items[i].Size = sizes[n]
			}
		}
	}
	set(e.attachments)
	set(e.originalAttachments)
	set(e.attachmentSnapshot)
}

func (e *entryEditor) openEditorAttachments() {
	if e.attachmentsOpen {
		return
	}
	e.attachmentSnapshot = append([]editorAttachment(nil), e.attachments...)
	e.attachmentsOpen, e.attachmentEdit, e.attachmentIndex = true, false, 0
}

func (e *entryEditor) closeEditorAttachments(save bool) {
	if !save {
		e.attachments = append(e.attachments[:0], e.attachmentSnapshot...)
	}
	e.attachmentSnapshot = nil
	e.attachmentsOpen, e.attachmentEdit = false, false
	e.attachmentName.clear()
	e.attachmentErr = ""
}

func (e *entryEditor) startAttachmentEdit(index int) {
	e.attachmentIndex, e.attachmentFocus, e.attachmentEdit = index, 0, true
	e.attachmentName.clear()
	e.attachmentErr = ""
	if index < len(e.attachments) {
		item := e.attachments[index]
		e.attachmentName.set(item.Name)
	}
}

func (e *entryEditor) saveAttachmentEdit() {
	value := strings.TrimSpace(e.attachmentName.value())
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		e.attachmentErr = "File name is required."
		return
	}
	item := editorAttachment{OriginalIndex: -1, Size: -1}
	if e.attachmentIndex < len(e.attachments) {
		item = e.attachments[e.attachmentIndex]
	}
	if e.attachmentIndex >= len(e.attachments) {
		item.Path = expandPath(value)
		item.Name = filepath.Base(item.Path)
		if item.Name == "." || item.Name == string(filepath.Separator) {
			e.attachmentErr = "Enter a file name."
			return
		}
	} else {
		if filepath.Base(value) != value || value == "." || value == ".." {
			e.attachmentErr = "Enter a file name without a path."
			return
		}
		item.Name = value
	}
	for i, existing := range e.attachments {
		if i != e.attachmentIndex && existing.Name == item.Name && (e.attachmentIndex >= len(e.attachments) || e.attachments[e.attachmentIndex].Name != item.Name) {
			e.attachmentErr = "Attachment name already exists."
			return
		}
	}
	if e.attachmentIndex < len(e.attachments) {
		e.attachments[e.attachmentIndex] = item
	} else {
		e.attachments = append(e.attachments, item)
	}
	e.attachmentIndex = min(e.attachmentIndex, len(e.attachments)-1)
	e.attachmentEdit = false
	e.attachmentName.clear()
	e.attachmentErr = ""
}

func (e *entryEditor) attachmentKey(k tea.KeyMsg) {
	key := k.String()
	if e.attachmentEdit {
		switch key {
		case "esc":
			e.attachmentEdit = false
			e.attachmentName.clear()
			e.attachmentErr = ""
		case "ctrl+s":
			e.saveAttachmentEdit()
		case "tab", "shift+tab", "up", "down":
			focuses := []int{0, 1, 2, 3}
			if e.attachmentIndex >= len(e.attachments) {
				focuses = []int{0, 1, 3}
			}
			orderedFocus(key, focuses, &e.attachmentFocus, key == "tab" || key == "shift+tab")
		case "enter":
			switch e.attachmentFocus {
			case 0:
				e.attachmentFocus = 1
			case 1:
				e.saveAttachmentEdit()
			case 2:
				if e.attachmentIndex < len(e.attachments) {
					e.attachments = append(e.attachments[:e.attachmentIndex], e.attachments[e.attachmentIndex+1:]...)
					e.attachmentIndex = min(e.attachmentIndex, len(e.attachments))
				}
				e.attachmentEdit = false
				e.attachmentName.clear()
				e.attachmentErr = ""
			case 3:
				e.attachmentEdit = false
				e.attachmentName.clear()
				e.attachmentErr = ""
			}
		default:
			if e.attachmentFocus == 0 {
				e.attachmentName.update(k)
				e.attachmentErr = ""
			}
		}
		return
	}
	switch key {
	case "esc":
		e.closeEditorAttachments(false)
	case "up", "down", "tab", "shift+tab":
		dialogLinearFocus(key, len(e.attachments)+3, &e.attachmentIndex, key == "tab" || key == "shift+tab")
	case "enter":
		if e.attachmentIndex == len(e.attachments)+2 {
			e.closeEditorAttachments(false)
		} else if e.attachmentIndex == len(e.attachments)+1 {
			e.closeEditorAttachments(true)
		} else {
			e.startAttachmentEdit(e.attachmentIndex)
		}
	case "a":
		e.startAttachmentEdit(len(e.attachments))
	}
}

func (m *Model) editorAttachmentsDialog(width, height int) []string {
	opts, _ := m.editorAttachmentsDialogOptions(width, height)
	return dialogrender.Render(opts)
}

func (m *Model) editorAttachmentsDialogOptions(width, height int) (dialogrender.Options, []int) {
	e := m.editor
	base := *e
	base.attachmentsOpen = false
	backdrop := base.renderAppearance(width, height, m.compactAppearance(), m.settings.ASCII)
	outer := m.dialogOptions()
	outer.Mode, outer.Title, outer.Backdrop = dialogrender.Overlay, "Attachments", backdrop
	outer.Width, outer.ScreenWidth, outer.Height, outer.X, outer.Y = min(width-2, 58), width, height, -1, -1
	innerWidth := m.dialogInnerWidth(outer.Width)
	capacity := max(1, min(8, height-12))
	start, end := viewportRange(len(e.attachments), capacity, viewportOffset(e.attachmentIndex, len(e.attachments), capacity, 0))
	rows := make([]listComponentRow, 0, end-start)
	for i := start; i < end; i++ {
		selected := !e.attachmentEdit && i == e.attachmentIndex
		shortcut, label := "", safe(e.attachments[i].Name)
		if selected {
			shortcut = "enter"
			if m.compactAppearance() {
				label = " " + label
				shortcut += " "
			}
		}
		rows = append(rows, listComponentRow{kind: listComponentItem, item: i, text: label, right: shortcut, selected: selected, activeFill: true, pillSelected: true, classicInset: true})
	}
	var targets []int
	if len(rows) == 0 {
		outer.Body = []string{m.colors().secondary.Render(" No attachments")}
		targets = []int{-1}
	} else {
		outer.Body, targets = listContent(m.renderListComponent(rows, listComponentOptions{width: innerWidth, compact: m.compactAppearance(), ascii: m.settings.ASCII, focused: !e.attachmentEdit && e.attachmentIndex < len(e.attachments), sidebar: true}))
	}
	actionFocus := -1
	if !e.attachmentEdit && e.attachmentIndex >= len(e.attachments) {
		actionFocus = e.attachmentIndex - len(e.attachments)
	}
	for i, row := range m.dialogActionRows([]dialogAction{{"Add New…", ""}, {"Save", ""}, {"Cancel", "esc"}}, actionFocus, innerWidth) {
		outer.Actions = append(outer.Actions, dialogrender.Action{Text: row, Target: 100000 + i, Focused: i == actionFocus})
	}
	if !e.attachmentEdit {
		return outer, targets
	}
	inner := m.dialogOptions()
	inner.Mode, inner.Title, inner.Backdrop = dialogrender.Overlay, "Attachment", dialogrender.Render(outer)
	inner.Width, inner.ScreenWidth, inner.Height, inner.X, inner.Y = min(width-2, 58), width, height, -1, -1
	innerWidth = m.dialogInnerWidth(inner.Width)
	inner.Body = m.dialogFields([]inputFieldView{{label: "File name", value: &e.attachmentName, focused: e.attachmentFocus == 0, indent: 1}}, innerWidth, false)
	targets = make([]int, len(inner.Body))
	if e.attachmentIndex < len(e.attachments) {
		size := e.attachments[e.attachmentIndex].Size
		text := "File size: unavailable"
		if size >= 0 {
			text = fmt.Sprintf("File size: %d bytes", size)
		}
		inner.Body = append(inner.Body, "", m.colors().secondary.Render(" "+text))
		targets = append(targets, -1, -1)
	}
	inner.Error = safe(e.attachmentErr)
	actions := []dialogAction{{"Save", ""}, {"Delete", ""}, {"Cancel", "esc"}}
	targetsByAction := []int{1, 2, 3}
	if e.attachmentIndex >= len(e.attachments) {
		actions = []dialogAction{{"Save", ""}, {"Cancel", "esc"}}
		targetsByAction = []int{1, 3}
	}
	focusedAction := -1
	for i, target := range targetsByAction {
		if e.attachmentFocus == target {
			focusedAction = i
		}
	}
	for i, row := range m.dialogActionRows(actions, focusedAction, innerWidth) {
		inner.Actions = append(inner.Actions, dialogrender.Action{Text: row, Target: targetsByAction[i], Focused: e.attachmentFocus == targetsByAction[i]})
	}
	return inner, targets
}

// requestStagedAttachmentSize reads file metadata outside the UI update loop.
func (m *Model) requestStagedAttachmentSize() tea.Cmd {
	e := m.editor
	if e == nil || e.attachmentIndex < 0 || e.attachmentIndex >= len(e.attachments) {
		return nil
	}
	index := e.attachmentIndex
	item := e.attachments[index]
	if item.OriginalIndex >= 0 || item.Path == "" {
		return nil
	}
	return func() tea.Msg {
		size, err := attachmentFileSize(item.Path)
		if err != nil {
			size = -1
		}
		return stagedAttachmentSizeMsg{editor: e, index: index, path: item.Path, size: size}
	}
}

type stagedAttachmentSizeMsg struct {
	editor *entryEditor
	index  int
	path   string
	size   int64
}
