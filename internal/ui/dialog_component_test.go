package ui

import "testing"

func TestDialogLinearFocusWrapAndClamp(t *testing.T) {
	focus := 0
	if !dialogLinearFocus("up", 4, &focus, true) || focus != 3 {
		t.Fatal("wrapped focus did not reach the last field")
	}
	dialogLinearFocus("down", 4, &focus, false)
	if focus != 3 {
		t.Fatal("clamped focus moved past the last field")
	}
	if dialogLinearFocus("enter", 4, &focus, false) {
		t.Fatal("Enter was treated as navigation")
	}
}

func TestOrderedFocusHandlesNoncontiguousFields(t *testing.T) {
	order := []int{3, 7, 12}
	focus := 12
	orderedFocus("tab", order, &focus, true)
	if focus != 3 {
		t.Fatalf("wrapped focus = %d, want 3", focus)
	}
	orderedFocus("up", order, &focus, false)
	if focus != 3 {
		t.Fatalf("clamped focus = %d, want 3", focus)
	}
}

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

func TestDialogGeometryActionHitTesting(t *testing.T) {
	for _, appearance := range []string{"classic", "compact"} {
		m := New(Options{})
		m.width, m.height, m.settings.Appearance = 80, 24, appearance
		g := m.centeredDialogGeometry(40, 6)
		for i := 0; i < 2; i++ {
			got, ok := dialogActionAt(g, g.X+1, g.Y+g.HeaderRows+4+i, 4, 2)
			if !ok || got != i {
				t.Fatalf("%s action %d: got %d, %t", appearance, i, got, ok)
			}
		}
		if _, ok := dialogActionAt(g, g.X-1, g.Y+g.HeaderRows+4, 4, 2); ok {
			t.Fatalf("%s accepted click outside dialog", appearance)
		}
	}
}

func TestDialogGeometryClampsToSmallScreen(t *testing.T) {
	m := New(Options{})
	m.width, m.height = 48, 14
	for _, appearance := range []string{"classic", "compact"} {
		m.settings.Appearance = appearance
		g := m.centeredDialogGeometry(80, 6)
		if g.Width != 46 || g.X < 0 || g.X+g.Width > m.width || g.Y < 0 || g.Y+g.Height > m.height {
			t.Fatalf("%s geometry does not fit: %+v", appearance, g)
		}
	}
}
