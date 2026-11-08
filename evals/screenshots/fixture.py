"""Disposable KDBX fixture created with the in-process vault backend."""
import os
from pathlib import Path
import subprocess

MASTER = "visual-demo-passphrase"


def build(root: Path) -> Path:
    project = Path(__file__).resolve().parents[2]
    vault = root / "demo-vault.kdbx"
    env = os.environ.copy()
    env["BYPASS_SCREENSHOT_FIXTURE"] = str(vault)
    result = subprocess.run(
        ["go", "run", "./evals/screenshots/fixturegen"],
        cwd=project,
        env=env,
        capture_output=True,
        text=True,
        timeout=180,
    )
    if result.returncode:
        raise RuntimeError("in-process screenshot fixture generation failed")
    return vault
