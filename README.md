# KeePassUI

Go terminal workspace for KeePass-compatible vaults. Native edits use encrypted
staging files, backups and external-change checks.
Requires Go 1.24+ and `keepassxc-cli` on PATH.

```sh
just run
# Browse without writes: just run --read-only
# or: go run ./cmd/keepassui [vault.kdbx]
just check
just eval # deterministic terminal integration tests; requires tmux
```

`Ctrl+P` opens commands; `?` lists controls. `1`–`9` switch tabs outside text inputs.
`n`/`e` create/edit; `Ctrl+S` saves; `c` copies;
`G` opens Grand View with numbered characters for the focused field.
`v` briefly reveals; `Ctrl+L` locks the current vault; `L` locks all vaults.
`/` filters the focused pane; `Ctrl+F` searches all entries. `J` starts jump mode;
`z` zooms a pane; `[`/`]` resize it. Choose themes and Compact/Classic in commands.
`Ctrl+K` protects the screen with a session PIN after vault authentication.
Clipboard defaults to 10s; idle protection to 15m (PIN when set, otherwise full lock).
Secrets stay in memory; backups remain encrypted. Config uses platform/XDG
config storage (`config.json`); recent paths and backups use state storage.

Grand View alternates character backgrounds in groups of two. Set
`"grand_view_group_size": 2` in `config.json` to change the group size (positive integer).

`N` creates a vault. Commands include TOTP codes, attachment import/export/replace/remove,
read-only entry history, database info/settings, and password/key-file changes.
Existing cipher/KDF settings are read only in the native CLI; new vaults support
a target unlock delay. Compare/merge is deferred.
