package platform

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunTimeoutConfiguration(t *testing.T) {
	paths := Paths{Config: t.TempDir(), State: t.TempDir()}
	for _, tc := range []struct {
		value string
		want  time.Duration
		bad   bool
	}{{`{}`, 0, false}, {`{"run_timeout_seconds":120}`, 2 * time.Minute, false}, {`{"run_timeout_seconds":0}`, 0, true}, {`{"run_timeout_seconds":-1}`, 0, true}} {
		if err := os.WriteFile(filepath.Join(paths.Config, "config.json"), []byte(tc.value), 0600); err != nil {
			t.Fatal(err)
		}
		settings, err := LoadSettings(paths)
		if (err != nil) != tc.bad || (!tc.bad && settings.RunTimeout != tc.want) {
			t.Fatalf("%s: timeout=%v error=%v", tc.value, settings.RunTimeout, err)
		}
	}
}
