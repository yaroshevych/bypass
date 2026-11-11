#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
bindir="${XDG_BIN_HOME:-$HOME/.local/bin}"
pluginroot="${XDG_CONFIG_HOME:-$HOME/.config}/omarchy/plugins"
plugindir="$pluginroot/bypass.vault"

command -v omarchy >/dev/null || { printf '%s\n' 'Omarchy is required.' >&2; exit 1; }
command -v go >/dev/null || { printf '%s\n' 'Go is required.' >&2; exit 1; }
omarchy plugin validate "$root/integrations/omarchy/bypass"
mkdir -p "$root/bin"
(cd "$root" && go build -trimpath -o bin/bypass ./cmd/bypass)
install -d "$bindir" "$pluginroot"
tmpbinary=$(mktemp "$bindir/.bypass.XXXXXX")
staging=""
cleanup() {
  rm -f "$tmpbinary"
  [ -n "$staging" ] || return
  if [ -e "$staging/previous" ] || [ -L "$staging/previous" ]; then
    if [ ! -e "$plugindir" ] && [ ! -L "$plugindir" ]; then
      mv "$staging/previous" "$plugindir"
    fi
  fi
  rm -rf "$staging"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
staging=$(mktemp -d "$pluginroot/../.bypass-install.XXXXXX")
install -m 755 "$root/bin/bypass" "$tmpbinary"
mv -f "$tmpbinary" "$bindir/bypass"
install -d "$staging/plugin/i18n"
install -m 644 "$root/integrations/omarchy/bypass/Panel.qml" "$root/integrations/omarchy/bypass/manifest.json" "$staging/plugin/"
install -m 644 "$root/integrations/omarchy/bypass/i18n/"*.json "$staging/plugin/i18n/"
omarchy plugin validate "$staging/plugin"
if [ -e "$plugindir" ] || [ -L "$plugindir" ]; then
  mv "$plugindir" "$staging/previous"
fi
mv "$staging/plugin" "$plugindir"
omarchy restart shell
omarchy bar put bypass.vault --section right
printf '%s\n' "Installed Bypass to $bindir and added its Omarchy widget."
printf '%s\n' 'Restart any running Bypass TUI so both processes use the same executable.'
