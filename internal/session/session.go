// Package session provides UI-independent operations for an unlocked vault.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/search"
	"bypass/internal/vault"
)

var ErrLocked = errors.New("vault is locked")
var ErrNoDatabase = errors.New("backend returned no database")
var ErrFieldUnavailable = errors.New("vault field is unavailable")

type detailsReader interface {
	EntryDetails(context.Context, *vault.Database, string) ([]vault.Field, error)
}
type fieldResolver interface {
	ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
}
type totpResolver interface {
	TOTP(context.Context, *vault.Database, vault.Credentials, string) (vault.TOTPResult, error)
}
type attachmentReader interface {
	ExportAttachment(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
}

// Session owns one decrypted database. Database returns a borrowed pointer for
// the TUI's current workspace migration; callers must not clear it directly.
// Async work should use Snapshot and clear the snapshot when it completes.
type Session struct {
	mu      sync.Mutex
	backend vault.Backend
	db      *vault.Database
	creds   vault.Credentials
	gen     uint64
	clip    *clipboard.Manager
	timeout time.Duration
}

// Snapshot is an isolated database clone for one asynchronous operation.
type Snapshot struct {
	owner       *Session
	db          *vault.Database
	credentials vault.Credentials
	generation  uint64
}

func (s *Snapshot) Clear() {
	if s == nil {
		return
	}
	if s.db != nil {
		s.db.Clear()
		s.db = nil
	}
	s.credentials.Clear()
}

func New(backend vault.Backend, clip *clipboard.Manager, timeout time.Duration) *Session {
	return &Session{backend: backend, clip: clip, timeout: timeout}
}

// Open replaces the current database only after the requested vault opens.
// Credentials are always cleared before this method returns.
func (s *Session) Open(ctx context.Context, path string, credentials vault.Credentials) error {
	defer credentials.Clear()
	if s == nil || s.backend == nil {
		return ErrUnsupported
	}
	s.mu.Lock()
	generation := s.gen
	s.mu.Unlock()
	db, err := s.backend.Open(ctx, path, credentials)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		if db != nil {
			db.Clear()
		}
		return err
	}
	if db == nil {
		return ErrNoDatabase
	}
	s.mu.Lock()
	if s.gen != generation {
		s.mu.Unlock()
		db.Clear()
		return ErrLocked
	}
	if s.db != nil && s.db != db {
		s.db.Clear()
	}
	s.db = db
	s.gen++
	s.creds.Clear()
	if !db.Unlocked() {
		s.creds = credentials.Clone()
	}
	s.mu.Unlock()
	return nil
}

// Adopt transfers ownership of db to the session and clears any prior DB.
func (s *Session) Adopt(db *vault.Database) {
	if s == nil {
		if db != nil {
			db.Clear()
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	if s.db != nil && s.db != db {
		s.db.Clear()
	}
	s.db = db
	s.creds.Clear()
}

// Database returns the current database as a borrowed pointer.
func (s *Session) Database() *vault.Database {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db
}

// Credentials returns a caller-owned copy for legacy workspace adapters.
func (s *Session) Credentials() vault.Credentials {
	if s == nil {
		return vault.Credentials{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds.Clone()
}

// SetCredentials stores only the fallback credentials required by backends
// that do not retain an unlocked database in memory.
func (s *Session) SetCredentials(credentials vault.Credentials) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds.Clear()
	if s.db == nil || !s.db.Unlocked() {
		s.creds = credentials.Clone()
	}
}

// Snapshot returns a clone that can be used by a background operation.
func (s *Session) Snapshot() *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return &Snapshot{owner: s, db: s.db.Clone(), credentials: s.creds.Clone(), generation: s.gen}
}

func (s *Session) validSnapshot(snapshot *Snapshot) bool {
	if s == nil || snapshot == nil || snapshot.owner != s || snapshot.db == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db != nil && s.gen == snapshot.generation
}

// ReplaceDatabase transfers ownership of a write or reload result.
func (s *Session) ReplaceDatabase(db *vault.Database) {
	if s == nil {
		if db != nil {
			db.Clear()
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	if s.db != nil && s.db != db {
		s.db.TransferBackend(db)
		s.db.Clear()
	}
	s.db = db
}

// Search ranks entries using the shared search engine and excludes secret
// fields. The caller supplies entries already filtered for its current view.
func (s *Session) Search(query string) ([]string, error) {
	if s == nil {
		return nil, ErrLocked
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrLocked
	}
	return s.searchEntries(query, s.db.Entries, s.db.Unlocked(), true), nil
}

// SearchEntries ranks a caller-filtered subset while preserving the TUI's
// group and tab ordering decisions.
func (s *Session) SearchEntries(query string, entries []vault.Entry) []string {
	db := s.Database()
	return s.searchEntries(query, entries, db != nil && db.Unlocked(), false)
}

func (s *Session) searchEntries(query string, entries []vault.Entry, unlocked, skipRecycled bool) []string {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	docs := make([]search.Document, 0, len(entries))
	for _, entry := range entries {
		if skipRecycled && entry.Recycled {
			continue
		}
		get := func(name string) string {
			if entry.IsProtected(name) || name == "Password" || (unlocked && name == "Notes") {
				return ""
			}
			return entry.Get(name)
		}
		docs = append(docs, search.Document{ID: entry.UUID, Title: get("Title"), Username: get("UserName"), URL: get("URL"), Notes: get("Notes"), Group: entry.GroupPath, Tags: entry.Tags})
	}
	matches, err := search.Find(query, docs)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}
	return ids
}

func (s *Session) EntryDetails(ctx context.Context, snapshot *Snapshot, id string) ([]vault.Field, error) {
	reader, ok := s.backend.(detailsReader)
	if !ok {
		return nil, ErrUnsupported
	}
	if !s.validSnapshot(snapshot) {
		return nil, ErrLocked
	}
	fields, err := reader.EntryDetails(ctx, snapshot.db, id)
	if !s.validSnapshot(snapshot) {
		return nil, ErrLocked
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return fields, err
}

func (s *Session) CanEntryDetails() bool     { _, ok := s.backend.(detailsReader); return ok }
func (s *Session) CanResolveField() bool     { _, ok := s.backend.(fieldResolver); return ok }
func (s *Session) CanTOTP() bool             { _, ok := s.backend.(totpResolver); return ok }
func (s *Session) CanExportAttachment() bool { _, ok := s.backend.(attachmentReader); return ok }

func (s *Session) ResolveField(ctx context.Context, snapshot *Snapshot, id, field string) ([]byte, error) {
	resolver, ok := s.backend.(fieldResolver)
	if !ok {
		return nil, ErrUnsupported
	}
	if !s.validSnapshot(snapshot) {
		return nil, ErrLocked
	}
	value, err := resolver.ResolveField(ctx, snapshot.db, snapshot.credentials, id, field)
	if !s.validSnapshot(snapshot) || ctx.Err() != nil {
		clear(value)
		if !s.validSnapshot(snapshot) {
			return nil, ErrLocked
		}
		return nil, ctx.Err()
	}
	return value, err
}

func (s *Session) TOTP(ctx context.Context, snapshot *Snapshot, id string) (vault.TOTPResult, error) {
	resolver, ok := s.backend.(totpResolver)
	if !ok {
		return vault.TOTPResult{}, ErrUnsupported
	}
	if !s.validSnapshot(snapshot) {
		return vault.TOTPResult{}, ErrLocked
	}
	result, err := resolver.TOTP(ctx, snapshot.db, snapshot.credentials, id)
	if !s.validSnapshot(snapshot) || ctx.Err() != nil {
		clear(result.Code)
		if !s.validSnapshot(snapshot) {
			return vault.TOTPResult{}, ErrLocked
		}
		return vault.TOTPResult{}, ctx.Err()
	}
	return result, err
}

func (s *Session) ExportAttachment(ctx context.Context, snapshot *Snapshot, id, name string) ([]byte, error) {
	reader, ok := s.backend.(attachmentReader)
	if !ok {
		return nil, ErrUnsupported
	}
	if !s.validSnapshot(snapshot) {
		return nil, ErrLocked
	}
	data, err := reader.ExportAttachment(ctx, snapshot.db, snapshot.credentials, id, name)
	if !s.validSnapshot(snapshot) || ctx.Err() != nil {
		clear(data)
		if !s.validSnapshot(snapshot) {
			return nil, ErrLocked
		}
		return nil, ctx.Err()
	}
	return data, err
}

// CopyValue resolves a field within the session and sends it through the
// managed clipboard. The resolved bytes never leave this method.
func (s *Session) CopyValue(ctx context.Context, id, field string, now time.Time) error {
	if s == nil {
		return ErrLocked
	}
	snapshot := s.Snapshot()
	defer snapshot.Clear()
	return s.CopyField(ctx, snapshot, id, field, s.clip, now, s.timeout)
}

// CopyField resolves against an isolated snapshot, then checks that the
// session is still at that snapshot's generation before writing to clipboard.
func (s *Session) CopyField(ctx context.Context, snapshot *Snapshot, id, field string, manager *clipboard.Manager, now time.Time, timeout time.Duration) error {
	if s == nil {
		return ErrLocked
	}
	if manager == nil {
		return errors.New("clipboard unavailable")
	}
	if !s.validSnapshot(snapshot) {
		return ErrLocked
	}
	value, err := s.ResolveField(ctx, snapshot, id, field)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFieldUnavailable, err)
	}
	defer clear(value)
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || snapshot.generation != s.gen {
		return ErrLocked
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return manager.Copy(ctx, value, now, timeout)
}

// Lock clears decrypted data and the shared managed clipboard.
func (s *Session) Lock(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.gen++
	if s.db != nil {
		s.db.Clear()
		s.db = nil
	}
	s.creds.Clear()
	defer s.mu.Unlock()
	if s.clip != nil {
		return s.clip.Clear(ctx)
	}
	return nil
}
