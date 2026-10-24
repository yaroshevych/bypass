package workspace

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bypass/internal/vault"
)

type Session struct {
	ID           string
	Path         string
	ActiveViewID string
	DB           *vault.Database
	Credentials  vault.Credentials
	ReadOnly     bool
	Stale        bool
	LastActivity time.Time
	Usage        map[string]Access
	Destinations []string
}

type Access struct {
	Count int
	Last  time.Time
}

type View struct {
	ID           string
	SessionID    string
	GroupUUID    string
	SelectedUUID string
	Query        string
	Focus        int
	NavIndex     int
	EntryOffset  int
	DetailOffset int
	DetailManual bool
	FieldIndex   int
	NavKey       string
	NavFilter    string
	DetailFilter string
	NavOffset    int
	NavWidth     int
	DetailWidth  int
	Zoom         bool
	QueryScope   string
}

type Workspace struct {
	Sessions map[string]*Session
	Views    []View
	Active   int
	nextID   uint64
}

const MaxTabs = 9

func (w *Workspace) CanOpen(path string) bool {
	if len(w.Views) < MaxTabs {
		return true
	}
	path = canonicalPath(path)
	for _, view := range w.Views {
		if session := w.Sessions[view.SessionID]; session != nil && session.Path == path {
			return true
		}
	}
	return false
}

func New() *Workspace { return &Workspace{Sessions: make(map[string]*Session)} }

// Open activates a view for db, sharing an existing session when its path
// resolves to the same file. It creates a view only when the session has none;
// a locked session is refreshed with db.
func (w *Workspace) Open(db *vault.Database) (reused bool) {
	if db == nil {
		return false
	}
	if !w.CanOpen(db.Path) {
		db.Clear()
		return false
	}
	path := canonicalPath(db.Path)
	var session *Session
	for _, candidate := range w.Sessions {
		if path != "" && candidate.Path == path {
			session = candidate
			break
		}
	}
	if session != nil {
		reused = true
		if session.DB == nil {
			w.ReplaceDatabase(session.ID, db)
		} else if session.DB != db {
			// The caller may have just decrypted a second copy of the same
			// vault. Keep one shared database and discard the duplicate plaintext.
			db.Clear()
		}
		for i := range w.Views {
			if w.Views[i].SessionID == session.ID && w.Views[i].ID == session.ActiveViewID {
				w.setActive(i)
				return true
			}
		}
		for i := range w.Views {
			if w.Views[i].SessionID == session.ID {
				w.setActive(i)
				return true
			}
		}
	} else {
		session = &Session{ID: w.id("session"), Path: path, DB: db, ReadOnly: true, LastActivity: time.Now(), Usage: make(map[string]Access)}
		w.Sessions[session.ID] = session
	}
	w.Views = append(w.Views, View{ID: w.id("view"), SessionID: session.ID, GroupUUID: db.RootUUID})
	w.setActive(len(w.Views) - 1)
	return reused
}

func (w *Workspace) ActiveView() *View {
	if w == nil || w.Active < 0 || w.Active >= len(w.Views) {
		return nil
	}
	return &w.Views[w.Active]
}

func (w *Workspace) ActiveSession() *Session {
	view := w.ActiveView()
	if view == nil {
		return nil
	}
	return w.Sessions[view.SessionID]
}

func (w *Workspace) Duplicate() bool {
	view := w.ActiveView()
	if view == nil || len(w.Views) >= MaxTabs {
		return false
	}
	copy := *view
	copy.ID = w.id("view")
	w.Views = append(w.Views, copy)
	w.setActive(len(w.Views) - 1)
	return true
}

func (w *Workspace) Switch(index int) bool {
	if w == nil || index < 0 || index >= len(w.Views) {
		return false
	}
	w.setActive(index)
	return true
}

func (w *Workspace) setActive(index int) {
	w.Active = index
	view := w.ActiveView()
	if view != nil {
		if session := w.Sessions[view.SessionID]; session != nil {
			session.ActiveViewID = view.ID
		}
	}
}

func (w *Workspace) CloseView() bool {
	if w.ActiveView() == nil {
		return false
	}
	removed := w.Views[w.Active]
	last := len(w.Views) - 1
	copy(w.Views[w.Active:], w.Views[w.Active+1:])
	w.Views[last] = View{}
	w.Views = w.Views[:last]
	if len(w.Views) == 0 {
		w.Active = 0
	} else if w.Active >= len(w.Views) {
		w.Active = len(w.Views) - 1
	}
	if session := w.Sessions[removed.SessionID]; session != nil && session.ActiveViewID == removed.ID {
		for _, view := range w.Views {
			if view.SessionID == removed.SessionID {
				session.ActiveViewID = view.ID
				break
			}
		}
	}
	w.rememberActiveView()
	w.cleanupSession(removed.SessionID)
	return true
}

// LockActive clears shared database contents for every view of the active
// session while retaining the view navigation UUIDs.
func (w *Workspace) LockActive() bool {
	session := w.ActiveSession()
	if session == nil {
		return false
	}
	return w.LockSession(session.ID)
}

func (w *Workspace) ReplaceDatabase(sessionID string, db *vault.Database) bool {
	session := w.Sessions[sessionID]
	if session == nil || db == nil {
		return false
	}
	if session.DB != nil && session.DB != db {
		session.DB.Clear()
	}
	session.DB = db
	session.Stale = false
	session.LastActivity = time.Now()
	return true
}

func (w *Workspace) SetCredentials(sessionID string, credentials vault.Credentials) bool {
	session := w.Sessions[sessionID]
	if session == nil {
		return false
	}
	owned := credentials.Clone()
	session.Credentials.Clear()
	session.Credentials = owned
	session.LastActivity = time.Now()
	return true
}

func (w *Workspace) RecordAccess(sessionID, entryUUID string, now time.Time) bool {
	session := w.Sessions[sessionID]
	if session == nil || session.DB == nil || entryUUID == "" {
		return false
	}
	found := false
	for _, entry := range session.DB.Entries {
		if entry.UUID == entryUUID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if session.Usage == nil {
		session.Usage = make(map[string]Access)
	}
	access := session.Usage[entryUUID]
	if access.Count < int(^uint(0)>>1) {
		access.Count++
	}
	access.Last = now
	session.Usage[entryUUID] = access
	return true
}

func (w *Workspace) LockSession(sessionID string) bool {
	session := w.Sessions[sessionID]
	if session == nil {
		return false
	}
	if session.DB != nil {
		session.DB.Clear()
		session.DB = nil
	}
	session.Credentials.Clear()
	for i := range w.Views {
		if w.Views[i].SessionID == sessionID {
			w.Views[i].Query = ""
			w.Views[i].QueryScope = ""
			w.Views[i].NavFilter = ""
			w.Views[i].DetailFilter = ""
			w.Views[i].DetailOffset = 0
			w.Views[i].DetailManual = false
			w.Views[i].FieldIndex = 0
		}
	}
	clearSessionMemory(session)
	return true
}

func (w *Workspace) LockAll() {
	if w == nil {
		return
	}
	for id := range w.Sessions {
		w.LockSession(id)
	}
}

// CloseVault closes all views for the active vault and clears its data.
func (w *Workspace) CloseVault() bool {
	session := w.ActiveSession()
	if session == nil {
		return false
	}
	if session.DB != nil {
		session.DB.Clear()
		session.DB = nil
	}
	session.Credentials.Clear()
	clearSessionMemory(session)
	oldLen := len(w.Views)
	kept := w.Views[:0]
	newActive := 0
	for i, view := range w.Views {
		if view.SessionID == session.ID {
			continue
		}
		if i < w.Active {
			newActive++
		}
		kept = append(kept, view)
	}
	for i := len(kept); i < oldLen; i++ {
		w.Views[i] = View{}
	}
	w.Views = kept
	if len(w.Views) == 0 {
		w.Active = 0
	} else if newActive >= len(w.Views) {
		w.Active = len(w.Views) - 1
	} else {
		w.Active = newActive
	}
	delete(w.Sessions, session.ID)
	w.rememberActiveView()
	return true
}

func (w *Workspace) rememberActiveView() {
	view := w.ActiveView()
	if view != nil {
		if session := w.Sessions[view.SessionID]; session != nil {
			session.ActiveViewID = view.ID
		}
	}
}

// Close clears all open databases and drops every view and session.
func (w *Workspace) Close() {
	if w == nil {
		return
	}
	for i := range w.Views {
		w.Views[i] = View{}
	}
	for _, session := range w.Sessions {
		if session != nil {
			if session.DB != nil {
				session.DB.Clear()
				session.DB = nil
			}
			session.Credentials.Clear()
			clearSessionMemory(session)
		}
	}
	w.Views = nil
	w.Sessions = make(map[string]*Session)
	w.Active = 0
}

func (w *Workspace) cleanupSession(id string) {
	for _, view := range w.Views {
		if view.SessionID == id {
			return
		}
	}
	if session := w.Sessions[id]; session != nil {
		if session.DB != nil {
			session.DB.Clear()
			session.DB = nil
		}
		session.Credentials.Clear()
		clearSessionMemory(session)
		delete(w.Sessions, id)
	}
}

func clearSessionMemory(session *Session) {
	if session == nil {
		return
	}
	clear(session.Usage)
	session.Destinations = nil
	session.Usage = nil
}

func (w *Workspace) id(kind string) string {
	w.nextID++
	return kind + "-" + strconv.FormatUint(w.nextID, 10)
}

func canonicalPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}
