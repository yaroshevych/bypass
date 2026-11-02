package ui

import "testing"

func TestDialogFocusNavigatesListAndActions(t *testing.T) {
	item, action, buttons := 0, 0, false
	for _, key := range []string{"down", "tab", "down"} {
		dialogFocus(key, 2, 2, &item, &action, &buttons)
	}
	if !buttons || action != 1 {
		t.Fatal("navigation did not reach the last action")
	}
	dialogFocus("tab", 2, 2, &item, &action, &buttons)
	if buttons || item != 0 {
		t.Fatal("Tab did not wrap to the first list item")
	}
	dialogFocus("shift+tab", 2, 2, &item, &action, &buttons)
	if !buttons || action != 1 {
		t.Fatal("Shift+Tab did not wrap to the last action")
	}
	dialogFocus("up", 0, 2, &item, &action, &buttons)
	dialogFocus("up", 0, 2, &item, &action, &buttons)
	if !buttons || action != 0 {
		t.Fatal("empty list did not retain action focus")
	}
}
