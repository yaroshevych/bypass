package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxAttachmentBytes = 64 << 20

// TOTPResult contains an ephemeral code. Callers own Code and should clear it
// as soon as they finish displaying or copying it.
type TOTPResult struct {
	Code             []byte
	PeriodSeconds    int
	RemainingSeconds int
}

// TOTP reads the current code through KeePassXC CLI instead of calculating it
// from exported seed data. It uses only the standalone `show -q -t` output.
func (c *CLI) TOTP(ctx context.Context, db *Database, creds Credentials, entryUUID string) (TOTPResult, error) {
	entry, path, release, err := lockAndSelectEntry(db, entryUUID)
	if err != nil {
		return TOTPResult{}, err
	}
	defer release()
	if !entry.entry.HasTOTP {
		return TOTPResult{}, ErrUnsupported
	}
	if err := validateCredentials(creds); err != nil {
		return TOTPResult{}, err
	}
	period := entry.entry.TOTPPeriodSeconds
	if period < 1 || period > 86400 {
		period = 30
	}
	args := []string{"show", "-q", "-t"}
	args = appendAuthArgs(args, creds)
	args = append(args, path, entry.entryPath)
	for attempt := 0; attempt < 2; attempt++ {
		started := time.Now()
		out, err := c.runReadCommand(ctx, path, db.Revision, creds, args, 128)
		if err != nil {
			return TOTPResult{}, err
		}
		finished := time.Now()
		defer zero(out)
		if started.Unix()/int64(period) != finished.Unix()/int64(period) {
			zero(out)
			continue
		}
		code := bytes.TrimSuffix(out, []byte("\n"))
		code = bytes.TrimSuffix(code, []byte("\r"))
		if !safeTOTPCode(code) {
			return TOTPResult{}, ErrWrite
		}
		remaining := period - int(finished.Unix()%int64(period))
		return TOTPResult{Code: append([]byte(nil), code...), PeriodSeconds: period, RemainingSeconds: remaining}, nil
	}
	return TOTPResult{}, ErrWrite
}

// TOTP output is rendered in a terminal by callers. Accept only the ASCII
// letters and digits used by numeric and Steam-style codes; reject control
// bytes and terminal escape sequences from an unexpected CLI response.
func safeTOTPCode(code []byte) bool {
	if len(code) == 0 || len(code) > 32 {
		return false
	}
	for _, b := range code {
		if (b < '0' || b > '9') && (b < 'A' || b > 'Z') && (b < 'a' || b > 'z') {
			return false
		}
	}
	return true
}

// ExportAttachment reads attachment bytes through KeePassXC CLI into a
// private temporary file. KeePassXC's --stdout path passes binary content
// through QTextStream and can replace invalid UTF-8 bytes. The caller owns and
// should clear the returned bytes.
func (c *CLI) ExportAttachment(ctx context.Context, db *Database, creds Credentials, entryUUID, name string) ([]byte, error) {
	entry, path, release, err := lockAndSelectEntry(db, entryUUID)
	if err != nil {
		return nil, err
	}
	defer release()
	if !validAttachmentName(name) || !containsString(entry.entry.AttachmentNames, name) {
		return nil, ErrUnsupported
	}
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "keepassui-attachment-export-")
	if err != nil {
		return nil, ErrWrite
	}
	defer os.Remove(tempDir)
	tempFile, err := os.CreateTemp(tempDir, "payload-*.bin")
	if err != nil {
		return nil, ErrWrite
	}
	tempPath := tempFile.Name()
	if err := tempFile.Chmod(0600); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return nil, ErrWrite
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, ErrWrite
	}
	defer removeStagedAttachment(tempPath)
	args := []string{"attachment-export", "-q"}
	args = appendAuthArgs(args, creds)
	args = append(args, "--", path, entry.entryPath, name, tempPath)
	if out, err := c.runReadCommand(ctx, path, db.Revision, creds, args, 1); err != nil {
		return nil, err
	} else {
		zero(out)
	}
	info, err := os.Lstat(tempPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxAttachmentBytes {
		if err != nil {
			return nil, ErrWrite
		}
		if info.Size() > maxAttachmentBytes {
			return nil, ErrTooLarge
		}
		return nil, ErrWrite
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, ErrWrite
	}
	data, err := os.ReadFile(tempPath)
	if err != nil || int64(len(data)) != info.Size() {
		zero(data)
		return nil, ErrWrite
	}
	return data, nil
}

func lockAndSelectEntry(db *Database, entryUUID string) (selectedChange, string, func(), error) {
	if db == nil || db.Path == "" || entryUUID == "" {
		return selectedChange{}, "", func() {}, ErrInvalidPath
	}
	path, release, err := lockCanonicalPath(db.Path)
	if err != nil {
		return selectedChange{}, "", func() {}, ErrInvalidPath
	}
	bad := func(err error) (selectedChange, string, func(), error) {
		release()
		return selectedChange{}, "", func() {}, err
	}
	revision, err := Revision(path)
	if err != nil {
		return bad(err)
	}
	if revision != db.Revision {
		return bad(ErrExternalChanged)
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return bad(err)
	}
	entryPath, entry, err := selectors.uniqueEntry(entryUUID)
	if err != nil {
		return bad(err)
	}
	return selectedChange{entryPath: entryPath, entry: entry, current: entry}, path, release, nil
}

func (c *CLI) runReadCommand(ctx context.Context, path string, revision [32]byte, creds Credentials, args []string, outputLimit int) ([]byte, error) {
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if outputLimit < 1 {
		return nil, ErrUnsupported
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	var stdout limitedBuffer
	stdout.limit = outputLimit
	defer zero(stdout.Bytes())
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if !creds.NoPassword {
		input := append(append([]byte(nil), creds.Password...), '\n')
		cmd.Stdin = bytes.NewReader(input)
		defer zero(input)
	}
	runErr := cmd.Run()
	finalRevision, revisionErr := Revision(path)
	if revisionErr != nil || finalRevision != revision {
		return nil, ErrExternalChanged
	}
	if runErr != nil {
		if stdout.exceeded {
			return nil, ErrTooLarge
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		return nil, ErrWrite
	}
	if stdout.exceeded {
		return nil, ErrTooLarge
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func totpMetadata(fields []Field) (bool, int) {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		values[strings.ToLower(strings.TrimSpace(f.Name))] = strings.TrimSpace(f.Value)
	}
	secret := ""
	for _, key := range []string{
		"totp seed", "timeotp-secret-base32", "timeotp-secret-hex", "timeotp-secret-base64", "timeotp-secret-utf8",
		"keepass2timeotp-secret-base32", "keepass2timeotp-secret-hex", "keepass2timeotp-secret-base64", "keepass2timeotp-secret-utf8",
	} {
		if values[key] != "" {
			secret = values[key]
			break
		}
	}
	period := 0
	for _, key := range []string{"timeotp-period", "timeotp-step", "keepass2timeotp-step"} {
		if period = parsePositivePeriod(values[key]); period > 0 {
			break
		}
	}
	settings := values["totp settings"]
	if settings != "" {
		if parts := strings.Split(settings, ";"); len(parts) > 0 && period == 0 {
			period = parsePositivePeriod(parts[0])
		}
	}
	otp := values["otp"]
	if otp != "" {
		if parsed, err := url.Parse(otp); err == nil {
			query := parsed.Query()
			if secret == "" {
				secret = query.Get("secret")
				if secret == "" {
					secret = query.Get("key")
				}
			}
			if period == 0 {
				period = parsePositivePeriod(query.Get("period"))
				if period == 0 {
					period = parsePositivePeriod(query.Get("step"))
				}
			}
		}
		if secret == "" && strings.Contains(otp, "=") {
			if query, err := url.ParseQuery(otp); err == nil {
				secret = query.Get("secret")
				if secret == "" {
					secret = query.Get("key")
				}
				if period == 0 {
					period = parsePositivePeriod(query.Get("period"))
					if period == 0 {
						period = parsePositivePeriod(query.Get("step"))
					}
				}
			}
		}
	}
	hasTOTP := secret != "" || otp != ""
	if !hasTOTP {
		return false, 0
	}
	if period == 0 {
		period = 30
	}
	return true, period
}

func parsePositivePeriod(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 86400 {
		return 0
	}
	return n
}

func validAttachmentName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\r\n\x00")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
