import hashlib
import pathlib
import subprocess
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("update-homebrew.py")
PLATFORMS = ("darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64")


class UpdateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.formula = self.root / "bypass.rb"
        formula = '  version "0.3.1"\n'
        for platform in PLATFORMS:
            formula += f'  url "https://github.com/yaroshevych/bypass/releases/download/v0.3.1/bypass_0.3.1_{platform}.tar.gz"\n  sha256 "' + '0' * 64 + '"\n'
        self.formula.write_text(formula)
        lines = []
        for platform in PLATFORMS:
            name = f"bypass_0.3.2_{platform}.tar.gz"
            data = platform.encode()
            (self.root / name).write_bytes(data)
            lines.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
        (self.root / "SHA256SUMS").write_text(''.join(lines))

    def run_update(self, tag="v0.3.2"):
        return subprocess.run(["python3", str(SCRIPT), tag, str(self.root), str(self.formula)], capture_output=True, text=True)

    def test_update_repeat_and_downgrade(self):
        self.assertEqual(self.run_update().returncode, 0)
        updated = self.formula.read_text()
        self.assertIn('version "0.3.2"', updated)
        self.assertEqual(self.run_update().returncode, 0)
        self.assertEqual(self.formula.read_text(), updated)
        self.assertNotEqual(self.run_update("v0.3.1").returncode, 0)
        self.assertEqual(self.formula.read_text(), updated)

    def test_invalid_input_and_bad_archive_leave_formula_unchanged(self):
        original = self.formula.read_text()
        self.assertNotEqual(self.run_update("../../bad").returncode, 0)
        (self.root / "bypass_0.3.2_linux_arm64.tar.gz").write_bytes(b"bad")
        self.assertNotEqual(self.run_update().returncode, 0)
        self.assertEqual(self.formula.read_text(), original)


if __name__ == "__main__":
    unittest.main()
