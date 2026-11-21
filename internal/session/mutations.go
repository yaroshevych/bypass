package session

import (
	"context"

	"bypass/internal/vault"
)

func (s *Session) CanApply() bool {
	if s == nil {
		return false
	}
	_, ok := s.backend.(vault.Writer)
	return ok
}

// Apply returns an updated model for the caller to accept after checking
// its operation identity. The caller owns the result until acceptance.
func (s *Session) Apply(ctx context.Context, snapshot *Snapshot, change vault.Change, backupDir string) (*vault.Database, error) {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return nil, err
	}
	writer, ok := s.backend.(vault.Writer)
	if !ok {
		return nil, ErrUnsupported
	}
	return writer.Apply(ctx, snapshot.db, snapshot.credentials, change, backupDir)
}

// Reload opens the current vault again. The caller owns the result until
// it is accepted into the session.
func (s *Session) Reload(ctx context.Context, snapshot *Snapshot) (*vault.Database, error) {
	if err := s.snapshotError(ctx, snapshot); err != nil {
		return nil, err
	}
	if snapshot.db.Unlocked() {
		if backend, ok := s.backend.(interface {
			Reload(context.Context, *vault.Database) (*vault.Database, error)
		}); ok {
			return backend.Reload(ctx, snapshot.db)
		}
	}
	if s.backend == nil {
		return nil, ErrUnsupported
	}
	return s.backend.Open(ctx, snapshot.db.Path, snapshot.credentials)
}
