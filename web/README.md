# Bypass Web

KeePass client in a single HTML file, offline and portable in any desktop browser.

A KeePass viewer with keyboard workflows from Bypass. It opens vaults read-only by default and can edit entry fields when read-only mode is turned off. Download the built `index.html` and open it directly in a modern browser. The viewer opens KDBX 3 and 4 vaults on your device using a password. It can display live TOTP codes, search entries, and copy selected fields. The page needs no server or network connection.


| <img width="1792" height="1016" alt="dark" src="https://github.com/user-attachments/assets/833bb366-5b0e-4be5-85d8-8426702a902c" /> | <img width="1802" height="1012" alt="light" src="https://github.com/user-attachments/assets/7a8a14d2-8795-497e-bbdd-546a89917605" />
| --- | --- |
| Dark theme | Light theme |

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

The sidebar includes All Entries, Favorites, Recent, Bin, Tags, and Groups. Recent uses vault modification dates and activity from the current unlocked session. The viewer adapts the [shadcn/ui Base Nova sidebar demo](https://ui.shadcn.com/docs/components/base/sidebar) to native HTML and embeds Geist and Geist Mono. It follows your system appearance until you choose a mode with the header switch.

## Security

**Bypass Web opens vaults read-only by default.** To edit entry fields, turn off Read only in Menu, select an entry, and click Edit. Use Save vault as… to save an encrypted copy. Your original file is not changed unless you choose to overwrite it. Browsers without a save picker download the file using their download settings.

The built HTML file runs locally and does not need a server or network connection. Vault decryption and saving happen on your device. Entry URLs open when you follow them.

The viewer uses [kdbxweb](https://github.com/keeweb/kdbxweb) to read and decrypt KeePass vaults and [hash-wasm](https://github.com/Daninet/hash-wasm) for Argon2d and Argon2id version 1.3 key derivation.

The vault remains in memory while unlocked. Locking clears the session and its views; copied secrets may remain on the system clipboard after locking.

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
automatically unlocks the embedded `../../test-vault.kdbx` fixture (relative to `web/`) with `password`.
Rebuild after editing the fixture. The debug file contains both the fixture and
its password, so keep it local. Git ignores `dist-debug/`.

## History

The web project was imported with its original Git history. To inspect commits from before the import, run `git log <web-import-commit>^2 -- src/` from the repository root, using the subtree merge commit as `<web-import-commit>`.
