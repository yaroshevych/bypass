package widget

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"bypass/internal/run"
	"bypass/internal/vault"
)

type Target struct {
	Reference, EntryID, Title, Group, Field string
	Revision                                [32]byte `json:"-"`
}

// Targets resolves every requested field against the current unlocked vault.
// Ambiguous or missing references never select an arbitrary entry.
func (s *Session) Targets(ctx context.Context, references []string) ([]Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targets(ctx, references)
}

func (s *Session) targets(ctx context.Context, references []string) ([]Target, error) {
	if s.db == nil {
		return nil, errors.New("vault is locked")
	}
	var targets []Target
	for _, reference := range references {
		ref, err := run.ParseReference(reference)
		if err != nil {
			return nil, err
		}
		parts := ref.Components
		if ref.Absolute {
			vaultParts := strings.Split(strings.TrimPrefix(s.db.Path, "/"), "/")
			if len(parts) <= len(vaultParts) {
				return nil, errors.New("entry not found in unlocked vault")
			}
			for i := range vaultParts {
				if parts[i] != vaultParts[i] {
					return nil, errors.New("entry not found in unlocked vault")
				}
			}
			parts = parts[len(vaultParts):]
		} else if len(parts) > 1 && parts[0] == filepath.Base(s.db.Path) {
			parts = parts[1:]
		}
		field := ref.Field
		switch strings.ToLower(field) {
		case "password":
			field = "Password"
		case "username":
			field = "UserName"
		case "title":
			field = "Title"
		case "url":
			field = "URL"
		case "notes":
			field = "Notes"
		}
		var selected *vault.Entry
		for i := range s.db.Entries {
			entry := &s.db.Entries[i]
			if entry.Recycled || entry.Get("Title") != parts[len(parts)-1] {
				continue
			}
			if len(parts) > 1 && strings.TrimPrefix(strings.Trim(entry.GroupPath, "/"), "Passwords/") != strings.Join(parts[:len(parts)-1], "/") {
				continue
			}
			if selected != nil {
				return nil, errors.New("entry reference is ambiguous")
			}
			selected = entry
		}
		if selected == nil {
			return nil, errors.New("entry not found in unlocked vault")
		}
		fields, err := s.backend.EntryDetails(ctx, s.db, selected.UUID)
		if err != nil {
			return nil, err
		}
		found := field == "TOTP" && selected.HasTOTP
		for _, item := range fields {
			if item.Name == field {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("field not found in unlocked vault")
		}
		targets = append(targets, Target{reference, selected.UUID, selected.Get("Title"), selected.GroupPath, field, s.db.Revision})
	}
	return targets, nil
}

// ResolveApproved checks targets again while holding the session lock, so a
// vault change between review and approval cannot release different secrets.
func (s *Session) ResolveApproved(ctx context.Context, reviewed []Target) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := make([]string, len(reviewed))
	for i := range reviewed {
		refs[i] = reviewed[i].Reference
	}
	current, err := s.targets(ctx, refs)
	if err != nil {
		return nil, err
	}
	for i := range current {
		if current[i] != reviewed[i] {
			return nil, errors.New("vault changed during review")
		}
	}
	values := make(map[string][]byte, len(current))
	for _, target := range current {
		value, err := s.backend.ResolveField(ctx, s.db, vault.Credentials{}, target.EntryID, target.Field)
		if err != nil {
			for _, v := range values {
				clear(v)
			}
			return nil, err
		}
		values[target.Reference] = value
	}
	return values, nil
}
