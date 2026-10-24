Run from the repository root:

```sh
python3 -m pip install -r evals/screenshots/requirements.txt
just screenshots
just screenshot palette compact latte
just screenshot editor compact macchiato 60 20
just screenshot-list
just screenshot-check
```

Requires Python/Pillow, tmux, keepassxc-cli, Go, just, and Menlo on macOS.
Set `KEEPASSUI_SCREENSHOT_FONT` to another monospace font on other systems.
Artifacts: `evals/artifacts/screenshots/index.html`, PNGs, ANSI captures, plain text, metadata.

These are real keyboard journeys against disposable native demo vaults, rasterized
from tmux captures. Host vaults, settings and clipboard are isolated. They are not
OS terminal-window screenshots. Font/rasterizer versions are recorded. Temporary
paths and cursor state can vary; no approved reference images or pixel comparisons
exist yet. Add those after manual polish. The renderer covers the SGR and simple
Unicode used in these fixtures; it does not reproduce every terminal/font engine.
