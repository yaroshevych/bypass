// Package session provides UI-independent operations for an unlocked vault.
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"bypass/internal/clipboard"
	"bypass/internal/search"
	"bypass/internal/vault"
)

var ErrLocked = errors.New("vault is locked")
var ErrChanged = errors.New("vault contents changed during operation")
var ErrNoDatabase = errors.New("backend returned no database")
var ErrFullSnapshotRequired = errors.New("operation requires a full database snapshot")
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

// Session owns one decrypted database. Database returns a borrowed pointer;
// callers must not clear it directly.
// Async work should use Snapshot and clear the snapshot when it completes.
type Session struct {
	mu        sync.Mutex
	copyMu    sync.Mutex
	backend   vault.Backend
	db        *vault.Database
	creds     vault.Credentials
	gen       uint64
	lifecycle uint64
	clip      *clipboard.Manager
	timeout   time.Duration
}

// Snapshot is an isolated database clone for one asynchronous operation.
type Snapshot struct {
	owner          *Session
	db             *vault.Database
	credentials    vault.Credentials
	generation     uint64
	lifecycleEpoch uint64
	complete       bool
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
		if db != nil {
			db.Clear()
		}
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
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	s.mu.Lock()
	if s.gen != generation {
		s.mu.Unlock()
		db.Clear()
		return ErrLocked
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		db.Clear()
		return err
	}
	if s.db != nil && s.db != db {
		s.db.Clear()
	}
	s.db = db
	s.gen++
	s.lifecycle++
	s.creds.Clear()
	if !db.Unlocked() {
		s.creds = credentials.Clone()
	}
	s.mu.Unlock()
	return nil
}

// Adopt transfers ownership of db to the session. If db shares backend state
// with the current database, ownership is transferred before the old model is
// cleared.
func (s *Session) Adopt(db *vault.Database) {
	if s == nil {
		if db != nil {
			db.Clear()
		}
		return
	}
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gen++
	s.lifecycle++
	if s.db != nil && s.db != db {
		if db != nil {
			s.db.TransferBackend(db)
		}
		s.db.Clear()
	}
	s.db = db
	s.creds.Clear()
}

// Database returns a borrowed presentation model. The caller must serialize
// its use with session lifecycle changes. Background work uses Snapshot.
func (s *Session) Database() *vault.Database {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db
}

// DiscardDatabase clears a rejected result. Native results produced from
// snapshots borrow the live backend until accepted by ReplaceDatabase.
func (s *Session) DiscardDatabase(db *vault.Database) {
	if db == nil {
		return
	}
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if db == s.db {
			return
		}
	}
	db.Clear()
}

// Credentials returns a caller-owned copy. The caller must clear it after use.
func (s *Session) Credentials() vault.Credentials {
	if s == nil {
		return vault.Credentials{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds.Clone()
}

// CredentialMetadata returns non-secret credential selectors without copying
// the password bytes into workspace/UI code.
func (s *Session) CredentialMetadata() (keyFile, yubiKey string) {
	if s == nil {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds.KeyFile, s.creds.YubiKey
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
	return &Snapshot{owner: s, db: s.db.Clone(), credentials: s.creds.Clone(), generation: s.gen, lifecycleEpoch: s.lifecycle, complete: true}
}

// ReadSnapshot returns a lightweight native handle when the exact native
// backend is in use. Wrapped and custom backends receive a full model clone.
func (s *Session) ReadSnapshot() *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	var db *vault.Database
	complete := false
	if _, native := s.backend.(*vault.NativeBackend); native && s.db.Unlocked() {
		db = s.db.NativeReadView()
	} else {
		db = s.db.Clone()
		complete = true
	}
	return &Snapshot{owner: s, db: db, credentials: s.creds.Clone(), generation: s.gen, lifecycleEpoch: s.lifecycle, complete: complete}
}

func (s *Session) snapshotStateErrorLocked(snapshot *Snapshot) error {
	if s == nil || snapshot == nil || snapshot.owner != s || snapshot.db == nil || s.db == nil || s.lifecycle != snapshot.lifecycleEpoch {
		return ErrLocked
	}
	if s.gen != snapshot.generation {
		return ErrChanged
	}
	return nil
}

func (s *Session) snapshotError(ctx context.Context, snapshot *Snapshot) error {
	if s == nil {
		return ErrLocked
	}
	s.mu.Lock()
	err := s.snapshotStateErrorLocked(snapshot)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Session) fullSnapshotError(ctx context.Context, snapshot *Snapshot) error {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return err
	}
	if !snapshot.complete {
		return ErrFullSnapshotRequired
	}
	return nil
}

// ReplaceDatabase transfers ownership of a write or reload result.
func (s *Session) ReplaceDatabase(db *vault.Database) bool {
	if s == nil {
		if db != nil {
			db.Clear()
		}
		return false
	}
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if db != nil && s.db == nil {
		return false
	}
	if db != nil && db == s.db {
		return true
	}
	s.gen++
	if db == nil || db.Unlocked() {
		s.creds.Clear()
	}
	if db == nil {
		s.lifecycle++
	}
	if s.db != nil && s.db != db {
		if db != nil {
			s.db.TransferBackend(db)
		}
		s.db.Clear()
	}
	s.db = db
	return true
}

// Search ranks the current vault's entries using non-secret search fields.
func (s *Session) Search(query string) ([]string, error) {
	if s == nil {
		return nil, ErrLocked
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrLocked
	}
	return searchEntries(query, s.db.Entries, s.db.Unlocked(), true)
}

// SearchEntries ranks a caller-filtered subset while preserving the TUI's
// group and tab ordering decisions.
func (s *Session) SearchEntries(query string, entries []vault.Entry) []string {
	unlocked := false
	if s != nil {
		s.mu.Lock()
		unlocked = s.db.Unlocked()
		s.mu.Unlock()
	}
	ids, _ := searchEntries(query, entries, unlocked, false)
	return ids
}

func searchEntries(query string, entries []vault.Entry, unlocked, skipRecycled bool) ([]string, error) {
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
		return nil, err
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}
	return ids, nil
}

func (s *Session) EntryDetails(ctx context.Context, snapshot *Snapshot, id string) ([]vault.Field, error) {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return nil, err
	}
	reader, ok := s.backend.(detailsReader)
	if !ok {
		return nil, ErrUnsupported
	}
	fields, err := reader.EntryDetails(ctx, snapshot.db, id)
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return nil, err
	}
	return fields, err
}

func (s *Session) CanEntryDetails() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(detailsReader)
	return ok
}

func (s *Session) CanResolveField() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(fieldResolver)
	return ok
}

func (s *Session) CanTOTP() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(totpResolver)
	return ok
}

func (s *Session) CanExportAttachment() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(attachmentReader)
	return ok
}

func (s *Session) ResolveField(ctx context.Context, snapshot *Snapshot, id, field string) ([]byte, error) {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return nil, err
	}
	resolver, ok := s.backend.(fieldResolver)
	if !ok {
		return nil, ErrUnsupported
	}
	value, err := resolver.ResolveField(ctx, snapshot.db, snapshot.credentials, id, field)
	if err := s.snapshotError(ctx, snapshot); err != nil {
		clear(value)
		return nil, err
	}
	return value, err
}

func (s *Session) TOTP(ctx context.Context, snapshot *Snapshot, id string) (vault.TOTPResult, error) {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return vault.TOTPResult{}, err
	}
	resolver, ok := s.backend.(totpResolver)
	if !ok {
		return vault.TOTPResult{}, ErrUnsupported
	}
	result, err := resolver.TOTP(ctx, snapshot.db, snapshot.credentials, id)
	if err := s.snapshotError(ctx, snapshot); err != nil {
		clear(result.Code)
		return vault.TOTPResult{}, err
	}
	return result, err
}

func (s *Session) ExportAttachment(ctx context.Context, snapshot *Snapshot, id, name string) ([]byte, error) {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return nil, err
	}
	reader, ok := s.backend.(attachmentReader)
	if !ok {
		return nil, ErrUnsupported
	}
	data, err := reader.ExportAttachment(ctx, snapshot.db, snapshot.credentials, id, name)
	if err := s.snapshotError(ctx, snapshot); err != nil {
		clear(data)
		return nil, err
	}
	return data, err
}

// CopyValue resolves a field within the session and sends it through the
// managed clipboard. The resolved bytes never leave this method.
func (s *Session) CopyValue(ctx context.Context, id, field string, now time.Time) error {
	if s == nil {
		return ErrLocked
	}
	snapshot := s.ReadSnapshot()
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
	value, err := s.ResolveField(ctx, snapshot, id, field)
	defer clear(value)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFieldUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	s.mu.Lock()
	if err := s.snapshotStateErrorLocked(snapshot); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return manager.CopySensitive(ctx, value, now, timeout)
}

// Lock clears decrypted data and the shared managed clipboard.
func (s *Session) Lock(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	s.mu.Lock()
	s.gen++
	s.lifecycle++
	if s.db != nil {
		s.db.Clear()
		s.db = nil
	}
	s.creds.Clear()
	clip := s.clip
	s.mu.Unlock()
	if clip != nil {
		return clip.Clear(ctx)
	}
	return nil
}
