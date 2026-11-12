# Bypass Secrets Manager

**Private, modern secrets manager.**

Use your KeePass vaults from the terminal. Enjoy a modern, beautiful interface
where each detail is crafted: navigation, controls, keyboard shortcuts, and themes.

Bypass supports KDBX 3.x and 4.x vaults. YubiKey authentication is unsupported.
See the [CLI reference](CLI.md) for compatibility and write behavior.

Beta for Linux and macOS.

<img width="2880" height="1800" alt="Bypass terminal vault interface" src="https://github.com/user-attachments/assets/fbd1676b-00a6-439e-9812-50138bdd63ce" />

## Install

Install the latest release for your OS and CPU architecture:

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

The agent runs the script through Bypass:

```sh
bypass run --env-file .env.deploy -- ./scripts/deploy.sh
```

Approve the request in Bypass. The script receives `DEPLOY_TOKEN` in its
environment; the agent uses the reference without putting the token in its
prompt or command line.

Approve once or grant an agent identity access to those fields until the vault
locks or Bypass exits. Review or clear these in-memory permissions in the TUI.

Agents can set `BYPASS_IDENTITY_TOKEN` to identify themselves across requests
and keep session permissions separate by agent.

Commands keep their terminal behavior and exit status. Locking a vault or closing
Bypass does not interrupt a launched command.

## CLI reference

```text
bypass run [--identity-token NAME-SECRET] [--env-file FILE]... [--timeout DURATION] -- COMMAND [ARGS...]
```

| Option | Behavior |
| --- | --- |
| `--identity-token NAME-SECRET` | Identify an agent across requests. Prefer `BYPASS_IDENTITY_TOKEN`. |
| `--env-file FILE` | Load variables from a file. Repeat to combine files; later files win. |
| `--timeout DURATION` | Limit connection, approval, unlock, and secret delivery; does not limit command runtime. Default: `60s` or config. |
| `--help` | Show command help. |

References can also come from the inherited environment. Files override inherited
variables. The default field is `password`; append `#field` to select another.
Commands and other received paths must not contain symlinks.

See the [full command reference](CLI.md) for reference syntax, environment
rules, permissions, and timeout configuration.

## Security

**Bypass relies on mature gokeepasslib for vault encryption and saving rather than
reimplementing them.**

Each vault write produces a backup; Bypass keeps the five newest across all vaults.

If saving would discard unsupported vault information, Bypass explains what will
be lost and asks for confirmation. Approval applies only to that save, and the
backup must succeed first.

## License

[MIT](LICENSE).
