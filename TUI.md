# Bypass TUI reference

The terminal interface manages vaults, entries, and approvals for commands that
request secrets. Start it with `bypass`, or open a vault with
`bypass path/to/work.kdbx`. See [CLI.md](CLI.md) for startup arguments and
`bypass run` usage.

## Command palette

<img width="1271" height="739" alt="Image" src="https://github.com/user-attachments/assets/6abc6b97-3920-411e-917f-934b32175dc8" />

Press `ctrl+p` to open the command palette. Type to filter actions, move with
Up/Down or Tab/Shift+Tab, and press Enter to select. Esc goes back or closes the
palette. Disabled actions show a reason, such as **Unlock a vault first** or
**Vault is read only**.

The following tables follow the palette's sections. Shortcuts apply in the
relevant workspace context; inputs and dialogs have their own bindings. An em
dash means the action has no dedicated shortcut. The **Shortcuts** action opens
the in-app reference.

<img width="1277" height="735" alt="Image" src="https://github.com/user-attachments/assets/3478d32d-f369-4979-90d8-731cde29d40c" />

## Vaults

| Action | Shortcut | Behavior |
| --- | --- | --- |
| Lock UI with PIN | `ctrl+k` | Cover the interface with the session PIN. Requires an unlocked vault and a configured PIN when selected from the palette. |
| Lock All Vaults | `ctrl+l` | Lock every unlocked vault and clear their authenticated sessions. |
| Open Vault… | `ctrl+o` | Select a vault file and unlock it. |
| Close Vault | — | Close the current vault and its views. |
| New Vault… | `shift+n` | Create a vault. Requires backend support and permission to write. |
| Lock Vault | — | Lock the current vault. Other unlocked vaults remain available. |
| Reload Vault | `ctrl+r` | Reload the current vault, including changes made outside Bypass. |
| Database Info… | — | Show information about the current database when supported by the backend. |
| Change Password… | — | Change the current vault's master password. Requires a writable vault and backend support. |
| Read Only / Enable Writes | `shift+r` | Change the current vault's write mode, subject to startup and external-change restrictions. |
| Quit bypass | `q` | Close Bypass. Unsaved entry drafts may require a decision first. |
| Pending Secret Requests… | `ctrl+b` | Review requests from `bypass run`, including the readable name for an identified agent and requests waiting for vault selection or approval. |
| Saved Run Permissions… | — | Open the current vault's saved permission entries for review or revocation. Requires an unlocked vault with saved permissions. |

### Locking and PIN protection

PIN protection covers the interface while authenticated vault sessions remain
open. **Change PIN…** configures the session PIN. Full vault locking clears the
affected decrypted data and closes its authenticated backend session; unlock the
vault with its credentials to use it again.

Locking a vault or closing Bypass never interrupts a command already launched
through `bypass run`. That command retains the secrets it received.

### External changes and writes

If another application changes a vault, reload it before writing again. The
external-change dialog requires a decision and cannot be dismissed with Esc.
Choosing to keep the current view does not remove the reload requirement.

KeePassXC handles vault saving. Before an edit, Bypass copies the encrypted vault
to its backup directory and keeps the five newest backups across all vaults.
Write actions can be unavailable when the vault is read only, has external
changes, or has no usable backup location.

## View

| Action | Shortcut | Behavior |
| --- | --- | --- |
| New Tab | `t` | Create another view of the current unlocked vault. It does not create a new vault. |
| Close Tab | `ctrl+w` | Close the current view. |
| Previous Tab | `[` | Switch to the previous tab. |
| Next Tab | `]` | Switch to the next tab. |
| Search | `/` | Search the current vault's entry list, starting from All Entries. |
| Zoom | `z` | Toggle zoom for the current view. |

Views of the same vault share its data, write mode, and lock state. Their
selection, filters, and pane positions are independent. Bypass supports up to
nine tabs; number keys `1`–`9` select tabs directly.

Use the navigation pane to select views or groups, the entry list to select an
entry, and the details pane to select a field. Arrow keys and Tab move through
the workspace. The mouse can select items and drag pane dividers. Narrow
terminals show fewer panes at once; the underlying vault and selection remain
the same.

## Entries

| Action | Shortcut | Behavior |
| --- | --- | --- |
| New Entry… | `n` | Start an entry draft in a writable vault. |
| Edit Entry… | `e` | Edit the selected entry. Requires a writable vault. |
| New Group… | `g` | Create a group in a writable vault. |
| Jump… | `shift+j` | Focus a field of the selected entry. |
| Move Entry… | `m` | Choose a destination group for the selected entry. Requires a writable vault. |
| Delete… | `ctrl+d` | Confirm deletion of the selected entry or group. Requires a writable vault. |
| Save Entry | `ctrl+s` | Save the active entry draft. |
| Cancel | `esc` | Leave the entry editor, with a decision for an unsaved draft when needed. |

**Jump…** accepts `p` for Password, `u` for Username, `w` for URL, `n` for Notes,
and `e` for Email when that field exists. It changes focus without copying or
revealing the value.

Deletion uses the vault's recycle bin when applicable. Items already in the bin
are deleted permanently after confirmation. Root and recycle-bin groups have
additional deletion restrictions.

## Secrets

| Action | Shortcut | Behavior |
| --- | --- | --- |
| Copy | `c` | Copy the selected field. Finish editing before copying a stored field. |
| Reveal | `v` | Toggle visibility of the selected field. Passwords are masked by default. |
| Password Generator… | `ctrl+g` | Open the password generator. |
| Large View… | `shift+g` | Display the selected field in large, individually readable characters. Requires a selected entry and no active entry editor. |

Copy feedback does not include the value. Password copying uses KeePassXC's
native clipboard command. Clipboard behavior and timeouts are described by the
startup options in [CLI.md](CLI.md#bypass-start-the-tui).

## Settings

| Action | Shortcut | Behavior |
| --- | --- | --- |
| Theme… | `shift+t` | Preview and select a color theme. |
| Layout… | — | Choose Classic or Compact appearance. |
| Change PIN… | — | Set or change the session PIN. Requires at least one unlocked vault. |
| Shortcuts | `?` | Open the searchable shortcut reference. |

Themes: Macchiato, Mocha, Frappe, Latte, Tokyo Night, Gruvbox, Dracula, Nord, and
Vesper. Theme and layout choices preview while selected; Enter saves the choice
and Esc cancels the preview. Classic and Compact use the same actions and
interaction model.

## Approve commands requested by agents

An agent invokes a specific command through `bypass run`. **Pending Secret
Requests…** lists the pending requests; select them in the order you want to
review them. Resolve any vault ambiguity before approving the requested fields.
The request identifies the command, arguments, working directory, and secret
fields. Available caller ancestry is context, not proof of the initiating agent.

| Decision | Scope |
| --- | --- |
| Deny | Reject this batch and prevent command launch. |
| Allow once | Authorize only this request. |
| Allow for this path | Save approval for the executable and requested fields in the exact current working directory. |
| Always allow | Save approval for that executable and those fields across working directories. |

Persistent approval requires writable vaults. Shells and known interpreters
currently support one-time approval only. Requests expire at the CLI's deadline;
see [CLI.md](CLI.md#timeouts-and-configuration) to configure it.

**Saved Run Permissions…** opens the vault's **Bypass Run Permissions** group.
Select a permission entry to inspect its details. Delete an entry to revoke that
grant, or delete the group to revoke all grants in the vault. Renaming or moving
a secret entry requires fresh approval; changing its value retains the grant.

Only the launched command receives the resolved environment. A command that
prints secrets can expose them to the invoking agent because output is not
masked. See [CLI.md](CLI.md#supported-paths-and-security-limits) for the security
limits of this workflow.
