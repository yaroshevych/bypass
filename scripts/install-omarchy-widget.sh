#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
bindir="${XDG_BIN_HOME:-$HOME/.local/bin}"
plugindir="${XDG_CONFIG_HOME:-$HOME/.config}/omarchy/plugins/bypass.vault"

command -v omarchy >/dev/null || { printf '%s\n' 'Omarchy is required.' >&2; exit 1; }
command -v go >/dev/null || { printf '%s\n' 'Go is required.' >&2; exit 1; }
omarchy plugin validate "$root/integrations/omarchy/bypass"
mkdir -p "$root/bin"
(cd "$root" && go build -trimpath -o bin/bypass ./cmd/bypass)
install -d "$bindir" "$plugindir"
tmpbinary=$(mktemp "$bindir/.bypass.XXXXXX")
trap 'rm -f "$tmpbinary"' EXIT HUP INT TERM
install -m 755 "$root/bin/bypass" "$tmpbinary"
mv -f "$tmpbinary" "$bindir/bypass"
install -m 644 "$root/integrations/omarchy/bypass/manifest.json" "$root/integrations/omarchy/bypass/Panel.qml" "$plugindir/"
omarchy-shell shell rescanPlugins
omarchy bar put bypass.vault --section right
printf '%s\n' "Installed Bypass to $bindir and added its Omarchy widget."
printf '%s\n' 'Restart any running Bypass TUI so both processes use the same executable.'
