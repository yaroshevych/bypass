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

func TestEnvironmentReservesRunIdentity(t *testing.T) {
	got, err := Environment([]string{"BYPASS_IDENTITY=from-parent", "KEEP=yes"}, strings.NewReader("BYPASS_IDENTITY=from-file\n"))
	if err != nil || got["BYPASS_IDENTITY"] != "" || got["KEEP"] != "yes" {
		t.Fatal("reserved identity leaked into command environment")
	}
}
func TestEnvironmentRejectsInvalidEntries(t *testing.T) {
	for _, line := range []string{"no equals", "1KEY=value", "KEY='unterminated"} {
		if _, err := Environment(nil, strings.NewReader(line)); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}
