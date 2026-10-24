//go:build integration

package evals

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// Exercise the production command-based clipboard driver without accessing the
// developer's clipboard or requiring a desktop session on CI.
func (e *evalSession) initClipboard() {
	e.t.Helper()
	e.clipboardPath = filepath.Join(e.root, "clipboard.bin")
	e.clipboardBin = filepath.Join(e.root, "clipboard-bin")
	if err := os.MkdirAll(e.clipboardBin, 0o700); err != nil {
		e.t.Fatal(err)
	}
	e.replaceClipboard(nil)
	write := `#!/bin/sh
set -eu
: "${KEEPASSUI_EVAL_CLIPBOARD:?missing isolated clipboard path}"
umask 077
tmp="${KEEPASSUI_EVAL_CLIPBOARD}.$$"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
cat > "$tmp"
mv -f "$tmp" "$KEEPASSUI_EVAL_CLIPBOARD"
`
	read := `#!/bin/sh
set -eu
: "${KEEPASSUI_EVAL_CLIPBOARD:?missing isolated clipboard path}"
cat "$KEEPASSUI_EVAL_CLIPBOARD"
`
	for _, name := range []string{"pbcopy", "wl-copy"} {
		if err := os.WriteFile(filepath.Join(e.clipboardBin, name), []byte(write), 0o700); err != nil {
			e.t.Fatal(err)
		}
	}
	for _, name := range []string{"pbpaste", "wl-paste"} {
		if err := os.WriteFile(filepath.Join(e.clipboardBin, name), []byte(read), 0o700); err != nil {
			e.t.Fatal(err)
		}
	}
	xclip := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "-out" ]; then exec "$(dirname "$0")/pbpaste"; fi
done
exec "$(dirname "$0")/pbcopy"
`
	if err := os.WriteFile(filepath.Join(e.clipboardBin, "xclip"), []byte(xclip), 0o700); err != nil {
		e.t.Fatal(err)
	}
}

func (e *evalSession) replaceClipboard(value []byte) {
	e.t.Helper()
	if err := os.WriteFile(e.clipboardPath, value, 0o600); err != nil {
		e.t.Fatal("write isolated clipboard:", err)
	}
}

func (e *evalSession) mustClipboardEqual(want []byte) {
	e.t.Helper()
	e.waitFor("isolated clipboard to contain expected bytes", func() bool {
		got, err := os.ReadFile(e.clipboardPath)
		defer clear(got)
		return err == nil && bytes.Equal(got, want)
	})
}

func (e *evalSession) registerSecret(value string) {
	if value != "" {
		e.secrets = append(e.secrets, value)
	}
}

func (e *evalSession) writeConfig(settings map[string]any) {
	e.t.Helper()
	dir := filepath.Join(e.root, "xdg-config", "keepassui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		e.t.Fatal(err)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		e.t.Fatal(err)
	}
}
