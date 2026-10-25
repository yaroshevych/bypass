# Development guidelines

## Dependencies and checks

- Go 1.24+, Make, and `keepassxc-cli` on `PATH`. The UI uses Bubble Tea and
  Lip Gloss; use the versions in `go.mod`.
- `make build` builds `bin/bypass`; `make install` installs it.
- Development recipes require `just`. End-to-end tests also require `tmux`.
- Run `just check` for Go changes (format check, vet, and race tests). Run
  `gofmt` on changed Go files first.
- Run `just eval` for changes to navigation, dialogs, input, or user journeys.
  Add focused regression tests for changed behavior.
- Run `just verify` for vault writes, locking, credentials, clipboard, or async
  operation changes. It includes native CLI and terminal integration tests.
- Before release, also run `just eval-repeat` and `just mutations`.
  See [test coverage](evals/COVERAGE.md) for scope and platform limits.
- `just clean` stops project eval processes and removes generated binaries, artifacts,
  and temporary test fixtures. Requires Python 3; `just clean --dry-run` previews it.
- Documentation-only changes need link, command, and diff checks; no Go test run.

## Code and data

- Keep UI, workspace, vault, platform, privacy, clipboard, and search concerns in
  their existing packages. Keep I/O in commands; `View()` must not change state.
- Preserve operation IDs and cancellation guards. Late results must not restore
  data after a lock. PIN protection covers the UI; full lock clears authentication.
- The default backend opens the original vault in KeePassXC and sends native
  commands over stdin. KeePassXC owns encryption, saving, and conflict handling.
  Backups are plain copies of the encrypted file before an edit. Keep at most five
  backups in total across vaults. Do not add custom
  staging, publication, cryptographic validation, or XML export/import rewrites.
- Never put passwords in process arguments, logs, notices, or test diagnostics.
  Use disposable vaults and the isolated clipboard for tests. Do not commit `.env`,
  vaults, credentials, or generated test artifacts.
- Copy stored passwords with native `clip` and timeout `0`. Do not fetch plaintext
  for copying or register native copies for clipboard cleanup.
- Synchronize terminal tests on observable state with deadlines, not fixed sleeps.
  Check saved data and privacy boundaries, not only visible text.

## UI and content

- Product name: **Bypass Secrets Manager**; executable and short UI name: `bypass`.
  README prose follows ASD-STE100 principles: short sentences, direct instructions,
  consistent terms, and concrete claims. Describe only features in the current UI.
- Use the four text roles: primary, secondary, accent, and error. Bold adds emphasis;
  do not add text roles, underline, or italics. Use shared theme surfaces and dividers.
- Reuse `internal/ui/dialog`, stacked inputs, action buttons, and filter headers.
  Keep rendering and mouse hit testing on the same layout geometry.
- Keep Classic and Compact content spacing consistent. Change their decoration,
  not their interaction. Preserve responsive layouts and ASCII rendering.
- Put labels above inputs. Start forms on the first input. Use Tab/Shift+Tab and
  Up/Down for focus, Enter to activate, and Esc to cancel where permitted.
- Use safe confirmation defaults. The external-change dialog cannot be dismissed
  with Esc. Preserve its reload requirement before writes can resume.
- Stack dialog action buttons at full content width. Show lowercase shortcut labels
  on their buttons (`enter`, `esc`, `ctrl+s`); do not repeat them in a footer.
  Join action separator rules to the frame edges.
- Omit empty or redundant dialog footers. Right-align any remaining dialog hints.
  Keep workspace hints lowercase and notices brief, lowercase, and without final
  punctuation. Prevent overlap with status text and the clipboard countdown.
- Match shortcut hints to actual bindings. `ctrl+l` locks all vaults, including
  from the editor; `[` and `]` switch tabs. Resize panes by dragging dividers.
- Mask passwords by default; show other standard fields. Cache list/search fields
  in memory and load remaining details on item focus. Copy feedback must not contain
  the copied value.
  Keep the PIN cover opaque and prevent async results from exposing secrets.
- Review affected dialogs in both appearances and relevant terminal sizes. Screenshot
  tooling is documented in [evals/screenshots/USAGE.md](evals/screenshots/USAGE.md).
  Generated screenshots are review artifacts, not approved pixel baselines.
