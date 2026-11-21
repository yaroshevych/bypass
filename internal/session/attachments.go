package session

import (
	"context"
	"io"
	"os"
)

// ExportAttachmentToFile writes an attachment to a new private file.
// The caller chooses the destination and obtains any required consent.
func (s *Session) ExportAttachmentToFile(ctx context.Context, snapshot *Snapshot, id, name, path string) error {
	data, err := s.ExportAttachment(ctx, snapshot, id, name)
	defer clear(data)
	if err != nil {
		return err
	}
	return writePrivateExclusive(ctx, path, data)
}

func writePrivateExclusive(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	created := true
	defer func() {
		if created {
			_ = os.Remove(path)
		}
	}()
	for start := 0; start < len(data); {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return err
		}
		end := min(len(data), start+64*1024)
		n, err := file.Write(data[start:end])
		if err != nil {
			_ = file.Close()
			return err
		}
		if n != end-start {
			_ = file.Close()
			return io.ErrShortWrite
		}
		start = end
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	created = false
	return nil
}
