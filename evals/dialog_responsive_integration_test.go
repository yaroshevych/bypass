//go:build integration

package evals

import (
	"strings"
	"testing"
)

func TestUnlockErrorWrapsInSmallTerminal(t *testing.T) {
	t.Parallel()
	e := newEval(t)
	defer e.close()
	createRealVault(e)
	e.startWith(e.realDB)
	e.mustShow("UNLOCK VAULT")
	e.resize(48, 16)
	e.typeText(fixtureWrongPassword)
	e.key("Enter")
	e.mustShow("Could not unlock", "backend support", "Open", "Cancel")
}

func TestCommandAndThemeDialogsInSmallTerminal(t *testing.T) {
	t.Parallel()
	e := newEval(t)
	defer e.close()
	e.start()
	for _, size := range [][2]int{{48, 16}, {60, 20}, {120, 35}} {
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
