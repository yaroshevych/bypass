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
	"strconv"
	"strings"
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

// runCreate lets KeePassXC create the encrypted file at its final path.
func (c *commandCLI) runCreate(ctx context.Context, path string, creds Credentials, options CreateOptions) error {
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
	args = append(args, path)
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
		if strings.HasPrefix(line, "Recycle bin is enabled.") {
			result.RecycleBinEnabled = true
		}
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
