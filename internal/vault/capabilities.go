package vault

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
)

const maxCapabilityHelpBytes = 64 << 10

var knownCLICommands = []string{
	"add", "analyze", "attachment-export", "attachment-import", "attachment-rm", "clip", "close",
	"db-create", "db-edit", "db-info", "diceware", "edit", "estimate", "export", "generate",
	"help", "import", "ls", "merge", "mkdir", "mv", "open", "rm", "rmdir", "search", "show",
}

// CommandCapabilities probes the bounded native --help output and returns a
// complete map for the commands recognized by this application. Help output
// is never returned or logged.
func (c *CLI) CommandCapabilities(ctx context.Context) (map[string]bool, error) {
	if _, err := exec.LookPath(c.binary); err != nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary, "--help")
	cmd.WaitDelay = 250 * time.Millisecond
	var out limitedBuffer
	out.limit = maxCapabilityHelpBytes
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if out.exceeded {
			return nil, ErrTooLarge
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrCanceled
		}
		var execErr *exec.Error
		if errors.As(err, &execErr) || errors.Is(err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return nil, ErrUnsupported
	}
	if out.exceeded {
		return nil, ErrTooLarge
	}
	return parseCommandCapabilities(out.Bytes())
}

func parseCommandCapabilities(help []byte) (map[string]bool, error) {
	known := make(map[string]bool, len(knownCLICommands))
	for _, name := range knownCLICommands {
		known[name] = false
	}
	recognized := false
	for _, line := range strings.Split(string(help), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, ok := known[fields[0]]; ok {
			known[fields[0]] = true
			recognized = true
		}
	}
	if !recognized {
		return nil, ErrUnsupported
	}
	return known, nil
}
