Bypass is a local secrets manager supporting KeePass vaults. The points below describe the terminal and CLI. For the read-only offline viewer, see the [web security notes](web/README.md#security).

- Bypass relies on [gokeepasslib](https://github.com/tobischo/gokeepasslib) to encrypt and save KeePass vaults.
- The optional PIN protects the screen; it does not encrypt or lock the vault.
- Idle timeouts can protect the screen or lock the vault, depending on the settings.
- Vault writes require an encrypted backup; Bypass keeps the five newest backups across all vaults.
- Agent requests require approval; agent identities are self-claimed, and session grants end when the vault locks or Bypass exits.
- Malicious processes running as the same user are outside the security boundary and can inspect or interfere with local processes.
- Memory and crash dumps can contain secrets; Bypass cannot erase every copy from memory.
