package ui

import (
	"strings"

	dialogrender "bypass/internal/ui/dialog"
)

func renderErrorLines(message string, width int, styles *styleSet) []string {
	parts := wrappedTextLines(message, width)
	for i := range parts {
		parts[i] = styles.error.Render(parts[i])
	}
	return parts
}

func wrappedTextLines(message string, width int) []string {
	return dialogrender.WrapMessage(safe(message), width)
}

// dialogFocus navigates a selectable list followed by stacked actions.
// Tab wraps; arrow keys stop at the ends.
func dialogFocus(key string, itemCount, actionCount int, item, action *int, actionsFocused *bool) bool {
	switch key {
	case "up", "shift+tab":
		if *actionsFocused {
			if *action > 0 {
				*action--
			} else if itemCount > 0 {
				*actionsFocused = false
				*item = itemCount - 1
			} else if key == "shift+tab" {
				*action = actionCount - 1
			}
		} else if *item > 0 {
			*item--
		} else if key == "shift+tab" {
			*actionsFocused = true
			*action = actionCount - 1
		}
	case "down", "tab":
		if !*actionsFocused && *item+1 < itemCount {
			*item++
		} else if !*actionsFocused {
			*actionsFocused = true
			*action = 0
		} else if *action+1 < actionCount {
			*action++
		} else if key == "tab" {
			if itemCount > 0 {
				*actionsFocused = false
				*item = 0
			} else {
				*action = 0
			}
		}
	default:
		return false
	}
	return true
}

type dialogAction struct{ label, key string }

func dialogActionFocus(key string, count int, focus *int) bool {
	item, actionsFocused := 0, true
	return dialogFocus(key, 0, count, &item, focus, &actionsFocused)
}

// dialogLinearFocus navigates a linear sequence of fields and actions.
func dialogLinearFocus(key string, count int, focus *int, wrap bool) bool {
	if count <= 0 {
		return false
	}
	delta := 0
	switch key {
	case "tab", "down":
		delta = 1
	case "shift+tab", "up":
		delta = -1
	default:
		return false
	}
	if wrap {
		*focus = (*focus + count + delta) % count
	} else {
		*focus = min(count-1, max(0, *focus+delta))
	}
	return true
}

// orderedFocus navigates fields whose identifiers are not contiguous.
func orderedFocus(key string, order []int, focus *int, wrap bool) bool {
	position := 0
	for i, id := range order {
		if id == *focus {
			position = i
			break
		}
	}
	if !dialogLinearFocus(key, len(order), &position, wrap) {
		return false
	}
	*focus = order[position]
	return true
}

func (m *Model) compactAppearance() bool {
	return m.smallDialogScreen() || strings.EqualFold(m.settings.Appearance, "compact")
}

func (m *Model) dialogOptions() dialogrender.Options {
	c := m.colors()
	theme := colorsForTheme(m.settings.Theme)
	return dialogrender.Options{
		ScreenWidth: m.width, Height: m.height, Compact: m.compactAppearance(), ASCII: m.settings.ASCII, Fullscreen: m.smallDialogScreen(),
		Surface: c.primary, Accent: c.accent, Secondary: c.secondary, ErrorStyle: c.error, Pill: c.active, Divider: c.divider,
		RestoreResets: func(text string) string { return restoreThemeResets(text, theme.primary, theme.background) },
	}
}

// dialogInnerWidth is the width available to fields, lists and actions.
func (m *Model) dialogInnerWidth(frameWidth int) int {
	if m.smallDialogScreen() {
		return max(1, m.width)
	}
	return max(1, frameWidth-4)
}

// dialogContentCapacity gives small panes their available fullscreen body rows.
func (m *Model) dialogContentCapacity(framedReserve, minimum int) int {
	if m.smallDialogScreen() {
		return max(minimum, m.height-1)
	}
	return max(minimum, m.height-framedReserve)
}

func (m *Model) centeredDialogGeometry(width, bodyRows int) dialogrender.OverlayGeometry {
	return dialogrender.LayoutOverlay(width, m.width, m.height, bodyRows, m.compactAppearance(), -1, -1)
}

func (m *Model) dialogActionRows(actions []dialogAction, focus, width int) []string {
	rows := make([]string, 0, len(actions))
	for i, action := range actions {
		rows = append(rows, renderDialogAction(action.label, action.key, width, m.compactAppearance(), m.settings.ASCII, i == focus, m.colors()))
	}
	return rows
}

// dialogFields composes the shared stacked input fields used in modal forms.
func (m *Model) dialogFields(fields []inputFieldView, width int, spaced bool) []string {
	var rows []string
	for _, field := range fields {
		if spaced {
			rows = append(rows, "")
		}
		rows = append(rows, renderStackedInputField(field, width, m.compactAppearance(), m.colors())...)
	}
	return rows
}
