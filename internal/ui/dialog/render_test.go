package dialog

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestClassicBoxJoinsHeaderAndRightAlignsFooter(t *testing.T) {
	lines := Render(Options{
		Mode: Box, Title: "New Vault", Body: []string{"Path"}, Footer: "Tab next · Ctrl+S submit",
		Backdrop: make([]string, 20), Width: 40, ScreenWidth: 60, Height: 20,
	})
	joined := strings.Join(strip(lines), "\n")
	if !strings.Contains(joined, "NEW VAULT") || !strings.Contains(joined, "tab next · ctrl+S submit") {
		t.Fatalf("classic dialog title or footer was not formatted: %q", joined)
	}
	foundHeader := false
	for _, line := range strip(lines) {
		if strings.HasPrefix(strings.TrimSpace(line), "├") && strings.HasSuffix(strings.TrimSpace(line), "┤") {
			foundHeader = true
		}
	}
	if !foundHeader {
		t.Fatalf("classic box header is not connected to its frame: %q", joined)
	}
}

func TestCompactOverlayKeepsTitleCaseAndOmitsEmptyFooter(t *testing.T) {
	lines := Render(Options{
		Mode: Overlay, Title: "History", Body: []string{"Snapshot"},
		Backdrop: make([]string, 18), Width: 36, ScreenWidth: 50, Height: 18, X: -1, Y: -1, Compact: true,
	})
	joined := strings.Join(strip(lines), "\n")
	if !strings.Contains(joined, "History") || strings.Contains(joined, "HISTORY") {
		t.Fatalf("compact overlay title casing changed: %q", joined)
	}
	firstFrame := -1
	for _, line := range strip(lines) {
		if strings.Contains(line, "┌") {
			firstFrame = strings.Index(line, "┌")
			break
		}
	}
	if firstFrame != 7 {
		t.Fatalf("overlay was not centered horizontally: left edge %d, want 7", firstFrame)
	}
	for _, line := range strip(lines) {
		if strings.Contains(line, "tab next") || strings.Contains(line, "ctrl+s") {
			t.Fatalf("empty footer unexpectedly rendered: %q", line)
		}
	}
}

func strip(lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = ansi.Strip(line)
	}
	return result
}
