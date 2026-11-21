// Package session exposes reusable capabilities that are not tied to one
// already-open vault session.
package session

import (
	"context"
	"errors"

	"bypass/internal/vault"
)

var ErrUnsupported = errors.New("session capability is unsupported")

type vaultCreator interface {
	CreateWithOptions(context.Context, string, vault.Credentials, vault.CreateOptions) (*vault.Database, error)
}

type passwordGenerator interface {
	Generate(context.Context, int) ([]byte, error)
}

// Service provides backend capabilities that don't require an unlocked
// session. Vault-bound operations belong to Session.
type Service struct {
	backend vault.Backend
}

func NewService(backend vault.Backend) *Service { return &Service{backend: backend} }

func (s *Service) CanCreateVault() bool {
	if s == nil {
		return false
	}
	_, ok := s.backend.(vaultCreator)
	return ok
}

// CreateVault creates and unlocks a new vault. The caller owns the returned
// database and must either adopt it into a Session or clear it.
func (s *Service) CreateVault(ctx context.Context, path string, credentials vault.Credentials, options vault.CreateOptions) (*vault.Database, error) {
	if s == nil {
		return nil, ErrUnsupported
	}
	creator, ok := s.backend.(vaultCreator)
	if !ok {
		return nil, ErrUnsupported
	}
	return creator.CreateWithOptions(ctx, path, credentials, options)
}

func (s *Service) CanGeneratePassword() bool {
	if s == nil {
		return false
	}
	_, ok := s.backend.(passwordGenerator)
	return ok
}

// GeneratePassword returns caller-owned bytes. The caller must clear them
// after use.
func (s *Service) GeneratePassword(ctx context.Context, length int) ([]byte, error) {
	if s == nil {
		return nil, ErrUnsupported
	}
	generator, ok := s.backend.(passwordGenerator)
	if !ok {
		return nil, ErrUnsupported
	}
	return generator.Generate(ctx, length)
}
