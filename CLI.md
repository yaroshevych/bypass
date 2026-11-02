# Bypass CLI reference

Bypass lets an agent invoke commands that need credentials without retrieving
those credentials into its prompt. The agent supplies secret references; Bypass
resolves them after approval and passes the values to the launched command.

The launched command receives plaintext secrets. Its output returns to the agent
and is **not masked**, so scripts must avoid printing credentials. This workflow
does not isolate secrets from malicious processes running as the same user.

## Commands

| Command | Purpose |
| --- | --- |
| `bypass [OPTIONS] [VAULT]` | Open the terminal vault manager and approval interface. |
| `bypass run [OPTIONS] -- COMMAND [ARGS...]` | Resolve references and launch a command with the resulting environment. |
| `bypass --version` | Print the application version. |
| `bypass --help` | Show startup options. |
| `bypass run --help` | Show run options. |

## Agent workflow

Start Bypass and unlock the vault in one terminal:

```sh
bypass work.kdbx
```

Suppose a project's `scripts/deploy.sh` reads `DEPLOY_TOKEN` from its environment.
Create `.env.deploy` with a reference instead of a credential:

```dotenv
DEPLOY_TOKEN=bypass://Deployment#password
```

The agent can invoke the script:

```sh
bypass run --env-file .env.deploy -- ./scripts/deploy.sh --target staging
```

Approve the request in Bypass. Only the launched command receives the resolved
environment; the invoking agent's environment and `.env.deploy` are unchanged.
The script can use the credential and return a deployment result to the agent.

Project instructions can tell an agent which command to use:

> To deploy to staging, run
> `bypass run --env-file .env.deploy -- ./scripts/deploy.sh --target staging`.
> Credentials are supplied by Bypass after user approval. Do not retrieve or
> print their values.

Launch the specific command that needs credentials through Bypass. Launching the
agent itself through `bypass run` would give that agent the resolved environment.

## `bypass run`

### Synopsis

```text
bypass run [--env-file FILE]... [--timeout DURATION] -- COMMAND [ARGS...]
```

### Arguments and options

| Argument | Description | Default |
| --- | --- | --- |
| `--env-file FILE` | Read variables from an environment file. Repeatable; files are processed in order. | No files; inherited environment only. |
| `--timeout DURATION` | Positive duration for IPC connection, approval, unlocking, and receipt of secrets. Examples: `30s`, `2m`, `1m30s`. | `60s`, or the configured value. |
| `--` | Separate Bypass options from the command. Required. | — |
| `COMMAND` | Executable name on `PATH`, or a relative or absolute executable path. | Required. |
| `ARGS...` | Arguments passed to the executable, including its own flags. | None. |
| `--help`, `-h` | Show run help and exit. | — |

Startup options such as `--backend` and `--read-only` belong to the TUI command,
not to `bypass run`. Put run options before `--` and command arguments after it.

```sh
bypass run --timeout 2m --env-file .env.deploy -- ./scripts/deploy.sh --verbose
```

### Execution

1. Merge the inherited environment with the specified files.
2. Find references in the resulting environment values.
3. Ask the running Bypass TUI to resolve and authorize the batch.
4. Launch the command with the resolved values in its environment.

Every requested reference must resolve and be authorized before launch. A batch
can use multiple vaults. Bypass does not write resolved values to the environment
files or parent shell. It does not resolve references in command arguments or
script contents. If the environment contains no references, it launches the
command without contacting the TUI.

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

Here `.env.local` overrides `.env.staging`, which overrides `.env.base`. A
reference in an overridden value is not requested. All references remaining in
the merged environment are requested, including inherited variables unrelated
to the supplied files.

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

Values are literal. Bypass does not perform shell expansion, expand `$VARIABLE`
inside a reference, or interpret quoted escape sequences. Use whole-line
comments; trailing comments are not stripped from values.

### Inherited references

An environment file is optional. An agent can supply a reference directly:

```sh
GITHUB_TOKEN='bypass://GitHub' bypass run -- ./scripts/publish.sh
```

Quote references containing spaces or shell metacharacters. In particular, use
single quotes around `${bypass://...}` values in shell commands to prevent the
shell from interpreting them before Bypass runs.

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

A vault file does not need a `.kdbx` extension. For an absolute reference, Bypass
walks from the filesystem root until it finds a file; the remaining components
identify the group and entry. A missing file or a file that is not an open vault
fails resolution.

Unqualified references search unlocked vaults. When several entries or vaults
match, choose the intended match in Bypass. A named locked vault can be unlocked
while the request waits for approval.

### Fields and encoding

Standard field names include `password`, `username`, `title`, `url`, and `notes`.
Arbitrary custom fields and attachments are not currently supported.

Percent-encode reserved characters within individual names. For example, use
`%23` for a literal `#` and `%20` for a space. Path separators remain `/`.

```text
bypass://work.kdbx/Services/Deployment%20Token#password
```

### Embedded references

A standalone value can use `bypass://...` directly. A reference inside other text
must use `${bypass://...}`. A value can contain multiple braced references:

```dotenv
DATABASE_URL=postgres://${bypass://Database#username}:${bypass://Database#password}@localhost/app
```

Only the reference portions are replaced. Ordinary text is preserved.

## Approval and saved permissions

The TUI must be running when a request needs secret resolution. Review pending
requests in Bypass. The request identifies the recipient command, arguments,
working directory, and requested fields. Caller ancestry, when available, is
informational; it does not prove which agent initiated the request.

| Decision | Effect |
| --- | --- |
| Deny | Reject the batch; do not launch the command. |
| Allow once | Authorize this request only. |
| Allow for this path | Remember access for this executable in the exact current working directory. |
| Always allow | Remember access for this executable across working directories. |

Each saved grant covers a particular vault entry and field. Executables are
identified by their absolute paths. Replacing an executable at the same path
retains its grants. Shells and known interpreters currently support one-time
approval only.

Persistent grants require a writable vault and are saved in its **Bypass Run
Permissions** group. Use **Saved Run Permissions** in the command menu to review
them. Delete a grant entry to revoke it, or delete the group to revoke all grants
in that vault. Renaming or moving a secret entry requires fresh approval;
changing its value does not.

Saves across vaults are not atomic. If saving any required grant fails, the
command does not launch, but earlier saves can remain. The failure reports
confirmed saves.

## Timeouts and configuration

The timeout applies to connection, approval, unlocking, and secret delivery. It
does not limit the launched command's runtime. The CLI owns the deadline;
disconnecting or timing out removes its pending request from the TUI.

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

The configuration value is a positive whole number of seconds. The CLI option
accepts positive durations, including fractional seconds. Zero does not disable
the deadline. A grant save already in progress can finish after a client timeout;
review saved permissions if you need to revoke it.

## Process behavior and exit status

Bypass preserves terminal I/O, signals, and the command's exit status. Once the
complete secret batch reaches the client, launch no longer depends on the TUI.
**Bypass never interrupts a running command**, including when a vault locks or
the TUI closes. Locking cannot retract credentials already delivered.

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

Secrets are plaintext in the launched command's environment. Its descendants can
inherit them. Output is not masked: a script that prints a credential can expose
it to the invoking agent or logs. Approval governs secret delivery; it does not
sandbox the recipient or prevent it from writing or transmitting credentials.

IPC uses a private Unix socket, peer UID checks, and executable-content checks.
Vault-resolved values use byte buffers during resolution, transfer, and
substitution. Bypass clears these buffers after use and on failed delivery.
The final process launch requires Go strings. These strings cannot be cleared
in place. A successful launch replaces the runner process memory. Literal
values from inherited environment variables and environment files still use
the string-based environment parser. Buffer clearing does not guarantee that
Go runtime, operating system, or crash-dump copies are erased.

The response protocol requires the TUI and runner to use the same version.
Restart the TUI after upgrading Bypass.

Recipient-specific grants trust the authenticated Bypass client to launch the
reported command. These checks are best-effort local authentication, not a
strong security boundary against malicious same-user processes.

## `bypass`: start the TUI

### Synopsis

```text
bypass [OPTIONS] [VAULT]
```

Put options before the optional vault path. Without a path, Bypass opens its
welcome screen. With a path, it opens the vault's unlock screen.

### Arguments and options

| Argument | Description | Default |
| --- | --- | --- |
| `VAULT` | Vault file to open. | None. |
| `--backend EXECUTABLE` | KeePassXC CLI executable to use. | `keepassxc-cli`. |
| `--read-only` | Open vaults read only. Persistent grants cannot be saved; one-time approval remains available. | Disabled. |
| `--idle-timeout DURATION` | Vault lock timeout without a PIN. Explicit nonzero values must be between `1s` and `24h`. | Configuration, otherwise `15m`. |
| `--clipboard-timeout DURATION` | Clipboard timeout. Explicit nonzero values must be between `1s` and `10m`. | Configuration, otherwise `10s`. |
| `--log-file FILE` | Write private vault-open diagnostics to a file with mode `0600`. | No diagnostic file. |
| `--version` | Print the application version and exit. | — |
| `--help`, `-h` | Show startup help and exit. | — |

For the two startup timeout flags, `0` selects the configured/default value; it
does not disable the timeout. These flags do not change the run approval timeout.

```sh
bypass --read-only work.kdbx
bypass --idle-timeout 30m --clipboard-timeout 20s work.kdbx
bypass --backend /opt/keepassxc/bin/keepassxc-cli work.kdbx
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
| Persistent approval unavailable | Check that the vault is writable and the recipient is not a shell or known interpreter. |
| IPC executable mismatch | Use the same Bypass build for the TUI and CLI; restart the TUI after updating it. |
