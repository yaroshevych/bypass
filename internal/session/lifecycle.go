package session

import (
	"context"

	"bypass/internal/vault"
)

type credentialChanger interface {
	ChangeCredentials(context.Context, *vault.Database, vault.Credentials, vault.Credentials, string) (*vault.Database, error)
}

type credentialLossChanger interface {
	ChangeCredentialsAllowingMetadataLoss(context.Context, *vault.Database, vault.Credentials, string, [32]byte) (*vault.Database, error)
}

type databaseInspector interface {
	InfoDatabase(context.Context, *vault.Database) (vault.DatabaseInfo, error)
}

func (s *Session) CanChangeCredentials() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(credentialChanger)
	return ok
}

func (s *Session) ChangeCredentials(ctx context.Context, snapshot *Snapshot, next vault.Credentials, backupDir string) (*vault.Database, error) {
	if !s.validSnapshot(snapshot) {
		return nil, ErrLocked
	}
	changer, ok := s.backend.(credentialChanger)
	if !ok {
		return nil, ErrUnsupported
	}
	old := snapshot.credentials.Clone()
	defer old.Clear()
	return changer.ChangeCredentials(ctx, snapshot.db, old, next, backupDir)
}

func (s *Session) CanChangeCredentialsAllowingMetadataLoss() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(credentialLossChanger)
	return ok
}

func (s *Session) ChangeCredentialsAllowingMetadataLoss(ctx context.Context, snapshot *Snapshot, next vault.Credentials, backupDir string, consent [32]byte) (*vault.Database, error) {
	if !s.validSnapshot(snapshot) {
		return nil, ErrLocked
	}
	changer, ok := s.backend.(credentialLossChanger)
	if !ok {
		return nil, ErrUnsupported
	}
	return changer.ChangeCredentialsAllowingMetadataLoss(ctx, snapshot.db, next, backupDir, consent)
}

func (s *Session) CanInspectDatabase() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(databaseInspector)
	return ok
}

func (s *Session) InfoDatabase(ctx context.Context, snapshot *Snapshot) (vault.DatabaseInfo, error) {
	if !s.validSnapshot(snapshot) {
		return vault.DatabaseInfo{}, ErrLocked
	}
	inspector, ok := s.backend.(databaseInspector)
	if !ok {
		return vault.DatabaseInfo{}, ErrUnsupported
	}
	return inspector.InfoDatabase(ctx, snapshot.db)
}
