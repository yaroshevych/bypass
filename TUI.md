# Bypass TUI reference

Start the vault manager with `bypass`, or open a vault with
`bypass path/to/work.kdbx`. See [CLI.md](CLI.md) for startup options and
`bypass run` usage.

## Command palette

<img width="1271" height="739" alt="Image" src="https://github.com/user-attachments/assets/6abc6b97-3920-411e-917f-934b32175dc8" />

Press `ctrl+p` from the workspace (including inputs), Large View, or
Requests. Press `?` in the workspace to open it as well. Close other overlays first. Type to filter, move with Up/Down or
Tab/Shift+Tab, and press Enter to select. Esc goes back or closes the palette.
Disabled actions show a reason, such as "Unlock a vault first" or
"Vault is read only".

Zellij's default bindings intercept `ctrl+p` and `ctrl+s`. Configure Zellij to
forward them or use its locked mode. Some terminals send `ctrl+shift+p` and
`ctrl+shift+s` as the same input as `ctrl+p` and `ctrl+s`; this does not reliably
avoid multiplexer bindings.

Workspace shortcuts depend on context; inputs and dialogs have their own
bindings. "None" means there is no dedicated shortcut. Press `ctrl+p` or `?`
to open the searchable Command Palette and see available actions and shortcuts.

<img width="1277" height="735" alt="Image" src="https://github.com/user-attachments/assets/3478d32d-f369-4979-90d8-731cde29d40c" />

## Vaults

| Action | Shortcut | Behavior |
| --- | --- | --- |
| Lock UI with PIN | `shift+p` | Cover the interface with the session PIN. Requires an unlocked vault and a configured PIN. |
| Lock All Vaults | `shift+l` | Lock every unlocked vault and clear their authenticated sessions. Unsaved entry drafts require a decision first. |
| Open Vault… | `o` | Select a vault file and unlock it. |
| Close Vault | None | Close the current vault and its views. |
| New Vault… | `n` on welcome screen | Create a vault. Elsewhere, use the command palette. Requires permission to write. |
| Lock Vault | None | Lock the current vault. Other unlocked vaults remain available. |
| Reload Vault | `ctrl+r` | Reload the current vault, including changes made outside Bypass. |
| Database Info… | None | Show information about the current database. |
| Change Password… | None | Change the current vault's master password. Requires a writable vault. |
| Read Only / Enable Writes | `r` | Change the current vault's write mode, subject to startup and external-change restrictions. |
| Quit bypass | `q`, `ctrl+c` | Close Bypass. Unsaved entry drafts may require a decision first. |
| Pending Secret Requests… | `shift+s` | Review requests from `bypass run`, including the readable name for an identified agent and requests waiting for entry selection or approval. |

### Secret requests

Close the editor or dialog, then press `shift+s` in the workspace. Select a
request and press `enter` to review its fields over the list, without showing
their values. The field list is not selectable.

The request list has Deny all (`d`), Approve all once (`a`), Review session
permissions (`r`), and Close (`esc`). Approve all asks for confirmation; requests
with unavailable secrets or unresolved vault selection still need individual review.

Review actions are Deny (`d`), Approve once (`a`), Approve for this session (`s`),
and Back (`esc`, selected by default). Session approval requires a named identity
and works with read-only vaults. Tab or arrow keys move between buttons;
Page Up and Page Down scroll the fields.

Review session permissions shows active permissions. Clear all (`c`) removes
session access; pending requests stay queued.

### Locking and PIN protection

Change PIN… sets the session PIN. PIN protection covers the interface while
vaults stay unlocked. Full locking clears the vault's decrypted data;
vault credentials are needed to unlock again.

Locking a vault or closing Bypass never interrupts a command already launched
through `bypass run`. That command retains the secrets it received.

### External changes and writes

External changes require a reload before writing, even if you choose to keep
the current view. The external-change dialog requires a decision; Esc cannot
close it.

Each edit requires an encrypted backup. Bypass keeps the five newest across all
vaults. If saving would discard unsupported vault information, Bypass lists what
will be lost and asks you to confirm that save. The backup must succeed first. Read-only
mode, external changes, or an unusable backup location can prevent writes.

## View

| Action | Shortcut | Behavior |
| --- | --- | --- |
| New Tab | `t` | Create another view of the current unlocked vault. It does not create a new vault. |
| Close Tab | `ctrl+w` | Close the current view from the workspace. In text fields, `ctrl+w` deletes the previous word. Close Tab is also available in the command palette. |
| Previous Tab | `[` | Switch to the previous tab. |
| Next Tab | `]` | Switch to the next tab. |
| Search | `/` | Search the current entry list. The palette's Search action starts from All Entries. |
| Zoom | `z` | Toggle zoom for the current view. |

Views of the same vault share its data, write mode, and lock state. Their
selection, filters, and pane positions are independent. Bypass supports up to
nine tabs; number keys `1` to `9` select tabs directly. On the welcome screen,
`delete` or `backspace` removes the selected recent vault from the list.

Select views and groups in navigation, entries in the list, and fields in details.
Use arrow keys, Tab, or the mouse; drag dividers to resize panes. Narrow terminals
show fewer panes without changing the vault or selection.

## Entries

| Action | Shortcut | Behavior |
| --- | --- | --- |
| New Entry… | `n` | Start an entry draft in a writable vault. |
| Edit Entry… | `e` | Edit the selected entry. Requires a writable vault. |
| New Group… | `g` | Create a group in a writable vault. |
| Jump… | `shift+j` | Focus a field of the selected entry. |
| Move Entry… | `m` | Choose a destination group for the selected entry. Requires a writable vault. |
| Delete… | `delete` | Confirm deletion of the selected entry or group. Requires a writable vault. |
| Save Entry | `ctrl+s` | Save the active entry draft. |
| Cancel | `esc` | Leave the entry editor, with a decision for an unsaved draft when needed. |

Jump… accepts `p` for Password, `u` for Username, `w` for URL, `n` for Notes,
and `e` for Email when that field exists. It changes focus without copying or
revealing the value.

Letter shortcuts are case-sensitive: for example, `g` creates a group and
`shift+g` opens Large View. Inputs and dialogs use their own shortcuts.
`ctrl+l` redraws the terminal without changing the current vault or draft.
The `shift+p` and `shift+l` shortcuts do not intercept text input; move focus
to an action or the workspace before using them. Open requests with `shift+s`
from the workspace after closing the editor or dialog.

Deletion uses the vault's recycle bin when applicable. Items already in the bin
are deleted permanently after confirmation. Root and recycle-bin groups have
additional deletion restrictions.

## Secrets

| Action | Shortcut | Behavior |
| --- | --- | --- |
| Copy | `c` | Copy the selected field. Finish editing before copying a stored field. |
| Reveal | `v` | Toggle visibility of the selected field. Passwords are masked by default. |
| Password Generator… | None | Open the password generator from the editor or command palette. |
| Large View… | `shift+g` | Display the selected field in large, individually readable characters. Requires a selected entry and no active entry editor. |

Copy feedback does not include the value. Bypass clears copied values from the
clipboard after the configured timeout. See the startup options in
[CLI.md](CLI.md#bypass-start-the-tui).

## Settings

| Action | Shortcut | Behavior |
| --- | --- | --- |
| Settings… | `shift+t` | Choose a theme or Classic/Compact layout. Switch tabs with `1`/`2`, `[`/`]`, or Left/Right; use Up/Down to select and Enter to save. Esc cancels. |
| Change PIN… | None | Set or change the session PIN. Requires at least one unlocked vault. |

Themes: Macchiato, Mocha, Frappe, Latte, Tokyo Night, Gruvbox, Dracula, Nord,
Vesper, and Omarchy. Omarchy follows system palette changes live. Classic and
Compact have the same actions and controls.

Set `"watch_omarchy_theme": false` in `~/.config/bypass/config.json` to
disable live theme watching.

## Approve commands requested by agents

A request from `bypass run` identifies the command, arguments, working directory,
and secret fields. Select pending requests in any order and resolve vault
ambiguity before approval. Caller ancestry is context; it does not prove which
agent initiated the request.

Deny prevents command launch. Allow once approves only that request. Session
approval lets an identified agent access the chosen fields across commands and
directories until the vault locks or Bypass exits. Permissions stay in memory;
existing permission entries in a vault do not authorize requests. Inspect or
clear grants through Review session permissions in Secret Requests.

Requests expire at the CLI deadline. See
[CLI.md](CLI.md#timeouts-and-configuration) to configure it.

Only the launched command receives the resolved environment. Its output is
unmasked, so printed secrets can reach the agent. See
[CLI.md](CLI.md#supported-paths-and-security-limits) for security limits.
