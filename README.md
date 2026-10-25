# Bypass Secrets Manager

**Private, modern secrets manager.**

Use your KeePass vaults from the terminal. Enjoy a modern, beautiful interface
where each detail is crafted: navigation, controls, keyboard shortcuts, and themes.

Bypass is using KeePassXC to manage your secrets - a hardened, independently
reviewed open source software for KDBX vaults.

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
