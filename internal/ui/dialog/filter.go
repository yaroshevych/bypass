package dialog

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type FilterStyles struct {
	Text, Placeholder, Fill, Caret  lipgloss.Style
	Count, Accent, Divider, Surface lipgloss.Style
}

// FilterHeader builds the common filtered-dialog header for classic and compact
// layouts. Compact accessory width includes its right cap.
func FilterHeader(title, value string, cursor int, placeholder, count string, width int, compact, ascii bool, styles FilterStyles) Header {
	maxInputWidth := 0
	if compact {
		maxInputWidth = 29
	}
	field := FilterInput(value, cursor, placeholder, maxInputWidth, compact, styles)
	count = styles.Count.Render(count)
	if compact {
		return Header{Accessory: field, AccessoryStyle: styles.Fill, Count: count}
	}

	label := styles.Accent.Render(strings.ToUpper(title))
	separatorText := " │"
	if ascii {
		separatorText = " |"
	}
	separator := styles.Divider.Render(separatorText)
	filterWidth := max(1, width-lipgloss.Width(label)-lipgloss.Width(separator))
	row := FilterRow(field, count, filterWidth, styles.Surface)
	return Header{Content: label + separator + row, Divider: lipgloss.Width(label) + 3}
}

// FilterInput renders a placeholder or editable query with a visible block caret.
// Filled mode uses the secondary tab color; the caret keeps the active color.
func FilterInput(value string, cursor int, placeholder string, maxWidth int, filled bool, styles FilterStyles) string {
	if placeholder == "" {
		placeholder = "filter"
	}
	textStyle, placeholderStyle := styles.Text, styles.Placeholder
	if filled {
		textStyle, placeholderStyle = styles.Fill, styles.Fill
	}
	var field string
	if value == "" {
		field = textStyle.Render(" ") + styles.Caret.Render(string([]rune(placeholder)[0])) + placeholderStyle.Render(string([]rune(placeholder)[1:])+" ")
	} else {
		runes := []rune(value)
		if maxWidth > 0 && len(runes)+3 > maxWidth {
			visible := max(0, maxWidth-4)
			runes = append(append([]rune(nil), runes[:min(len(runes), visible)]...), '…')
		}
		cursor = min(cursor, len(runes))
		prefix, suffix := string(runes[:cursor]), string(runes[cursor:])
		caret := " "
		if suffix != "" {
			caret = string([]rune(suffix)[0])
			suffix = string([]rune(suffix)[1:])
		}
		field = textStyle.Render(" "+prefix) + styles.Caret.Render(caret) + textStyle.Render(suffix+" ")
	}
	minimum := lipgloss.Width(textStyle.Render(" ") + placeholderStyle.Render(placeholder+" "))
	fieldWidth := lipgloss.Width(field)
	if fieldWidth < minimum {
		field += textStyle.Render(strings.Repeat(" ", minimum-fieldWidth))
	}
	if maxWidth > 0 {
		field = ansi.Truncate(field, maxWidth, "…")
	}
	return field
}

// FilterRow places a count at the right edge of a fixed-width filter row.
func FilterRow(field, count string, width int, gapStyle lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	countWidth := lipgloss.Width(count)
	fieldBudget := max(1, width-countWidth-1)
	field = ansi.Truncate(field, fieldBudget, "…")
	gap := max(1, width-lipgloss.Width(field)-countWidth)
	return field + gapStyle.Render(strings.Repeat(" ", gap)) + count
}
