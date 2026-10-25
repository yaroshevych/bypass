# Test coverage

Run from the Go module root:

```sh
just verify       # format, vet, race tests, native CLI and tmux journeys
just eval-repeat  # run active terminal journeys twice
just mutations    # deliberately broken guards must fail their tests
just coverage     # statement report for application packages
```

`verify` requires tmux and `keepassxc-cli`. Native runtime checked locally:
KeePassXC CLI 2.8.0 on macOS. Other versions and platforms need their own runs.

## Active checks

- Native sessions: authentication, quoting, direct saves, independent saved-file
  readback, encrypted backups, backup retention, groups, moves, deletion, database
  info, password rotation, cancellation, process cleanup, and external conflicts.
  See [vault tests](../internal/vault/interactive_write_test.go) and
  [lifecycle tests](../internal/vault/interactive_lifecycle_test.go).
- Terminal journeys: Classic and Compact browsing, filtering, password copy/reveal,
  edits, new entries, saved-file readback, backups, database info, deletion,
  password rotation, lock/unlock, and clean exit. See
  [native-session journeys](interactive_integration_test.go).
- UI tests: supported dialog labels, layout, mouse hit targets, navigation,
  draft preservation, late async results, password masking, clipboard cleanup,
  PIN protection and configurable inactivity, and full locking.
- Platform, workspace, search, privacy, and clipboard packages have independent
  tests. Clipboard journeys use an isolated file-backed driver.

Mouse tests locate visible text independently of production hit-test coordinates.
Layout changes must preserve the correspondence between visible rows and clicks.
Tests for unsupported features remain in source with an explicit `t.Skip` reason.
Passing a run with skipped tests does not establish coverage for those features.

## Deferred checks

| Tests | Reason to skip |
|---|---|
| Key-file-only creation and key-file rotation terminal journeys | Non-password authentication controls are hidden in beta |
| TOTP, History, and attachment UI/terminal journeys | These features are hidden in beta |
| ASCII toggle subtest | The toggle is hidden in beta |
| Standalone CLI edit interception and staged-save failure injection | Persistent sessions send commands over stdin; the old wrappers cannot inject these failures |
| Cached-credential reload journey | Native sessions require lock/unlock instead of reusing a retained master password |

Retained sources: [creation](creation_integration_test.go),
[extras](extras_integration_test.go), [use actions](use_actions_integration_test.go),
[failure injection](failures_integration_test.go),
[legacy save harness](privacy_integration_test.go),
[UI extras](../internal/ui/extras_test.go), and
[async boundaries](../internal/ui/privacy_boundaries_test.go).
Native pipe-level tests continue to cover cancellation, process lifetime,
backup failure, and an already completed save surviving a full lock.

## Limits

All fixtures are disposable vaults. Failure diagnostics redact registered secrets.
Statement coverage excludes the separate terminal application process. Mutation
checks require a passing baseline and test specific guards, not every branch.
Desktop clipboard integration, physical hardware factors, OS-session locking,
Linux desktop behavior, accessibility, and visual polish still need manual checks.
Crash and power-loss behavior is not exhaustively fault-injected.
