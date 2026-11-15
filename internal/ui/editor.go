package ui

import (
	wipebuf "bypass/internal/secret"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	editTitle = iota
	editUserName
	editPassword
	editURL
	editEmail
	editNotes
	editGenerate
	editTags
	editCustomFields
	editFavorite
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
	editorTags
	editorFavorite
	editorCustomFields
)

var editorFields = [...]string{"Title", "UserName", "Password", "URL", "Email", "Notes"}

// entryEditor edits the user-facing fields without changing the rest of
// an entry. The password is kept in its own rune buffer and never enters the
// values map.
type entryEditor struct {
	styles               *styleSet
	compact              bool
	ascii                bool
	inputs               [6]input
	original             map[string]string
	originalPassword     []rune
	readonlyFields       []vault.Field
	customFields         []vault.Field
	originalCustomFields []vault.Field
	customFieldsSnapshot []vault.Field
	customOpen           bool
	customEdit           bool
	customIndex          int
	customFocus          int
	customName           input
	customValue          input
	customConcealed      bool
	customReveal         bool
	customErr            string
	protected            [6]bool
	protectedEdited      [6]bool
	validation           string
	validationFocus      int
	focus                int
	hover                int
	isNew                bool
	EntryUUID            string
	GroupUUID            string
	originalTags         []string
	tags                 []string
	favorite             bool
	header               itemHeader
	headerNow            time.Time
	err                  string
}

func newEntryEditor(e *vault.Entry, groupUUID string) *entryEditor {
	editor := &entryEditor{
		original:        make(map[string]string, 4),
		GroupUUID:       groupUUID,
		header:          newItemHeader(e),
		headerNow:       time.Now(),
		isNew:           e == nil,
		validationFocus: -1,
		hover:           -1,
	}
	editor.header.tags = nil
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
		editor.originalTags = append([]string(nil), e.Tags...)
		editor.tags = append([]string(nil), e.Tags...)
		editor.favorite = hasFavoriteTag(e.Tags)
		if e.GroupUUID != "" {
			editor.GroupUUID = e.GroupUUID
		}
		for _, field := range e.Fields {
			if !isStandardEditorField(field.Name) {
				editor.readonlyFields = append(editor.readonlyFields, field)
				if !vault.IsTOTPField(field.Name) {
					editor.customFields = append(editor.customFields, field)
				}
			}
		}
		editor.originalCustomFields = append([]vault.Field(nil), editor.customFields...)
	}
	return editor
}

func (e *entryEditor) dirty() bool {
	if e == nil {
		return false
	}
	for _, name := range []string{"Title", "UserName", "URL", "Email", "Notes"} {
		if e.inputs[fieldIndex(name)].value() != e.original[name] {
			return true
		}
	}
	return !runesEqual(e.inputs[editPassword].text, e.originalPassword) || e.tagsChanged() || !sameVaultFields(e.customFields, e.originalCustomFields)
}

func sameVaultFields(a, b []vault.Field) bool {
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

func hasFavoriteTag(tags []string) bool {
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), "Favorite") {
			return true
		}
	}
	return false
}

func favoriteChangedTags(tags []string, favorite bool) []string {
	updated := make([]string, 0, len(tags)+1)
	found := false
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), "Favorite") {
			if favorite && !found {
				updated = append(updated, "Favorite")
				found = true
			}
			continue
		}
		updated = append(updated, tag)
	}
	if favorite && !found {
		updated = append(updated, "Favorite")
	}
	return updated
}

func (e *entryEditor) tagsChanged() bool {
	if e == nil || len(e.tags) != len(e.originalTags) {
		return e != nil
	}
	for i := range e.tags {
		if e.tags[i] != e.originalTags[i] {
			return true
		}
	}
	return false
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
	for i := range e.customFields {
		e.customFields[i].Value = ""
	}
	e.customName.clear()
	e.customValue.clear()
	e.readonlyFields = nil
	e.customFields = nil
	for i := range e.originalCustomFields {
		e.originalCustomFields[i].Value = ""
	}
	e.originalCustomFields = nil
	for i := range e.customFieldsSnapshot {
		e.customFieldsSnapshot[i].Value = ""
	}
	e.customFieldsSnapshot = nil
	wipebuf.Wipe(e.originalPassword)
	e.originalPassword = nil
	for key := range e.original {
		e.original[key] = ""
		delete(e.original, key)
	}
	e.original = nil
	e.originalTags = nil
	e.tags = nil
	e.favorite = false
	e.EntryUUID = ""
	e.GroupUUID = ""
	e.header = itemHeader{}
	e.headerNow = time.Time{}
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
	for _, name := range []string{"Title", "UserName", "URL", "Email", "Notes"} {
		values[name] = e.inputs[fieldIndex(name)].value()
	}
	if e.tagsChanged() {
		values["Tags"] = strings.Join(e.tags, ", ")
	}
	return values
}

func (e *entryEditor) password() []byte {
	if e == nil {
		return nil
	}
	return e.inputs[editPassword].bytes()
}

func (e *entryEditor) passwordChanged() bool {
	return e != nil && !runesEqual(e.inputs[editPassword].text, e.originalPassword)
}

func (e *entryEditor) setPassword(generated []byte) {
	if e == nil {
		return
	}
	e.inputs[editPassword].clear()
	e.inputs[editPassword].text = make([]rune, 0, utf8.RuneCount(generated))
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
	for _, name := range []string{"Title", "UserName", "URL", "Email", "Notes"} {
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
	if e.customOpen {
		e.customKey(k)
		return editorNone
	}
	switch key {
	case "esc":
		return editorCancel
	case "ctrl+s":
		return editorSave
	case "tab":
		e.moveFocus(1)
		return editorNone
	case "shift+tab":
		e.moveFocus(-1)
		return editorNone
	}
	if e.focus >= editGenerate {
		if orderedFocus(key, e.focusOrder(), &e.focus, false) {
			return editorNone
		}
		if key == "enter" {
			switch e.focus {
			case editGenerate:
				return editorGenerate
			case editTags:
				return editorTags
			case editCustomFields:
				e.openCustomFields()
				return editorCustomFields
			case editFavorite:
				return editorFavorite
			case editSave:
				return editorSave
			case editCancel:
				return editorCancel
			}
		}
		return editorNone
	}
	if key == "up" || key == "down" {
		delta := -1
		if key == "down" {
			delta = 1
		}
		if e.focus == editNotes && strings.Contains(e.inputs[editNotes].value(), "\n") {
			if !e.moveNotesCursor(delta) {
				orderedFocus(key, e.focusOrder(), &e.focus, false)
			}
		} else {
			orderedFocus(key, e.focusOrder(), &e.focus, false)
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
			e.moveFocus(1)
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

func (e *entryEditor) pointer(row, width, height int, compact, ascii bool) editorAction {
	if e == nil || row < 0 || width <= 0 || height <= 0 {
		return editorNone
	}
	renderer := *e
	renderer.compact, renderer.ascii = compact, ascii
	lines := renderer.layoutLines(width, height)
	if row >= len(lines) {
		return editorNone
	}
	focus := lines[row].focus
	if focus < 0 || focus >= editFocusCount || !e.hasFocus(focus) {
		return editorNone
	}
	e.focus = focus
	e.clearValidation()
	switch focus {
	case editGenerate:
		return editorGenerate
	case editTags:
		return editorTags
	case editCustomFields:
		e.openCustomFields()
		return editorNone
	case editFavorite:
		return editorFavorite
	case editSave:
		return editorSave
	case editCancel:
		return editorCancel
	default:
		return editorNone
	}
}

func (e *entryEditor) actionFocuses() []int {
	actions := []int{editCustomFields, editGenerate, editTags}
	if !e.isNew {
		actions = append(actions, editFavorite)
	}
	actions = append(actions, editSave, editCancel)
	return actions
}

func (e *entryEditor) focusOrder() []int {
	order := []int{editTitle, editUserName, editPassword, editURL, editEmail, editNotes}
	return append(order, e.actionFocuses()...)
}

func (e *entryEditor) hasFocus(focus int) bool {
	for _, candidate := range e.focusOrder() {
		if candidate == focus {
			return true
		}
	}
	return false
}

func (e *entryEditor) moveFocus(delta int) {
	key := "tab"
	if delta < 0 {
		key = "shift+tab"
	}
	orderedFocus(key, e.focusOrder(), &e.focus, true)
}

func (e *entryEditor) render(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	if e == nil {
		return padLines([]string{defaultStyles.secondary.Render("Editor unavailable")}, height)
	}
	if e.customOpen {
		return e.renderCustom(width, height)
	}
	lines, offset, total := e.layoutViewport(width, height)
	out := make([]string, len(lines))
	for i, line := range lines {
		if total > height {
			out[i] = fit(line.text, width-1) + editorScrollbarMarker(i, height, offset, total, e.ascii, e.styleSet())
		} else {
			out[i] = fit(line.text, width)
		}
	}
	return padLines(out, height)
}

func (e *entryEditor) openCustomFields() {
	if e.customOpen {
		return
	}
	e.customFieldsSnapshot = append([]vault.Field(nil), e.customFields...)
	e.customOpen, e.customEdit, e.customIndex = true, false, 0
}

func (e *entryEditor) closeCustomFields(save bool) {
	if !save {
		e.customFields = append(e.customFields[:0], e.customFieldsSnapshot...)
	}
	for i := range e.customFieldsSnapshot {
		e.customFieldsSnapshot[i].Value = ""
	}
	e.customFieldsSnapshot = nil
	e.customOpen = false
}

func (e *entryEditor) customKey(k tea.KeyMsg) {
	key := k.String()
	if e.customEdit {
		switch key {
		case "esc":
			e.customEdit = false
			e.customName.clear()
			e.customValue.clear()
			e.customErr = ""
		case "tab", "shift+tab":
			if key == "tab" {
				e.customFocus = (e.customFocus + 1) % 6
			} else {
				e.customFocus = (e.customFocus + 5) % 6
			}
		case "ctrl+s":
			e.saveCustomField()
		case "ctrl+r":
			if e.customConcealed {
				e.customReveal = !e.customReveal
			}
		case "enter":
			if e.customFocus == 1 {
				insertInputRune(&e.customValue, '\n')
			} else if e.customFocus == 2 {
				e.customConcealed = !e.customConcealed
				e.customReveal = false
			} else if e.customFocus == 3 {
				e.saveCustomField()
			} else if e.customFocus == 4 {
				if e.customIndex < len(e.customFields) {
					copy(e.customFields[e.customIndex:], e.customFields[e.customIndex+1:])
					e.customFields[len(e.customFields)-1] = vault.Field{}
					e.customFields = e.customFields[:len(e.customFields)-1]
					e.customIndex = min(e.customIndex, len(e.customFields))
				}
				e.customEdit = false
				e.customName.clear()
				e.customValue.clear()
				e.customErr = ""
			} else if e.customFocus == 5 {
				e.customEdit = false
				e.customName.clear()
				e.customValue.clear()
				e.customErr = ""
			}
		case "up", "down":
			if e.customFocus == 1 {
				tmp := entryEditor{}
				tmp.inputs[editNotes] = e.customValue
				delta := 1
				if key == "up" {
					delta = -1
				}
				if tmp.moveNotesCursor(delta) {
					e.customValue.cursor = tmp.inputs[editNotes].cursor
				} else {
					e.customFocus = min(5, max(0, e.customFocus+delta))
				}
			} else {
				dialogLinearFocus(key, 6, &e.customFocus, false)
			}
		default:
			if e.customFocus == 2 && k.Type == tea.KeySpace {
				e.customConcealed = !e.customConcealed
				e.customReveal = false
				return
			}
			if e.customFocus == 0 {
				e.customName.update(k)
				e.customErr = ""
			} else if e.customFocus == 1 {
				e.customValue.update(k)
			}
		}
		return
	}
	switch key {
	case "esc":
		e.closeCustomFields(false)
	case "up", "down", "tab", "shift+tab":
		dialogLinearFocus(key, len(e.customFields)+3, &e.customIndex, key == "tab" || key == "shift+tab")
	case "enter":
		if e.customIndex == len(e.customFields)+2 {
			e.closeCustomFields(false)
		} else if e.customIndex == len(e.customFields)+1 {
			e.closeCustomFields(true)
		} else if e.customIndex == len(e.customFields) {
			e.customEdit = true
			e.customReveal = false
			e.customFocus = 0
			e.customName.clear()
			e.customValue.clear()
			e.customConcealed = false
		} else {
			f := e.customFields[e.customIndex]
			e.customName.set(f.Name)
			e.customValue.set(f.Value)
			e.customConcealed = f.Protected
			e.customReveal = false
			e.customEdit = true
			e.customReveal = false
			e.customFocus = 0
		}
	case "a":
		e.customIndex = len(e.customFields)
		e.customEdit = true
		e.customReveal = false
		e.customFocus = 0
		e.customName.clear()
		e.customValue.clear()
		e.customConcealed = false
	}
}

func insertInputRune(i *input, r rune) {
	if len(i.text) >= 4096 {
		return
	}
	i.text = append(i.text, 0)
	copy(i.text[i.cursor+1:], i.text[i.cursor:len(i.text)-1])
	i.text[i.cursor] = r
	i.cursor++
}

func (e *entryEditor) saveCustomField() {
	name := strings.TrimSpace(e.customName.value())
	if name == "" || strings.ContainsAny(name, "\r\n\x00") {
		e.customErr = "Field name is required."
		return
	}
	if isStandardEditorField(name) || name == "Tags" || vault.IsTOTPField(name) {
		e.customErr = "This field name is reserved."
		return
	}
	for i, f := range e.customFields {
		if f.Name == name && (!e.customEdit || i != e.customIndex) {
			e.customErr = "Field name already exists."
			return
		}
	}
	f := vault.Field{Name: name, Value: e.customValue.value(), Protected: e.customConcealed}
	if e.customIndex < len(e.customFields) {
		e.customFields[e.customIndex] = f
	} else {
		e.customFields = append(e.customFields, f)
	}
	e.customIndex = min(len(e.customFields)-1, e.customIndex)
	e.customEdit = false
	e.customErr = ""
	e.customName.clear()
	e.customValue.clear()
}

func (e *entryEditor) renderCustom(width, height int) []string {
	styles := e.styleSet()
	lines := []string{styles.accent.Render(" CUSTOM FIELDS "), styles.rule(width)}
	if e.customEdit {
		nameLabel, valueLabel := " FIELD NAME", " FIELD VALUE"
		if e.customFocus == 0 {
			nameLabel = ">" + nameLabel
		}
		if e.customFocus == 1 {
			valueLabel = ">" + valueLabel
		}
		lines = append(lines, nameLabel, " "+fit(e.customName.value(), width-2), valueLabel)
		for _, line := range strings.Split(e.customValue.value(), "\n") {
			lines = append(lines, " "+fit(line, width-2))
		}
		concealed := fmt.Sprintf(" [%s] Concealed", map[bool]string{true: "x", false: " "}[e.customConcealed])
		if e.customFocus == 2 {
			concealed = ">" + concealed
		}
		lines = append(lines, concealed)
		save := " Save"
		cancel := " Cancel (esc)"
		if e.customFocus == 3 {
			save = ">" + save
		}
		if e.customFocus == 4 {
			cancel = ">" + cancel
		}
		lines = append(lines, save+"   "+cancel)
		if e.customErr != "" {
			lines = append(lines, styles.error.Render(e.customErr))
		}
	} else {
		for i, f := range e.customFields {
			marker := "  "
			if i == e.customIndex {
				marker = "> "
				lines = append(lines, styles.active.Render(marker+safe(f.Name)+" (enter)"))
			} else {
				lines = append(lines, styles.secondary.Render(marker+safe(f.Name)))
			}
		}
		if e.customIndex == len(e.customFields) {
			lines = append(lines, styles.active.Render("> Add"))
		} else {
			lines = append(lines, "  Add (a)")
		}
		lines = append(lines, "  Close (esc)")
	}
	return padLines(lines, height)
}

func (e *entryEditor) renderAppearance(width, height int, compact, ascii bool) []string {
	if e == nil {
		return e.render(width, height)
	}
	renderer := *e
	renderer.compact, renderer.ascii = compact, ascii
	return renderer.render(width, height)
}

type editorLine struct {
	text   string
	focus  int
	error  bool
	cursor bool
}

func (e *entryEditor) layoutLines(width, height int) []editorLine {
	lines, _, _ := e.layoutViewport(width, height)
	return lines
}

func (e *entryEditor) layoutViewport(width, height int) ([]editorLine, int, int) {
	title := ""
	if e.isNew {
		title = "New Entry"
		if e.dirty() {
			title += " *"
		}
	}
	lines := e.renderLines(title, width, height)
	if width > 2 && height > 0 && len(lines) > height {
		lines = e.renderLines(title, width-1, height)
		focusedLine := 0
		for i, line := range lines {
			if line.focus == e.focus && line.cursor {
				focusedLine = i
				break
			}
			if line.focus == e.focus {
				focusedLine = i
			}
		}
		visible, offset := renderEditorViewport(lines, focusedLine, e.focus, height)
		return visible, offset, len(lines)
	}
	for len(lines) < height {
		lines = append(lines, editorLine{focus: -1})
	}
	return lines, 0, 0
}

func editorScrollbarMarker(row, height, offset, total int, ascii bool, styles *styleSet) string {
	thumbHeight := max(1, height*height/total)
	thumbStart := (height - thumbHeight) * min(offset, total-height) / (total - height)
	track, thumb := "│", "▐"
	if ascii {
		track, thumb = "|", "#"
	}
	if row >= thumbStart && row < thumbStart+thumbHeight {
		return styles.secondary.Render(thumb)
	}
	return styles.divider.Render(track)
}

func (e *entryEditor) renderLines(title string, width, height int) []editorLine {
	styles := e.styleSet()
	if e.header.exists {
		lines := make([]editorLine, 0, 8)
		for _, line := range e.header.renderWithIndent(styles, width, e.headerNow, true, e.dirty(), 1) {
			lines = append(lines, editorLine{text: line, focus: -1})
		}
		return e.renderFieldLines(lines, width, height)
	}
	lines := []editorLine{{text: styles.accent.Render(" " + title), focus: -1}, {text: styles.rule(width), focus: -1}}
	return e.renderFieldLines(lines, width, height)
}

func (e *entryEditor) renderFieldLines(lines []editorLine, width, height int) []editorLine {
	styles := e.styleSet()
	for i, name := range editorFields {
		lines = append(lines, editorLine{focus: -1})
		label := " " + strings.ToUpper(editorLabel(name))
		if e.fieldDirty(i) {
			label += " *"
		}
		if i == e.focus {
			label = styles.accent.Render(label)
		} else {
			label = styles.secondary.Render(label)
		}
		lines = append(lines, editorLine{text: label, focus: i})
		if i != editNotes || !strings.Contains(e.inputs[i].value(), "\n") {
			field := inputFieldView{value: &e.inputs[i], secret: i == editPassword, focused: i == e.focus, indent: 1}
			value := renderInputBox(field, max(1, width-1), e.compact, 0, styles)
			lines = append(lines, editorLine{text: value, focus: i, cursor: i == e.focus})
		} else {
			valueLines, cursorLine := e.renderValue(i, max(1, width-2))
			for j, value := range valueLines {
				if i == e.focus {
					value = renderEditorMultilineLine(value, j == cursorLine, max(1, width-1), styles)
				} else {
					value = " " + value
				}
				lines = append(lines, editorLine{text: value, focus: i, cursor: i == e.focus && j == cursorLine})
			}
		}
		if e.validation != "" && e.validationFocus == i {
			for _, text := range renderErrorLines(e.validation, max(1, width-1), styles) {
				lines = append(lines, editorLine{text: text, focus: i, error: true})
			}
		}
	}
	if showNonPasswordVaultSecurity {
		for _, field := range e.readonlyFields {
			lines = append(lines, editorLine{focus: -1})
			label := " " + strings.ToUpper(safe(field.Name)) + " (READ ONLY)"
			lines = append(lines, editorLine{text: styles.secondary.Render(label), focus: -1})
			value := field.Value
			if value == "" {
				value = "—"
			}
			for _, paragraph := range strings.Split(value, "\n") {
				wrapped := ansi.Wrap(safe(paragraph), max(1, width-1), "")
				for _, text := range strings.Split(wrapped, "\n") {
					lines = append(lines, editorLine{text: " " + text, focus: -1})
				}
			}
		}
	}
	lines = append(lines, editorLine{text: styles.rule(width), focus: -1})
	actions := []struct {
		label    string
		index    int
		shortcut string
	}{{fmt.Sprintf("Custom Fields (%d)…", len(e.customFields)), editCustomFields, ""}, {"Generate Password…", editGenerate, ""}}
	actions = append(actions, struct {
		label    string
		index    int
		shortcut string
	}{"Tags…", editTags, ""})
	if !e.isNew {
		actions = append(actions, struct {
			label    string
			index    int
			shortcut string
		}{map[bool]string{true: "Remove from Favorites", false: "Add to Favorites"}[e.favorite], editFavorite, ""})
	}
	actions = append(actions,
		struct {
			label    string
			index    int
			shortcut string
		}{"Save", editSave, "ctrl+s"},
		struct {
			label    string
			index    int
			shortcut string
		}{"Cancel", editCancel, "esc"},
	)
	for _, action := range actions {
		text := renderEditorAction(action.label, action.shortcut, width, e.compact, e.ascii, action.index == e.focus, styles)
		if action.index == e.hover && action.index != e.focus {
			text = styles.inactive.Render(ansi.Strip(text))
		}
		lines = append(lines, editorLine{text: text, focus: action.index})
		if action.index == e.focus && e.err != "" && e.validation == "" {
			for _, text := range renderErrorLines(e.err, max(1, width-1), styles) {
				lines = append(lines, editorLine{text: text, focus: -1, error: true})
			}
		}
	}
	return lines
}

func renderEditorAction(label, shortcut string, width int, compact, ascii, selected bool, styles *styleSet) string {
	return renderDialogAction(label, shortcut, width, compact, ascii, selected, styles)
}

func renderEditorMultilineLine(value string, cursor bool, width int, styles *styleSet) string {
	if !cursor {
		return styles.inactive.Render(fit(" "+value, width))
	}
	before, after, _ := strings.Cut(value, "▏")
	caret := " "
	if after != "" {
		r, size := utf8.DecodeRuneInString(after)
		caret, after = string(r), after[size:]
	}
	padding := max(0, width-ansi.StringWidth(" "+before+caret+after))
	return styles.inactive.Render(" "+before) + styles.active.Render(caret) + styles.inactive.Render(after+strings.Repeat(" ", padding))
}

func (e *entryEditor) styleSet() *styleSet {
	if e != nil && e.styles != nil {
		return e.styles
	}
	return &defaultStyles
}

func renderEditorViewport(lines []editorLine, focusedLine, focus, height int) ([]editorLine, int) {
	if height <= 0 {
		return nil, 0
	}
	// Keep the heading and focused field label visible, then display a
	// contiguous window around the cursor. Selecting individual nearby lines
	// lets unrelated controls jump into and out of the middle of long notes.
	focusLabel, cursorLine := -1, -1
	for i, line := range lines {
		if line.focus != focus {
			continue
		}
		if line.cursor {
			cursorLine = i
		} else if focusLabel < 0 {
			focusLabel = i
		}
	}
	anchor := focusedLine
	if cursorLine >= 0 {
		anchor = cursorLine
	} else if focusLabel >= 0 {
		anchor = focusLabel
	}
	anchor = max(0, min(anchor, len(lines)-1))
	pinned := map[int]bool{0: true}
	if focusLabel >= 0 && focusLabel != anchor && height > 2 {
		pinned[focusLabel] = true
	}
	capacity := max(0, height-len(pinned))
	start, end := anchor, anchor
	for capacity > 0 && (start > 0 || end < len(lines)) {
		if end < len(lines) {
			if !pinned[end] {
				capacity--
			}
			end++
		}
		if capacity <= 0 {
			break
		}
		if start > 0 {
			start--
			if !pinned[start] {
				capacity--
			}
		}
	}
	visible := make([]editorLine, 0, height)
	for i, line := range lines {
		if pinned[i] || (i >= start && i < end) {
			visible = append(visible, line)
		}
	}
	for len(visible) < height {
		visible = append(visible, editorLine{focus: -1})
	}
	// Pinned rows do not belong to the scrolling window. Map its extent to
	// the scrollbar range so the thumb reaches both ends of the track.
	offset := start
	if end == len(lines) {
		offset = max(0, len(lines)-height)
	}
	return visible, offset
}

func (e *entryEditor) renderValue(field, width int) ([]string, int) {
	value := &e.inputs[field]
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
	wipebuf.Wipe(tail)
}

func (e *entryEditor) moveNotesCursor(delta int) bool {
	value := &e.inputs[editNotes]
	lineStart := 0
	for i := value.cursor - 1; i >= 0; i-- {
		if value.text[i] == '\n' {
			lineStart = i + 1
			break
		}
	}
	column := value.cursor - lineStart
	if delta < 0 {
		if lineStart == 0 {
			return false
		}
		previousEnd := lineStart - 1
		previousStart := 0
		for i := previousEnd - 1; i >= 0; i-- {
			if value.text[i] == '\n' {
				previousStart = i + 1
				break
			}
		}
		value.cursor = previousStart + min(column, previousEnd-previousStart)
		return true
	}
	lineEnd := len(value.text)
	for i := value.cursor; i < len(value.text); i++ {
		if value.text[i] == '\n' {
			lineEnd = i
			break
		}
	}
	if lineEnd == len(value.text) {
		return false
	}
	nextStart := lineEnd + 1
	nextEnd := len(value.text)
	for i := nextStart; i < len(value.text); i++ {
		if value.text[i] == '\n' {
			nextEnd = i
			break
		}
	}
	value.cursor = nextStart + min(column, nextEnd-nextStart)
	return true
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
