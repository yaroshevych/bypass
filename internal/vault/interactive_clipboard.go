package vault

import (
	"bytes"
	"context"
	"time"
)

// CopyPassword delegates copying to KeePassXC. Its output
// contains only status messages; the password never crosses Bypass's pipe.
func (c *InteractiveCLI) CopyPassword(ctx context.Context, db *Database, entryID string) error {
	if db == nil || db.interactive == nil {
		return ErrUnlock
	}
	s := db.interactive
	s.transactionMu.Lock()
	defer s.transactionMu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if s.failed {
		return ErrInteractiveSessionLost
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return err
	}
	path, _, err := selectors.uniqueEntry(entryID)
	if err != nil {
		return err
	}
	// Drain this short command even if another UI copy cancels this request.
	// Full vault lock still terminates the child directly.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	output, err := s.command(ctx, "clip", "--", path, "0")
	defer zero(output)
	if err != nil {
		return err
	}
	if !bytes.Contains(output, []byte("attribute copied to the clipboard!")) {
		return ErrWrite
	}
	return nil
}
