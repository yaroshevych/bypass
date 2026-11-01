package run

import (
	"strings"
	"testing"
)

func TestEnvironmentPrecedence(t *testing.T) {
	got, err := Environment([]string{"A=original", "B=base"}, strings.NewReader("A=first\nC='value here'\n"), strings.NewReader("A=last\n"))
	if err != nil || got["A"] != "last" || got["B"] != "base" || got["C"] != "value here" {
		t.Fatalf("unexpected environment: %v, %v", got, err)
	}
}
func TestEnvironmentRejectsInvalidEntries(t *testing.T) {
	for _, line := range []string{"no equals", "1KEY=value", "KEY='unterminated"} {
		if _, err := Environment(nil, strings.NewReader(line)); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}
