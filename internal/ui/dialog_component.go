package ui

import (
	"strings"

	dialogrender "bypass/internal/ui/dialog"
)

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

func (m *Model) compactAppearance() bool {
	return strings.EqualFold(m.settings.Appearance, "compact")
}

func (m *Model) dialogActions(body []string, actions []dialogAction, focus, width, rows int) []string {
	capacity := max(0, rows-len(actions)-1)
	if len(body) > capacity {
		body = body[:capacity]
	}
	if len(body) == 0 {
		body = append(body, "")
	}
	body = append(body, dialogrender.BodyRule)
	for i, action := range actions {
		body = append(body, renderDialogAction(action.label, action.key, max(1, width-4), m.compactAppearance(), m.settings.ASCII, i == focus, m.colors()))
	}
	return body
}
