# Bypass Secrets Manager

Manage KeePass vaults from the terminal and approve secrets for commands run by
agents or scripts with `bypass run`.

Bypass opens KDBX 3.x and 4.x vaults without KeePassXC. It uses `gokeepasslib`
v3.6.0 and requires Go 1.24+ to build. YubiKey authentication is unsupported.
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

Agents can use `--identity NAME-SECRET` or `BYPASS_IDENTITY` to keep session
permissions separate by agent.

Commands keep their terminal behavior and exit status. Locking a vault or closing
Bypass does not interrupt a launched command.

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

Bypass reads and writes vaults in process. Vaults and backups stay encrypted on
disk. Each write requires a backup; Bypass keeps the five newest across all vaults.

If a save would discard unsupported XML metadata, Bypass asks for confirmation.
Approval covers only that operation and vault revision. The backup must succeed
before saving.

Only the launched command receives the resolved environment. Output is unmasked,
so a command that prints a secret can expose it to the agent. Locking cannot
retract delivered secrets. Local IPC checks offer limited protection against
malicious processes running as the same user.

## License

[MIT](LICENSE).
