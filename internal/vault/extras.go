package vault

import (
	"strings"
)

const maxAttachmentBytes = 64 << 20

// TOTPResult contains an ephemeral code. Callers own Code and should clear it
// as soon as they finish displaying or copying it.
type TOTPResult struct {
	Code             []byte
	PeriodSeconds    int
	RemainingSeconds int
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
