# Bypass Secrets Manager

**Private, modern secrets manager.**

Use your KeePass vaults from the terminal. Enjoy a modern, beautiful interface
where each detail is crafted: navigation, controls, keyboard shortcuts, and themes.

Bypass opens KeePass KDBX vaults directly. It does not need KeePassXC installed.

Use `bypass run` to pass approved secrets to an agent commands or script. You stay in control
with interactive permission TUI, while protecting secrets from exposure to agents.

**Beta for Linux and macOS.**

<img width="2880" height="1800" alt="Bypass terminal vault interface" src="https://github.com/user-attachments/assets/fbd1676b-00a6-439e-9812-50138bdd63ce" />

## Install

Install the latest release for your operating system and CPU architecture:

```sh
curl -fsSL https://raw.githubusercontent.com/yaroshevych/bypass/main/install.sh | bash
```

The installer downloads `bypass`. Set `BINDIR` to change the install directory.
You can also download Bypass archives from
[GitHub Releases](https://github.com/yaroshevych/bypass/releases).

For example, use `curl -fsSL https://raw.githubusercontent.com/yaroshevych/bypass/main/install.sh | BINDIR="$HOME/bin" bash` to install into `~/bin`.

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

**Bypass reads and writes KeePass KDBX vaults in process.**

Vaults and backups remain encrypted on disk. Before a write, Bypass creates an
encrypted backup. It keeps the five newest backups across all vaults.

Only the launched command receives the resolved environment; the invoking
agent does not inherit it. **Output is not masked:** a command that prints a
secret can expose it to the agent. Locking a vault cannot retract secrets already
delivered. Local IPC checks are best-effort authentication, not a strong boundary
against
malicious processes running as the same user.

## License

[MIT](LICENSE).
