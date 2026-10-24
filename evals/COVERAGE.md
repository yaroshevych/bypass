# Test-hardening gate

Run from the Go module root:

```sh
just verify       # format, vet, race tests, native CLI and tmux journeys
just eval-repeat  # run every terminal journey twice
just mutations    # five deliberately broken guards must fail their tests
just coverage     # statement report for application packages
```

`verify` requires both tmux and keepassxc-cli; it fails if either is missing.
Other individual Go test commands can skip native/platform-specific tests.
Native runtime checked locally: KeePassXC CLI 2.8.0. This is not a claim of
runtime validation on every KeePassXC version.

## Behavior matrix

| Area | Terminal journey | Additional evidence |
|---|---|---|
| Open, picker, wrong password, retry, recent paths, empty/locked views | `TestMilestone1TmuxLifecycle`, `TestNavigationActionsAndSessionCleanupTmux`, `TestMilestone4VaultLifecycleTmux` | CLI opaque errors, bounded/canceled exports; input isolation and UUID model tests |
| Create password/key-file-only vaults; add a password; database info/settings | `TestKeyFileOnlyCreationUnlockAndPasswordAdditionTmux`, `TestMilestone4VaultLifecycleTmux` | Native no-clobber publication, supported settings validation, private permissions |
| Password and key-file add/preserve/remove; unlock with required factors | `TestKeyFileUnlockAndPasswordKeyFileRotationTmux`, `TestMilestone4VaultLifecycleTmux` | Fresh native opens with accepted/rejected factors; encrypted backup opens with old credentials |
| Entry create/edit, multiline notes, generator acceptance/cancel | `TestMilestone2RealCLITmuxWorkflow`, `TestWorkspaceTinyResizePreservesNativeDraftAndGuardsInput` | Native reopen verifies saved fields; editor validation and unsupported fields remain protected |
| Entry/group deletion, cancel, recycle, restore, permanent entry deletion | `TestNativeDeleteCancelRecyclePermanentAndGroupTmux`, `TestMilestone2RealCLITmuxWorkflow` | Ciphertext unchanged on cancel; native reopen checks survivor data and recycled/permanent outcomes |
| Field copy, clipboard timeout, external ownership, quit | `TestClipboardCopyExpiryAndExternalReplacementTmux` | Actual production command driver talks to private file-backed clipboard; controlled-clock ownership/failure tests |
| Grand View: indexes, configured grouping, centering, plain Notes, q close | `TestGrandViewGroupingCenteringNotesAndQCloseTmux`, `TestMilestone3PaletteSearchPrivacyAndResponsiveJourney` | Unicode/control handling, long scrolling, reference resolution and buffer cleanup tests |
| TOTP code display, copy, refresh | `TestTOTPClipboardAndRefreshUseControlledBackendTmux`, `TestMilestone4EntryExtrasNativeTmux` | Marker-controlled CLI codes, clipboard equality, stale-generation/expiry/period-boundary and unsafe-output tests |
| History navigation, multiline values, copy, reveal, remask | `TestHistoryCopyRevealAndRemaskTmux`, `TestMilestone4EntryExtrasNativeTmux` | Timed reveal, protected seed handling, no restore action, long-value scrolling |
| Attachment add/export/replace/remove and safe confirmation defaults | `TestMilestone4EntryExtrasNativeTmux` | Independent binary-byte comparison, 0600 export, no overwrite; stale/ambiguous/failed/canceled native operations |
| Read-only blocks high-impact actions and disk writes | `TestReadOnlyBlocksHighImpactPaletteActionsTmux`, `TestFailedWritesKeepVaultAndDraftAndAllowRetryTmux` | Ciphertext revision and attachment bytes unchanged; writable/native capability guards |
| Command failure, failed backup, read-only file, retry | `TestFailedWritesKeepVaultAndDraftAndAllowRetryTmux` | Failed-operation ciphertext hash unchanged, independent reopen, retained draft, successful repair/retry |
| External edit with draft: keep, blocked save, cancel reload, discard/reload | `TestExternalChangeDraftCancelAndDiscardReloadTmux` | External ciphertext retained; native reopen proves local draft did not overwrite it |
| Multiple vaults, duplicate views, shared saves, per-view search, independent locks | `TestWorkspaceNativeViewsDraftsAndSessionLocks`, `TestMilestone1TmuxLifecycle` | Workspace UUID/credential cleanup tests and distinct master-password authentication |
| Draft Save/Discard/Cancel when navigating | `TestWorkspaceNativeViewsDraftsAndSessionLocks` | Pending navigation only runs after successful save; failed writes retain draft |
| Search, ranking/history, clear filters/history, jump, zoom, resize, ASCII, help, close vault | `TestNavigationActionsAndSessionCleanupTmux`, `TestMilestone3PaletteSearchPrivacyAndResponsiveJourney` | Fuzzy ranking/protected-field exclusion, theme preview rollback, geometry and pointer tests |
| Wide/medium/narrow/minimum; tiny draft preservation | `TestWorkspaceNativeViewsDraftsAndSessionLocks`, `TestWorkspaceTinyResizePreservesNativeDraftAndGuardsInput` | 120×35, 90×25, 60×20, 48×14, 20×5; pure view and input-guard tests |
| Session PIN, wrong PIN, retry, removal, opaque screen | `TestMilestone3PaletteSearchPrivacyAndResponsiveJourney`, `TestNavigationActionsAndSessionCleanupTmux` | Gate rate limiting; all-session idle, protected input and credential-form preservation |
| PIN during native save; full lock cancels a paused save | `TestPINMasksNativeSaveAndFullLockCancelsNextSaveTmux` | Controlled subprocess boundary, clipboard clearing, original hash and independent native reopen |
| Full lock and idle lock reject late open/create/save/copy/reveal/TOTP/export results | `TestMilestone2IdleLockTimeout` plus public UI boundary tests | Channel-controlled operations and clock-driven ticks; check cleared buffers, locked sessions and absent plaintext exports |
| Credential/attachment CLI failure and cancellation | Public UI boundary tests plus native failure tests | Failed backup, wrong credentials, corrupt staging output, read-only, stale revision, FIFO-controlled stalled CLI; original hash/reopen unchanged |
| Unrelated data survives native mutations | Native compatibility round trip | History, OTP, protected custom fields, tags, expiry state and binary attachments survive edits, credential rotation and attachment changes |

Test sources: [terminal harness](tmux_integration_test.go),
[workspace](workspace_integration_test.go), [use actions](use_actions_integration_test.go),
[failure recovery](failures_integration_test.go), [privacy](privacy_integration_test.go),
[navigation](navigation_integration_test.go), [deletion](delete_integration_test.go),
[lifecycle](lifecycle_integration_test.go), [key-file-only creation](creation_integration_test.go), [extras](extras_integration_test.go),
[public UI boundaries](../internal/ui/privacy_boundaries_test.go),
[native failures](../internal/vault/failure_paths_test.go).

## Test strength and limits

- Assertions check saved data/clipboard/exported bytes where applicable, not
  only text on a screen. Polling and channel waits have deadlines. No arbitrary
  fixed delay is used to decide when a UI action is complete.
- Mutation checks use Go source overlays, never modify the checkout, require a
  passing baseline, and count only the expected test failure. They cover five
  specific guards, not exhaustive mutation analysis.
- Statement coverage excludes the separately launched tmux application. It is
  a diagnostic, not a release percentage target or proof of all branch coverage.
- All fixtures are synthetic or disposable vaults. Clipboard commands resolve
  to private test adapters; native desktop clipboard interoperability is not
  established by those adapters. Failure screens redact registered secrets and
  withhold Grand View content while explicit secret display is active.
- Still manual/platform-dependent: physical hardware factors, actual desktop
  clipboard utilities and OS-session locking, Windows terminal behavior,
  terminal-emulator/key-release differences, accessibility and visual polish.
  Crash/power-loss/fsync behavior is not exhaustively fault-injected. New CLI
  versions and additional OS environments need their own runs.
