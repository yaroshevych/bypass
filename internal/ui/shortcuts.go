package ui

// Workspace shortcuts use the same key values in handlers and palette/help.
// Modal-specific shortcuts remain with the modal that owns them.
const (
	keyNewVault       = "ctrl+n"
	keyOpenVault      = "ctrl+o"
	keyCloseView      = "ctrl+w"
	keyLockAll        = "alt+l"
	keyRedraw         = "ctrl+l"
	keyProtect        = "ctrl+k"
	keyRequests       = "alt+b"
	keyRequestsLegacy = "ctrl+b"
	keyReload         = "ctrl+r"
	keyReadOnly       = "R"
	keyNewEntry       = "n"
	keyEditEntry      = "e"
	keyNewGroup       = "g"
	keyMoveEntry      = "m"
	keyDelete         = "delete"
	keyDeleteAlias    = "ctrl+d"
	keyCopy           = "c"
	keyReveal         = "v"
	keyGenerator      = "ctrl+g"
	keyGrand          = "G"
	keySearch         = "/"
	keyJump           = "J"
	keyZoom           = "z"
	keyNewTab         = "t"
	keyPreviousTab    = "["
	keyNextTab        = "]"
	keyThemes         = "T"
	keyHelp           = "?"
	keyQuit           = "q"
)

type shortcut struct{ key, label string }

var workspaceShortcuts = map[string]shortcut{
	"new-vault": {keyNewVault, "Ctrl+N"}, "open": {keyOpenVault, "Ctrl+O"},
	"close-view": {keyCloseView, "Ctrl+W"}, "lock-all": {keyLockAll, "Alt+L"},
	"protect": {keyProtect, "Ctrl+K"}, "run-requests": {keyRequests, "Alt+B"},
	"reload": {keyReload, "Ctrl+R"}, "read-only": {keyReadOnly, "Shift+R"},
	"new": {keyNewEntry, "n"}, "edit": {keyEditEntry, "e"}, "group": {keyNewGroup, "g"},
	"move": {keyMoveEntry, "m"}, "delete": {keyDelete, "Delete"},
	"copy-focused": {keyCopy, "c"}, "reveal": {keyReveal, "v"}, "generator": {keyGenerator, "Ctrl+G"},
	"grand-view": {keyGrand, "Shift+G"}, "search": {keySearch, "/"}, "jump": {keyJump, "Shift+J"},
	"zoom": {keyZoom, "z"}, "duplicate": {keyNewTab, "t"},
	"previous-tab": {keyPreviousTab, "["}, "next-tab": {keyNextTab, "]"},
	"theme": {keyThemes, "Shift+T"}, "help": {keyHelp, "?"}, "quit": {keyQuit, "q"},
}
