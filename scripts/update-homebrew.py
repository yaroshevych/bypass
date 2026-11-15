#!/usr/bin/env python3
"""Update the Bypass Homebrew formula from a verified release download."""

import hashlib
import pathlib
import re
import sys


def main():
    if len(sys.argv) != 4 or not re.fullmatch(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", sys.argv[1]):
        sys.exit("usage: update-homebrew.py vMAJOR.MINOR.PATCH RELEASE_DIR FORMULA")
    version = sys.argv[1][1:]
    release = pathlib.Path(sys.argv[2])
    formula = pathlib.Path(sys.argv[3])
    original = formula.read_text()
    match = re.search(r'^  version "(\d+\.\d+\.\d+)"$', original, re.M)
    if not match:
        sys.exit("formula version missing or invalid")
    if tuple(map(int, version.split('.'))) < tuple(map(int, match[1].split('.'))):
        sys.exit("refusing to downgrade formula")

    manifest = (release / "SHA256SUMS").read_text().splitlines()
    updated = original.replace(match[0], f'  version "{version}"', 1)
    for platform in ("darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"):
        name = f"bypass_{version}_{platform}.tar.gz"
        entries = [line.split() for line in manifest if len(line.split()) == 2 and line.split()[1].lstrip('*') == name]
        if len(entries) != 1 or not re.fullmatch(r"[a-fA-F0-9]{64}", entries[0][0]):
            sys.exit(f"missing or invalid checksum: {name}")
        digest = hashlib.sha256((release / name).read_bytes()).hexdigest()
        if digest != entries[0][0].lower():
            sys.exit(f"checksum mismatch: {name}")
        pattern = rf'url "https://github\.com/yaroshevych/bypass/releases/download/v[^"/]+/bypass_[^"/]+_{platform}\.tar\.gz"\n(\s*)sha256 "[a-f0-9]{{64}}"'
        replacement = f'url "https://github.com/yaroshevych/bypass/releases/download/v{version}/{name}"\n'
        updated, count = re.subn(pattern, lambda m: replacement + m[1] + f'sha256 "{digest}"', updated)
        if count != 1:
            sys.exit(f"formula platform missing or duplicated: {platform}")
    if updated != original:
        formula.write_text(updated)
        print(f"Updated Bypass to {version}")
    else:
        print(f"Bypass {version} already up to date")


if __name__ == "__main__":
    main()
