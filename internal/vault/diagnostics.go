package vault

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sync/atomic"
)

type diagnosticKey struct{}

var openAttempt atomic.Uint64
var backendVersion = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+\b`)

// SetLogger enables metadata-only diagnostics. Never pass CLI output, arguments,
// paths, field values, or credentials to this logger.
func (c *InteractiveCLI) SetLogger(logger *slog.Logger) { c.logger = logger }

func diagnostic(ctx context.Context, event string, attributes ...any) {
	if ctx == nil {
		return
	}
	if logger, ok := ctx.Value(diagnosticKey{}).(*slog.Logger); ok {
		logger.Info(event, attributes...)
	}
}

// Classify errors instead of logging their text: wrapped errors can contain
// user-controlled paths or native output.
func diagnosticError(err error) string {
	for _, known := range []struct {
		err  error
		name string
	}{
		{nil, "none"}, {ErrCanceled, "canceled"}, {context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"},
		{ErrUnavailable, "cli_unavailable"}, {ErrInvalidPath, "invalid_path"},
		{ErrUnlock, "unlock_or_protocol_failure"}, {ErrMalformed, "malformed_response"},
		{ErrTooLarge, "size_limit"}, {ErrAmbiguous, "ambiguous_data"},
		{ErrUnsupported, "unsupported_data"},
	} {
		if errors.Is(err, known.err) {
			return known.name
		}
	}
	return "operation_failed"
}

// Interpret known native errors in memory; never emit the original response.
func classifyUnlockFailure(output []byte) string {
	for _, known := range []struct{ text, reason string }{
		{"Invalid credentials", "credentials_rejected"},
		{"HMAC mismatch", "authentication_check_failed"},
		{"Failed to load key file", "key_file_error"},
		{"not readable", "file_not_readable"},
		{"not found", "file_not_found"},
		{"not a plain file", "not_a_regular_file"},
		{"Unable to open file", "file_open_failed"},
		{"Unsupported KeePass", "unsupported_database_format"},
		{"Error while reading the database", "database_read_error"},
		{"Unknown option", "unsupported_cli_option"},
		{"Missing positional argument", "invalid_cli_arguments"},
		{"YubiKey", "hardware_key_error"},
	} {
		if bytes.Contains(output, []byte(known.text)) {
			return known.reason
		}
	}
	if len(output) == 0 {
		return "no_native_output"
	}
	return "unrecognized_native_response"
}
