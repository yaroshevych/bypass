package vault

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrAlreadyExists = errors.New("vault already exists")

const (
	maxInfoBytes = 64 << 10
	maxKDFMillis = 5000
	minKDFMillis = 100
)

// CreateOptions contains settings supported by keepassxc-cli db-create.
type CreateOptions struct {
	// DecryptionTimeMS is the requested KDF delay in milliseconds. Zero uses
	// the native default; KeePassXC accepts values from 100 through 5000.
	DecryptionTimeMS int
}

// DatabaseInfo contains the allowlisted, non-secret summary emitted by
// keepassxc-cli db-info. Description, file location, and raw output are omitted.
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

// DatabaseSettings exposes database properties the CLI can report. The
// current CLI can read these values but cannot edit them on an existing vault.
type DatabaseSettings struct {
	Cipher            string
	KDF               string
	RecycleBinEnabled bool
}

// Create creates an encrypted vault at path without replacing an existing
// file. Passwords are passed to the native CLI over stdin, never argv.
func (c *CLI) Create(ctx context.Context, path string, creds Credentials) (*Database, error) {
	return c.CreateWithOptions(ctx, path, creds, CreateOptions{})
}

// CreateWithOptions is Create with the native CLI's supported create-time KDF
// decryption delay setting.
func (c *CLI) CreateWithOptions(ctx context.Context, path string, creds Credentials, options CreateOptions) (*Database, error) {
	if options.DecryptionTimeMS != 0 && (options.DecryptionTimeMS < minKDFMillis || options.DecryptionTimeMS > maxKDFMillis) {
		return nil, ErrUnsupported
	}
	if err := validateNewCredentials(creds); err != nil {
		return nil, err
	}
	if creds.YubiKey != "" {
		return nil, ErrUnsupported
	}
	target, release, err := lockCreateTarget(path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	defer release()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrInvalidPath
	}
	parent := filepath.Dir(target)
	stageDir, err := os.MkdirTemp(parent, ".bypass-create-*")
	if err != nil {
		return nil, ErrWrite
	}
	defer os.RemoveAll(stageDir)
	if err := os.Chmod(stageDir, 0700); err != nil {
		return nil, ErrWrite
	}
	stagePath := filepath.Join(stageDir, "new.kdbx")
	if err := c.runCreate(ctx, stagePath, creds, options); err != nil {
		return nil, err
	}
	stageInfo, err := os.Lstat(stagePath)
	if err != nil || !stageInfo.Mode().IsRegular() {
		return nil, ErrWrite
	}
	if err := os.Chmod(stagePath, 0600); err != nil {
		return nil, ErrWrite
	}
	stageCreds := creds.Clone()
	db, err := c.Open(ctx, stagePath, stageCreds)
	stageCreds.Clear()
	if err != nil {
		return nil, ErrWrite
	}
	keepDB := false
	defer func() {
		if !keepDB {
			db.Clear()
		}
	}()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	// A hard link atomically publishes without clobbering a target created by
	// another process after the preflight check.
	if err := os.Link(stagePath, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrAlreadyExists
		}
		return nil, ErrWrite
	}
	if err := syncDirectory(parent); err != nil {
		_ = os.Remove(target)
		return nil, ErrWrite
	}
	revision, err := Revision(target)
	if err != nil {
		_ = os.Remove(target)
		return nil, ErrWrite
	}
	if revision != db.Revision {
		return nil, ErrExternalChanged
	}
	db.Path = target
	keepDB = true
	return db, nil
}

func (c *CLI) runCreate(ctx context.Context, stagePath string, creds Credentials, options CreateOptions) error {
	args := []string{"db-create", "-q"}
	if creds.KeyFile != "" {
		args = append(args, "--set-key-file", creds.KeyFile)
	}
	if len(creds.Password) > 0 {
		args = append(args, "--set-password")
	}
	if options.DecryptionTimeMS > 0 {
		args = append(args, "--decryption-time", strconv.Itoa(options.DecryptionTimeMS))
	}
	args = append(args, stagePath)
	stdin := confirmedPasswordInput(creds.Password)
	defer zero(stdin)
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		return ErrWrite
	}
	return nil
}

// ChangeCredentials replaces a vault's master credentials transactionally.
// It accepts only password and key-file factors supported by the native CLI.
func (c *CLI) ChangeCredentials(ctx context.Context, db *Database, oldCreds, newCreds Credentials, backupDir string) (*Database, error) {
	if db == nil || db.Path == "" || backupDir == "" {
		return nil, ErrWrite
	}
	if err := validateCredentials(oldCreds); err != nil {
		return nil, err
	}
	if err := validateNewCredentials(newCreds); err != nil {
		return nil, err
	}
	if oldCreds.YubiKey != "" || newCreds.YubiKey != "" {
		return nil, ErrUnsupported
	}
	if oldCreds.Password == nil && !oldCreds.NoPassword {
		return nil, ErrUnlock
	}
	if oldCreds.Password != nil && oldCreds.NoPassword {
		return nil, ErrUnlock
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	path, unlock, err := lockCanonicalPath(db.Path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	defer unlock()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	if info.Mode().Perm()&0222 == 0 {
		return nil, ErrReadOnly
	}
	initialRevision, err := Revision(path)
	if err != nil {
		return nil, err
	}
	if initialRevision != db.Revision {
		return nil, ErrExternalChanged
	}
	if !credentialsDiffer(oldCreds, newCreds) {
		return nil, ErrUnsupported
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	stage, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".bypass-*.kdbx")
	if err != nil {
		return nil, ErrWrite
	}
	stagePath := stage.Name()
	stageClosed := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
		_ = os.Remove(stagePath)
	}()
	if err := copyEncrypted(path, stage, 0600); err != nil {
		return nil, ErrWrite
	}
	if err := stage.Close(); err != nil {
		stageClosed = true
		return nil, ErrWrite
	}
	stageClosed = true
	stageRevision, err := Revision(stagePath)
	if err != nil || stageRevision != initialRevision {
		return nil, ErrExternalChanged
	}
	if err := c.runCredentialChange(ctx, stagePath, oldCreds, newCreds); err != nil {
		return nil, err
	}
	verifyCreds := newCreds.Clone()
	updated, err := c.Open(ctx, stagePath, verifyCreds)
	verifyCreds.Clear()
	if err != nil {
		return nil, ErrWrite
	}
	keepDB := false
	defer func() {
		if !keepDB {
			updated.Clear()
		}
	}()
	if err := verifyCredentialChange(db, updated); err != nil {
		return nil, err
	}
	if err := createBackup(backupDir, path, initialRevision); err != nil {
		return nil, ErrBackup
	}
	currentRevision, err := Revision(path)
	if err != nil || currentRevision != initialRevision {
		return nil, ErrExternalChanged
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	newRevision, err := Revision(stagePath)
	if err != nil {
		return nil, ErrWrite
	}
	if err := os.Chmod(stagePath, info.Mode().Perm()); err != nil {
		return nil, ErrWrite
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := os.Rename(stagePath, path); err != nil {
		return nil, ErrWrite
	}
	stagePath = ""
	stageClosed = true
	updated.Path = path
	updated.Revision = newRevision
	keepDB = true
	return updated, nil
}

func (c *CLI) runCredentialChange(ctx context.Context, path string, oldCreds, newCreds Credentials) error {
	args := []string{"db-edit", "-q"}
	args = appendAuthArgs(args, oldCreds)
	if len(newCreds.Password) > 0 {
		args = append(args, "--set-password")
	} else if !oldCreds.NoPassword {
		args = append(args, "--unset-password")
	}
	if oldCreds.KeyFile != newCreds.KeyFile {
		if newCreds.KeyFile != "" {
			args = append(args, "--set-key-file", newCreds.KeyFile)
		} else if oldCreds.KeyFile != "" {
			args = append(args, "--unset-key-file")
		}
	}
	args = append(args, path)
	stdin := credentialChangeInput(oldCreds, newCreds)
	defer zero(stdin)
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		return ErrWrite
	}
	return nil
}

// Info returns only selected database metadata and counts. Raw CLI output,
// path and description are intentionally not returned.
func (c *CLI) Info(ctx context.Context, path string, creds Credentials) (DatabaseInfo, error) {
	if err := validateCredentials(creds); err != nil {
		return DatabaseInfo{}, err
	}
	canonical, unlock, err := lockCanonicalPath(path)
	if err != nil {
		return DatabaseInfo{}, ErrInvalidPath
	}
	defer unlock()
	before, err := Revision(canonical)
	if err != nil {
		return DatabaseInfo{}, err
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	args := []string{"db-info", "-q"}
	args = appendAuthArgs(args, creds)
	args = append(args, canonical)
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.WaitDelay = 250 * time.Millisecond
	var out limitedBuffer
	out.limit = maxInfoBytes
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if !creds.NoPassword {
		passwordLine := append(append([]byte(nil), creds.Password...), '\n')
		cmd.Stdin = bytes.NewReader(passwordLine)
		defer zero(passwordLine)
	}
	cmd.Env = envWithLocaleC()
	if err := cmd.Run(); err != nil {
		if out.exceeded {
			return DatabaseInfo{}, ErrTooLarge
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return DatabaseInfo{}, fmt.Errorf("%w: %v", ErrCanceled, ctx.Err())
		}
		return DatabaseInfo{}, ErrUnlock
	}
	if out.exceeded {
		return DatabaseInfo{}, ErrTooLarge
	}
	after, err := Revision(canonical)
	if err != nil || after != before {
		return DatabaseInfo{}, ErrExternalChanged
	}
	return parseDatabaseInfo(out.Bytes())
}

// Settings returns the settings the native CLI can report. There are no
// writable settings for existing databases in the supported CLI interface.
func (c *CLI) Settings(ctx context.Context, path string, creds Credentials) (DatabaseSettings, error) {
	info, err := c.Info(ctx, path, creds)
	if err != nil {
		return DatabaseSettings{}, err
	}
	return DatabaseSettings{Cipher: info.Cipher, KDF: info.KDF, RecycleBinEnabled: info.RecycleBinEnabled}, nil
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

func credentialsDiffer(oldCreds, newCreds Credentials) bool {
	return !bytes.Equal(oldCreds.Password, newCreds.Password) || oldCreds.NoPassword != newCreds.NoPassword || oldCreds.KeyFile != newCreds.KeyFile
}

func confirmedPasswordInput(password []byte) []byte {
	if len(password) == 0 {
		return nil
	}
	input := make([]byte, 0, len(password)*2+2)
	input = append(input, password...)
	input = append(input, '\n')
	input = append(input, password...)
	input = append(input, '\n')
	return input
}

func credentialChangeInput(oldCreds, newCreds Credentials) []byte {
	input := make([]byte, 0, len(oldCreds.Password)+2*len(newCreds.Password)+3)
	if !oldCreds.NoPassword {
		input = append(input, oldCreds.Password...)
		input = append(input, '\n')
	}
	if len(newCreds.Password) > 0 {
		input = append(input, newCreds.Password...)
		input = append(input, '\n')
		input = append(input, newCreds.Password...)
		input = append(input, '\n')
	}
	return input
}

func verifyCredentialChange(before, after *Database) error {
	if before == nil || after == nil || before.RootUUID != after.RootUUID || before.Name != after.Name ||
		before.RecycleBinUUID != after.RecycleBinUUID || before.RecycleBinEnabled != after.RecycleBinEnabled ||
		!slices.Equal(before.Groups, after.Groups) || !entrySlicesEqual(before.Entries, after.Entries) {
		return ErrWrite
	}
	return nil
}

func entrySlicesEqual(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		left, right := a[i], b[i]
		if left.UUID != right.UUID || left.GroupUUID != right.GroupUUID || left.GroupPath != right.GroupPath ||
			!slices.Equal(left.Fields, right.Fields) || !slices.Equal(left.Tags, right.Tags) || left.Recycled != right.Recycled ||
			!left.Created.Equal(right.Created) || !left.Modified.Equal(right.Modified) || !left.Expires.Equal(right.Expires) ||
			left.ExpiryEnabled != right.ExpiryEnabled || !slices.Equal(left.AttachmentNames, right.AttachmentNames) ||
			left.HistoryCount != right.HistoryCount || left.HasTOTP != right.HasTOTP || left.TOTPPeriodSeconds != right.TOTPPeriodSeconds ||
			!entrySlicesEqual(left.History, right.History) {
			return false
		}
	}
	return true
}

func lockCreateTarget(path string) (string, func(), error) {
	if strings.TrimSpace(path) == "" {
		return "", nil, ErrInvalidPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	base := filepath.Base(abs)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "", nil, ErrInvalidPath
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", nil, ErrInvalidPath
	}
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return "", nil, ErrInvalidPath
	}
	target := filepath.Join(parent, base)
	pathLocks.Lock()
	mu := pathLocks.byPath[target]
	if mu == nil {
		mu = &sync.Mutex{}
		pathLocks.byPath[target] = mu
	}
	pathLocks.Unlock()
	mu.Lock()
	return target, mu.Unlock, nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func envWithLocaleC() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "LC_ALL=") && !strings.HasPrefix(item, "LANG=") {
			env = append(env, item)
		}
	}
	return append(env, "LC_ALL=C", "LANG=C")
}

func parseDatabaseInfo(data []byte) (DatabaseInfo, error) {
	result := DatabaseInfo{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), maxInfoBytes)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "UUID":
			result.UUID = safeInfoValue(value, 128)
		case "Name":
			result.Name = safeInfoValue(value, 512)
		case "Cipher":
			result.Cipher = safeInfoValue(value, 128)
		case "KDF":
			result.KDF = safeInfoValue(value, 512)
		case "Number of groups":
			result.GroupCount, _ = strconv.Atoi(value)
		case "Number of entries":
			result.EntryCount, _ = strconv.Atoi(value)
		case "Number of expired entries":
			result.ExpiredEntryCount, _ = strconv.Atoi(value)
		}
		if strings.HasPrefix(line, "Recycle bin is enabled.") {
			result.RecycleBinEnabled = true
		}
	}
	if err := scanner.Err(); err != nil {
		return DatabaseInfo{}, ErrTooLarge
	}
	if result.UUID == "" {
		return DatabaseInfo{}, ErrMalformed
	}
	return result, nil
}

func safeInfoValue(value string, maxBytes int) string {
	if len(value) > maxBytes {
		value = value[:maxBytes]
	}
	return strings.ToValidUTF8(value, "�")
}
