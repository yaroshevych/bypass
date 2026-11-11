# Omarchy bar widget

The Bypass widget opens an existing vault from the Omarchy bar. It shows
favorites, searches vault fields, copies values and field references, and
reviews `bypass run` requests. It does not change vault contents.

Build and install the widget with:

```sh
sh scripts/install-omarchy-widget.sh
```

The script installs `bypass` in `~/.local/bin`, installs the widget in the
Omarchy user plugin directory, and adds it to the bar. Ensure `~/.local/bin`
is on your `PATH`. Restart any running Bypass terminal sessions after an
update: the approval protocol verifies that the processes use the same build.

For a first test, use a disposable KeePass vault. Open it from the bar, search
for an entry, and copy a field reference. Start the terminal UI with the same
vault and run a command with a `bypass://` reference in its environment. The
request should appear in both interfaces. Approve or deny it in one and check
that it disappears from the other. Repeat with either interface stopped.

The widget locks after the configured Bypass idle timeout. Copies use the
configured clipboard timeout. The widget must remain in the bar to receive
requests; removing it stops its service.
