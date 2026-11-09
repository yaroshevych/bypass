package ui

import (
	"bytes"
	"strings"
	"testing"

	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestEntryEditorDirtyValuesAndPasswordSeparation(t *testing.T) {
	newEditor := newEntryEditor(nil, "root-group")
	if newEditor.dirty() {
		t.Fatal("new editor starts dirty")
	}
	if newEditor.GroupUUID != "root-group" || newEditor.EntryUUID != "" || !newEditor.isNew {
		t.Fatalf("new editor identity = %#v", newEditor)
	}
	newEditor.inputs[editTitle].set("New site")
	newEditor.inputs[editPassword].set("secret-value")
	if !newEditor.dirty() {
		t.Fatal("editing new entry should mark it dirty")
	}
	values := newEditor.values()
	if values["Title"] != "New site" {
		t.Fatalf("Title = %q", values["Title"])
	}
	if _, ok := values["Password"]; ok {
		t.Fatal("password must not be returned in values")
	}
	newEditor.focus = editPassword
	view := strings.Join(newEditor.render(65, 22), "\n")
	if strings.Contains(view, "secret-value") || !strings.Contains(view, "••••••") {
		t.Fatal("new password must be masked in edit form")
	}
	if !strings.Contains(view, "••••••") {
		t.Fatal("focused password is not masked")
	}
	password := newEditor.password()
	if !bytes.Equal(password, []byte("secret-value")) {
		t.Fatalf("password() = %q", password)
	}
	clear(password)
	newEditor.clear()
	if got := newEditor.password(); len(got) != 0 {
		t.Fatalf("password remains after clear: %q", got)
	}
	if len(newEditor.values()["Title"]) != 0 {
		t.Fatal("field buffer remains after clear")
	}
}

func TestTextInputOverlayCursor(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	styles := stylesForTheme("Mocha")
	var value input
	value.set("abc")
	value.cursor = 1
	for _, compact := range []bool{false, true} {
		line := renderInputBox(inputFieldView{value: &value, focused: true}, 20, compact, 0, &styles)
		if !strings.Contains(line, styles.active.Render("b")) || !strings.Contains(ansi.Strip(line), "abc") {
			t.Fatalf("compact=%t: cursor did not highlight b: %q", compact, line)
		}
		value.cursor = len(value.text)
		line = renderInputBox(inputFieldView{value: &value, focused: true}, 20, compact, 0, &styles)
		if !strings.Contains(line, styles.active.Render(" ")) {
			t.Fatalf("compact=%t: missing end cursor: %q", compact, line)
		}
		value.cursor = 1
	}
	line := renderInputBox(inputFieldView{value: &value, focused: true, secret: true}, 20, false, 0, &styles)
	if strings.Contains(line, "abc") || !strings.Contains(line, styles.active.Render("•")) {
		t.Fatalf("password cursor did not highlight a masked character: %q", line)
	}
	line = renderEditorMultilineLine("a▏bc", true, 20, &styles)
	if !strings.Contains(line, styles.active.Render("b")) || !strings.Contains(ansi.Strip(line), "abc") {
		t.Fatalf("multiline cursor did not highlight b: %q", line)
	}
}

func TestEntryEditorMasksExistingPassword(t *testing.T) {
	entry := &vault.Entry{Fields: []vault.Field{{Name: "Password", Value: "private-password", Protected: true}}}
	for _, compact := range []bool{false, true} {
		for _, focus := range []int{editTitle, editPassword} {
			e := newEntryEditor(entry, "group")
			e.focus = focus
			view := strings.Join(e.renderAppearance(65, 22, compact, false), "\n")
			if strings.Contains(view, "private-password") || !strings.Contains(view, "••••••") {
				t.Fatalf("password not masked: compact=%t focus=%d", compact, focus)
			}
		}
	}
}

func TestEntryEditorExistingDirtyTracksPasswordAndPreservesIdentity(t *testing.T) {
	entry := &vault.Entry{
		UUID:      "entry-uuid",
		GroupUUID: "entry-group",
		Fields: []vault.Field{
			{Name: "Title", Value: "Site"},
			{Name: "UserName", Value: "alice"},
			{Name: "Password", Value: "old-password", Protected: true},
			{Name: "URL", Value: "https://example.test"},
			{Name: "Notes", Value: "first line\nsecond line"},
		},
	}
	e := newEntryEditor(entry, "fallback-group")
	if e.dirty() {
		t.Fatal("unchanged entry should be clean")
	}
	if e.EntryUUID != entry.UUID || e.GroupUUID != entry.GroupUUID || e.isNew {
		t.Fatalf("existing editor identity = %#v", e)
	}
	e.inputs[editPassword].set("new-password")
	if !e.dirty() || !e.passwordChanged() {
		t.Fatal("password edit should mark entry dirty")
	}
	e.inputs[editPassword].set("old-password")
	if e.passwordChanged() || e.dirty() {
		t.Fatal("restoring password should restore clean state")
	}
	e.inputs[editNotes].set("changed")
	if !e.dirty() {
		t.Fatal("field edit should mark entry dirty")
	}
}

func TestEntryEditorDoesNotAdvertiseMoveShortcutWhileTyping(t *testing.T) {
	e := newEntryEditor(&vault.Entry{UUID: "id", Fields: []vault.Field{{Name: "Title", Value: "Site"}}}, "group")
	view := ansi.Strip(strings.Join(e.render(65, 25), "\n"))
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Move…") && strings.HasSuffix(strings.TrimSpace(line), "m") {
			t.Fatal("editor advertises m as a shortcut while it types into fields")
		}
	}
	e.key(editorTeaKey("m"))
	if e.inputs[editTitle].value() != "Sitem" {
		t.Fatal("typing m changed editor behavior")
	}
}

func TestEntryEditorTabActionsAndNotesEnter(t *testing.T) {
	e := newEntryEditor(nil, "group")
	for range e.focusOrder() {
		if action := e.key(editorTeaKey("tab")); action != editorNone {
			t.Fatalf("Tab returned action %v", action)
		}
	}
	if e.focus != editTitle {
		t.Fatalf("Tab cycle ended at focus %d", e.focus)
	}
	e.key(editorTeaKey("shift+tab"))
	if e.focus != editCancel {
		t.Fatalf("Shift+Tab focus = %d, want Cancel", e.focus)
	}
	if action := e.key(editorTeaKey("enter")); action != editorCancel {
		t.Fatalf("Enter on Cancel = %v", action)
	}
	e.focus = editGenerate
	if action := e.key(editorTeaKey("enter")); action != editorGenerate {
		t.Fatalf("Enter on Generator = %v", action)
	}
	e.focus = editGenerate
	e.key(editorTeaKey("down"))
	if e.focus != editTags {
		t.Fatalf("Down from Generator in new entry focused %d, want Tags", e.focus)
	}
	e.key(editorTeaKey("down"))
	if e.focus != editSave {
		t.Fatalf("Down from Tags in new entry focused %d, want Save", e.focus)
	}
	if action := e.key(tea.KeyMsg{Type: tea.KeyCtrlS}); action != editorSave {
		t.Fatalf("Ctrl+S = %v", action)
	}
	if action := e.key(editorTeaKey("esc")); action != editorCancel {
		t.Fatalf("Esc = %v", action)
	}

	e.focus = editNotes
	e.typeIntoFocused("first")
	e.key(editorTeaKey("enter"))
	e.typeIntoFocused("second")
	if got := e.values()["Notes"]; got != "first\nsecond" {
		t.Fatalf("multiline notes = %q", got)
	}
}

func TestEntryEditorNotesArrowKeysLeaveAtFirstAndLastLine(t *testing.T) {
	e := newEntryEditor(nil, "group")
	e.inputs[editNotes].set("first\nsecond\nthird")
	e.focus = editNotes
	e.inputs[editNotes].cursor = len("first\nsec")
	e.key(tea.KeyMsg{Type: tea.KeyUp})
	if e.focus != editNotes || e.inputs[editNotes].cursor != 3 {
		t.Fatalf("up within notes: focus=%d cursor=%d", e.focus, e.inputs[editNotes].cursor)
	}
	e.key(tea.KeyMsg{Type: tea.KeyUp})
	if e.focus != editURL {
		t.Fatalf("up from first line: focus=%d, want URL", e.focus)
	}
	e.focus = editNotes
	e.inputs[editNotes].cursor = len("first\nsec")
	e.key(tea.KeyMsg{Type: tea.KeyDown})
	if e.focus != editNotes || e.inputs[editNotes].cursor != len("first\nsecond\nthi") {
		t.Fatalf("down within notes: focus=%d cursor=%d", e.focus, e.inputs[editNotes].cursor)
	}
	e.key(tea.KeyMsg{Type: tea.KeyDown})
	if e.focus != editGenerate {
		t.Fatalf("down from last line: focus=%d, want Generate", e.focus)
	}
}

func TestEntryEditorPasswordGeneratorAndSecretVisibility(t *testing.T) {
	e := newEntryEditor(&vault.Entry{
		UUID: "id",
		Fields: []vault.Field{
			{Name: "Title", Value: "Private title", Protected: true},
			{Name: "UserName", Value: "Private user", Protected: true},
			{Name: "Password", Value: "original-password", Protected: true},
		},
	}, "root")
	e.focus = editGenerate
	if action := e.key(editorTeaKey("enter")); action != editorGenerate {
		t.Fatalf("Generator action = %v", action)
	}
	generated := []byte("generated-secret")
	e.setPassword(generated)
	if !bytes.Equal(e.password(), generated) || !e.passwordChanged() {
		t.Fatal("generated password was not inserted")
	}
	clear(generated)
	if e.values()["Title"] != "Private title" || e.values()["UserName"] != "Private user" {
		t.Fatal("values() should retain protected standard fields for save")
	}
	e.focus = editPassword
	view := strings.Join(e.render(65, 40), "\n")
	for _, secret := range []string{"Private title", "Private user"} {
		if !strings.Contains(view, secret) {
			t.Fatalf("edit form does not show %q", secret)
		}
	}
	if strings.Contains(view, "generated-secret") {
		t.Fatal("generated password is visible in edit form")
	}

	e.focus = editUserName
	for _, r := range "replacement" {
		e.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := e.values()["UserName"]; got != "replacement" {
		t.Fatalf("typing into protected field = %q, want replacement", got)
	}
	if !strings.Contains(strings.Join(e.render(65, 40), "\n"), "replacement") {
		t.Fatal("edited protected field is not visible")
	}
}

func TestEntryEditorValidation(t *testing.T) {
	e := newEntryEditor(nil, "root")
	e.focus = editSave
	if err := e.validate(); err == nil || e.validationFocus != editTitle {
		t.Fatalf("empty Title validation = %v, focus=%d", err, e.validationFocus)
	}
	if e.focus != editTitle || !strings.Contains(strings.Join(e.render(65, 16), "\n"), "Title is required.") {
		t.Fatal("Title validation did not focus and render the field error")
	}
	e.inputs[editTitle].set("Entry")
	e.setPassword([]byte("two\nlines"))
	e.focus = editSave
	if err := e.validate(); err == nil || e.validationFocus != editPassword {
		t.Fatalf("password newline validation = %v, focus=%d", err, e.validationFocus)
	}
	e.setPassword([]byte("valid"))
	e.inputs[editNotes].set(`literal\nsequence`)
	e.focus = editSave
	if err := e.validate(); err == nil || e.validationFocus != editNotes {
		t.Fatalf("literal slash-n validation = %v, focus=%d", err, e.validationFocus)
	}

	entry := &vault.Entry{Fields: []vault.Field{
		{Name: "Title", Value: "Entry"},
		{Name: "Notes", Value: "keep me"},
	}}
	existing := newEntryEditor(entry, "root")
	existing.inputs[editNotes].clear()
	if err := existing.validate(); err == nil || existing.validationFocus != editNotes {
		t.Fatalf("clearing existing Notes validation = %v, focus=%d", err, existing.validationFocus)
	}
	if !strings.Contains(existing.err, "Clearing existing fields") {
		t.Fatalf("generic validation message = %q", existing.err)
	}
}

func TestEntryEditorRejectsInvalidPasswordPasteWithoutPartialInput(t *testing.T) {
	for _, invalid := range [][]rune{
		{'a', '\n', 'b'},
		{'a', '\r', 'b'},
		{'a', 0, 'b'},
	} {
		e := newEntryEditor(nil, "root")
		e.inputs[editTitle].set("Entry")
		e.focus = editPassword
		action := e.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: invalid})
		if action != editorNone {
			t.Fatalf("invalid paste returned action %v", action)
		}
		if got := e.password(); len(got) != 0 {
			t.Fatalf("invalid paste partially changed password: %q", got)
		}
		if e.validation == "" || e.validationFocus != editPassword {
			t.Fatal("invalid paste did not show password validation")
		}
	}
}

func TestEntryEditorIgnoresUnchangedLiteralSlashNNotes(t *testing.T) {
	entry := &vault.Entry{Fields: []vault.Field{
		{Name: "Title", Value: "Entry"},
		{Name: "Notes", Value: `raw\nsequence`},
		{Name: "URL", Value: "https://old.test"},
	}}
	e := newEntryEditor(entry, "root")
	e.inputs[editURL].set("https://new.test")
	if err := e.validate(); err != nil {
		t.Fatalf("unchanged existing notes blocked another edit: %v", err)
	}
	e.inputs[editNotes].set(`new\nsequence`)
	if err := e.validate(); err == nil || e.validationFocus != editNotes {
		t.Fatalf("changed literal slash-n notes validation = %v", err)
	}
}

func TestEntryEditorMarksDirtyField(t *testing.T) {
	e := newEntryEditor(&vault.Entry{Fields: []vault.Field{
		{Name: "Title", Value: "Entry"},
		{Name: "Notes", Value: "old"},
	}}, "root")
	e.inputs[editNotes].set("new")
	view := strings.Join(e.render(65, 22), "\n")
	if !strings.Contains(view, "NOTES *") {
		t.Fatalf("dirty field label missing:\n%s", view)
	}
	if strings.Contains(view, "TITLE *") {
		t.Fatalf("unchanged field marked dirty:\n%s", view)
	}
}

func TestEntryEditorNotesCursorRemainsVisibleWhenPaneIsShort(t *testing.T) {
	e := newEntryEditor(nil, "root")
	e.focus = editNotes
	e.inputs[editNotes].set(strings.Repeat("line\n", 30))
	e.inputs[editNotes].cursor = len(e.inputs[editNotes].text)
	lines := e.render(28, 12)
	joined := strings.Join(lines, "\n")
	if len(lines) != 12 {
		t.Fatalf("render produced %d lines, want 12", len(lines))
	}
	for _, want := range []string{"New Entry", "NOTES"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("short-pane render missing %q:\n%s", want, joined)
		}
	}
}

func TestEntryEditorLongNotesKeepAdjacentLinesVisible(t *testing.T) {
	e := newEntryEditor(nil, "root")
	e.focus = editNotes
	e.inputs[editNotes].set("one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve")
	for _, cursor := range []int{len("one\ntwo\nthree\nfour\nfive"), len("one\ntwo\nthree\nfour\nfive\nsix")} {
		e.inputs[editNotes].cursor = cursor
		view := ansi.Strip(strings.Join(e.render(40, 9), "\n"))
		for _, want := range []string{"four", "five", "six", "seven"} {
			if !strings.Contains(view, want) {
				t.Fatalf("cursor %d: missing adjacent line %q:\n%s", cursor, want, view)
			}
		}
		if strings.Contains(view, "Cancel") {
			t.Fatalf("unrelated action interrupts long notes:\n%s", view)
		}
	}
}

func TestEntryEditorScrollbarShowsOverflowAndPosition(t *testing.T) {
	for _, compact := range []bool{false, true} {
		e := newEntryEditor(nil, "root")
		e.inputs[editNotes].set(strings.Repeat("line\n", 60) + "last")
		e.focus = editTitle
		lines := e.renderAppearance(40, 10, compact, true)
		if !strings.HasSuffix(ansi.Strip(lines[0]), "#") || !strings.HasSuffix(ansi.Strip(lines[9]), "|") {
			t.Fatalf("compact=%t: scrollbar not at top: %q", compact, lines)
		}
		e.focus = editCancel
		lines = e.renderAppearance(40, 10, compact, true)
		if !strings.HasSuffix(ansi.Strip(lines[9]), "#") {
			t.Fatalf("compact=%t: scrollbar not at bottom: %q", compact, lines)
		}
		e.focus = editNotes
		e.inputs[editNotes].cursor = len(e.inputs[editNotes].text) / 2
		lines = e.renderAppearance(40, 10, compact, true)
		if strings.HasSuffix(ansi.Strip(lines[0]), "#") || strings.HasSuffix(ansi.Strip(lines[9]), "#") {
			t.Fatalf("compact=%t: scrollbar not in middle: %q", compact, lines)
		}
		e.inputs[editNotes].set("")
		lines = e.renderAppearance(40, 40, compact, true)
		for _, line := range lines {
			if strings.HasSuffix(ansi.Strip(line), "|") || strings.HasSuffix(ansi.Strip(line), "#") {
				t.Fatalf("compact=%t: scrollbar without overflow: %q", compact, line)
			}
		}
	}
}

func TestEntryEditorPointerSharesRenderedFocusMetadata(t *testing.T) {
	e := newEntryEditor(nil, "root")
	for _, actionCase := range []struct {
		focus  int
		result editorAction
	}{
		{editGenerate, editorGenerate},
		{editSave, editorSave},
		{editCancel, editorCancel},
	} {
		lines := e.layoutLines(65, 22)
		row := -1
		for i, line := range lines {
			if line.focus == actionCase.focus {
				row = i
				break
			}
		}
		if row < 0 {
			t.Fatalf("action focus %d missing from layout", actionCase.focus)
		}
		if got := e.pointer(row, 65, 22, e.compact, e.ascii); got != actionCase.result {
			t.Fatalf("pointer on focus %d = %v, want %v", actionCase.focus, got, actionCase.result)
		}
		if e.focus != actionCase.focus {
			t.Fatalf("pointer focus = %d, want %d", e.focus, actionCase.focus)
		}
	}

	valueEditor := newEntryEditor(&vault.Entry{Fields: []vault.Field{{Name: "Title", Value: "Save"}}}, "root")
	lines := valueEditor.layoutLines(65, 22)
	row := -1
	for i, line := range lines {
		if line.focus == editTitle {
			row = i
			break
		}
	}
	if got := valueEditor.pointer(row, 65, 22, valueEditor.compact, valueEditor.ascii); got != editorNone {
		t.Fatalf("clicking a field whose value says Save returned %v", got)
	}
}

func TestExistingEntryActionsShowShortcutsAndMoveDialog(t *testing.T) {
	e := newEntryEditor(&vault.Entry{Fields: []vault.Field{{Name: "Title", Value: "Entry"}}}, "root")
	lines := e.layoutLines(65, 30)
	joined := strings.Join(func() []string {
		out := make([]string, len(lines))
		for i := range lines {
			out[i] = ansi.Strip(lines[i].text)
		}
		return out
	}(), "\n")
	for _, want := range []string{"Generate Password…", "Tags…", "Add to Favorites", "ctrl+s", "esc"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("edit actions missing %q:\n%s", want, joined)
		}
	}
	e.focus = editGenerate
	e.key(editorTeaKey("down"))
	if e.focus != editTags {
		t.Fatalf("Down from Generator focused %d, want Tags", e.focus)
	}
	e.key(editorTeaKey("down"))
	if e.focus != editFavorite {
		t.Fatalf("Down from Tags focused %d, want favorite action", e.focus)
	}
	e.key(editorTeaKey("down"))
	if e.focus != editSave {
		t.Fatalf("Down from favorite action focused %d, want Save", e.focus)
	}
	e.focus = editFavorite
	if action := e.key(editorTeaKey("enter")); action != editorFavorite {
		t.Fatalf("Enter on favorite action = %v", action)
	}
	if strings.Contains(joined, "Move…") {
		t.Fatal("Move action remains in the entry editor")
	}
}

func TestEntryEditorKeySpaceInFieldsAndProtectedReplacement(t *testing.T) {
	e := newEntryEditor(nil, "root")
	e.typeIntoFocused("Eval")
	e.key(tea.KeyMsg{Type: tea.KeySpace})
	e.typeIntoFocused("Title")
	if got := e.values()["Title"]; got != "Eval Title" {
		t.Fatalf("Title with KeySpace = %q", got)
	}

	e.focus = editPassword
	e.typeIntoFocused("secret")
	e.key(tea.KeyMsg{Type: tea.KeySpace})
	e.typeIntoFocused("phrase")
	if got := string(e.password()); got != "secret phrase" {
		t.Fatalf("Password with KeySpace = %q", got)
	}

	e.focus = editNotes
	e.typeIntoFocused("first")
	e.key(tea.KeyMsg{Type: tea.KeySpace})
	e.typeIntoFocused("line")
	if got := e.values()["Notes"]; got != "first line" {
		t.Fatalf("Notes with KeySpace = %q", got)
	}

	protected := newEntryEditor(&vault.Entry{Fields: []vault.Field{
		{Name: "Title", Value: "Entry"},
		{Name: "UserName", Value: "hidden", Protected: true},
	}}, "root")
	protected.focus = editUserName
	protected.key(tea.KeyMsg{Type: tea.KeySpace})
	if got := protected.values()["UserName"]; got != " " {
		t.Fatalf("first KeySpace did not replace protected value: %q", got)
	}
	if strings.Contains(strings.Join(protected.render(65, 22), "\n"), "hidden") {
		t.Fatal("protected field leaked after replacement")
	}
}

func TestEntryEditorShowsUnsupportedFieldsReadOnlyAndRevealsSecrets(t *testing.T) {
	const privateValue = "custom-private-value"
	entry := &vault.Entry{
		UUID:      "entry",
		GroupUUID: "group",
		Fields: []vault.Field{
			{Name: "Title", Value: "Entry"},
			{Name: "Email", Value: "person@example.test"},
			{Name: "Recovery Code", Value: privateValue, Protected: true},
		},
	}
	e := newEntryEditor(entry, "root")
	view := strings.Join(e.render(65, 40), "\n")
	for _, hidden := range []string{"EMAIL (READ ONLY)", "person@example.test", "RECOVERY CODE (READ ONLY)", privateValue} {
		if strings.Contains(view, hidden) {
			t.Fatalf("unsupported custom field rendered %q:\n%s", hidden, view)
		}
	}
	if _, ok := e.values()["Email"]; ok {
		t.Fatal("readonly custom field should not be returned as an editable value")
	}
	e.clear()
	if len(e.readonlyFields) != 0 {
		t.Fatal("readonly field buffers remain after editor clear")
	}
}

func TestEntryEditorUsesInjectedThemeStylesAndDefaultFallback(t *testing.T) {
	e := newEntryEditor(nil, "root")
	if got := e.styleSet(); got != &defaultStyles {
		t.Fatal("editor did not use immutable default styles when no theme is injected")
	}
	theme := stylesForTheme("Mocha")
	e.styles = &theme
	if got := e.styleSet(); got != &theme {
		t.Fatal("editor copied or replaced the injected theme style pointer")
	}
	lines := e.layoutLines(40, 40)
	if lines[1].text != theme.rule(40) {
		t.Fatalf("editor divider ignored injected theme styles: %q", lines[1].text)
	}
}

func editorTeaKey(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func (e *entryEditor) typeIntoFocused(s string) {
	for _, r := range s {
		e.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}
