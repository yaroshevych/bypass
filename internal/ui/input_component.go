package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type inputFieldView struct {
	label       string
	value       *input
	secret      bool
	focused     bool
	indent      int
	showChevron bool
}

func renderInputField(field inputFieldView, styles *styleSet) string {
	prefix := strings.Repeat(" ", max(0, field.indent))
	if field.focused && field.showChevron {
		prefix = "› "
	}
	line := prefix + field.label + ": " + inputFieldText(field)
	if field.focused {
		return styles.accent.Render(line)
	}
	return styles.secondary.Render(line)
}

func renderStackedInputField(field inputFieldView, width int, compact bool, styles *styleSet) []string {
	label := strings.Repeat(" ", max(0, field.indent)) + field.label
	if field.focused {
		label = styles.accent.Render(label)
	} else {
		label = styles.secondary.Render(label)
	}
	return []string{label, renderInputBox(field, width, compact, styles)}
}

func inputFieldText(field inputFieldView) string {
	value := inputFieldValue(field)
	if field.value == nil || !field.focused {
		return value
	}
	cursor := min(max(0, field.value.cursor), len([]rune(value)))
	runes := []rune(value)
	return string(runes[:cursor]) + "▏" + string(runes[cursor:])
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

func renderInputBox(field inputFieldView, width int, compact bool, styles *styleSet) string {
	if width <= 0 || field.value == nil {
		return ""
	}
	value := inputFieldValue(field)
	if !field.focused {
		indent := max(0, field.indent)
		if compact {
			indent = 2
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
		contentWidth := width - 4
		budget := max(0, contentWidth-1)
		beforeWidth := min(ansi.StringWidth(before), budget)
		if ansi.StringWidth(before) > beforeWidth && beforeWidth > 0 {
			before = "…" + ansi.Cut(before, ansi.StringWidth(before)-beforeWidth+1, ansi.StringWidth(before))
			beforeWidth = ansi.StringWidth(before)
		}
		after = ansi.Truncate(after, max(0, budget-beforeWidth), "")
		padding := max(0, contentWidth-beforeWidth-ansi.StringWidth(after)-1)
		center := styles.inactive.Render(" ") + styles.inactive.Render(before) + styles.active.Render(caret) + styles.inactive.Render(after) + styles.inactive.Render(strings.Repeat(" ", padding)) + styles.inactive.Render(" ")
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
