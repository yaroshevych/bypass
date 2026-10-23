package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"keepassui/internal/vault"
	"keepassui/internal/workspace"
)

type RecentStore interface {
	LoadRecent() ([]string, error)
	Remember(string) error
	Remove(string) error
}

type Options struct {
	Backend     vault.Backend
	Store       RecentStore
	InitialPath string
	Context     context.Context
}

type screen int

const (
	browse screen = iota
	chooseFile
	unlock
)

type Model struct {
	backend         vault.Backend
	store           RecentStore
	ctx             context.Context
	ws              *workspace.Workspace
	width, height   int
	screen          screen
	inputs          [3]input // password/path, key file, hardware slot
	inputFocus      int
	noPassword      bool
	path, directory string
	files           []fileItem
	fileIndex       int
	recent          []string
	recentIndex     int
	err, notice     string
	busy            bool
	cancel          context.CancelFunc
	request         uint64
	searching       bool
	query           input
	previousQuery   string
	help            bool
	backendVersion  string
	backendMissing  bool
}

type fileItem struct {
	name, path string
	dir        bool
}
type openedMsg struct {
	request uint64
	db      *vault.Database
	err     error
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
type listingMsg struct {
	directory string
	files     []fileItem
	err       error
}
type navItem struct {
	label, group string
	depth        int
	kind         string
}

func New(opts Options) *Model {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	m := &Model{backend: opts.Backend, store: opts.Store, ctx: ctx, ws: workspace.New(), width: 100, height: 30}
	if opts.InitialPath != "" {
		m.path = expandPath(opts.InitialPath)
		m.screen = unlock
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
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
	return tea.Batch(cmds...)
}

func (m *Model) Close() {
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

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		m.files, m.fileIndex = msg.files, 0
		if msg.err != nil {
			m.err = "Cannot list this directory. Enter a vault path directly."
		}
	case openedMsg:
		if msg.request != m.request {
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
			m.err = "Could not unlock. Backend returned no database."
			break
		}
		m.ws.Open(msg.db)
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
		m.adjustViewport()
		return m, cmd
	case tea.KeyMsg:
		cmd := m.key(msg)
		m.adjustViewport()
		return m, cmd
	}
	m.adjustViewport()
	return m, nil
}

func (m *Model) key(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		m.Close()
		return tea.Quit
	}
	if m.help {
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
		switch key {
		case "enter":
			m.searching = false
			m.query.clear()
			m.previousQuery = ""
		case "esc":
			if v := m.ws.ActiveView(); v != nil {
				v.Query = m.previousQuery
			}
			m.searching = false
			m.query.clear()
			m.previousQuery = ""
			m.normalize()
		default:
			m.query.update(k)
			if v := m.ws.ActiveView(); v != nil {
				v.Query = m.query.value()
				v.SelectedUUID = ""
				v.EntryOffset = 0
			}
			m.normalize()
		}
		return nil
	}
	switch key {
	case "q":
		m.Close()
		return tea.Quit
	case "ctrl+o":
		return m.openPicker()
	case "?":
		m.help = true
		return nil
	case "ctrl+t":
		m.ws.Duplicate()
		return nil
	case "ctrl+pgup":
		if n := len(m.ws.Views); n > 0 {
			m.ws.Switch((m.ws.Active + n - 1) % n)
		}
		return nil
	case "ctrl+pgdown":
		if n := len(m.ws.Views); n > 0 {
			m.ws.Switch((m.ws.Active + 1) % n)
		}
		return nil
	case "ctrl+w":
		m.ws.CloseView()
		m.err = ""
		m.normalize()
		return nil
	case "ctrl+l":
		m.ws.LockActive()
		m.query.clear()
		m.previousQuery = ""
		m.err = ""
		return nil
	}
	if strings.HasPrefix(key, "alt+") && len(key) == 5 && key[4] >= '1' && key[4] <= '9' {
		m.ws.Switch(int(key[4] - '1'))
		m.err = ""
		return nil
	}
	session := m.ws.ActiveSession()
	if session == nil {
		switch key {
		case "o", "enter":
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
	switch key {
	case "o":
		return m.openPicker()
	case "tab":
		v.Focus = (v.Focus + 1) % 3
	case "shift+tab":
		v.Focus = (v.Focus + 2) % 3
	case "left", "h":
		v.Focus = max(0, v.Focus-1)
	case "right", "l":
		v.Focus = min(2, v.Focus+1)
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-max(1, m.height-10))
	case "pgdown":
		m.move(max(1, m.height-10))
	case "enter":
		if v.Focus < 2 {
			v.Focus++
		}
	case "/", "ctrl+f":
		m.searching = true
		m.previousQuery = v.Query
		m.query.set(v.Query)
	case "esc":
		v.Query = ""
		m.normalize()
	}
	return nil
}

func (m *Model) beginUnlock(path string) {
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
		return openedMsg{id, db, err}
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
	m.fileIndex = 0
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
		m.fileIndex = max(0, m.fileIndex-1)
		return nil
	case "down":
		m.fileIndex = min(max(0, len(m.files)-1), m.fileIndex+1)
		return nil
	case "tab":
		if len(m.files) > 0 {
			m.inputs[0].set(m.files[m.fileIndex].path)
		}
		return nil
	case "enter":
		path := expandPath(m.inputs[0].value())
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			m.directory = path
			m.inputs[0].set(path + string(os.PathSeparator))
			return listDirectory(path)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			m.err = "Vault not found. Check path or choose another file."
			return nil
		}
		m.beginUnlock(path)
		return nil
	default:
		m.inputs[0].update(k)
		m.err = ""
	}
	return nil
}
