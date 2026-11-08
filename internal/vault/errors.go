package vault

import (
	"errors"
	"strings"

	wipebuf "bypass/internal/secret"
)

var (
	ErrUnavailable     = errors.New("vault backend unavailable")
	ErrInvalidPath     = errors.New("invalid vault path")
	ErrUnlock          = errors.New("unable to unlock vault")
	ErrCanceled        = errors.New("vault operation canceled")
	ErrMalformed       = errors.New("malformed vault database")
	ErrTooLarge        = errors.New("vault data exceeds size limit")
	ErrReadOnly        = errors.New("vault is read-only")
	ErrAmbiguous       = errors.New("vault selector is ambiguous")
	ErrUnsupported     = errors.New("vault change is unsupported")
	ErrUnsupportedData = errors.New("vault contains data this version cannot preserve; writes are disabled")
	ErrExternalChanged = errors.New("vault changed outside this application")
	ErrWrite           = errors.New("vault write failed")
	ErrBackup          = errors.New("vault backup failed")
)

func validateCredentials(creds Credentials) error {
	if creds.NoPassword && len(creds.Password) > 0 {
		return ErrUnlock
	}
	if creds.YubiKey != "" {
		return ErrUnsupported
	}
	for _, b := range creds.Password {
		if b == '\n' || b == '\r' || b == 0 {
			return ErrUnlock
		}
	}
	for _, value := range []string{creds.KeyFile, creds.YubiKey} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return ErrUnlock
		}
	}
	return nil
}

func zero(data []byte) { wipebuf.Wipe(data) }
