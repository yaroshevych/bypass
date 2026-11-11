package widget

import (
	"context"
	"errors"
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
	result := make([]Field, 0, len(fields))
	for _, field := range fields {
		value := field.Value
		if field.Protected {
			value = ""
		}
		result = append(result, Field{Name: field.Name, Value: value, Protected: field.Protected})
	}
	return result, nil
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
	found := false
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
