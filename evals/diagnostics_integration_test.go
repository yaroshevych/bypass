//go:build integration

package evals

import (
	"strings"
	"testing"
)

func TestFailureDiagnosticsRedactSecretDisplays(t *testing.T) {
	e := &evalSession{secrets: []string{"custom fixture seed"}}
	redacted := e.redact("Failure\ncustom fixture seed\nCode 837294\n" + fixturePassword)
	for _, secret := range []string{"custom fixture seed", "837294", fixturePassword} {
		if strings.Contains(redacted, secret) {
			t.Fatal("diagnostic retained a registered or displayed secret")
		}
	}
	grand := e.redact("Grand View\n m   2   -   e \n 1   2   3   4 ")
	if strings.Contains(grand, "m   2") || !strings.Contains(grand, "withheld") {
		t.Fatal("diagnostic exposed spaced Grand View characters")
	}
}
