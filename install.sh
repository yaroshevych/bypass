#!/usr/bin/env bash
set -euo pipefail

repo="yaroshevych/bypass"

case "$(uname -s)" in
  Darwin) goos="darwin" ;;
  Linux) goos="linux" ;;
  *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *) echo "Unsupported CPU architecture: $(uname -m)" >&2; exit 1 ;;
esac

for tool in curl tar awk install mktemp; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Required command not found: $tool" >&2
    exit 1
  fi
done
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  echo "Required command not found: sha256sum or shasum" >&2
  exit 1
fi

bindir="${BINDIR:-${HOME:?HOME must be set}/.local/bin}"
mkdir -p "$bindir"
bindir="$(cd "$bindir" && pwd -P)"
latest_url="https://github.com/${repo}/releases/latest"
resolved_url="$(curl --retry 3 --retry-delay 2 -fsSL -o /dev/null -w '%{url_effective}' "$latest_url")"
case "$resolved_url" in
  "https://github.com/${repo}/releases/tag/"*) tag="${resolved_url##*/}" ;;
  *) echo "Could not determine the latest stable release." >&2; exit 1 ;;
esac

version="${tag#v}"
archive="bypass_${version}_${goos}_${goarch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${tag}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/bypass-install.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

curl --retry 3 --retry-delay 2 -fsSL "$base_url/$archive" -o "$tmp_dir/$archive"
curl --retry 3 --retry-delay 2 -fsSL "$base_url/SHA256SUMS" -o "$tmp_dir/SHA256SUMS"
checksum_line="$(awk -v asset="$archive" '$2 == asset { print $1 "  " $2 }' "$tmp_dir/SHA256SUMS")"
if [[ -z "$checksum_line" ]]; then
  echo "No checksum found for $archive." >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp_dir" && printf '%s\n' "$checksum_line" | sha256sum -c -)
else
  expected="${checksum_line%% *}"
  actual="$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')"
  if [[ "$actual" != "$expected" ]]; then
    echo "Checksum verification failed for $archive." >&2
    exit 1
  fi
fi

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
if [[ ! -f "$tmp_dir/bypass" || ! -x "$tmp_dir/bypass" ]]; then
  echo "The release archive does not contain an executable bypass binary." >&2
  exit 1
fi

install -m 0755 "$tmp_dir/bypass" "$bindir/bypass"
if [[ "$goos" == "darwin" ]] && command -v xattr >/dev/null 2>&1; then
  xattr -d com.apple.quarantine "$bindir/bypass" 2>/dev/null || true
fi
printf 'Installed bypass %s to %s/bypass\n' "$version" "$bindir"
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) printf 'Add %s to your PATH if it is not already there.\n' "$bindir" ;;
esac
