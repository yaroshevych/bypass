# Bypass Secrets Manager

A terminal secrets manager for local KeePass vaults and agentic workflows.
Browse and edit secrets in the TUI, then let agents run approved commands that
need credentials. The agent uses secret references; Bypass supplies the values
to the command. KeePassXC CLI handles KDBX vault encryption and saving.

**Beta for Linux and macOS.**

<img width="2880" height="1800" alt="Bypass terminal vault interface" src="https://github.com/user-attachments/assets/fbd1676b-00a6-439e-9812-50138bdd63ce" />

## Install

Requires `keepassxc-cli` on your `PATH`.

Download the archive for your operating system and CPU architecture from
[GitHub Releases](https://github.com/yaroshevych/bypass/releases), extract `bypass`,
and place it in a directory on your `PATH`.

To build from source, install Go 1.24+ and Make, then run:

```sh
make install
```

This installs to `/usr/local/bin`. For a user installation, run
`make install PREFIX="$HOME/.local"` and add `~/.local/bin` to your `PATH`.

## Open a vault

```sh
bypass work.kdbx
```

Or run `bypass` to open or create a vault. The command menu (`ctrl+p`) provides
vault actions, themes, and layout settings. See the [TUI reference](TUI.md) for
the available actions.

## Let agents run commands that need secrets

Keep Bypass open with the required vault unlocked. Create an `.env.deploy` file
containing the references needed by your deployment script:

```dotenv
DEPLOY_TOKEN=bypass://Deployment#password
```

The agent can then invoke the script through Bypass:

```sh
bypass run --env-file .env.deploy -- ./scripts/deploy.sh
```

Approve the request in Bypass. The script receives `DEPLOY_TOKEN` through its
environment. The agent needs only the reference and command, without retrieving
the token or including it in the prompt or command line.

Approve a request once, or allow its fields for an agent identity until the
vault locks or Bypass exits. Session permissions stay in memory and can be
reviewed or reset in the TUI.

Agents can use `--identity NAME-SECRET` or `BYPASS_IDENTITY` to keep session
permissions separate by agent.

The command retains its terminal behavior and exit status. Once launched,
**Bypass never interrupts the process**, including when the vault locks or the
TUI closes.

## CLI reference

```text
bypass run [--identity NAME-SECRET] [--env-file FILE]... [--timeout DURATION] -- COMMAND [ARGS...]
```

| Option | Behavior |
| --- | --- |
| `--identity NAME-SECRET` | Scope session permissions to an agent. Defaults to `BYPASS_IDENTITY`. |
| `--env-file FILE` | Load variables from a file. Repeat to combine files; later files win. |
| `--timeout DURATION` | Limit the wait for approval and secrets. Default: `60s`. |
| `--help` | Show command help. |

References can also come from the inherited environment. Files override inherited
variables. The default field is `password`; append `#field` to select another.
Commands and other received paths must not contain symlinks.

See the [full command reference](CLI.md) for reference syntax, environment
rules, permissions, and timeout configuration.

## Security

**Bypass relies on KeePassXC for vault encryption and saving rather than
reimplementing them.**

Vaults and backups remain encrypted on disk. KeePassXC saves vaults directly;
Bypass keeps the five newest encrypted backups across all vaults.

Only the launched command receives the resolved environment; the invoking
agent does not inherit it. **Output is not masked:** a command that prints a
secret can expose it to the agent. Locking a vault cannot retract secrets already
delivered. Local IPC checks are best-effort authentication, not a strong boundary
against
malicious processes running as the same user.

## License

[MIT](LICENSE). KeePassXC is a separate program with its own license.
