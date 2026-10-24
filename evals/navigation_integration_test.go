//go:build integration

package evals

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestGrandViewGroupingCenteringNotesAndQCloseTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	backend, err := exec.LookPath("keepassxc-cli")
	if err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	e := newEval(t)
	defer e.close()
	createRealVault(e, backend)
	e.writeConfig(map[string]any{"grand_view_group_size": 3})
	e.startWith(backend, e.realDB)
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Seed Entry")
	e.key("J")
	e.key("p")
	e.mustShow("Password", "••••••••")
	e.key("G")
	e.mustShow("Grand View", "positions start at 1")
	e.waitFor("configured character background groups", func() bool {
		raw := e.captureEscaped()
		// tmux coalesces adjacent cells with the same attributes. The first
		// three characters share a background; the fourth starts a new group.
		return regexp.MustCompile(`m +2 +-`).MatchString(raw) && !regexp.MustCompile(`- +e`).MatchString(raw)
	})
	centered := false
	for _, line := range strings.Split(e.capture(), "\n") {
		leftByte, rightByte := strings.IndexRune(line, '┌'), strings.IndexRune(line, '┐')
		if leftByte < 0 || rightByte < 0 {
			continue
		}
		left := len([]rune(line[:leftByte]))
		width := len([]rune(line[leftByte:rightByte])) + 1
		right := 120 - left - width
		centered = left > 0 && width < 116 && left-right <= 1 && right-left <= 1
	}
	if !centered {
		e.failWithScreen("Grand View did not fit its content and center horizontally", e.capture())
	}
	e.key("q")
	e.mustShow("Seed Entry", "Password", "••••••••")
	e.mustNotShow(fixtureEntryPassword)
	e.key("J")
	e.key("n")
	e.key("G")
	e.mustShow("Grand View", "Notes", "synthetic seed note")
	if strings.Contains(e.capture(), "positions start at 1") {
		e.failWithScreen("Notes used indexed character mode", e.capture())
	}
	e.key("q")
	e.mustShow("Seed Entry")
	e.key("q")
	e.waitForCleanQuit()
}

func TestNavigationActionsAndSessionCleanupTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux evals require Unix")
	}
	e := newEval(t)
	defer e.close()
	e.start()
	e.openPath(e.db1)
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry")
	palette := func(query string) { e.key("C-p"); e.typeText(query); e.key("Enter") }
	divider := func() int {
		lines := strings.Split(e.capture(), "\n")
		if len(lines) < 5 {
			return -1
		}
		for col, r := range []rune(lines[4]) {
			if r == '│' {
				return col
			}
		}
		return -1
	}
	before := divider()
	if before < 0 {
		t.Fatal("wide view has no sidebar divider")
	}
	e.key("]")
	e.waitFor("sidebar grew", func() bool { return divider() > before })
	e.key("[")
	e.waitFor("sidebar width restored", func() bool { return divider() == before })
	e.key("J")
	e.key("p")
	e.key("z")
	e.waitFor("zoomed details after jump closes", func() bool {
		screen := e.capture()
		return !strings.Contains(screen, "Jump to…") && !strings.Contains(screen, "Website") && strings.Contains(screen, "Details")
	})
	e.mustShow("Details", "Password")
	e.mustNotShow("Website")
	e.key("z")
	e.mustShow("Website")
	e.key("C-f")
	e.typeText("Eval")
	e.key("Enter")
	e.waitFor("accepted search", func() bool {
		return !strings.Contains(e.capture(), "Enter apply") && strings.Contains(e.capture(), "Search · Eval")
	})
	palette("Search History")
	e.mustShow("Search History", "Eval")
	e.key("Enter")
	e.mustShow("Eval Entry", "Fuzzy search:")
	palette("Clear Search History")
	palette("Search History")
	e.mustShow("No matching actions")
	e.key("Escape")
	e.key("Escape")
	palette("Clear All Filters")
	e.mustShow("Eval Entry")
	palette("Choose Compact")
	e.mustShow("Choose Appearance")
	e.typeText("Compact")
	e.key("Enter")
	e.mustSettingsContain(`"appearance": "compact"`)
	palette("Toggle ASCII")
	e.mustSettingsContain(`"ascii": true`)
	e.mustShow("[ 1 Eval Entry ]")
	e.key("?")
	e.mustShow("Keyboard Shortcuts")
	e.typeText("Grand")
	e.mustShow("Grand View")
	e.key("Escape")
	palette("Set Session PIN")
	e.mustShow("Set Session PIN")
	e.typeText("2468")
	e.key("Enter")
	e.mustShow("Confirm Session PIN")
	e.typeText("2468")
	e.key("Enter")
	e.mustShow("Session PIN set")
	palette("Remove Session PIN")
	e.mustShow("Session PIN removed")
	e.key("C-t")
	e.mustShow("2 Eval Entry")
	palette("Close Current Vault")
	e.mustShow("bypass", "Open Vault", "Recent Vaults", "first.kdbx")
	e.key("Enter")
	e.mustShow("Unlock Vault")
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("Eval Entry")
	e.key("q")
	e.waitForCleanQuit()
}
