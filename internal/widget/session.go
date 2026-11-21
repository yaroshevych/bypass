package widget

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/vault"
)

// Session owns at most one unlocked vault and its managed clipboard.
type Session struct {
	mu               sync.Mutex
	backend          *vault.NativeBackend
	db               *vault.Database
	clipboard        *clipboard.Manager
	clipboardTimeout time.Duration
	backupDir        string
	fileStamp        fileStamp
}

type fileStamp struct {
	size    int64
	modTime time.Time
	info    os.FileInfo
}

func stamp(path string) (fileStamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{size: info.Size(), modTime: info.ModTime(), info: info}, nil
}

func sameStamp(a, b fileStamp) bool {
	return a.size == b.size && a.modTime.Equal(b.modTime) && a.info != nil && b.info != nil && os.SameFile(a.info, b.info)
}

type Item struct {
	ID, Title, Group string
	Favorite         bool
}
type Field struct {
	Group       string `json:"group,omitempty"`
	Name, Value string
	Protected   bool
}

func NewSession(driver clipboard.Driver, timeout time.Duration, backupDir string) *Session {
	return &Session{backend: vault.NewBackend(), clipboard: clipboard.New(driver), clipboardTimeout: timeout, backupDir: backupDir}
}

// Open replaces the current vault only after unlocking the new one succeeds.
func (s *Session) Open(ctx context.Context, path string, password []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	creds := vault.Credentials{Password: password}
	defer creds.Clear()
	db, err := s.backend.Open(ctx, path, creds)
	if err != nil {
		return err
	}
	if s.db != nil {
		s.db.Clear()
	}
	s.db = db
	// The file may have changed while Open was reading it. Force one initial
	// revision check before caching metadata.
	s.fileStamp = fileStamp{}
	return nil
}

// ReloadIfChanged refreshes an unlocked vault without retaining its password.
func (s *Session) ReloadIfChanged(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return false, nil
	}
	currentStamp, err := stamp(s.db.Path)
	if err != nil {
		return false, err
	}
	if sameStamp(currentStamp, s.fileStamp) {
		return false, nil
	}
	revision, err := vault.Revision(s.db.Path)
	if err != nil {
		return false, err
	}
	if revision == s.db.Revision {
		s.fileStamp = currentStamp
		return false, nil
	}
	updated, err := s.backend.Reload(ctx, s.db)
	if err != nil {
		return false, err
	}
	loadedStamp, stampErr := stamp(updated.Path)
	if stampErr == nil && sameStamp(currentStamp, loadedStamp) {
		s.fileStamp = loadedStamp
	} else {
		s.fileStamp = fileStamp{}
	}
	s.db.Clear()
	s.db = updated
	return true, nil
}

func (s *Session) Lock(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.db.Clear()
		s.db = nil
	}
	return s.clipboard.Clear(ctx)
}

func (s *Session) Items(ctx context.Context, query string) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(query) > 4096 {
		return nil, errors.New("search query is too long")
	}
	if s.db == nil {
		return nil, errors.New("vault is locked")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries := make([]vault.Entry, 0, len(s.db.Entries))
	for _, entry := range s.db.Entries {
		if entry.Recycled {
			continue
		}
		entries = append(entries, entry)
	}
	ids, err := Search(query, entries)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]vault.Entry, len(entries))
	for _, entry := range entries {
		byID[entry.UUID] = entry
	}
	items := make([]Item, 0, len(ids))
	for _, id := range ids {
		entry := byID[id]
		favorite := false
		for _, tag := range entry.Tags {
			if strings.EqualFold(tag, "favorite") {
				favorite = true
				break
			}
		}
		items = append(items, Item{ID: id, Title: entry.Get("Title"), Group: entry.GroupPath, Favorite: favorite})
	}
	return items, nil
}

// SetFavorite updates the Favorite tag using the normal vault write safeguards.
func (s *Session) SetFavorite(ctx context.Context, id string, favorite bool, consent *[32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("vault is locked")
	}
	for _, entry := range s.db.Entries {
		if entry.UUID != id || entry.Recycled {
			continue
		}
		tags := make([]string, 0, len(entry.Tags)+1)
		hasFavorite := false
		for _, tag := range entry.Tags {
			if strings.EqualFold(tag, "favorite") {
				hasFavorite = true
				continue
			}
			tags = append(tags, tag)
		}
		if hasFavorite == favorite {
			return nil
		}
		if favorite {
			tags = append(tags, "Favorite")
		}
		change := vault.Change{Kind: vault.EditEntry, EntryUUID: id, Values: map[string]string{"Tags": strings.Join(tags, ", ")}}
		if consent != nil {
			change.AllowDataLoss = true
			change.DataLossConsent = *consent
		}
		updated, err := s.backend.Apply(ctx, s.db, vault.Credentials{}, change, s.backupDir)
		if err != nil {
			return err
		}
		s.db.Clear()
		s.db = updated
		return nil
	}
	return errors.New("entry unavailable")
}

func (s *Session) Fields(ctx context.Context, id string) ([]Field, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("vault is locked")
	}
	group := ""
	for _, entry := range s.db.Entries {
		if entry.UUID == id && !entry.Recycled {
			group = strings.TrimPrefix(strings.Trim(entry.GroupPath, "/"), "Passwords/")
			break
		}
	}
	fields, err := s.backend.EntryDetails(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	hasTOTP := false
	for _, entry := range s.db.Entries {
		if entry.UUID == id && !entry.Recycled && entry.HasTOTP {
			hasTOTP = true
			filtered := fields[:0]
			for _, field := range fields {
				if field.Name != "TOTP" {
					filtered = append(filtered, field)
				}
			}
			fields = append(filtered, vault.Field{Name: "TOTP", Protected: true})
			break
		}
	}
	sort.SliceStable(fields, func(i, j int) bool { return fieldOrder(fields[i].Name) < fieldOrder(fields[j].Name) })
	result := make([]Field, 0, len(fields))
	for _, field := range fields {
		if field.Value == "" && !(field.Name == "TOTP" && hasTOTP) {
			continue
		}
		value := field.Value
		protected := field.Protected || field.Name == "Password" || field.Name == "TOTP"
		if protected {
			value = ""
		}
		result = append(result, Field{Group: group, Name: field.Name, Value: value, Protected: protected})
	}
	return result, nil
}

func fieldOrder(name string) int {
	switch name {
	case "UserName":
		return 0
	case "Password":
		return 1
	case "URL":
		return 2
	case "Notes":
		return 3
	case "Email":
		return 4
	case "TOTP":
		return 5
	default:
		return 6
	}
}

// Reveal returns a protected field or generated one-time code. The caller owns
// the returned bytes and must clear them after use.
func (s *Session) Reveal(ctx context.Context, id, field string) ([]byte, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, 0, errors.New("vault is locked")
	}
	if field == "TOTP" {
		result, err := s.backend.TOTP(ctx, s.db, vault.Credentials{}, id)
		return result.Code, result.RemainingSeconds, err
	}
	if field != "Password" {
		fields, err := s.backend.EntryDetails(ctx, s.db, id)
		if err != nil {
			return nil, 0, err
		}
		protected := false
		for _, candidate := range fields {
			if candidate.Name == field && candidate.Protected {
				protected = true
				break
			}
		}
		if !protected {
			return nil, 0, errors.New("field cannot be revealed")
		}
	}
	value, err := s.backend.ResolveField(ctx, s.db, vault.Credentials{}, id, field)
	return value, 0, err
}

func (s *Session) CopyValue(ctx context.Context, id, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("vault is locked")
	}
	value, err := s.backend.ResolveField(ctx, s.db, vault.Credentials{}, id, field)
	if err != nil {
		return err
	}
	defer clear(value)
	return s.clipboard.CopySensitive(ctx, value, time.Now(), s.clipboardTimeout)
}

// CopyReference copies a reference only when it uniquely identifies the entry.
func (s *Session) CopyReference(ctx context.Context, id, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("vault is locked")
	}
	var selected *vault.Entry
	for i := range s.db.Entries {
		entry := &s.db.Entries[i]
		if entry.UUID == id && !entry.Recycled {
			selected = entry
			break
		}
	}
	if selected == nil {
		return errors.New("entry unavailable")
	}
	for _, entry := range s.db.Entries {
		if !entry.Recycled && entry.UUID != id && entry.GroupPath == selected.GroupPath && entry.Get("Title") == selected.Get("Title") {
			return errors.New("entry reference is ambiguous")
		}
	}
	fields, err := s.backend.EntryDetails(ctx, s.db, id)
	if err != nil {
		return err
	}
	found := field == "TOTP" && selected.HasTOTP
	for _, candidate := range fields {
		if candidate.Name == field {
			found = true
			break
		}
	}
	if !found {
		return errors.New("field unavailable")
	}
	group := strings.TrimPrefix(strings.Trim(selected.GroupPath, "/"), "Passwords/")
	ref, err := relativeFieldReference(group, selected.Get("Title"), field)
	if err != nil {
		return err
	}
	value := []byte(ref)
	defer clear(value)
	return s.clipboard.Copy(ctx, value, time.Now(), s.clipboardTimeout)
}

func (s *Session) ExpireClipboard(ctx context.Context, now time.Time) error {
	_, err := s.clipboard.Expire(ctx, now)
	return err
}
