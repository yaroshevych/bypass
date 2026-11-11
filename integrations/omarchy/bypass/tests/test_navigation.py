import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class WidgetNavigationTest(unittest.TestCase):
    def test_password_and_item_navigation(self):
        panel = (Path(__file__).resolve().parents[1] / "Panel.qml").read_text()
        start = panel.index("  function moveList(")
        end = panel.index("\n  function reviewRequest(", start)
        move_list = panel[start:end]
        qml = '''import QtQuick
QtObject {
 id: root
 property string last: ""
 property var recent: []
 property var items: []
 property var fields: ["username", "password"]
 property var panel: QtObject {
  property var loadedContent: QtObject {
   property var passwordField: QtObject { function forceActiveFocus() { root.last = "password" } }
   property var unlockButton: QtObject { function forceActiveFocus() { root.last = "unlock" } }
   property var passwordBackButton: QtObject { function forceActiveFocus() { root.last = "back" } }
   property var fieldsBackButton: QtObject { function forceActiveFocus() { root.last = "fields-back" } }
   property var fieldRepeater: QtObject {
    function itemAt(index) { return index === 0 ? first : second }
    property var first: QtObject { function forceActiveFocus() { root.last = "username" } }
    property var second: QtObject { function forceActiveFocus() { root.last = "password-field" } }
   }
   function ensureVisible(item) {}
  }
 }
''' + move_list + '''
 Component.onCompleted: {
  moveList("password", 0, 1); if (last !== "unlock") throw new Error("password down")
  moveList("password", 1, 1); if (last !== "back") throw new Error("unlock down")
  moveList("password", 2, -1); if (last !== "unlock") throw new Error("back up")
  moveList("password", 1, -1); if (last !== "password") throw new Error("unlock up")
  moveList("fields", 0, 1); if (last !== "username") throw new Error("fields first")
  moveList("fields", 1, 1); if (last !== "password-field") throw new Error("fields down")
  moveList("fields", 2, -1); if (last !== "username") throw new Error("fields up")
  moveList("fields", 1, -1); if (last !== "fields-back") throw new Error("fields back")
  console.log("navigation passed")
  Qt.quit()
 }
}
'''
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory) / "navigation.qml"
            fixture.write_text(qml)
            env = dict(os.environ, QT_QPA_PLATFORM="offscreen", QT_QPA_PLATFORMTHEME="",
                       QT_STYLE_OVERRIDE="Fusion", QT_FORCE_STDERR_LOGGING="1")
            result = subprocess.run(["/usr/lib/qt6/bin/qml", "-f", str(fixture)],
                                    env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("navigation passed", result.stderr)


if __name__ == "__main__":
    unittest.main()
