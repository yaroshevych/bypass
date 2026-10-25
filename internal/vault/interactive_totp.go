package vault

import (
	"context"
)

// TOTP asks the authenticated native session for a code, never its seed.
func (c *InteractiveCLI) TOTP(ctx context.Context, db *Database, _ Credentials, entryID string) (TOTPResult, error) {
	if db == nil || db.interactive == nil {
		return TOTPResult{}, ErrUnlock
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s := db.interactive
	s.transactionMu.Lock()
	defer s.transactionMu.Unlock()
	if err := contextError(ctx); err != nil {
		return TOTPResult{}, err
	}
	if s.failed {
		return TOTPResult{}, ErrInteractiveSessionLost
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return TOTPResult{}, err
	}
	path, _, err := selectors.uniqueEntry(entryID)
	if err != nil {
		return TOTPResult{}, err
	}
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultTimeout)
	defer cancel()
	output, err := s.command(readCtx, "show", "-q", "--totp", "--", path)
	if canceled := contextError(ctx); canceled != nil {
		zero(output)
		return TOTPResult{}, canceled
	}
	if err != nil {
		zero(output)
		return TOTPResult{}, err
	}
	code := trimOutputLine(output)
	if !validTOTPCode(code) {
		zero(output)
		return TOTPResult{}, ErrUnsupported
	}
	// Native show exposes no expiry metadata. Poll rather than guess the
	// configured period or read the seed to calculate an expiry ourselves.
	return TOTPResult{Code: code}, nil
}

func validTOTPCode(code []byte) bool {
	if len(code) < 5 || len(code) > 10 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			if len(code) != 5 || c < 'A' || c > 'Z' {
				return false
			}
		}
	}
	return true
}
