#!/usr/bin/env bash
set -euo pipefail

# Release configuration
repo="yaroshevych/bypass"
kpxc_version="2.8.0"
kpxc_repo="keepassxreboot/keepassxc"

# KeePassXC 2.8.0 assets and published SHA-256 digests.
kpxc_linux_amd64_asset="KeePassXC-2.8.0-1-x86_64.AppImage"
kpxc_linux_amd64_sha256="3ab1bfbb422272334b48057e6bfc984c839375d9a1cd9350c4ea62ca0b55ca5f"
kpxc_linux_arm64_asset="KeePassXC-2.8.0-1-aarch64.AppImage"
kpxc_linux_arm64_sha256="ead773e5b2903e23695157c0c955fe294761b1bd00b78c697b67727fd13e61cc"
kpxc_darwin_arm64_asset="KeePassXC-2.8.0-1-arm64.dmg"
kpxc_darwin_arm64_sha256="85f0e75e258ef513f0fc7f5bf470cad0943b7d7e8191b2171f9898ad32e62b9c"
kpxc_darwin_amd64_asset="KeePassXC-2.8.0-1-x86_64.dmg"
kpxc_darwin_amd64_sha256="28492c5535570020179150b2871b3c54abd032cbb8cdee2f1b3fa7c46cdba43e"

latest_url="https://github.com/${repo}/releases/latest"
kpxc_base_url="https://github.com/${kpxc_repo}/releases/download/${kpxc_version}"

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

require_tool() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

for tool in curl tar awk install mktemp; do
  require_tool "$tool"
done
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  echo "Required command not found: sha256sum or shasum" >&2
  exit 1
fi
curl_retry() {
  curl --retry 3 --retry-delay 2 "$@"
}

bindir="${BINDIR:-${HOME:?HOME must be set}/.local/bin}"
mkdir -p "$bindir"
bindir="$(cd "$bindir" && pwd -P)"
app_root="$bindir/keepassxc"

resolved_url="$(curl_retry -fsSL -o /dev/null -w '%{url_effective}' "$latest_url")"
case "$resolved_url" in
  "https://github.com/${repo}/releases/tag/"*) tag="${resolved_url##*/}" ;;
  *) echo "Could not determine the latest stable release." >&2; exit 1 ;;
esac

version="${tag#v}"
archive="bypass_${version}_${goos}_${goarch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${tag}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/bypass-install.XXXXXX")"
mount_point=""
cleanup() {
  if [[ -n "$mount_point" ]]; then
    hdiutil detach "$mount_point" -quiet >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

verify_sha256() {
  local file="$1" expected="$2" actual
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    echo "Install sha256sum or shasum to verify downloads." >&2
    exit 1
  fi
  if [[ "$actual" != "$expected" ]]; then
    echo "Checksum verification failed for $(basename "$file")." >&2
    exit 1
  fi
}

version_at_least() {
  local output="$1" minimum="$2"
  awk -v output="$output" -v minimum="$minimum" '
    BEGIN {
      if (match(output, /[0-9]+\.[0-9]+\.[0-9]+/) == 0) exit 1
      found = substr(output, RSTART, RLENGTH)
      if (match(minimum, /[0-9]+\.[0-9]+\.[0-9]+/) == 0) exit 1
      required = substr(minimum, RSTART, RLENGTH)
      split(found, found_parts, /\./)
      split(required, required_parts, /\./)
      for (i = 1; i <= 3; i++) {
        if (found_parts[i] + 0 > required_parts[i] + 0) exit 0
        if (found_parts[i] + 0 < required_parts[i] + 0) exit 1
      }
      exit 0
    }
  '
}

install_linux_keepassxc_cli() {
  local asset expected appimage_path cli_command use_extracted_appimage
  use_extracted_appimage=false
  case "$goarch" in
    amd64)
      asset="$kpxc_linux_amd64_asset"
      expected="$kpxc_linux_amd64_sha256"
      ;;
    arm64)
      asset="$kpxc_linux_arm64_asset"
      expected="$kpxc_linux_arm64_sha256"
      ;;
    *)
      echo "Unsupported Linux architecture: $goarch" >&2
      exit 1
      ;;
  esac

  curl_retry -fsSL "$kpxc_base_url/$asset" -o "$tmp_dir/$asset"
  verify_sha256 "$tmp_dir/$asset" "$expected"
  mkdir -p "$app_root"
  appimage_path="$app_root/$asset"
  install -m 0755 "$tmp_dir/$asset" "$appimage_path"

  if "$appimage_path" cli --version >/dev/null 2>&1; then
    cli_command="$appimage_path"
  else
    printf 'AppImage could not run directly; extracting it for CLI use instead.\n'
    (cd "$tmp_dir" && "$appimage_path" --appimage-extract >/dev/null)
    if [[ ! -x "$tmp_dir/squashfs-root/AppRun" ]]; then
      echo "Could not extract the KeePassXC AppImage." >&2
      exit 1
    fi
    rm -rf "$app_root/squashfs-root"
    mv "$tmp_dir/squashfs-root" "$app_root/squashfs-root"
    cli_command="$app_root/squashfs-root/AppRun"
    use_extracted_appimage=true
  fi
  if [[ "$use_extracted_appimage" == true ]]; then
    cat > "$tmp_dir/keepassxc-cli" <<EOF
#!/usr/bin/env bash
exec env APPDIR="$app_root/squashfs-root" \\
  LD_LIBRARY_PATH="$app_root/squashfs-root/usr/lib\${LD_LIBRARY_PATH:+:\$LD_LIBRARY_PATH}" \\
  "$cli_command" cli "\$@"
EOF
  else
    cat > "$tmp_dir/keepassxc-cli" <<EOF
#!/usr/bin/env bash
exec "$cli_command" cli "\$@"
EOF
  fi
  rm -f "$bindir/keepassxc-cli"
  install -m 0755 "$tmp_dir/keepassxc-cli" "$bindir/keepassxc-cli"
}

install_keepassxc_cli() {
  local existing_version
  if command -v keepassxc-cli >/dev/null 2>&1; then
    if ! existing_version="$(keepassxc-cli --version 2>&1)"; then
      echo "keepassxc-cli was found on PATH but could not run; expected KeePassXC $kpxc_version." >&2
      exit 1
    fi
    if ! version_at_least "$existing_version" "$kpxc_version"; then
      echo "Incompatible keepassxc-cli on PATH: $existing_version (requires KeePassXC $kpxc_version or newer)." >&2
      exit 1
    fi
    printf 'Found compatible keepassxc-cli on PATH (%s).\n' "$existing_version"
    return
  fi

  local asset expected cli_path app_bundle candidate
  case "$goos/$goarch" in
    linux/amd64|linux/arm64)
      install_linux_keepassxc_cli
      ;;
    darwin/arm64|darwin/amd64)
      case "$goarch" in
        arm64)
          asset="$kpxc_darwin_arm64_asset"
          expected="$kpxc_darwin_arm64_sha256"
          ;;
        amd64)
          asset="$kpxc_darwin_amd64_asset"
          expected="$kpxc_darwin_amd64_sha256"
          ;;
        *)
          echo "Unsupported macOS architecture: $goarch" >&2
          exit 1
          ;;
      esac
      cli_path=""
      for app_bundle in "/Applications/KeePassXC.app" "$HOME/Applications/KeePassXC.app" "$app_root/KeePassXC.app"; do
        candidate="$app_bundle/Contents/MacOS/keepassxc-cli"
        if [[ -x "$candidate" ]]; then
          cli_path="$candidate"
          break
        fi
      done
      if [[ -z "$cli_path" ]]; then
        require_tool ditto
        require_tool hdiutil
        curl_retry -fsSL "$kpxc_base_url/$asset" -o "$tmp_dir/$asset"
        verify_sha256 "$tmp_dir/$asset" "$expected"
        mount_point="$tmp_dir/keepassxc-dmg"
        mkdir -p "$mount_point"
        hdiutil attach "$tmp_dir/$asset" -nobrowse -quiet -mountpoint "$mount_point"
        app_bundle="$app_root/KeePassXC.app"
        if [[ ! -d "$mount_point/KeePassXC.app" ]]; then
          echo "KeePassXC disk image does not contain KeePassXC.app." >&2
          exit 1
        fi
        mkdir -p "$app_root"
        ditto "$mount_point/KeePassXC.app" "$app_bundle"
        xattr -dr com.apple.quarantine "$app_bundle" 2>/dev/null || true
        hdiutil detach "$mount_point" -quiet
        mount_point=""
        cli_path="$app_bundle/Contents/MacOS/keepassxc-cli"
      fi
      if [[ ! -x "$cli_path" ]]; then
        echo "KeePassXC app bundle does not contain keepassxc-cli." >&2
        exit 1
      fi
      if ! existing_version="$("$cli_path" --version 2>&1)"; then
        echo "KeePassXC CLI in $app_bundle could not run; expected KeePassXC $kpxc_version or newer." >&2
        exit 1
      fi
      if ! version_at_least "$existing_version" "$kpxc_version"; then
        echo "Incompatible KeePassXC CLI in $app_bundle: $existing_version (requires KeePassXC $kpxc_version or newer)." >&2
        exit 1
      fi
      rm -f "$bindir/keepassxc-cli"
      ln -s "$cli_path" "$bindir/keepassxc-cli"
      ;;
  esac

  if ! existing_version="$("$bindir/keepassxc-cli" --version 2>&1)"; then
    echo "KeePassXC CLI was installed but could not be started." >&2
    exit 1
  fi
  if ! version_at_least "$existing_version" "$kpxc_version"; then
    echo "Incompatible keepassxc-cli: $existing_version (requires KeePassXC $kpxc_version or newer)." >&2
    exit 1
  fi
  printf 'Configured keepassxc-cli for %s/%s (%s).\n' "$goos" "$goarch" "$existing_version"
}

install_keepassxc_cli

curl_retry -fsSL "$base_url/$archive" -o "$tmp_dir/$archive"
curl_retry -fsSL "$base_url/SHA256SUMS" -o "$tmp_dir/SHA256SUMS"

checksum_line="$(awk -v asset="$archive" '$2 == asset { print $1 "  " $2 }' "$tmp_dir/SHA256SUMS")"
if [[ -z "$checksum_line" ]]; then
  echo "No checksum found for $archive." >&2
  exit 1
fi
expected_bypass_sha256="${checksum_line%% *}"
verify_sha256 "$tmp_dir/$archive" "$expected_bypass_sha256"

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
