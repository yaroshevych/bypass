package dialog

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
		Pill: lipgloss.NewStyle().Bold(true),
	})
	joined := strings.Join(strip(lines), "\n")
	if !strings.Contains(joined, "HISTORY") {
		t.Fatalf("compact overlay title casing changed: %q", joined)
	}
	for _, marker := range []string{"╭", "╮", "╰", "╯", "", ""} {
		if !strings.Contains(joined, marker) {
			t.Fatalf("compact overlay missing %q styling: %q", marker, joined)
		}
	}
	titleOnTopLeft := false
	for _, line := range strip(lines) {
		if strings.Contains(line, "╭─ HISTORY") && strings.Contains(line, "╮") {
			titleOnTopLeft = true
			break
		}
	}
	if !titleOnTopLeft {
		t.Fatalf("compact overlay title is not left-aligned in the top border: %q", joined)
	}
	firstFrame := -1
	for _, line := range strip(lines) {
		if strings.Contains(line, "╭") {
			firstFrame = strings.Index(line, "╭")
			break
		}
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

func TestCompactBoxUsesRoundedFrameAndTitlePill(t *testing.T) {
	lines := Render(Options{
		Mode: Box, Title: "New Vault", Body: []string{"Path"},
		Backdrop: make([]string, 20), Width: 40, ScreenWidth: 60, Height: 20, Compact: true,
		Pill: lipgloss.NewStyle().Bold(true),
	})
	joined := strings.Join(strip(lines), "\n")
	for _, marker := range []string{"╭", "╮", "╰", "╯", "", "NEW VAULT", ""} {
		if !strings.Contains(joined, marker) {
			t.Fatalf("compact box missing %q styling: %q", marker, joined)
		}
	}
	titleOnTopLeft := false
	for _, line := range strip(lines) {
		if strings.Contains(line, "╭─ NEW VAULT") && strings.Contains(line, "╮") {
			titleOnTopLeft = true
			break
		}
	}
	if !titleOnTopLeft {
		t.Fatalf("compact box title is not left-aligned in the top border: %q", joined)
	}
}

func strip(lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = ansi.Strip(line)
	}
	return result
}
