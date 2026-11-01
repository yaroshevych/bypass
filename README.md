# Bypass Secrets Manager

**Private, modern secrets manager.**

Use your KeePass vaults from the terminal. Enjoy a modern, beautiful interface
where each detail is crafted: navigation, controls, keyboard shortcuts, and themes.

Bypass is using KeePassXC to manage your secrets - a hardened, independently
reviewed open source software for KDBX vaults.

<img width="2880" height="1800" alt="Image" src="https://github.com/user-attachments/assets/fbd1676b-00a6-439e-9812-50138bdd63ce" />

Bypass is in **beta** for Linux and macOS.

- **Commands within reach.** Press `ctrl+p` to find an action. Press `?` to see
  the keyboard shortcuts.
- **Your choice of style.** Select a theme and a Classic or Compact layout.
- **Simple navigation.** Use the keyboard or mouse. Open vaults in tabs, filter
  entries, and keep the fields you need in view.
- **Passwords when you need them.** Copy a field, reveal a value, or use Large
  View to read individual characters. Create passwords with the password generator.
- **Quick privacy controls.** Lock the interface with a PIN. Press `ctrl+l` to
  lock all vaults.

## Install

Install Go 1.24 or later, Make, and KeePassXC CLI. Make sure `keepassxc-cli` is
on your `PATH`. From the source directory, run:

```sh
make install
```

This command builds Bypass and installs it in `/usr/local/bin`. If that directory
requires administrator access, use `sudo make install`. To install in your home
directory, use `make install PREFIX="$HOME/.local"`. Make sure the installation
directory is on your `PATH`.

## Start

```sh
bypass
# Or open a vault directly:
bypass path/to/vault.kdbx
```

Open a vault or create one from the command menu (`ctrl+p`). No configuration is
necessary. Select **Theme** or **Layout** from the same menu to change the interface.

| Shortcut | Action |
| --- | --- |
| `ctrl+p` | Open commands |
| `?` | Show keyboard shortcuts |
| `/` | Filter entries |
| `n` / `e` | Create / edit an entry |
| `ctrl+s` | Save an entry |
| `c` / `v` | Copy / reveal the selected field |
| `t` | Open a new tab for the current vault |
| `[` / `]` | Navigate tab |
| `ctrl+l` | Lock all vaults |

## Vault protection

Bypass uses KeePassXC CLI to open and change vaults. KeePassXC has undergone
[independent security reviews](https://keepassxc.org/audits/). These reviews apply
to specific KeePassXC versions and platforms; they do not cover Bypass.

Vault files and backups stay encrypted on disk. A strong master password helps
protect stored passwords. When a vault is unlocked, passwords can appear in the
interface.

KeePassXC opens and saves the vault directly. Before an edit, Bypass copies the
encrypted vault file to the backup directory. Bypass keeps the five newest backups
in total across all vaults.

## License

Bypass uses the [MIT License](LICENSE). KeePassXC is a separate program with its
own license.

### Run a command with secrets

Open Bypass and unlock the vault first. Put references in the environment or a
file, then run the command:

```sh
bypass run --env-file .env -- codex
```

```dotenv
API_KEY=bypass://OpenAI#password
DATABASE_URL=postgres://${bypass://work.kdbx/Database#password}@localhost/db
```

Use `#field` for a standard or custom field. The default is `password`. Use
`${bypass://...}` when a reference is part of a larger value. A vault qualifier
can be a filename or an absolute file path, for example
`bypass:///Users/me/vaults/work/Work/OpenAI#password`. Absolute paths do not
require a file extension. Percent-encode reserved characters within names.
Later `--env-file` files override earlier files and the inherited environment.
Environment files support `KEY=value` and single or double quoted values;
values are literal and do not undergo shell expansion.

Press `ctrl+r` in the TUI to view pending requests. Select a request and press
Enter to review it. Use `a` to allow once, `p` to allow in the current directory,
`w` to allow that recipient across directories, or `d` to deny. Choose a vault
in the TUI if a reference has more than one match. Press Esc to return to the
vault, for example to unlock another vault while the request waits.

Persistent grants are metadata entries in the vault's **Bypass Run Permissions**
group. They require a writable vault. From the pending requests screen, press
`g` to open that group in the current vault. Delete a permission entry to revoke
it, or delete the group to revoke all its permissions. A rename or move of the
secret entry requires approval again. Changing its value retains approval.
Shells and known interpreters currently support Allow once only.

The default approval timeout is 60 seconds. Set `--timeout 2m` for one command,
or set `"run_timeout_seconds": 120` in the Bypass `config.json` file. The command
line takes precedence. The timeout must be positive and applies only before
launch. Once launched, **Bypass never interrupts the child**, including when a
vault locks or the TUI closes. The child keeps the environment it received.
Output is not masked.

Only plain paths are supported. Bypass rejects observable symlinks in vault,
command, environment-file and working-directory paths. Use the physical
executable path if a command on PATH is a symlink.

IPC uses a private Unix socket, verifies the peer UID, and compares executable
content with the running Bypass binary. This is best-effort local authentication.
It does not create a strong boundary against malicious software running as the
same user. Recipient permissions trust the authenticated Bypass client to launch
the command it reports. The child receives plaintext secrets in its environment.
