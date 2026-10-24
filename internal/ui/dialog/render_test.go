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

func TestClassicDialogBodyUsesOneCellInset(t *testing.T) {
	for _, mode := range []Mode{Box, Overlay} {
		t.Run(map[Mode]string{Box: "box", Overlay: "overlay"}[mode], func(t *testing.T) {
			bodyText := lipgloss.NewStyle().Foreground(lipgloss.Color("#aaaaaa")).Render("Item")
			lines := Render(Options{
				Mode: mode, Title: "Dialog", Body: []string{bodyText},
				Backdrop: make([]string, 16), Width: 24, ScreenWidth: 40, Height: 16, X: -1, Y: -1,
			})
			for _, line := range strip(lines) {
				runes := []rune(line)
				index := runeIndex(line, "Item")
				if index < 0 {
					continue
				}
				border := runeIndex(string(runes[:index]), "│")
				if border < 0 || index-border != 2 {
					t.Fatalf("body text starts at column %d after border; want one-cell inset: %q", index-border, line)
				}
				if runes[index+len([]rune("Item"))] != ' ' {
					t.Fatalf("body text has no right-side padding: %q", line)
				}
				return
			}
			t.Fatalf("body text missing from dialog: %q", strings.Join(strip(lines), "\n"))
		})
	}
}

func TestClassicOverlayHeaderDividerConnectsTopAndBottomRules(t *testing.T) {
	lines := Render(Options{
		Mode: Overlay, Title: "Commands", HeaderContent: "COMMANDS │ type to filter 18 commands", HeaderDivider: 11,
		Body: []string{"APP"}, Backdrop: make([]string, 18), Width: 40, ScreenWidth: 50, Height: 18, X: -1, Y: -1,
	})
	var top, header, bottom string
	for _, line := range strip(lines) {
		if strings.Contains(line, "┬") {
			top = line
		}
		if strings.Contains(line, "COMMANDS") {
			header = line
		}
		if strings.Contains(line, "┴") {
			bottom = line
		}
	}
	if top == "" || header == "" || bottom == "" {
		t.Fatalf("header divider junctions are missing: %q", strings.Join(strip(lines), "\n"))
	}
	divider := runeIndex(header, "COMMANDS") + len([]rune("COMMANDS")) + 1
	headerRunes := []rune(header)
	outer := runeIndex(header, "│")
	inner := outer + 1 + runeIndex(string(headerRunes[outer+1:]), "│")
	if runeIndex(top, "┬") != divider || inner != divider || runeIndex(bottom, "┴") != divider {
		t.Fatalf("header divider does not connect through rules: expected=%d top=%d header=%d bottom=%d (%q / %q / %q)", divider, runeIndex(top, "┬"), inner, runeIndex(bottom, "┴"), top, header, bottom)
	}
}

func TestClassicBoxHeaderDividerConnectsTopAndBottomRules(t *testing.T) {
	const title = "KEYBOARD SHORTCUTS"
	lines := Render(Options{
		Mode: Box, Title: "Keyboard Shortcuts", HeaderContent: "  KEYBOARD SHORTCUTS │", HeaderDivider: len(title) + 3,
		Body: []string{"Navigate"}, Backdrop: make([]string, 18), Width: 40, ScreenWidth: 50, Height: 18,
	})
	var top, header, bottom string
	for _, line := range strip(lines) {
		if strings.Contains(line, "┬") {
			top = line
		}
		if strings.Contains(line, title) {
			header = line
		}
		if strings.Contains(line, "┴") {
			bottom = line
		}
	}
	if top == "" || header == "" || bottom == "" {
		t.Fatalf("shortcuts header divider junctions are missing: %q", strings.Join(strip(lines), "\n"))
	}
	divider := runeIndex(header, title) + len([]rune(title)) + 1
	outer := runeIndex(header, "│")
	inner := outer + 1 + runeIndex(string([]rune(header)[outer+1:]), "│")
	if runeIndex(top, "┬") != divider || inner != divider || runeIndex(bottom, "┴") != divider {
		t.Fatalf("shortcuts header divider does not connect through rules: %q / %q / %q", top, header, bottom)
	}
}

func runeIndex(text, target string) int {
	textRunes, targetRunes := []rune(text), []rune(target)
	for i := 0; i+len(targetRunes) <= len(textRunes); i++ {
		if string(textRunes[i:i+len(targetRunes)]) == target {
			return i
		}
	}
	return -1
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
