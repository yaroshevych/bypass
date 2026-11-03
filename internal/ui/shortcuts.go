package ui

// Workspace shortcuts use the same key values in handlers and palette/help.
// Modal-specific shortcuts remain with the modal that owns them.
const (
	keyNewVault    = "ctrl+n"
	keyOpenVault   = "ctrl+o"
	keyCloseView   = "ctrl+w"
	keyLockAll     = "ctrl+l"
	keyProtect     = "ctrl+k"
	keyRequests    = "ctrl+b"
	keyReload      = "ctrl+r"
	keyReadOnly    = "R"
	keyNewEntry    = "n"
	keyEditEntry   = "e"
	keyNewGroup    = "g"
	keyMoveEntry   = "m"
	keyDelete      = "ctrl+d"
	keyCopy        = "c"
	keyReveal      = "v"
	keyGenerator   = "ctrl+g"
	keyGrand       = "G"
	keySearch      = "/"
	keyJump        = "J"
	keyZoom        = "z"
	keyNewTab      = "t"
	keyPreviousTab = "["
	keyNextTab     = "]"
	keyThemes      = "T"
	keyHelp        = "?"
	keyQuit        = "q"
)

type shortcut struct{ key, label string }

var workspaceShortcuts = map[string]shortcut{
	"new-vault": {keyNewVault, "Ctrl+N"}, "open": {keyOpenVault, "Ctrl+O"},
	"close-view": {keyCloseView, "Ctrl+W"}, "lock-all": {keyLockAll, "Ctrl+L"},
	"protect": {keyProtect, "Ctrl+K"}, "run-requests": {keyRequests, "Ctrl+B"},
	"reload": {keyReload, "Ctrl+R"}, "read-only": {keyReadOnly, "Shift+R"},
	"new": {keyNewEntry, "n"}, "edit": {keyEditEntry, "e"}, "group": {keyNewGroup, "g"},
	"move": {keyMoveEntry, "m"}, "delete": {keyDelete, "Ctrl+D"},
	"copy-focused": {keyCopy, "c"}, "reveal": {keyReveal, "v"}, "generator": {keyGenerator, "Ctrl+G"},
	"grand-view": {keyGrand, "G"}, "search": {keySearch, "/"}, "jump": {keyJump, "Shift+J"},
	"zoom": {keyZoom, "z"}, "duplicate": {keyNewTab, "t"},
	"previous-tab": {keyPreviousTab, "["}, "next-tab": {keyNextTab, "]"},
	"theme": {keyThemes, "Shift+T"}, "help": {keyHelp, "?"}, "quit": {keyQuit, "q"},
}
