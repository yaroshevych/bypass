package vault

import (
	"errors"
	"os"
)

var ErrAlreadyExists = errors.New("vault already exists")

const (
	maxKDFMillis = 5000
	minKDFMillis = 100
)

// CreateOptions configures a new KeePass vault.
type CreateOptions struct {
	// DecryptionTimeMS is the requested key derivation delay in milliseconds.
	// Zero selects the runtime default.
	DecryptionTimeMS int
}

// DatabaseInfo contains non-secret information about an open database.
type DatabaseInfo struct {
	Name              string
	UUID              string
	Cipher            string
	KDF               string
	RecycleBinEnabled bool
	GroupCount        int
	EntryCount        int
	ExpiredEntryCount int
}

func validateNewCredentials(creds Credentials) error {
	if err := validateCredentials(creds); err != nil {
		return err
	}
	if creds.NoPassword {
		if creds.KeyFile == "" {
			return ErrUnlock
		}
	} else if len(creds.Password) == 0 {
		return ErrUnlock
	}
	if creds.KeyFile != "" {
		info, err := os.Stat(creds.KeyFile)
		if err != nil || !info.Mode().IsRegular() {
			return ErrInvalidPath
		}
	}
	return nil
}
