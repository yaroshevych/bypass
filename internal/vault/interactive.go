package vault

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

const maxResolvedFieldBytes = 1 << 20

// InteractiveCLI keeps an authenticated KeePassXC child on the original vault.
// It never retains the master password after Open returns.
type InteractiveCLI struct {
	cli    *commandCLI
	logger *slog.Logger
}

func NewInteractiveCLI(binary string) *InteractiveCLI {
	return &InteractiveCLI{cli: newCommandCLI(binary)}
}
func (c *InteractiveCLI) Probe(ctx context.Context) (string, error) {
	version, err := c.cli.Probe(ctx)
	if c.logger != nil {
		c.logger.Info("backend_version", "version", backendVersion.FindString(version), "error", diagnosticError(err))
	}
	return version, err
}
func (c *InteractiveCLI) CommandCapabilities(ctx context.Context) (map[string]bool, error) {
	capabilities, err := c.cli.CommandCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	for key := range capabilities {
		switch key {
		case "open", "ls", "show", "clip", "add", "edit", "mkdir", "mv", "generate", "db-create", "db-info", "db-edit", "rm", "rmdir":
		default:
			capabilities[key] = false
		}
	}
	return capabilities, nil
}

func (c *InteractiveCLI) Open(ctx context.Context, path string, creds Credentials) (_ *Database, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.logger != nil {
		ctx = context.WithValue(ctx, diagnosticKey{}, c.logger.With("attempt", openAttempt.Add(1)))
	}
	started, stage := time.Now(), "validate_credentials"
	diagnostic(ctx, "open_started")
	defer func() {
		event := "open_completed"
		if err != nil {
			event = "open_failed"
		}
		diagnostic(ctx, event, "stage", stage, "error", diagnosticError(err), "elapsed_ms", time.Since(started).Milliseconds())
	}()
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	// Reload requires fresh authentication; a live session cannot reload an
	// externally changed file without a password. Never fall back to cached secrets.
	if creds.Password == nil && !creds.NoPassword {
		return nil, ErrUnlock
	}
	stage = "validate_path"
	path, err = canonicalPath(path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	stage = "start_session"
	session, err := startInteractive(ctx, c.cli.binary, path, creds)
	if err != nil {
		return nil, err
	}
	stage = "find_duplicates"
	renames, err := session.duplicateRenames(ctx)
	if err != nil {
		session.close()
		return nil, err
	}
	if len(renames) > 0 {
		return nil, &DuplicateEntries{Path: path, Renames: renames, session: session}
	}
	stage = "load_metadata"
	db, err := session.load(ctx, path)
	if err != nil {
		session.close()
		return nil, err
	}
	db.interactive = session
	db.interactiveOwner = true
	return db, nil
}

func (c *InteractiveCLI) ResolveField(ctx context.Context, db *Database, _ Credentials, entryUUID, field string) ([]byte, error) {
	if field == "TOTP" {
		result, err := c.TOTP(ctx, db, Credentials{}, entryUUID)
		return result.Code, err
	}
	if db == nil || db.interactive == nil {
		return nil, ErrUnlock
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	db.interactive.transactionMu.Lock()
	defer db.interactive.transactionMu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if db.interactive.failed {
		return nil, ErrInteractiveSessionLost
	}
	allowed := false
	for _, entry := range db.Entries {
		if entry.UUID == entryUUID {
			for _, f := range entry.Fields {
				if f.Name == field {
					allowed = true
				}
			}
		}
	}
	if !allowed {
		return nil, ErrUnsupported
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		return nil, err
	}
	path, _, err := selectors.uniqueEntry(entryUUID)
	if err != nil {
		return nil, err
	}
	// A canceled reveal/copy drains its response without destroying the
	// authenticated session. Full lock closes the process explicitly, bypassing
	// the transaction lock. Results still obey the caller's cancellation.
	if ctx == nil {
		ctx = context.Background()
	}
	readContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultTimeout)
	defer cancel()
	result, err := db.interactive.command(readContext, "show", "-q", "-a", field, "--", path)
	if canceled := contextError(ctx); canceled != nil {
		zero(result)
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	return trimOutputLine(result), nil
}

// stdout and stderr share one OS pipe so the random completion marker follows
// command output in order. All native output stays private and bounded.
type interactiveSession struct {
	transactionMu sync.Mutex
	failed        bool
	recycleBinID  string
	commandMu     sync.Mutex
	closeOnce     sync.Once
	cmd           *exec.Cmd
	input         io.WriteCloser
	output        *os.File
	reader        *bufio.Reader
	done          chan struct{}
	prompt        []byte
	echo          bool
	unlockFailure string
}

func startInteractive(ctx context.Context, binary, path string, creds Credentials) (_ *interactiveSession, err error) {
	stage := "create_pipes"
	defer func() {
		if err != nil {
			diagnostic(ctx, "session_failed", "stage", stage, "error", diagnosticError(err))
		}
	}()
	openArgs := []string{"open", "-q"}
	if ctx.Value(diagnosticKey{}) != nil {
		// Quiet mode suppresses the native reason for authentication failures.
		// Keep the response in the private pipe and log only its classification.
		openArgs = []string{"open"}
	}
	diagnostic(ctx, "authentication_options", "password_enabled", !creds.NoPassword, "key_file_enabled", creds.KeyFile != "", "hardware_key_enabled", creds.YubiKey != "")
	args := appendAuthArgs(openArgs, creds)
	args = append(args, "--", path)
	cmd := exec.Command(binary, args...)
	// Readline echoes piped input. Reserve enough columns to prevent its
	// horizontal-scroll display from truncating quoted commands and markers.
	cmd.Env = append(envWithLocaleC(), "COLUMNS=1048576", "TERM=dumb")
	configureInteractiveProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrUnavailable
	}
	output, childOutput, err := os.Pipe()
	if err != nil {
		input.Close()
		return nil, ErrUnavailable
	}
	cmd.Stdout, cmd.Stderr = childOutput, childOutput
	s := &interactiveSession{cmd: cmd, input: input, output: output, reader: bufio.NewReader(output), done: make(chan struct{})}
	stage = "start_process"
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		childOutput.Close()
		return nil, ErrUnavailable
	}
	childOutput.Close()
	go func() { _ = cmd.Wait(); close(s.done) }()
	_, err = s.withContext(ctx, func() ([]byte, error) {
		stage = "send_credentials"
		if !creds.NoPassword {
			line := append(append([]byte(nil), creds.Password...), '\n')
			defer zero(line)
			if _, err := input.Write(line); err != nil {
				return nil, ErrUnlock
			}
		}
		// Wait until authentication has consumed its input before sending commands.
		stage = "wait_for_unlock_prompt"
		initial, err := s.readUntil([]byte("> "), maxInfoBytes)
		if err != nil {
			zero(initial)
			return nil, ErrUnlock
		}
		defer zero(initial)
		stage = "probe_command_protocol"
		marker, err := completionMarker()
		if err != nil {
			return nil, ErrUnavailable
		}
		if _, err := io.WriteString(input, marker+"\n"); err != nil {
			return nil, ErrUnlock
		}
		tail, err := s.readUntil([]byte("Unknown command "+marker+"\n"), maxInfoBytes)
		if err != nil {
			zero(tail)
			return nil, ErrUnlock
		}
		defer zero(tail)
		prefix := append(append([]byte(nil), initial...), tail[:len(tail)-len("Unknown command "+marker+"\n")]...)
		defer zero(prefix)
		s.echo = bytes.HasSuffix(prefix, []byte(marker+"\n"))
		if s.echo {
			prefix = prefix[:len(prefix)-len(marker)-1]
		}
		stage = "validate_command_prompt"
		if len(prefix) == 0 || !bytes.HasSuffix(prefix, []byte("> ")) {
			return nil, ErrMalformed
		}
		// Qt can emit locale warnings before the first prompt. They are not
		// part of the prompt and must never be replayed into command parsing.
		if index := bytes.LastIndexByte(prefix, '\n'); index >= 0 {
			prefix = prefix[index+1:]
		}
		s.prompt = append([]byte(nil), prefix...)
		return nil, nil
	})
	if err != nil {
		s.close()
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		diagnostic(ctx, "unlock_diagnostic", "reason", s.unlockFailure, "exit_code", exitCode, "context_error", diagnosticError(ctx.Err()))
		return nil, err
	}
	diagnostic(ctx, "session_authenticated", "readline_echo", s.echo)
	return s, nil
}

func completionMarker() (string, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return "bypass_complete_" + hex.EncodeToString(nonce[:]), nil
}

// KeePassXC has its own command parser, not a shell. Quote every argument and
// escape its backslashes and double quotes. Line breaks are never allowed.
func interactiveLine(args ...string) (string, error) {
	var quoted []string
	for _, arg := range args {
		if strings.IndexFunc(arg, unicode.IsControl) >= 0 {
			return "", ErrUnsupported
		}
		quoted = append(quoted, `"`+strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(arg)+`"`)
	}
	line := strings.Join(quoted, " ")
	if len(line) > maxInfoBytes {
		return "", ErrTooLarge
	}
	return line, nil
}

func (s *interactiveSession) command(ctx context.Context, args ...string) ([]byte, error) {
	return s.commandInput(ctx, nil, "", args...)
}

func (s *interactiveSession) commandInput(ctx context.Context, password []byte, passwordPrompt string, args ...string) ([]byte, error) {
	if passwordPrompt == "" {
		return s.commandPrompts(ctx, nil, nil, args...)
	}
	return s.commandPrompts(ctx, [][]byte{password}, []string{passwordPrompt}, args...)
}

func (s *interactiveSession) commandPrompts(ctx context.Context, passwords [][]byte, prompts []string, args ...string) (_ []byte, err error) {
	command := "other"
	if len(args) > 0 {
		switch args[0] {
		case "db-info", "ls", "show", "clip", "add", "edit", "mkdir", "mv", "rm", "rmdir", "db-edit":
			command = args[0]
		}
	}
	started := time.Now()
	attribute := ""
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "-a" {
			switch args[i+1] {
			case "Uuid", "Title", "UserName", "URL", "Tags", "Notes", "Password":
				attribute = args[i+1]
			}
		}
	}
	diagnostic(ctx, "command_started", "command", command, "attribute", attribute)
	defer func() {
		diagnostic(ctx, "command_completed", "command", command, "error", diagnosticError(err), "elapsed_ms", time.Since(started).Milliseconds())
	}()
	line, err := interactiveLine(args...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	// Requests are serialized. Cancellation of a command terminates
	// the process, so no unread response can be used by a subsequent request.
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	return s.withContext(ctx, func() ([]byte, error) {
		marker, err := completionMarker()
		if err != nil {
			return nil, ErrUnavailable
		}
		var preliminary []byte
		defer func() { zero(preliminary) }()
		if len(prompts) > 0 {
			if _, err := io.WriteString(s.input, line+"\n"); err != nil {
				return nil, ErrUnlock
			}
			for index, prompt := range prompts {
				part, err := s.readUntil([]byte(prompt), maxInfoBytes*2)
				if err != nil {
					zero(part)
					return nil, err
				}
				preliminary = append(preliminary, part...)
				zero(part)
				input := append(append([]byte(nil), passwords[index]...), '\n')
				_, writeErr := s.input.Write(input)
				zero(input)
				if writeErr != nil {
					return nil, ErrUnlock
				}
				consumed, err := s.readUntil([]byte("\n"), maxInfoBytes)
				if err != nil {
					zero(consumed)
					return nil, err
				}
				preliminary = append(preliminary, consumed...)
				zero(consumed)
			}
			if _, err := io.WriteString(s.input, marker+"\n"); err != nil {
				return nil, ErrUnlock
			}
		} else {
			if _, err := io.WriteString(s.input, line+"\n"+marker+"\n"); err != nil {
				return nil, ErrUnlock
			}
		}
		suffix := []byte("Unknown command " + marker + "\n")
		limit := maxResolvedFieldBytes + maxInfoBytes
		if len(args) > 0 && args[0] == "ls" {
			limit = maxExportBytes
		}
		response, err := s.readUntil(suffix, limit)
		if err != nil {
			zero(response)
			return nil, err
		}
		if len(preliminary) > 0 {
			combined := append(append([]byte(nil), preliminary...), response...)
			zero(response)
			response = combined
		}
		response = response[:len(response)-len(suffix)]
		prefix := s.prompt
		end := s.prompt
		if s.echo {
			prefix = append(append([]byte(nil), prefix...), []byte(line+"\n")...)
			end = append(append([]byte(nil), end...), []byte(marker+"\n")...)
		}
		if !bytes.HasPrefix(response, prefix) || !bytes.HasSuffix(response, end) || len(response) < len(prefix)+len(end) {
			zero(response)
			return nil, ErrMalformed
		}
		data := append([]byte(nil), response[len(prefix):len(response)-len(end)]...)
		zero(response)
		return data, nil
	})
}

func (s *interactiveSession) readUntil(suffix []byte, limit int) ([]byte, error) {
	var data []byte
	for len(data) < limit {
		b, err := s.reader.ReadByte()
		if err != nil {
			if s.prompt == nil {
				s.unlockFailure = classifyUnlockFailure(data)
			}
			zero(data)
			return nil, ErrUnlock
		}
		data = append(data, b)
		if bytes.HasSuffix(data, suffix) {
			return data, nil
		}
	}
	if s.prompt == nil {
		s.unlockFailure = "startup_output_limit"
	}
	zero(data)
	return nil, ErrTooLarge
}

func (s *interactiveSession) withContext(ctx context.Context, operation func() ([]byte, error)) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ErrCanceled
	}
	type result struct {
		data []byte
		err  error
	}
	complete := make(chan result, 1)
	go func() { data, err := operation(); complete <- result{data, err} }()
	select {
	case result := <-complete:
		if ctx.Err() != nil {
			s.close()
			zero(result.data)
			return nil, ErrCanceled
		}
		if result.err != nil {
			s.close()
		}
		return result.data, result.err
	case <-ctx.Done():
		s.close()
		result := <-complete
		zero(result.data)
		return nil, ErrCanceled
	}
}

func (s *interactiveSession) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		killInteractiveProcess(s.cmd)
		_ = s.input.Close()
		_ = s.output.Close()
		<-s.done
	})
}

func trimOutputLine(data []byte) []byte {
	// Exactly one trailing line ending belongs to the CLI, not to the field.
	if bytes.HasSuffix(data, []byte("\n")) {
		return data[:len(data)-1]
	}
	return data
}

func groupID(path string) string {
	digest := sha256.Sum256([]byte("bypass-interactive-group:" + path))
	return normalizeUUID(hex.EncodeToString(digest[:16]))
}

func (s *interactiveSession) load(ctx context.Context, path string) (*Database, error) {
	// Stock CLI does not expose group UUIDs or protection metadata. Group IDs
	// are view-local. List/search fields are cached; Notes are loaded on focus.
	db := &Database{Path: path, Name: filepath.Base(path), RootUUID: groupID("/")}
	db.Groups = append(db.Groups, Group{UUID: db.RootUUID, Name: "Passwords", Path: "Passwords", SearchEnabled: true})
	rawInfo, err := s.command(ctx, "db-info", "-q")
	if err != nil {
		return nil, err
	}
	info, err := parseDatabaseInfo(rawInfo)
	zero(rawInfo)
	if err != nil {
		diagnostic(ctx, "metadata_rejected", "reason", "database_info_parse", "error", diagnosticError(err))
		return nil, err
	}
	db.RecycleBinEnabled = info.RecycleBinEnabled
	seenEntries := map[string]bool{}
	var walk func(string, string, int) error
	walk = func(groupPath, parentID string, depth int) error {
		reject := func(reason string, row int) {
			diagnostic(ctx, "metadata_rejected", "reason", reason, "depth", depth, "row", row, "groups", len(db.Groups), "entries", len(db.Entries))
		}
		diagnostic(ctx, "group_listing", "group_index", len(db.Groups)-1, "depth", depth)
		if depth > 64 || len(db.Entries)+len(db.Groups) > 10000 {
			reject("group_depth_or_entity_limit", -1)
			return ErrTooLarge
		}
		raw, err := s.command(ctx, "ls", "-q", "-f", "--", groupPath)
		if err != nil {
			return err
		}
		defer zero(raw)
		listing := string(trimOutputLine(raw))
		if listing == "" {
			return nil
		}
		if listing == "[empty]" {
			// The empty-group marker is also a legal entry title. Probe its
			// UUID before deciding that this group has no entries.
			probe, err := s.command(ctx, "show", "-q", "-a", "Uuid", "--", groupPath+"[empty]")
			if err != nil {
				return err
			}
			id := normalizeUUID(strings.Trim(string(trimOutputLine(probe)), "{}"))
			zero(probe)
			if id == "" {
				return nil
			}
		}
		seen := map[string]bool{}
		for row, item := range strings.Split(listing, "\n") {
			if len(db.Entries)+len(db.Groups) >= 10000 {
				reject("entity_limit", row)
				return ErrTooLarge
			}
			if item == "" || seen[item] || strings.IndexFunc(item, unicode.IsControl) >= 0 {
				reason := "duplicate_listing_name"
				if item == "" {
					reason = "empty_listing_name"
				} else if strings.IndexFunc(item, unicode.IsControl) >= 0 {
					reason = "control_character_in_listing_name"
				}
				reject(reason, row)
				return ErrAmbiguous
			}
			seen[item] = true
			if strings.HasSuffix(item, "/") {
				name := strings.TrimSuffix(item, "/")
				if name == "" || strings.Contains(name, "/") {
					reject("invalid_group_name", row)
					return ErrUnsupported
				}
				childPath := groupPath + name + "/"
				id := groupID(childPath)
				db.Groups = append(db.Groups, Group{UUID: id, ParentUUID: parentID, Name: name, Path: "Passwords/" + strings.Trim(childPath, "/"), SearchEnabled: true})
				if err := walk(childPath, id, depth+1); err != nil {
					return err
				}
				continue
			}
			entryPath := groupPath + item
			rawID, err := s.command(ctx, "show", "-q", "-a", "Uuid", "--", entryPath)
			if err != nil {
				return err
			}
			id := normalizeUUID(strings.Trim(string(trimOutputLine(rawID)), "{}"))
			zero(rawID)
			if id == "" || seenEntries[id] {
				reason := "duplicate_entry_uuid"
				if id == "" {
					reason = "invalid_entry_uuid"
				}
				reject(reason, row)
				return ErrAmbiguous
			}
			seenEntries[id] = true
			entry := Entry{UUID: id, GroupUUID: parentID, GroupPath: "Passwords"}
			if nested := strings.Trim(groupPath, "/"); nested != "" {
				entry.GroupPath += "/" + nested
			}
			// Read each field separately: multiline Notes cannot spoof another field.
			for _, field := range []string{"Title", "UserName", "Password", "URL", "Notes"} {
				value := ""
				if field == "Title" {
					value = item
				} else if field == "UserName" || field == "URL" {
					rawValue, err := s.command(ctx, "show", "-q", "-a", field, "--", entryPath)
					if err != nil {
						return err
					}
					value = string(trimOutputLine(rawValue))
					zero(rawValue)
				}
				entry.Fields = append(entry.Fields, Field{Name: field, Value: value, Protected: field == "Password"})
			}
			// Tags are metadata, not a delimiter for other values.
			rawTags, err := s.command(ctx, "show", "-q", "-a", "Tags", "--", entryPath)
			if err != nil {
				return err
			}
			tags := string(trimOutputLine(rawTags))
			zero(rawTags)
			if tags != "" {
				entry.Tags = splitTags(tags)
			}
			db.Entries = append(db.Entries, entry)
		}
		return nil
	}
	if err := walk("/", db.RootUUID, 0); err != nil {
		db.Clear()
		return nil, err
	}
	s.markRecycled(db)
	diagnostic(ctx, "metadata_loaded", "groups", len(db.Groups), "entries", len(db.Entries))
	return db, nil
}

// EntryDetails fetches fields that are not needed for the list or its filter.
// Passwords stay in KeePassXC until explicitly copied, revealed, or edited.
func (c *InteractiveCLI) EntryDetails(ctx context.Context, db *Database, entryID string) ([]Field, error) {
	notes, err := c.ResolveField(ctx, db, Credentials{}, entryID, "Notes")
	if err != nil {
		return nil, err
	}
	defer zero(notes)
	fields := []Field{{Name: "Notes", Value: string(notes)}}
	result, err := c.TOTP(ctx, db, Credentials{}, entryID)
	zero(result.Code)
	if err == nil {
		fields = append(fields, Field{Name: "TOTP", Protected: true})
	} else if err != ErrUnsupported {
		return nil, err
	}
	return fields, nil
}
