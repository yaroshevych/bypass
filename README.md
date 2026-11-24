# Bypass Secrets Manager

**Modern, local secrets manager.**

Bypass TUI: use your KeePass vaults from the terminal. Enjoy a modern, beautiful interface where each detail is crafted: navigation, controls, keyboard shortcuts, and themes.

[Bypass Web](web/README.md): KeePass client in a single HTML file, offline and portable in any desktop browser.

https://github.com/user-attachments/assets/5d8a0287-4225-40f2-9566-9e1c9029e827

10 themes supported, including dynamic Omarchy theme.

| <img width="1006" height="579" alt="compact" src="https://github.com/user-attachments/assets/3833abf8-9bb6-4315-8fb6-82a0ca4296d6" /> | <img width="999" height="578" alt="light" src="https://github.com/user-attachments/assets/fedd1743-7315-48ff-8628-216cec0811a1" /> | <img width="1004" height="582" alt="mocha" src="https://github.com/user-attachments/assets/61abd94f-5606-40bf-9e19-2e00fefe0bdb" />
| --- | --- | --- |
| Vesper theme with Compact layout | Latte theme with Command palette | Mocha theme with Settings dialog |


## Bypass TUI

### Install

Homebrew (default):
```sh
brew install yaroshevych/tap/bypass
```

Other options:
```sh
# Direct install from GitHub releases
curl -fsSL https://raw.githubusercontent.com/yaroshevych/bypass/main/install.sh | bash

#Same, installing into `~/bin`
curl -fsSL https://raw.githubusercontent.com/yaroshevych/bypass/main/install.sh | BINDIR="$HOME/bin" bash

# Build and install from source (requires Go 1.24+ and Make)
make install

#Same, installing into `~/.local/bin`
make install PREFIX="$HOME/.local"
```

### Open a vault

```sh
bypass work.kdbx
```

Or run `bypass` to open or create a vault. The command menu (`ctrl+p`) provides
vault actions, themes, and layout settings. See the [TUI reference](TUI.md) for
the available actions.

### Let agents run commands that need secrets

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

### CLI reference

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

## Bypass Web

| <img width="2176" height="1040" alt="Bypass Web light theme" src="https://github.com/user-attachments/assets/1ad86843-cf99-4dc3-8140-563cad17744b" /> | <img width="2174" height="1038" alt="Bypass Web dark theme" src="https://github.com/user-attachments/assets/5455d8ed-f2a1-4977-a06f-5635ded9c23e" /> |
| --- | --- |
| Light theme | Dark theme |

The web client opens vaults read-only by default. Turn off Read only in Menu and click Edit to change entry fields, then use Save vault as… to save a copy.

Build it with Node.js 22 or newer:

```sh
cd web
npm ci
npm run build
```

Open or share `dist/index.html`. The [web guide](web/README.md) covers its keyboard shortcuts, security, and verification. Download `bypass-web.html` from a [GitHub release](https://github.com/yaroshevych/bypass/releases), or use the web CI workflow artifact.

## Security

**Bypass relies on open-source libraries gokeepasslib, kdbxweb, hash-wasm for vault encryption and saving rather than
reimplementing them.**

Bypass TUI: each vault write produces a backup; Bypass keeps the five newest across all vaults.

If saving would discard unsupported vault information, Bypass explains what will
be lost and asks for confirmation. Approval applies only to that save, and the
backup must succeed first.

See [SECURITY.md](SECURITY.md) and the [web security notes](web/README.md#security) for security assumptions and limitations.

## License

[MIT](LICENSE).
