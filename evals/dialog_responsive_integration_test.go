//go:build integration

package evals

import (
	"strings"
	"testing"
)

func TestCommandAndThemeDialogsInSmallTerminal(t *testing.T) {
	t.Parallel()
	e := newEval(t)
	defer e.close()
	e.start()
	for _, size := range [][2]int{{48, 14}, {60, 20}, {120, 35}} {
		e.resize(size[0], size[1])
		e.key("C-p")
		e.mustShow("COMMANDS")
		e.typeText("Theme")
		e.key("Enter")
		e.mustShow("THEME", "Macchiato")
		e.key("Escape")
		e.waitFor("theme dialog to close", func() bool {
			screen := e.capture()
			return strings.Contains(screen, "VAULTS") && !strings.Contains(screen, "THEME") && !strings.Contains(screen, "COMMANDS")
		})
	}
}
