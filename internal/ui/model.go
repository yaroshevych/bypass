package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/platform"
	"bypass/internal/privacy"
	"bypass/internal/vault"
	"bypass/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

type RecentStore interface {
	LoadRecent() ([]string, error)
	Remember(string) error
	Remove(string) error
}

type Options struct {
	Backend      vault.Backend
	Store        RecentStore
	InitialPath  string
	Context      context.Context
	Settings     platform.Settings
	Clipboard    *clipboard.Manager
	ReadOnly     bool
	Now          func() time.Time
	Warning      string
	SaveSettings func(platform.Settings) error
}

type screen int

const (
	browse screen = iota
	chooseFile
	unlock
)

type Model struct {
	hoverTab                                int
	tabHovered                              bool
	lifecycle                               *lifecycleState
	extras                                  *extraState
	styles                                  styleSet
	palette                                 *paletteState
	jump                                    bool
	grand                                   bool
	grandOffset                             int
	gate                                    *privacy.Gate
	pinInput                                input
	pinError                                string
	pinFullLock                             bool
	saveSettings                            func(platform.Settings) error
	helpQuery                               input
	searchColumn                            int
	searchError                             string
	dragging                                int
	dragStartX                              int
	pickerReturn                            *paletteState
	searchCache                             *entrySearchCache
	searchOrigin                            *workspace.View
	backend                                 vault.Backend
	store                                   RecentStore
	ctx                                     context.Context
	ws                                      *workspace.Workspace
	width, height                           int
	screen                                  screen
	inputs                                  [3]input // password/path, key file, hardware slot
	inputFocus                              int
	noPassword                              bool
	path, directory                         string
	files                                   []fileItem
	fileIndex, fileOffset                   int
	pickerEditing                           bool
	recent                                  []string
	recentIndex                             int
	err, notice                             string
	busy                                    bool
	cancel                                  context.CancelFunc
	request                                 uint64
	searching                               bool
	query                                   input
	previousQuery                           string
	help                                    bool
	helpOffset                              int
	backendVersion                          string
	backendCapabilities                     map[string]bool
	backendMissing                          bool
	settings                                platform.Settings
	clipboard                               *clipboard.Manager
	readOnly                                bool
	now                                     func() time.Time
	editor                                  *entryEditor
	editorSession                           string
	dialog                                  *actionDialog
	afterSave                               func() tea.Cmd
	operation                               uint64
	operationCancel                         context.CancelFunc
	operationSession                        string
	writing                                 bool
	clipGeneration                          uint64
	clipCancel                              context.CancelFunc
	clipboardExpires                        time.Time
	noticeExpires                           time.Time
	revealEntry, revealField, revealSession string
	revealUntil                             time.Time
	revealValue                             []byte
	revealGeneration                        uint64
	revealCancel                            context.CancelFunc
	checking                                map[string]bool
	changeDismissed                         map[string]bool
	ticks                                   uint64
	cleanupErr                              error
}

type fileItem struct {
	name, path string
	dir        bool
}
type openedMsg struct {
	request     uint64
	db          *vault.Database
	err         error
	credentials vault.Credentials
}
type recentMsg struct {
	paths []string
	err   error
}
type rememberedMsg struct{ err error }
type probedMsg struct {
	version string
	err     error
}
type capabilitiesMsg struct {
	commands map[string]bool
	err      error
}
type listingMsg struct {
	directory string
	files     []fileItem
	err       error
}
type navItem struct {
	label, group string
	depth        int
	kind         string
	value        string
	key          string
	record       *workspace.SearchRecord
}

func New(opts Options) *Model {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	m := &Model{backend: opts.Backend, store: opts.Store, ctx: ctx, ws: workspace.New(), width: 100, height: 30}
	m.now = opts.Now
	if m.now == nil {
		m.now = time.Now
	}
	m.settings = opts.Settings
	if m.settings.GrandViewGroupSize <= 0 {
		m.settings.GrandViewGroupSize = 2
	}
	if m.settings.Theme == "" {
		m.settings.Theme = "macchiato"
	}
	if m.settings.Appearance == "" {
		m.settings.Appearance = "classic"
	}
	m.styles = stylesForTheme(m.settings.Theme)
	m.gate = privacy.New()
	m.saveSettings = opts.SaveSettings
	if m.settings.IdleTimeout <= 0 {
		m.settings.IdleTimeout = 15 * time.Minute
	}
	if m.settings.ClipboardTimeout <= 0 {
		m.settings.ClipboardTimeout = 10 * time.Second
	}
	m.clipboard = opts.Clipboard
	m.readOnly = opts.ReadOnly
	m.notice = opts.Warning
	m.checking = make(map[string]bool)
	m.changeDismissed = make(map[string]bool)
	if opts.InitialPath != "" {
		m.path = expandPath(opts.InitialPath)
		m.screen = unlock
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, nextTick())
	if m.store != nil {
		store := m.store
		cmds = append(cmds, func() tea.Msg { paths, err := store.LoadRecent(); return recentMsg{paths, err} })
	}
	if backend, ok := m.backend.(interface {
		Probe(context.Context) (string, error)
	}); ok {
		ctx := m.ctx
		cmds = append(cmds, func() tea.Msg { version, err := backend.Probe(ctx); return probedMsg{version, err} })
	}
	if backend, ok := m.backend.(interface {
		CommandCapabilities(context.Context) (map[string]bool, error)
	}); ok {
		ctx := m.ctx
		cmds = append(cmds, func() tea.Msg {
			commands, err := backend.CommandCapabilities(ctx)
			return capabilitiesMsg{commands, err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) Close() {
	m.clearExtras()
	m.clearLifecycle()
	m.closePalette(false)
	m.gate.Clear()
	m.pinInput.clear()
	m.helpQuery.clear()
	m.searchCache = nil
	m.searchOrigin = nil
	m.pickerReturn = nil
	m.cancelOperation()
	m.clearEditor()
	m.clearDialog()
	m.hideReveal()
	if m.clipCancel != nil {
		m.clipCancel()
	}
	m.clipGeneration++
	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	if err := m.clipboard.Clear(ctx); err != nil {
		m.cleanupErr = err
	}
	cancel()
	m.request++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	for i := range m.inputs {
		m.inputs[i].clear()
	}
	m.query.clear()
	m.previousQuery = ""
	m.ws.Close()
}

func (m *Model) CleanupError() error { return m.cleanupErr }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.searchCache = nil
	if cmd, ok := m.updateLifecycle(msg); ok {
		m.adjustViewport()
		return m, cmd
	}
	if cmd, ok := m.updateExtras(msg); ok {
		m.adjustViewport()
		return m, cmd
	}
	if cmd, ok := m.updateAdvanced(msg); ok {
		m.adjustViewport()
		return m, cmd
	}
	if cmd, ok := m.updateOperation(msg); ok {
		m.adjustViewport()
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.hideReveal()
		m.width, m.height = msg.Width, msg.Height
	case capabilitiesMsg:
		if msg.err == nil {
			m.backendCapabilities = msg.commands
		}
	case probedMsg:
		m.backendVersion = safe(msg.version)
		m.backendMissing = msg.err != nil
	case recentMsg:
		m.recent = msg.paths
		m.recentIndex = min(m.recentIndex, max(0, len(m.recent)-1))
		if msg.err != nil {
			m.notice = "Recent vaults unavailable; Open Vault still works."
		}
	case rememberedMsg:
		if msg.err != nil {
			m.notice = "Vault opened; recent-file state could not be saved."
		}
	case listingMsg:
		if m.screen != chooseFile || msg.directory != m.directory {
			break
		}
		m.files, m.fileIndex, m.fileOffset = msg.files, 0, 0
		if msg.err != nil {
			m.err = "Cannot list this directory. Enter a vault path directly."
		}
	case openedMsg:
		if msg.request != m.request {
			msg.credentials.Clear()
			if msg.db != nil {
				msg.db.Clear()
			}
			break
		}
		m.busy = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		if msg.err != nil {
			m.err = "Could not unlock. Check credentials, file access and backend support."
			msg.credentials.Clear()
			if errors.Is(msg.err, vault.ErrUnavailable) {
				m.err = "Could not unlock. Install KeePassXC or set --backend executable."
			}
			if errors.Is(msg.err, vault.ErrInvalidPath) {
				m.err = "Could not unlock. Vault file is missing or unreadable; choose another path."
			}
			if errors.Is(msg.err, vault.ErrMalformed) {
				m.err = "Could not unlock. Vault export is unsupported or malformed."
			}
			if errors.Is(msg.err, vault.ErrTooLarge) {
				m.err = "Could not unlock. Vault export exceeds the 32 MiB safety limit."
			}
			break
		}
		if msg.db == nil {
			msg.credentials.Clear()
			m.err = "Could not unlock. Backend returned no database."
			break
		}
		if !m.ws.CanOpen(msg.db.Path) {
			msg.db.Clear()
			msg.credentials.Clear()
			m.err = "Maximum 9 tabs. Close a tab before opening another vault."
			break
		}
		reused := m.ws.Open(msg.db)
		session := m.ws.ActiveSession()
		if session != nil {
			m.ws.SetCredentials(session.ID, msg.credentials)
			session.LastActivity = m.now()
			if !reused {
				session.ReadOnly = m.readOnly
				if info, err := os.Stat(session.Path); err == nil && info.Mode().Perm()&0222 == 0 {
					session.ReadOnly = true
				}
			}
		}
		msg.credentials.Clear()
		m.screen, m.err, m.notice = browse, "", ""
		m.restoreGroup()
		m.normalize()
		if m.store != nil {
			path := msg.db.Path
			return m, func() tea.Msg {
				err := m.store.Remember(path)
				paths, loadErr := m.store.LoadRecent()
				if err != nil {
					return rememberedMsg{err}
				}
				return recentMsg{paths, loadErr}
			}
		}
	case tea.MouseMsg:
		cmd := m.pointer(msg)
		if msg.Action != tea.MouseActionMotion {
			m.touchActive()
		}
		m.adjustViewport()
		return m, cmd
	case tea.KeyMsg:
		m.tabHovered = false
		cmd := m.key(msg)
		m.touchActive()
		m.adjustViewport()
		return m, cmd
	}
	m.adjustViewport()
	return m, nil
}

func (m *Model) touchActive() {
	m.syncPrivacyState()
	if s := m.ws.ActiveSession(); s != nil && s.DB != nil && m.screen == browse && !m.gate.Protected() {
		s.LastActivity = m.now()
	}
}

func (m *Model) key(k tea.KeyMsg) tea.Cmd {
	m.searchCache = nil
	key := k.String()
	if m.layout().Mode == "tiny" {
		if key == "ctrl+c" {
			m.Close()
			return tea.Quit
		}
		if key == "q" && !m.gate.Protected() && (m.editor == nil || !m.editor.dirty()) {
			m.Close()
			return tea.Quit
		}
		return nil
	}
	if m.gate.Protected() {
		return m.pinKey(k)
	}
	if key == "ctrl+k" {
		return m.protectScreen()
	}
	if key == "ctrl+l" && m.screen == unlock {
		m.cancelPendingOpen()
		return m.lockCurrent(false)
	}
	if m.dialog != nil {
		return m.dialogKey(k)
	}
	if m.lifecycle != nil {
		return m.lifecycleKey(k)
	}
	if cmd, handled := m.extrasKey(k); handled {
		return cmd
	}
	if m.grand {
		return m.grandKey(k)
	}
	if m.palette != nil {
		return m.paletteKey(k)
	}
	if m.jump {
		return m.jumpKey(k)
	}
	if m.dialog == nil && m.screen == browse && !m.writing {
		if key == "ctrl+p" {
			m.openPalette("actions", false)
			return nil
		}
		if key == "ctrl+k" {
			return m.protectScreen()
		}
	}
	if key != "v" {
		m.hideReveal()
	}
	if s := m.ws.ActiveSession(); s != nil && s.DB != nil && m.screen == browse {
		s.LastActivity = m.now()
	}
	if m.dialog != nil {
		return m.dialogKey(k)
	}
	if key == "ctrl+c" {
		return m.leaveEditor(func() tea.Cmd { m.Close(); return tea.Quit })
	}
	if m.writing {
		if key == "ctrl+l" || key == "L" {
			return m.lockCurrent(key == "L")
		}
		return nil
	}
	if m.editor != nil {
		return m.editorKey(k)
	}
	if m.help {
		switch key {
		case "up", "k":
			m.helpOffset = max(0, m.helpOffset-1)
		case "down", "j":
			m.helpOffset++
		case "pgup":
			m.helpOffset = max(0, m.helpOffset-8)
		case "pgdown":
			m.helpOffset += 8
		default:
			m.helpQuery.update(k)
			m.helpOffset = 0
		}
		if key == "esc" || key == "?" || key == "enter" {
			m.help = false
		}
		return nil
	}
	if m.screen == chooseFile {
		return m.fileKey(k)
	}
	if m.screen == unlock {
		return m.unlockKey(k)
	}
	if m.searching {
		return m.searchKey(k)
	}
	switch key {
	case "N", "ctrl+n":
		return m.leaveEditor(m.openCreateVault)
	case "q":
		m.Close()
		return tea.Quit
	case "ctrl+o":
		return m.openPicker()
	case "?":
		m.hideReveal()
		m.help = true
		m.helpOffset = 0
		m.helpQuery.clear()
		return nil
	case "ctrl+t":
		return m.duplicateTab()
	case "ctrl+pgup":
		if n := len(m.ws.Views); n > 0 {
			m.ws.Switch((m.ws.Active + n - 1) % n)
			m.normalize()
		}
		return nil
	case "ctrl+pgdown":
		if n := len(m.ws.Views); n > 0 {
			m.ws.Switch((m.ws.Active + 1) % n)
			m.normalize()
		}
		return nil
	case "ctrl+w":
		m.hideReveal()
		m.ws.CloseView()
		m.err = ""
		m.normalize()
		return m.clearClipboard()
	case "ctrl+l":
		return m.lockCurrent(false)
	case "L":
		return m.lockCurrent(true)
	}
	tabKey := strings.TrimPrefix(key, "alt+")
	if len(tabKey) == 1 && tabKey[0] >= '1' && tabKey[0] <= '9' {
		m.ws.Switch(int(tabKey[0] - '1'))
		m.normalize()
		m.err = ""
		return nil
	}
	session := m.ws.ActiveSession()
	if session == nil {
		switch key {
		case "o", "O", "enter":
			if key == "enter" && len(m.recent) > 0 {
				m.beginUnlock(m.recent[m.recentIndex])
				return nil
			}
			return m.openPicker()
		case "up", "k":
			m.recentIndex = max(0, m.recentIndex-1)
		case "down", "j":
			m.recentIndex = min(max(0, len(m.recent)-1), m.recentIndex+1)
		case "delete":
			if len(m.recent) > 0 && m.store != nil {
				path := m.recent[m.recentIndex]
				return func() tea.Msg {
					if err := m.store.Remove(path); err != nil {
						return rememberedMsg{err}
					}
					paths, err := m.store.LoadRecent()
					return recentMsg{paths, err}
				}
			}
		}
		return nil
	}
	if session.DB == nil {
		if key == "enter" {
			m.beginUnlock(session.Path)
		}
		return nil
	}
	v := m.ws.ActiveView()
	if cmd, handled := m.operationKey(k); handled {
		return cmd
	}
	if key == "tab" || key == "shift+tab" || key == "up" || key == "down" || key == "left" || key == "right" || key == "enter" || strings.HasPrefix(key, "alt+") {
		m.hideReveal()
	}
	switch key {
	case "o", "O":
		return m.openPicker()
	case "tab":
		v.DetailManual = false
		if v.Focus == 2 && v.FieldIndex < len(m.fieldNames())-1 {
			v.FieldIndex++
		} else {
			v.Focus = (v.Focus + 1) % 3
			v.FieldIndex = 0
		}
	case "shift+tab":
		v.DetailManual = false
		if v.Focus == 2 && v.FieldIndex > 0 {
			v.FieldIndex--
		} else {
			v.Focus = (v.Focus + 2) % 3
			if v.Focus == 2 {
				v.FieldIndex = max(0, len(m.fieldNames())-1)
			}
		}
	case "left", "h":
		v.Focus = max(0, v.Focus-1)
	case "right", "l":
		v.Focus = min(2, v.Focus+1)
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		if v.Focus == 2 {
			v.DetailManual = true
			v.DetailOffset = max(0, v.DetailOffset-max(1, m.height-10))
		} else {
			m.move(-max(1, m.height-10))
		}
	case "pgdown":
		if v.Focus == 2 {
			v.DetailManual = true
			v.DetailOffset += max(1, m.height-10)
		} else {
			m.move(max(1, m.height-10))
		}
	case "enter":
		if v.Focus < 2 {
			if v.Focus == 1 {
				if e := m.selected(); e != nil {
					m.ws.RecordAccess(session.ID, e.UUID, m.now())
					m.searchCache = nil
				}
			}
			v.Focus++
		}
	case "/", "ctrl+f":
		return m.beginSearch(key == "ctrl+f")
	case "esc":
		if m.clearFilter() {
			break
		}
		if m.layout().Mode != "wide" && v.Focus > 0 {
			v.Focus--
		}
		m.normalize()
	case "G":
		return m.openGrandView()
	case "J":
		m.jump = true
		m.hideReveal()
	case "z":
		v.Zoom = !v.Zoom
	case "[":
		m.resizeFocused(-2)
	case "]":
		m.resizeFocused(2)
	case "a":
		m.openPalette("actions", true)
	}
	return nil
}

func (m *Model) beginUnlock(path string) {
	m.hideReveal()
	for i := range m.inputs {
		m.inputs[i].clear()
	}
	m.path = expandPath(path)
	canonical, err := filepath.Abs(m.path)
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
			canonical = resolved
		}
		for _, session := range m.ws.Sessions {
			if session.Path != canonical || session.DB == nil {
				continue
			}
			for i, v := range m.ws.Views {
				if v.SessionID == session.ID {
					m.ws.Switch(i)
					m.screen = browse
					m.err = ""
					return
				}
			}
		}
	}
	m.screen = unlock
	m.inputFocus = 0
	m.noPassword = false
	m.err = ""
}

func (m *Model) unlockKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "esc" {
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.request++
		m.busy = false
		m.screen = browse
		m.err = ""
		for i := range m.inputs {
			m.inputs[i].clear()
		}
		return nil
	}
	if m.busy {
		return nil
	}
	if m.inputFocus == 0 && k.Type == tea.KeyRunes {
		for _, r := range k.Runes {
			if r == '\n' || r == '\r' || r == 0 {
				m.err = "Password cannot contain line breaks; CLI transport does not support them."
				return nil
			}
		}
	}
	switch key {
	case "tab":
		m.inputFocus = (m.inputFocus + 1) % 4
	case "shift+tab":
		m.inputFocus = (m.inputFocus + 3) % 4
	case " ":
		if m.inputFocus == 3 {
			m.noPassword = !m.noPassword
			return nil
		}
		m.inputs[m.inputFocus].update(k)
	case "enter":
		return m.startOpen()
	default:
		if m.inputFocus < 3 {
			m.inputs[m.inputFocus].update(k)
		}
	}
	return nil
}

func (m *Model) startOpen() tea.Cmd {
	if !m.ws.CanOpen(m.path) {
		m.err = "Maximum 9 tabs. Close a tab before opening another vault."
		return nil
	}
	if m.backend == nil {
		m.err = "Could not unlock. keepassxc-cli unavailable."
		return nil
	}
	if m.noPassword && m.inputs[1].value() == "" && m.inputs[2].value() == "" {
		m.err = "Choose a key file or hardware key when password is disabled."
		return nil
	}
	creds := vault.Credentials{KeyFile: expandOptional(m.inputs[1].value()), YubiKey: m.inputs[2].value(), NoPassword: m.noPassword}
	if !m.noPassword {
		creds.Password = []byte(m.inputs[0].value())
	}
	for i := range m.inputs {
		m.inputs[i].clear()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.request++
	id := m.request
	path := m.path
	backend := m.backend
	m.busy = true
	m.err = ""
	return func() tea.Msg {
		defer clear(creds.Password)
		db, err := backend.Open(ctx, path, creds)
		result := openedMsg{request: id, db: db, err: err}
		if err == nil {
			result.credentials = creds.Clone()
		}
		return result
	}
}

func expandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return filepath.Clean(path)
}
func expandOptional(path string) string {
	if path == "" {
		return ""
	}
	return expandPath(path)
}

func (m *Model) openPicker() tea.Cmd {
	m.hideReveal()
	m.err = ""
	m.screen = chooseFile
	m.inputFocus = 0
	dir, err := os.Getwd()
	if err != nil {
		dir = os.TempDir()
	}
	if s := m.ws.ActiveSession(); s != nil {
		dir = filepath.Dir(s.Path)
	}
	m.directory = dir
	m.inputs[0].set(dir + string(os.PathSeparator))
	m.files = nil
	m.fileIndex, m.fileOffset = 0, 0
	m.pickerEditing = false
	return listDirectory(dir)
}

func listDirectory(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(dir)
		files := []fileItem{}
		if parent := filepath.Dir(dir); parent != dir {
			files = append(files, fileItem{"..", parent, true})
		}
		for _, e := range entries {
			if e.IsDir() || strings.EqualFold(filepath.Ext(e.Name()), ".kdbx") {
				files = append(files, fileItem{e.Name(), filepath.Join(dir, e.Name()), e.IsDir()})
			}
		}
		sort.SliceStable(files, func(i, j int) bool {
			if files[i].name == ".." {
				return files[j].name != ".."
			}
			if files[j].name == ".." {
				return false
			}
			if files[i].dir != files[j].dir {
				return files[i].dir
			}
			return strings.ToLower(files[i].name) < strings.ToLower(files[j].name)
		})
		return listingMsg{dir, files, err}
	}
}

func (m *Model) fileKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.inputs[0].clear()
		m.screen = browse
		m.err = ""
		return nil
	case "up":
		m.pickerEditing = false
		m.fileIndex = max(0, m.fileIndex-1)
		return nil
	case "down":
		m.pickerEditing = false
		m.fileIndex = min(max(0, len(m.files)-1), m.fileIndex+1)
		return nil
	case "tab":
		m.pickerEditing = !m.pickerEditing
		return nil
	case "ctrl+u":
		m.pickerEditing = true
		m.inputs[0].clear()
		m.err = ""
		return nil
	case "left", "backspace":
		if !m.pickerEditing {
			return m.changeDirectory(filepath.Dir(m.directory))
		}
		m.inputs[0].update(k)
		return nil
	case "right":
		if !m.pickerEditing {
			if len(m.files) > 0 && m.files[m.fileIndex].dir {
				return m.changeDirectory(m.files[m.fileIndex].path)
			}
			return nil
		}
		m.inputs[0].update(k)
		return nil
	case "enter":
		path := m.directory
		if m.pickerEditing {
			if m.inputs[0].value() == "" {
				m.err = "Enter a vault or directory path."
				return nil
			}
			path = expandPath(m.inputs[0].value())
			if !filepath.IsAbs(path) {
				path = filepath.Join(m.directory, path)
			}
		} else if len(m.files) > 0 {
			path = m.files[m.fileIndex].path
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return m.changeDirectory(path)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			m.err = "Vault not found. Check path or choose another file."
			return nil
		}
		m.beginUnlock(path)
		return nil
	default:
		if !m.pickerEditing {
			if k.Type != tea.KeyRunes {
				return nil
			}
			m.pickerEditing = true
			m.inputs[0].clear()
		}
		m.inputs[0].update(k)
		m.err = ""
	}
	return nil
}

func (m *Model) changeDirectory(path string) tea.Cmd {
	m.directory = path
	m.files = nil
	m.fileIndex, m.fileOffset = 0, 0
	m.pickerEditing = false
	m.err = ""
	m.inputs[0].set(path + string(os.PathSeparator))
	return listDirectory(path)
}
