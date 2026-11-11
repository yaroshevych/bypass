package widget

import (
	"context"
	"errors"
	"sort"
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
}

type Item struct{ ID, Title, Group string }
type Field struct {
	Name, Value string
	Protected   bool
}

func NewSession(driver clipboard.Driver, timeout time.Duration) *Session {
	return &Session{backend: vault.NewBackend(), clipboard: clipboard.New(driver), clipboardTimeout: timeout}
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
	return nil
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
	entries := make([]vault.Entry, 0, len(s.db.Entries))
	for _, entry := range s.db.Entries {
		if entry.Recycled {
			continue
		}
		if query != "" {
			fields, err := s.backend.EntryDetails(ctx, s.db, entry.UUID)
			if err != nil {
				return nil, err
			}
			entry.Fields = fields
		}
		entries = append(entries, entry)
	}
	ids := Search(query, entries)
	byID := make(map[string]vault.Entry, len(entries))
	for _, entry := range entries {
		byID[entry.UUID] = entry
	}
	items := make([]Item, 0, len(ids))
	for _, id := range ids {
		entry := byID[id]
		items = append(items, Item{ID: id, Title: entry.Get("Title"), Group: entry.GroupPath})
	}
	return items, nil
}

func (s *Session) Fields(ctx context.Context, id string) ([]Field, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("vault is locked")
	}
	fields, err := s.backend.EntryDetails(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	for _, entry := range s.db.Entries {
		if entry.UUID == id && !entry.Recycled && entry.HasTOTP {
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
		value := field.Value
		protected := field.Protected || field.Name == "Password" || field.Name == "TOTP"
		if protected {
			value = ""
		}
		result = append(result, Field{Name: field.Name, Value: value, Protected: protected})
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

// Reveal returns only the password or generated one-time code. The caller
// owns the returned bytes and must clear them after use.
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
		return nil, 0, errors.New("field cannot be revealed")
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
	return s.clipboard.Copy(ctx, value, time.Now(), s.clipboardTimeout)
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
	ref, err := FieldReference(s.db.Path, selected.GroupPath, selected.Get("Title"), field)
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
