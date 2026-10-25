#!/usr/bin/env python3
"""Remove Bypass build outputs and disposable eval resources."""
import argparse
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
FIXTURE = re.compile(r"/bypass-tmux-eval-")
BUILD = re.compile(r"/bypass-evals-build-[^/]+/bypass(?: |$)")
TEMP_PREFIXES = ("bypass-readback-", "bypass-tmux-eval-", "bypass-evals-build-")


def processes():
    output = subprocess.check_output(["ps", "-axo", "pid=,ppid=,uid=,stat=,command="], text=True)
    result = {}
    for line in output.splitlines():
        fields = line.strip().split(None, 4)
        if len(fields) == 5 and int(fields[2]) == os.getuid() and "Z" not in fields[3]:
            result[int(fields[0])] = (int(fields[1]), fields[4])
    return result


def fixture_process(command):
    executable = command.split(None, 1)[0] if command else ""
    name = Path(executable).name
    if name == "tmux" and re.search(r" -L kui\d+\b", command) and FIXTURE.search(command):
        return True
    if BUILD.search(executable):
        return True
    return name == "keepassxc-cli" and bool(FIXTURE.search(command))


def project_cwd(pid):
    try:
        if Path(f"/proc/{pid}/cwd").exists():
            cwd = Path(os.readlink(f"/proc/{pid}/cwd")).resolve()
        else:
            result = subprocess.run(["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"], capture_output=True, text=True, timeout=2)
            names = [line[1:] for line in result.stdout.splitlines() if line.startswith("n")]
            if not names:
                return False
            cwd = Path(names[0]).resolve()
        return cwd == ROOT or ROOT in cwd.parents
    except (OSError, subprocess.TimeoutExpired):
        return False


def eval_processes(rows):
    selected = {pid for pid, (_, command) in rows.items() if fixture_process(command)}
    for pid, (_, command) in rows.items():
        executable = command.split(None, 1)[0] if command else ""
        if Path(executable).name == "evals.test" and project_cwd(pid):
            selected.add(pid)
            parent = rows[pid][0]
            while parent in rows:
                name = Path(rows[parent][1].split(None, 1)[0]).name
                if name not in ("go", "just", "rtk"):
                    break
                selected.add(parent)
                parent = rows[parent][0]
    while True:
        children = {pid for pid, (parent, _) in rows.items() if parent in selected}
        if children <= selected:
            break
        selected |= children
    selected.discard(os.getpid())
    return selected


def stop_evals(dry_run):
    original = processes()
    selected = eval_processes(original)
    print(f"{'Would stop' if dry_run else 'Stopping'} {len(selected)} eval processes")
    if dry_run:
        return
    # Stop runners first so they cannot start another test while cleanup runs.
    for pid in sorted(selected, key=lambda item: Path(original[item][1].split(None, 1)[0]).name not in ("go", "just", "rtk", "evals.test")):
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    deadline = time.monotonic() + 2
    remaining = set(selected)
    while remaining and time.monotonic() < deadline:
        current = processes()
        remaining = {pid for pid in remaining if pid in current and current[pid][1] == original[pid][1]}
        if remaining:
            time.sleep(.1)
    for pid in remaining:
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    leftovers = eval_processes(processes())
    if leftovers:
        raise RuntimeError(f"{len(leftovers)} eval processes remain")


def generated_paths():
    yield ROOT / "bin"
    yield ROOT / "evals" / "artifacts"
    yield ROOT / "scripts" / "__pycache__"
    yield ROOT / "evals" / "screenshots" / "__pycache__"
    parents = {Path(tempfile.gettempdir()).resolve(), Path("/tmp").resolve()}
    for parent in parents:
        for entry in parent.iterdir():
            if entry.name.startswith(TEMP_PREFIXES) and entry.lstat().st_uid == os.getuid():
                yield entry


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="show cleanup scope without stopping or deleting anything")
    options = parser.parse_args()
    stop_evals(options.dry_run)
    count = 0
    for path in generated_paths():
        if not path.exists() and not path.is_symlink():
            continue
        count += 1
        if options.dry_run:
            continue
        if path.is_symlink() or not path.is_dir():
            path.unlink()
        else:
            shutil.rmtree(path)
    print(f"{'Would remove' if options.dry_run else 'Removed'} {count} generated output/fixture paths")


if __name__ == "__main__":
    main()
