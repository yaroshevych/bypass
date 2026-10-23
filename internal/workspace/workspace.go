package workspace

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"keepassui/internal/vault"
)

type Session struct {
	ID           string
	Path         string
	DB           *vault.Database
	Credentials  vault.Credentials
	ReadOnly     bool
	Stale        bool
	LastActivity time.Time
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
}

type Workspace struct {
	Sessions map[string]*Session
	Views    []View
	Active   int
	nextID   uint64
}

func New() *Workspace { return &Workspace{Sessions: make(map[string]*Session)} }

// Open activates a view for db, sharing an existing session when its path
// resolves to the same file. It creates a view only when the session has none;
// a locked session is refreshed with db.
func (w *Workspace) Open(db *vault.Database) (reused bool) {
	if db == nil {
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
			if w.Views[i].SessionID == session.ID {
				w.Active = i
				return true
			}
		}
	} else {
		session = &Session{ID: w.id("session"), Path: path, DB: db, ReadOnly: true, LastActivity: time.Now()}
		w.Sessions[session.ID] = session
	}
	w.Views = append(w.Views, View{ID: w.id("view"), SessionID: session.ID, GroupUUID: db.RootUUID})
	w.Active = len(w.Views) - 1
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
	if view == nil {
		return false
	}
	copy := *view
	copy.ID = w.id("view")
	w.Views = append(w.Views, copy)
	w.Active = len(w.Views) - 1
	return true
}

func (w *Workspace) Switch(index int) bool {
	if w == nil || index < 0 || index >= len(w.Views) {
		return false
	}
	w.Active = index
	return true
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
			w.Views[i].DetailOffset = 0
			w.Views[i].DetailManual = false
			w.Views[i].FieldIndex = 0
		}
	}
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
	return true
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
		delete(w.Sessions, id)
	}
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
