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

`?` lists controls. `1`–`9` switch tabs outside text inputs.
`n`/`e` create/edit; `Ctrl+S` saves; `c` copies;
`v` briefly reveals; `Ctrl+L` locks. Clipboard defaults to 10s, idle lock to 15m.
Secrets stay in memory; backups remain encrypted. Config uses platform/XDG
config storage (`config.json`); recent paths and backups use state storage.
