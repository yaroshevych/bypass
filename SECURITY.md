# Security

Bypass is a local secrets manager for KeePass vaults. The terminal and web interfaces have different security behavior, described below.

## Terminal and CLI

- Bypass uses [gokeepasslib](https://github.com/tobischo/gokeepasslib) to encrypt and save vaults. See the [CLI reference](CLI.md) for vault compatibility and write behavior.
- Vault writes require an encrypted backup. Bypass keeps the five newest backups across all vaults.
- The optional PIN protects the screen; it does not encrypt or lock the vault. Idle timeouts can protect the screen or lock the vault, depending on the settings.
- Agent requests require approval. Agent identities are self-claimed, and session grants end when the vault locks or Bypass exits.

## Web

- The standalone HTML runs locally without a server or network connection. Vault decryption and saving happen in the browser. Links to entry URLs and external sites open when selected.
- Bypass Web uses [kdbxweb](https://github.com/keeweb/kdbxweb) to read and save vaults and [hash-wasm](https://github.com/Daninet/hash-wasm) for Argon2d and Argon2id version 1.3 key derivation. It opens KDBX 3 and 4 vaults using a password.
- Vaults open read-only by default. Editing requires turning off Read only. Save vault as… writes an encrypted file through the browser's save picker or download mechanism. Selecting the original file can overwrite it. The web client does not create the terminal application's automatic backups; keep a separate backup before saving.
- The unlocked vault and credentials remain in browser memory for the session. Locking clears the session and its views, but cannot guarantee every copy is erased. Copied secrets can remain on the system clipboard after locking.
- Browser extensions and other software with access to the browser may be able to inspect or interfere with the page. Use a trusted browser and a trusted copy of the HTML file.
- The local debug build embeds a vault fixture and its password. Keep that build local and use only disposable data in the fixture. See the [web guide](web/README.md#local-debug-build).

## Shared limitations

Malicious processes running as the same user are outside the security boundary and can inspect or interfere with local processes. Memory and crash dumps can contain secrets; Bypass cannot erase every copy from memory.
