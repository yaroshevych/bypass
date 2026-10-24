"""Disposable native KeePassXC fixture, never an existing user's vault."""
import base64
import datetime
from pathlib import Path
import shutil
import struct
import subprocess
import xml.etree.ElementTree as ET

MASTER = "visual-demo-passphrase"
PASSWORD = "Visual-Demo-Only-42!"
RECORDS = [
    ("Personal", "Proton Mail", "alex@example.test", "https://mail.proton.me", "Favorite;personal", "Personal email account.\nRecovery codes in this fixture are examples only."),
    ("Personal", "Netflix", "alex@example.test", "https://netflix.com", "personal", "Family streaming account."),
    ("Personal", "Spotify", "alex-listens", "https://spotify.com", "personal", ""),
    ("Personal", "Bank Account", "alex.s", "https://bank.example.test", "finance", "Monthly statements and payments."),
    ("Personal", "Home Wi-Fi", "home-network", "", "home", "Guest access uses a separate password."),
    ("Work", "GitHub", "alex-dev", "https://github.com", "Favorite;work", "Primary development account."),
    ("Work", "Linear", "alex@work.example", "https://linear.app", "work", ""),
    ("Work", "Figma", "alex@work.example", "https://figma.com", "work", ""),
    ("Work", "Cloud Console", "alex-admin", "https://console.example.test", "work", "Use the least-privileged role."),
    ("Work", "Package Registry", "alex-packages", "https://packages.example.test", "work", ""),
]


def build(root: Path) -> tuple[Path, Path]:
    cli = shutil.which("keepassxc-cli")
    if not cli:
        raise RuntimeError("screenshots require keepassxc-cli on PATH")

    def run(*args, stdin=MASTER + "\n"):
        result = subprocess.run([cli, *map(str, args)], input=stdin.encode(), capture_output=True, timeout=30)
        if result.returncode:
            raise RuntimeError(f"synthetic fixture command {args[0]} failed (exit {result.returncode})")
        return result.stdout

    source = root / "source.kdbx"
    run("db-create", "-q", "--set-password", "--decryption-time", "100", source, stdin=MASTER + "\n" + MASTER + "\n")
    for name in ("Personal", "Work", "Archive"):
        run("mkdir", "-q", source, name)
    for group, title, user, url, _, notes in RECORDS:
        args = ["add", "-q", "--password-prompt", "--username", user]
        if url:
            args += ["--url", url]
        if notes:
            args += ["--notes", notes]
        run(*args, source, group + "/" + title, stdin=MASTER + "\n" + PASSWORD + "\n")
    run("edit", "-q", "--username", "alex@example.test", "--notes", "Earlier visual fixture notes.", source, "Personal/Proton Mail")
    attachment = root / "recovery-notes.txt"
    attachment.write_text("Synthetic attachment for visual review. No real recovery codes.\n")
    run("attachment-import", "-q", source, "Personal/Proton Mail", attachment.name, attachment)
    document = ET.fromstring(run("export", "-q", "-f", "xml", source))
    document.find("Meta/DatabaseName").text = "Visual Demo Vault"
    root_group = document.find("Root/Group")
    root_group.find("Name").text = "Passwords"
    epoch = datetime.datetime(1, 1, 1, tzinfo=datetime.timezone.utc)
    fixed = datetime.datetime(2026, 10, 24, 12, tzinfo=datetime.timezone.utc)
    packed = base64.b64encode(struct.pack("<Q", int((fixed - epoch).total_seconds()))).decode()
    for node in document.iter():
        if node.tag in ("CreationTime", "LastModificationTime", "LastAccessTime", "ExpiryTime", "LocationChanged", "DatabaseNameChanged", "DatabaseDescriptionChanged", "DefaultUserNameChanged", "MasterKeyChanged", "SettingsChanged") and node.text:
            node.text = "2026-10-24T12:00:00Z" if "T" in node.text and "-" in node.text else packed
    for number, group in enumerate(root_group.iter("Group"), 1):
        group.find("UUID").text = base64.b64encode(number.to_bytes(16, "big")).decode()
    tags = {record[1]: record[4] for record in RECORDS}
    for number, entry in enumerate(root_group.findall(".//Group/Entry"), 100):
        title = next((field.findtext("Value", "") for field in entry.findall("String") if field.findtext("Key") == "Title"), "")
        identity = base64.b64encode(number.to_bytes(16, "big")).decode()
        entry.find("UUID").text = identity
        for historical in entry.findall("History/Entry"):
            historical.find("UUID").text = identity
        tag = entry.find("Tags")
        if tag is None:
            tag = ET.SubElement(entry, "Tags")
        tag.text = tags.get(title, "")
        if title == "Proton Mail":
            for field in entry.findall("String"):
                if field.findtext("Key") == "Notes":
                    field.find("Value").text = RECORDS[0][5]
            otp = ET.SubElement(entry, "String")
            ET.SubElement(otp, "Key").text = "otp"
            ET.SubElement(otp, "Value", {"ProtectInMemory": "True"}).text = "otpauth://totp/VisualDemo?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&period=30"
    xml_path = root / "fixture.xml"
    ET.ElementTree(document).write(xml_path, encoding="utf-8", xml_declaration=True)
    vault = root / "demo-vault.kdbx"
    run("import", "-q", "--set-password", "--decryption-time", "100", xml_path, vault, stdin=MASTER + "\n" + MASTER + "\n")
    return vault, Path(cli)
