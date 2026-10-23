# KeePassUI

Go terminal workspace for KeePass-compatible vaults. Foundation release is read-only.
Requires Go 1.24+ and `keepassxc-cli` on PATH.

```sh
just run
# or: go run ./cmd/keepassui [vault.kdbx]
just check
just eval # deterministic terminal integration tests; requires tmux
```

`?` lists current controls. Passwords stay in memory; only recent file paths
are saved in platform/XDG state storage. Never export plaintext to disk.
