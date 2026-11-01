# `bypass run`

Resolve references from local KDBX vaults and launch a command with the resulting
environment variables. An agent can invoke this command using references; the
resolved values go to the launched command, not the invoking agent's environment.
Requires a running Bypass TUI. Unlock the required vaults
and approve new access requests there.

## Synopsis

```text
bypass run [--env-file FILE]... [--timeout DURATION] -- COMMAND [ARGS...]
```

| Argument | Description |
| --- | --- |
| `--env-file FILE` | Read an environment file. Repeatable; later files override earlier files. |
| `--timeout DURATION` | Positive duration, such as `30s` or `2m`, for connection, approval, and receipt of secrets. |
| `--help` | Show command help. |
| `COMMAND [ARGS...]` | Executable and arguments, after the required `--` separator. |

## Secret references

```dotenv
API_KEY=bypass://OpenAI
API_KEY=bypass://work.kdbx/Work/OpenAI#password
API_KEY=bypass:///Users/me/vaults/work/Work/OpenAI#password
DATABASE_URL=postgres://${bypass://Database#password}@localhost/db
```

Each line above is an alternative example. A reference identifies an entry and
optionally its vault, group path, and field:

- Omit the vault to search unlocked vaults. If several match, select one in Bypass.
- Qualify the vault with a filename or absolute file path. Relative vault paths
  are unsupported. For an absolute path, the first file encountered identifies
  the vault; subsequent components identify the group and entry. No extension
  is required.
- The field defaults to `password`. Use `#field` for another standard
  entry field. Arbitrary custom fields and attachments are not currently supported.
- Percent-encode reserved characters within names. For references embedded in
  other text, use `${bypass://...}`. Multiple braced references can share a value.

## Environment rules

Bypass scans the inherited environment and specified files. Environment files
override inherited variables; later files override earlier files.

Files accept `KEY=value`, optional single or double quotes around values, blank
lines, and whole-line comments beginning with `#`. Values are literal: there is
no shell expansion. Resolved values are not written back to the files or parent
shell. Every reference must resolve and be authorized before the command starts.

For example, without an environment file:

```sh
GITHUB_TOKEN='bypass://GitHub' bypass run -- ./scripts/publish.sh
```

## Permissions

Approve once, save approval for the executable in the exact current working
directory, or save approval for that executable across directories. A grant
covers a particular vault entry and field. Replacing an executable at the same
path retains its grants. Shells and known interpreters currently support
one-time approval only.

Persistent grants require a writable vault. They are stored as metadata entries
in its **Bypass Run Permissions** group. Select **Saved Run Permissions** in the
command menu to review them. Delete an entry to revoke that grant, or delete the
group to revoke all grants in that vault. Renaming or moving a secret entry
requires fresh approval; changing its value does not.

A batch can use several vaults. If saving a grant fails, the command does not
start. Vault saves are separate operations, so grants already saved can remain;
the failure reports confirmed saves.

## Timeout and process lifetime

The default timeout is 60 seconds. Set `--timeout 2m` for a request, or add
`"run_timeout_seconds": 120` to the Bypass `config.json`. The command-line option
takes precedence. Durations must be positive.

The client owns the timeout. Pending requests can wait for a named vault to be
unlocked until the deadline. Unqualified references search unlocked vaults only.
A timeout, denial, or lost TUI connection before secret delivery prevents launch.

After launch, Bypass never interrupts the process. Vault locking and TUI shutdown
do not affect its runtime or revoke the environment it received. Terminal I/O,
signals, and exit status are preserved. Output is not masked.

## Paths and local authentication

Bypass rejects observable symlinks in vault, executable, recognized script,
environment-file, and working-directory paths. Use the physical executable path
when a command on `PATH` is a symlink. The OS may report a physical working
directory without revealing that the user entered it through a symlink.

IPC uses a private Unix socket, verifies the peer UID, and compares executable
content with the server's Bypass binary. Caller ancestry is informational.
Recipient-specific grants trust the authenticated client to launch its reported
command. These checks do not provide a strong boundary against malicious
same-user processes. The launched process receives plaintext secrets.
