package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type inputFieldView struct {
	label   string
	value   *input
	secret  bool
	focused bool
	indent  int
}

func renderDialogChoice(label string, compact, ascii, selected bool, styles *styleSet) string {
	return renderDialogAction(label, "", ansi.StringWidth(label)+2, compact, ascii, selected, styles)
}

func renderDialogChoices(labels []string, selected int, compact, ascii bool, styles *styleSet) string {
	choices := make([]string, len(labels))
	for i, label := range labels {
		choices[i] = renderDialogChoice(label, compact, ascii, i == selected, styles)
	}
	return strings.Join(choices, "  ")
}

func dialogBoxInputWidth(screenWidth int) int {
	return max(1, min(screenWidth-6, 82)-4)
}

func dialogOverlayInputWidth(screenWidth, maxWidth int) int {
	return max(1, min(maxWidth, screenWidth-4)-4)
}

func renderDialogAction(label, shortcut string, width int, compact, ascii, selected bool, styles *styleSet) string {
	if compact {
		innerWidth := max(0, width-2)
		labelWidth := innerWidth
		if shortcut != "" {
			labelWidth = max(0, innerWidth-ansi.StringWidth(shortcut)-1)
		}
		body := ansi.Truncate(label, labelWidth, "…")
		if shortcut != "" {
			body += strings.Repeat(" ", max(1, innerWidth-ansi.StringWidth(body)-ansi.StringWidth(shortcut))) + shortcut
		}
		body = fit(body, innerWidth)
		if selected {
			if ascii {
				return styles.active.Render(fit("["+body+"]", width))
			}
			edge := styles.primary.Foreground(styles.active.GetBackground())
			return fit(edge.Render("\ue0b6")+styles.active.Render(body)+edge.Render("\ue0b4"), width)
		}
		return styles.secondary.Render(fit(" "+body+" ", width))
	}
	shortcutWidth := ansi.StringWidth(shortcut)
	labelWidth := max(0, width-shortcutWidth-2)
	left := ansi.Truncate(" "+label, labelWidth, "…")
	rightPad := " "
	gap := 0
	if shortcut != "" {
		gap = max(1, width-ansi.StringWidth(left)-shortcutWidth-1)
		left += strings.Repeat(" ", gap) + shortcut
	}
	row := fit(left+rightPad, width)
	if selected {
		return styles.active.Render(row)
	}
	return styles.secondary.Render(row)
}

func renderStackedInputField(field inputFieldView, width int, compact bool, styles *styleSet) []string {
	label := strings.Repeat(" ", max(0, field.indent)) + field.label
	if field.focused {
		label = styles.accent.Render(label)
	} else {
		label = styles.secondary.Render(label)
	}
	return []string{label, renderInputBox(field, width, compact, 0, styles)}
}

func inputFieldValue(field inputFieldView) string {
	if field.value == nil {
		return ""
	}
	value := ""
	if field.secret {
		value = strings.Repeat("•", len(field.value.text))
	} else {
		value = safe(field.value.value())
	}
	return value
}

func renderInputBox(field inputFieldView, width int, compact bool, innerPadding int, styles *styleSet) string {
	if width <= 0 || field.value == nil {
		return ""
	}
	value := inputFieldValue(field)
	if !field.focused {
		indent := max(0, field.indent)
		if compact {
			indent = 1
		}
		return styles.secondary.Render(fit(strings.Repeat(" ", indent)+value, width))
	}
	text := []rune(value)
	cursor := min(max(0, field.value.cursor), len(text))
	before, after := string(text[:cursor]), string(text[cursor:])
	caret := "▏"
	if compact {
		if width < 4 {
			return styles.inactive.Render(fit(caret+after, width))
		}
		innerPadding = max(0, innerPadding)
		contentWidth := max(0, width-2-2*innerPadding)
		budget := max(0, contentWidth-1)
		beforeWidth := min(ansi.StringWidth(before), budget)
		if ansi.StringWidth(before) > beforeWidth && beforeWidth > 0 {
			before = "…" + ansi.Cut(before, ansi.StringWidth(before)-beforeWidth+1, ansi.StringWidth(before))
			beforeWidth = ansi.StringWidth(before)
		}
		after = ansi.Truncate(after, max(0, budget-beforeWidth), "")
		padding := max(0, contentWidth-beforeWidth-ansi.StringWidth(after)-1)
		pad := strings.Repeat(" ", innerPadding)
		center := styles.inactive.Render(pad+before) + styles.active.Render(caret) + styles.inactive.Render(after+strings.Repeat(" ", padding)+pad)
		edge := styles.primary.Foreground(styles.inactive.GetBackground())
		return fit(edge.Render("\ue0b6")+center+edge.Render("\ue0b4"), width)
	}
	indent := max(0, field.indent)
	budget := max(0, width-indent-2)
	beforeWidth := min(ansi.StringWidth(before), budget)
	if ansi.StringWidth(before) > beforeWidth && beforeWidth > 0 {
		before = "…" + ansi.Cut(before, ansi.StringWidth(before)-beforeWidth+1, ansi.StringWidth(before))
		beforeWidth = ansi.StringWidth(before)
	}
	after = ansi.Truncate(after, max(0, width-indent-beforeWidth-2), "")
	padding := max(0, width-indent-beforeWidth-ansi.StringWidth(after)-2)
	return styles.inactive.Render(strings.Repeat(" ", indent)+before) + styles.active.Render(caret) + styles.inactive.Render(after+strings.Repeat(" ", padding+1))
}
