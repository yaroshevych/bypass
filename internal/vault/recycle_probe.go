package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// discoverRecycleBin is called only after the caller has made an encrypted
// backup and received authorization for a write. The random entry lets the
// native search command reveal the actual bin without exporting vault data.
func (s *interactiveSession) discoverRecycleBin(ctx context.Context) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	title := "bypass-probe-" + hex.EncodeToString(random[:])
	rootPath := "/" + title
	command := func(expected string, args ...string) error {
		out, err := s.command(ctx, args...)
		ok := bytes.Equal(out, []byte(expected+"\n"))
		zero(out)
		if err != nil || !ok {
			return errors.Join(ErrWrite, err)
		}
		return nil
	}
	if err := command("Successfully added entry "+title+".", "add", "--", rootPath); err != nil {
		return "", fmt.Errorf("add probe: %w", err)
	}
	if err := command("Successfully recycled entry "+title+".", "rm", "--", rootPath); err != nil {
		return "", fmt.Errorf("recycle probe: %w", err)
	}
	out, err := s.command(ctx, "search", "--", title)
	if err != nil {
		zero(out)
		return "", err
	}
	path := string(trimOutputLine(out))
	zero(out)
	if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/"+title) || strings.Count(path, "\n") != 0 {
		return "", ErrAmbiguous
	}
	binPath := strings.TrimSuffix(path, title)
	if binPath == "/" || strings.ContainsAny(binPath, "\r\x00") {
		return "", ErrAmbiguous
	}
	if err := command("Successfully deleted entry "+title+".", "rm", "--", path); err != nil {
		return "", fmt.Errorf("remove probe: %w", err)
	}
	s.recycleBinPath = binPath
	s.recycleBinID = groupID(binPath)
	return binPath, nil
}
