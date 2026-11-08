# Bypass CLI reference

Agents supply secret references; Bypass resolves them after approval and passes
the values to the launched command without putting them in the agent's prompt.

The command receives plaintext secrets, and its output returns to the agent
unmasked. Avoid printing credentials. This workflow cannot isolate secrets from
malicious processes running as the same user.

## Commands

| Command | Purpose |
| --- | --- |
| `bypass [OPTIONS] [VAULT]` | Open the terminal vault manager and approval interface. |
| `bypass run [OPTIONS] -- COMMAND [ARGS...]` | Resolve references and launch a command with the resulting environment. |
| `bypass --version` | Print the application version. |
| `bypass --help` | Show startup options. |
| `bypass run --help` | Show run options. |

## Vault compatibility

Bypass supports KDBX 3.x and 4.x vaults. YubiKey authentication is unsupported.

If saving would discard unsupported vault information, Bypass lists what will be
lost and asks for confirmation. Approval applies only to that save, and an
encrypted backup must succeed first. This also applies to password changes.

## Agent workflow

Start Bypass and unlock the vault in one terminal:

```sh
bypass work.kdbx
```

If `scripts/deploy.sh` reads `DEPLOY_TOKEN` from its environment, put a reference
in `.env.deploy`:

```dotenv
DEPLOY_TOKEN=bypass://Deployment#password
```

The agent can invoke the script:

```sh
bypass run --env-file .env.deploy -- ./scripts/deploy.sh --target staging
```

Approve in Bypass. The script receives the credential and returns its result;
the agent's environment and `.env.deploy` stay unchanged.

Project instructions can tell an agent which command to use:

> To deploy to staging, run
> `bypass run --env-file .env.deploy -- ./scripts/deploy.sh --target staging`.
> Credentials are supplied by Bypass after user approval. Do not retrieve or
> print their values.

Use `bypass run` for the command that needs credentials. Running the agent itself
this way gives it the resolved environment.

## `bypass run`

### Synopsis

```text
bypass run [--identity NAME-SECRET] [--env-file FILE]... [--timeout DURATION] -- COMMAND [ARGS...]
```

### Arguments and options

| Argument | Description | Default |
| --- | --- | --- |
| `--identity NAME-SECRET` | Select an agent identity for session permissions. Overrides `BYPASS_IDENTITY`; an explicit empty value selects anonymous mode. | `BYPASS_IDENTITY`, otherwise anonymous. |
| `--env-file FILE` | Read variables from an environment file. Repeatable; files are processed in order. | No files; inherited environment only. |
| `--timeout DURATION` | Time allowed to connect to Bypass, approve, unlock, and receive secrets. Examples: `30s`, `2m`, `1m30s`. | `60s`, or the configured value. |
| `--` | Separate Bypass options from the command. Required. | None |
| `COMMAND` | Executable name on `PATH`, or a relative or absolute executable path. | Required. |
| `ARGS...` | Arguments passed to the executable, including its own flags. | None. |
| `--help`, `-h` | Show run help and exit. | None |

Use `--read-only` with the TUI command. For `bypass run`, put options before `--`
and command arguments after it.

An identity has a readable name of 1 to 64 ASCII letters, digits, spaces, `_`,
`.` or `-`, followed by `-` and 32 to 64 hexadecimal characters. Generate a unique
random secret once per agent and reuse the full identity:

```sh
export BYPASS_IDENTITY="deploy-$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
bypass run --env-file .env.deploy -- ./scripts/deploy.sh
```

The full identity is a private bearer credential; no registration is needed.
`--identity` overrides the environment value, and `--identity=` selects anonymous
access. Environment files cannot set `BYPASS_IDENTITY`; Bypass removes it from the
launched command's environment even when no references are used. The approval
screen shows only the readable name.

Prefer the environment variable to avoid exposing the bearer in command
arguments. Other processes running as the same user may still inspect it.

```sh
bypass run --timeout 2m --env-file .env.deploy -- ./scripts/deploy.sh --verbose
```

### Execution

1. Merge the inherited environment with the specified files.
2. Find references in the resulting environment values.
3. Ask the running Bypass TUI to resolve and authorize the batch.
4. Launch the command with the resolved values in its environment.

All references must resolve and be approved before launch; a batch can span
vaults. Resolved values never go into environment files or the parent shell.
References in command arguments and scripts are not resolved. With no environment
references, Bypass launches the command without contacting the TUI.

### Environment precedence

From highest to lowest:

1. Later environment files.
2. Earlier environment files.
3. Inherited environment variables.

```sh
bypass run \
  --env-file .env.base \
  --env-file .env.staging \
  --env-file .env.local \
  -- ./scripts/deploy.sh
```

Here `.env.local` overrides `.env.staging`, which overrides `.env.base`.
Bypass requests every reference left in the merged environment, including those
in inherited variables unrelated to the files. Overridden references are ignored.

There is no automatic `.env` discovery. Specify each file with `--env-file`.

### Environment-file format

```dotenv
# Ordinary values and references can share a file.
DEPLOY_TARGET=staging
DEPLOY_TOKEN=bypass://Deployment
SERVICE_USER='bypass://work.kdbx/Services/Deployment#username'
DATABASE_URL="postgres://${bypass://Database#username}:${bypass://Database#password}@localhost/app"
```

Files support `KEY=value`, blank lines, whole-line comments beginning with `#`,
and optional matching single or double quotes around values. Variable names use
letters, digits, and underscores and must not begin with a digit.

Values are literal: no shell expansion, `$VARIABLE` substitution inside
references, or quoted escape sequences. Trailing comments remain part of the
value; put comments on their own lines.

### Inherited references

An environment file is optional. An agent can supply a reference directly:

```sh
GITHUB_TOKEN='bypass://GitHub' bypass run -- ./scripts/publish.sh
```

Quote references with spaces or shell metacharacters. Use single quotes around
`${bypass://...}` in shell commands to prevent shell interpretation.

## Secret references

### Syntax

```text
bypass://ENTRY[#FIELD]
bypass://GROUP/.../ENTRY[#FIELD]
bypass://VAULT_FILENAME/GROUP/.../ENTRY[#FIELD]
bypass:///ABSOLUTE/VAULT/PATH/GROUP/.../ENTRY[#FIELD]
```

| Component | Behavior |
| --- | --- |
| Vault | Optional. Use a bare filename or an absolute file path. Relative vault paths are unsupported. |
| Group path | Optional. Without it, search by entry name. |
| Entry | Required. Identifies the vault entry. |
| `#FIELD` | Optional. Defaults to `password`. |

Examples:

```text
bypass://GitHub
bypass://Services/Deployment#password
bypass://work.kdbx/Services/Deployment#username
bypass:///Users/me/vaults/work/Services/Deployment#password
```

Vault files need no `.kdbx` extension. In an absolute reference, the first file
found from the filesystem root is the vault; the rest is the group and entry.
The file must exist and be open in Bypass.

Unqualified references search unlocked vaults. Choose in Bypass if several
entries or vaults match. You can unlock a named vault while approval is pending.

### Fields and encoding

Standard field names include `password`, `username`, `title`, `url`, and `notes`.
Arbitrary custom fields and attachments are not currently supported.

Percent-encode reserved characters within individual names. For example, use
`%23` for a literal `#` and `%20` for a space. Path separators remain `/`.

```text
bypass://work.kdbx/Services/Deployment%20Token#password
```

### Embedded references

Use `bypass://...` for a whole value, or `${bypass://...}` within text. Multiple
braced references can share a value:

```dotenv
DATABASE_URL=postgres://${bypass://Database#username}:${bypass://Database#password}@localhost/app
```

Only the reference portions are replaced. Ordinary text is preserved.

## Approval and session permissions

Keep the TUI running. Open requests with `shift+s`, then select one and press
`enter` to review. Approve all once (`a`) asks for confirmation and approves the
currently resolvable requests. Review session permissions (`r`) opens permissions;
Clear all (`c`) removes session access without removing pending requests.

The review dialog shows the requested fields without their values. Use Page Up
and Page Down to scroll. Back is selected by default.

| Decision | Effect |
| --- | --- |
| Deny (`d`) | Reject the request. |
| Approve once (`a`) | Authorize this request only. |
| Approve for this session (`s`) | Allow this identity to access these fields until the vault locks or Bypass exits. |
| Back (`esc`) | Return to Requests without a decision. |

Identities are self-claimed. Session permissions stay in memory and apply to the
chosen identity, vault, entry, and field across commands and directories,
including read-only vaults. Anonymous requests allow one-time approval only.
Existing vault permission entries stay untouched and do not authorize requests.
View active grants through Review session permissions in Secret Requests.

## Timeouts and configuration

The CLI deadline covers connection, approval, unlocking, and secret delivery.
Disconnecting or timing out removes the request from the TUI. The launched
command has no Bypass runtime limit.

Configuration precedence:

1. `--timeout DURATION`.
2. `run_timeout_seconds` in `config.json`.
3. The default of 60 seconds.

On Linux and macOS, configuration is normally in
`~/.config/bypass/config.json`. An absolute `XDG_CONFIG_HOME` changes that location
to `$XDG_CONFIG_HOME/bypass/config.json`.

```json
{
  "run_timeout_seconds": 120
}
```

Configuration requires positive whole seconds. The CLI accepts positive
durations, including fractional seconds. Zero does not disable the deadline.

## Process behavior and exit status

Bypass preserves terminal I/O, signals, and exit status. Once the client receives
the full secret batch, it can launch without the TUI. Locking a vault or closing
Bypass neither interrupts a running command nor retracts delivered credentials.

| Result | Exit behavior |
| --- | --- |
| The command runs | Its exit status is preserved. |
| Invalid arguments, missing command or file, resolution failure, denial, or timeout | Bypass prints an error to stderr and exits with status `1`. |
| Help for either command, or `bypass --version` | Exit successfully without launching a command. |

## Supported paths and security limits

`bypass run` rejects observable symlinks in executable, recognized script,
environment-file, working-directory, and referenced vault paths. Use physical
paths. The OS may report a physical working directory without revealing that
the user entered it through a symlink.

The command receives plaintext secrets, which its descendants can inherit.
Output is unmasked: printed credentials can reach the agent or logs. Approval
controls delivery; it cannot stop the command from storing or sending secrets.

The CLI communicates with Bypass locally. Both must use the same version;
restart the TUI after upgrading.

Bypass cannot guarantee that all copies of secrets are erased from memory or
crash dumps. Agent identities are self-reported, and local checks offer limited
protection against malicious programs running as the same user.

## `bypass`: start the TUI

### Synopsis

```text
bypass [OPTIONS] [VAULT]
```

Put options before the vault path. Omit the path for the welcome screen, or
supply one to open its unlock screen.

### Arguments and options

| Argument | Description | Default |
| --- | --- | --- |
| `VAULT` | Vault file to open. | None. |
| `--read-only` | Open vaults read only. Session and one-time approvals remain available. | Disabled. |
| `--idle-timeout DURATION` | Vault lock timeout without a PIN. Explicit nonzero values must be between `1s` and `24h`. | Configuration, otherwise `15m`. |
| `--clipboard-timeout DURATION` | Clipboard timeout. Explicit nonzero values must be between `1s` and `10m`. | Configuration, otherwise `10s`. |
| `--version` | Print the application version and exit. | None |
| `--help`, `-h` | Show startup help and exit. | None |

For startup timeout flags, `0` uses the configuration or default. It does not
disable timeouts or change the run approval deadline.

```sh
bypass --read-only work.kdbx
bypass --idle-timeout 30m --clipboard-timeout 20s work.kdbx
```

## Troubleshooting

| Problem | Check |
| --- | --- |
| TUI unavailable | Start Bypass as the same user and leave it running. |
| Reference unavailable | Unlock the required vault and check the entry name, group path, and field. |
| Approval timed out | Review the pending request before its deadline, or use a longer `--timeout`. |
| Symlink path rejected | Use the physical executable, file, or working-directory path. |
| Command unavailable | Check `PATH`, the executable path, and execute permissions. |
| Expected variable missing | Name each environment file explicitly and check later files for overrides. |
| Session approval unavailable | Supply a valid named identity; anonymous requests support one-time approval only. |
| IPC executable mismatch | Use the same Bypass build for the TUI and CLI; restart the TUI after updating it. |
