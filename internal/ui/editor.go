package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"keepassui/internal/vault"
)

const (
	editTitle = iota
	editUserName
	editPassword
	editURL
	editNotes
	editGenerate
	editSave
	editCancel
	editFocusCount
)

type editorAction uint8

const (
	editorNone editorAction = iota
	editorSave
	editorCancel
	editorGenerate
)

var editorFields = [...]string{"Title", "UserName", "Password", "URL", "Notes"}

// entryEditor edits the five user-facing fields without changing the rest of
// an entry. The password is kept in its own rune buffer and never enters the
// values map.
type entryEditor struct {
	inputs           [5]input
	original         map[string]string
	originalPassword []rune
	readonlyFields   []vault.Field
	protected        [5]bool
	protectedEdited  [5]bool
	validation       string
	validationFocus  int
	focus            int
	isNew            bool
	EntryUUID        string
	GroupUUID        string
	err              string
}

func newEntryEditor(e *vault.Entry, groupUUID string) *entryEditor {
	editor := &entryEditor{
		original:        make(map[string]string, 4),
		GroupUUID:       groupUUID,
		isNew:           e == nil,
		validationFocus: -1,
	}
	for i, name := range editorFields {
		value := ""
		if name == "Password" {
			editor.protected[i] = true
		}
		if e != nil {
			value = e.Get(name)
			if name != "Password" {
				editor.protected[i] = e.IsProtected(name)
			}
		}
		editor.inputs[i].set(value)
		if name == "Password" {
			editor.originalPassword = []rune(value)
		} else {
			editor.original[name] = value
		}
	}
	if e != nil {
		editor.EntryUUID = e.UUID
		if e.GroupUUID != "" {
			editor.GroupUUID = e.GroupUUID
		}
		for _, field := range e.Fields {
			if !isStandardEditorField(field.Name) {
				editor.readonlyFields = append(editor.readonlyFields, field)
			}
		}
	}
	return editor
}

func (e *entryEditor) dirty() bool {
	if e == nil {
		return false
	}
	for _, name := range []string{"Title", "UserName", "URL", "Notes"} {
		if e.inputs[fieldIndex(name)].value() != e.original[name] {
			return true
		}
	}
	return !runesEqual(e.inputs[editPassword].text, e.originalPassword)
}

// clear drops the editor's buffers. It cannot overwrite Go strings that were
// already copied from the vault model.
func (e *entryEditor) clear() {
	if e == nil {
		return
	}
	for i := range e.inputs {
		e.inputs[i].clear()
		e.protected[i] = false
		e.protectedEdited[i] = false
	}
	for i := range e.readonlyFields {
		e.readonlyFields[i].Value = ""
	}
	e.readonlyFields = nil
	clear(e.originalPassword)
	e.originalPassword = nil
	for key := range e.original {
		e.original[key] = ""
		delete(e.original, key)
	}
	e.original = nil
	e.EntryUUID = ""
	e.GroupUUID = ""
	e.validation = ""
	e.err = ""
}

// values returns the non-password standard fields. The password is
// deliberately available only through password().
func (e *entryEditor) values() map[string]string {
	values := make(map[string]string, 4)
	if e == nil {
		return values
	}
	for _, name := range []string{"Title", "UserName", "URL", "Notes"} {
		values[name] = e.inputs[fieldIndex(name)].value()
	}
	return values
}

func (e *entryEditor) password() []byte {
	if e == nil {
		return nil
	}
	return []byte(string(e.inputs[editPassword].text))
}

func (e *entryEditor) passwordChanged() bool {
	return e != nil && !runesEqual(e.inputs[editPassword].text, e.originalPassword)
}

func (e *entryEditor) setPassword(generated []byte) {
	if e == nil {
		return
	}
	e.inputs[editPassword].clear()
	for len(generated) > 0 {
		r, size := utf8.DecodeRune(generated)
		e.inputs[editPassword].text = append(e.inputs[editPassword].text, r)
		generated = generated[size:]
	}
	e.inputs[editPassword].cursor = len(e.inputs[editPassword].text)
	e.validation = ""
	e.err = ""
}

func (e *entryEditor) validate() error {
	if e == nil {
		return fmt.Errorf("editor unavailable")
	}
	e.validation, e.err = "", ""
	e.validationFocus = -1
	values := e.values()
	for _, name := range []string{"Title", "UserName", "URL", "Notes"} {
		if e.original[name] != "" && values[name] == "" {
			e.validationFocus = fieldIndex(name)
			e.focus = e.validationFocus
			e.validation = "Clearing existing fields is not supported."
			e.err = e.validation
			return fmt.Errorf("%s", e.validation)
		}
	}
	if strings.TrimSpace(values["Title"]) == "" {
		e.validationFocus = editTitle
		e.focus = e.validationFocus
		e.validation = "Title is required."
		e.err = e.validation
		return fmt.Errorf("%s", e.validation)
	}
	passwordToValidate := e.inputs[editPassword].text
	if !e.isNew && !e.passwordChanged() {
		passwordToValidate = nil
	}
	for _, r := range passwordToValidate {
		if r == '\n' || r == '\r' || r == 0 {
			e.validationFocus = editPassword
			e.focus = e.validationFocus
			e.validation = "Password contains an unsupported character."
			e.err = e.validation
			return fmt.Errorf("%s", e.validation)
		}
	}
	if values["Notes"] != e.original["Notes"] && strings.Contains(values["Notes"], `\n`) {
		e.validationFocus = editNotes
		e.focus = e.validationFocus
		e.validation = "Use Enter for a new line in Notes."
		e.err = e.validation
		return fmt.Errorf("%s", e.validation)
	}
	return nil
}

func (e *entryEditor) key(k tea.KeyMsg) editorAction {
	if e == nil {
		return editorNone
	}
	key := k.String()
	switch key {
	case "esc":
		return editorCancel
	case "ctrl+s":
		return editorSave
	case "tab":
		e.focus = (e.focus + 1) % editFocusCount
		return editorNone
	case "shift+tab":
		e.focus = (e.focus + editFocusCount - 1) % editFocusCount
		return editorNone
	}
	if e.focus >= editGenerate {
		if key == "enter" {
			switch e.focus {
			case editGenerate:
				return editorGenerate
			case editSave:
				return editorSave
			case editCancel:
				return editorCancel
			}
		}
		return editorNone
	}
	if e.focus == editPassword && k.Type == tea.KeyRunes && invalidPasswordRunes(k.Runes) {
		e.validationFocus = editPassword
		e.validation = "Password contains an unsupported character."
		e.err = e.validation
		return editorNone
	}
	if key == "enter" {
		if e.focus == editNotes {
			e.insertNotesNewline()
		} else {
			e.focus = (e.focus + 1) % editFocusCount
		}
		e.clearValidation()
		return editorNone
	}
	if e.focus == editNotes && k.Type == tea.KeyEnter {
		e.insertNotesNewline()
		e.clearValidation()
		return editorNone
	}
	if e.protected[e.focus] && e.focus != editPassword && e.isMutatingKey(k) {
		if !e.protectedEdited[e.focus] {
			e.inputs[e.focus].clear()
			e.protectedEdited[e.focus] = true
		}
	}
	e.inputs[e.focus].update(k)
	e.clearValidation()
	return editorNone
}

func (e *entryEditor) pointer(row, width, height int) editorAction {
	if e == nil || row < 0 || width <= 0 || height <= 0 {
		return editorNone
	}
	lines := e.layoutLines(width, height)
	if row >= len(lines) {
		return editorNone
	}
	focus := lines[row].focus
	if focus < 0 || focus >= editFocusCount {
		return editorNone
	}
	e.focus = focus
	e.clearValidation()
	switch focus {
	case editGenerate:
		return editorGenerate
	case editSave:
		return editorSave
	case editCancel:
		return editorCancel
	default:
		return editorNone
	}
}

func (e *entryEditor) render(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if e == nil {
		return padLines([]string{muted.Render("Editor unavailable")}, height)
	}
	lines := e.layoutLines(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = fit(line.text, width)
	}
	return padLines(out, height)
}

type editorLine struct {
	text  string
	focus int
}

func (e *entryEditor) layoutLines(width, height int) []editorLine {
	if e.isNew {
		title := "New Entry"
		if e.dirty() {
			title += " *"
		}
		return e.renderLines(title, width, height)
	}
	title := "Edit Entry"
	if e.dirty() {
		title += " *"
	}
	return e.renderLines(title, width, height)
}

func (e *entryEditor) renderLines(title string, width, height int) []editorLine {
	lines := []editorLine{{text: accent.Render(title), focus: -1}, {text: rule(width), focus: -1}}
	for i, name := range editorFields {
		label := editorLabel(name)
		if e.fieldDirty(i) {
			label += " *"
		}
		if i == e.focus {
			label = accent.Render(label)
		} else {
			label = muted.Render(label)
		}
		lines = append(lines, editorLine{text: label, focus: i})
		valueLines, cursorLine := e.renderValue(i, max(1, width-2))
		for j, value := range valueLines {
			if i == e.focus && j == cursorLine {
				value = accent.Render("› " + value)
			} else {
				value = "  " + value
			}
			lines = append(lines, editorLine{text: value, focus: i})
		}
		if e.validation != "" && e.validationFocus == i {
			lines = append(lines, editorLine{text: errorStyle.Render(e.validation), focus: i})
		}
	}
	for _, field := range e.readonlyFields {
		label := safe(field.Name) + " (read only)"
		lines = append(lines, editorLine{text: muted.Render(label), focus: -1})
		value := field.Value
		if field.Protected {
			value = strings.Repeat("•", len([]rune(value)))
		}
		if value == "" {
			value = "—"
		}
		for _, paragraph := range strings.Split(value, "\n") {
			wrapped := ansi.Wrap(safe(paragraph), max(1, width-2), "")
			for _, text := range strings.Split(wrapped, "\n") {
				lines = append(lines, editorLine{text: "  " + text, focus: -1})
			}
		}
	}
	for _, action := range []struct {
		label string
		index int
	}{{"Generate password", editGenerate}, {"Save", editSave}, {"Cancel", editCancel}} {
		text := muted.Render("  " + action.label)
		if action.index == e.focus {
			text = active.Render("› " + action.label)
		}
		lines = append(lines, editorLine{text: text, focus: action.index})
	}
	if e.err != "" && e.validation == "" {
		lines = append(lines, editorLine{text: errorStyle.Render(e.err), focus: -1})
	}
	lines = append(lines, editorLine{text: rule(width), focus: -1}, editorLine{text: muted.Render(editorFooter(width)), focus: -1})

	focusedLine := 0
	for i, line := range lines {
		if line.focus == e.focus && strings.Contains(line.text, "▏") {
			focusedLine = i
			break
		}
		if line.focus == e.focus {
			focusedLine = i
		}
	}
	if len(lines) > height {
		return renderEditorViewport(lines, focusedLine, e.focus, height)
	}
	for len(lines) < height {
		lines = append(lines, editorLine{focus: -1})
	}
	return lines
}

func renderEditorViewport(lines []editorLine, focusedLine, focus, height int) []editorLine {
	if height <= 0 {
		return nil
	}
	pinned := map[int]bool{0: true, len(lines) - 1: true}
	focusLabel, cursorLine := -1, -1
	for i, line := range lines {
		if line.focus != focus {
			continue
		}
		if strings.Contains(line.text, "▏") {
			cursorLine = i
		} else if focusLabel < 0 {
			focusLabel = i
		}
	}
	if focusLabel >= 0 {
		pinned[focusLabel] = true
	}
	if cursorLine >= 0 {
		pinned[cursorLine] = true
	}

	indices := make([]int, 0, len(lines))
	for i := range lines {
		if !pinned[i] {
			indices = append(indices, i)
		}
	}
	anchor := focusedLine
	if cursorLine >= 0 {
		anchor = cursorLine
	} else if focusLabel >= 0 {
		anchor = focusLabel
	}
	sort.SliceStable(indices, func(i, j int) bool {
		di := absInt(indices[i] - anchor)
		dj := absInt(indices[j] - anchor)
		if di == dj {
			return indices[i] < indices[j]
		}
		return di < dj
	})
	capacity := max(0, height-len(pinned))
	if capacity < len(indices) {
		indices = indices[:capacity]
	}
	for _, i := range indices {
		pinned[i] = true
	}
	visible := make([]editorLine, 0, min(height, len(pinned)))
	for i, line := range lines {
		if pinned[i] {
			visible = append(visible, line)
		}
	}
	if len(visible) > height {
		visible = visible[:height]
	}
	for len(visible) < height {
		visible = append(visible, editorLine{focus: -1})
	}
	return visible
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (e *entryEditor) renderValue(field, width int) ([]string, int) {
	value := &e.inputs[field]
	if e.protected[field] {
		text := strings.Repeat("•", len(value.text))
		if field == e.focus {
			text += "▏"
		}
		if text == "" {
			text = "—"
		}
		return []string{safe(text)}, 0
	}
	text := string(value.text)
	if field == e.focus {
		text = string(value.text[:value.cursor]) + "▏" + string(value.text[value.cursor:])
	}
	if field != editNotes {
		if text == "" {
			if field == e.focus {
				text = "▏"
			} else {
				text = "—"
			}
		}
		wrapped := ansi.Wrap(safe(text), max(1, width), "")
		lines := strings.Split(wrapped, "\n")
		return lines, findCursorLine(lines)
	}
	if text == "" {
		return []string{"—"}, 0
	}
	var lines []string
	cursorLine := 0
	for _, paragraph := range strings.Split(text, "\n") {
		wrapped := ansi.Wrap(safe(paragraph), max(1, width), "")
		for _, line := range strings.Split(wrapped, "\n") {
			if strings.Contains(line, "▏") {
				cursorLine = len(lines)
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		text := "—"
		if field == e.focus {
			text = "▏"
		}
		lines = []string{text}
	}
	return lines, cursorLine
}

func (e *entryEditor) insertNotesNewline() {
	if len(e.inputs[editNotes].text) >= 4096 {
		return
	}
	value := &e.inputs[editNotes]
	tail := append([]rune(nil), value.text[value.cursor:]...)
	value.text = append(value.text[:value.cursor], '\n')
	value.text = append(value.text, tail...)
	value.cursor++
	clear(tail)
}

func (e *entryEditor) clearValidation() {
	e.validation = ""
	e.validationFocus = -1
	e.err = ""
}

func (e *entryEditor) isMutatingKey(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes || k.Type == tea.KeySpace || k.Type == tea.KeyBackspace || k.Type == tea.KeyDelete || k.Type == tea.KeyCtrlU || k.Type == tea.KeyCtrlW
}

func (e *entryEditor) fieldDirty(index int) bool {
	if index == editPassword {
		return e.passwordChanged()
	}
	name := editorFields[index]
	return e.inputs[index].value() != e.original[name]
}

func invalidPasswordRunes(runes []rune) bool {
	for _, r := range runes {
		if r == '\n' || r == '\r' || r == 0 {
			return true
		}
	}
	return false
}

func editorLabel(name string) string {
	if name == "UserName" {
		return "Username"
	}
	return name
}

func dirtyMarker(dirty bool) string {
	if dirty {
		return " *"
	}
	return ""
}

func editorFooter(width int) string {
	switch {
	case width >= 40:
		return "Tab focus · Ctrl+S save · Esc cancel"
	case width >= 24:
		return "Ctrl+S save · Esc cancel"
	case width >= 12:
		return "Ctrl+S save · Esc"
	default:
		return "Ctrl+S/Esc"
	}
}

func fieldIndex(name string) int {
	for i, field := range editorFields {
		if field == name {
			return i
		}
	}
	return -1
}

func isStandardEditorField(name string) bool {
	for _, field := range editorFields {
		if field == name {
			return true
		}
	}
	return false
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findCursorLine(lines []string) int {
	for i, line := range lines {
		if strings.Contains(line, "▏") {
			return i
		}
	}
	return 0
}
