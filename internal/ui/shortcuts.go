package ui

// Workspace shortcuts use the same key values in handlers and palette/help.
// Modal-specific shortcuts remain with the modal that owns them.
const (
	keyNewVault    = "n"
	keyOpenVault   = "o"
	keyCloseView   = "ctrl+w"
	keyLockAll     = "L"
	keyRedraw      = "ctrl+l"
	keyProtect     = "P"
	keyRequests    = "S"
	keyReload      = "ctrl+r"
	keyReadOnly    = "r"
	keyNewEntry    = "n"
	keyEditEntry   = "e"
	keyNewGroup    = "g"
	keyMoveEntry   = "m"
	keyDelete      = "delete"
	keyCopy        = "c"
	keyReveal      = "v"
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
	"open":       {keyOpenVault, "o"},
	"close-view": {keyCloseView, "Ctrl+W"}, "lock-all": {keyLockAll, "Shift+L"},
	"protect": {keyProtect, "Shift+P"}, "run-requests": {keyRequests, "Shift+S"},
	"reload": {keyReload, "Ctrl+R"}, "read-only": {keyReadOnly, "r"},
	"new": {keyNewEntry, "n"}, "edit": {keyEditEntry, "e"}, "group": {keyNewGroup, "g"},
	"move": {keyMoveEntry, "m"}, "delete": {keyDelete, "Delete"},
	"copy-focused": {keyCopy, "c"}, "reveal": {keyReveal, "v"},
	"grand-view": {keyGrand, "Shift+G"}, "search": {keySearch, "/"}, "jump": {keyJump, "Shift+J"},
	"zoom": {keyZoom, "z"}, "duplicate": {keyNewTab, "t"},
	"previous-tab": {keyPreviousTab, "["}, "next-tab": {keyNextTab, "]"},
	"theme": {keyThemes, "Shift+T"}, "quit": {keyQuit, "q"},
}
