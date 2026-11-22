# bypass-web

A read-only KeePass viewer with keyboard workflows from Bypass. Download the built `index.html` and open it directly in a modern browser. The viewer opens KDBX 3 and 4 vaults on your device using a password. It can display live TOTP codes, search entries, and copy selected fields. The page needs no server or network connection.

## Keyboard workflow

| Keys | Action |
| --- | --- |
| `Ctrl+P` or `?` | Open Commands. Use the Commands button if the browser handles `Ctrl+P`. |
| `/` | Filter the current view. Choose Search in Menu to search All Entries. |
| Arrows, Tab, Enter | Navigate panes, groups, entries, fields, and commands |
| `Shift+J` | Jump to a field |
| `c`, `v` | Copy or reveal the focused field |
| `Shift+G` | Open Large View |
| `t`, `[`, `]`, `1`–`9` | Open or switch workspace tabs. Find Close Tab in Commands. |
| `o`, `Shift+L` | Open or lock the vault |
| Esc | Dismiss an overlay or leave search |

The viewer keeps the vault in memory while unlocked. Locking clears the session and its views. Copied secrets may remain on the system clipboard after locking. Entry URLs open when you follow them. The viewer supports Argon2d and Argon2id version 1.3.

The sidebar includes All Entries, Favorites, Recent, Bin, Tags, and Groups. Recent uses vault modification dates and activity from the current unlocked session. The viewer adapts the [shadcn/ui Base Nova sidebar demo](https://ui.shadcn.com/docs/components/base/sidebar) to native HTML and embeds Geist and Geist Mono. It follows your system appearance until you choose a mode with the header switch.

## Build and verify

Use Node.js 22 or newer.

```sh
npm ci
npm run build
npx playwright install chromium
npm test
```

Share `dist/index.html`. It contains all JavaScript, styles, and dependencies. The browser tests open this exact file with networking disabled and synthetic vaults. GitHub Actions also makes the verified file available as a workflow artifact.

## Local debug build

Run `npm run build:debug` and open `dist-debug/index.html`. On each load it
automatically unlocks the embedded `../test-vault.kdbx` fixture with `password`.
Rebuild after editing the fixture. The debug file contains both the fixture and
its password, so keep it local. Git ignores `dist-debug/`.
