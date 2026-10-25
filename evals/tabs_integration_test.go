//go:build integration

package evals

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTabPlusAndHoverThroughTerminalMouse(t *testing.T) {
	e := newEval(t)
	defer e.close()
	e.writeConfig(map[string]any{"appearance": "compact"})
	e.start()
	e.openPathBySelection()
	e.typeText(fixturePassword)
	e.key("Enter")
	e.mustShow("1 Eval Entry")
	selected := e.captureEscaped()
	e.typeText("\x1b[<35;4;2M")
	e.waitFor("selected tab text hover", func() bool { return e.captureEscaped() != selected })
	rows := strings.Split(e.capture(), "\n")
	offset := strings.Index(rows[1], "+")
	if offset < 0 {
		t.Fatal("new-tab button missing")
	}
	column := utf8.RuneCountInString(rows[1][:offset]) + 1
	before := e.captureEscaped()
	e.typeText(fmt.Sprintf("\x1b[<35;%d;2M", column))
	e.waitFor("hover style without switching tabs", func() bool { return e.captureEscaped() != before })
	e.mustNotShow("2 Eval Entry")
	e.typeText(fmt.Sprintf("\x1b[<0;%d;2M", column))
	e.typeText(fmt.Sprintf("\x1b[<0;%d;2m", column))
	e.mustShow("2 Eval Entry")
	e.key("q")
	e.waitForCleanQuit()
}
