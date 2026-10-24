// Package dialog renders the shared frame and layout for Bypass dialogs.
// It does not own dialog content, behavior, or modal backdrop generation.
package dialog

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type Mode uint8

const (
	Box Mode = iota
	Overlay
)

type Options struct {
	Mode                Mode
	Title               string
	Body                []string
	Footer              string
	Backdrop            []string
	Width, ScreenWidth  int
	Height, X, Y        int
	Compact, ASCII      bool
	Base, Accent, Muted lipgloss.Style
	Line                lipgloss.Style
}

// Render places a framed dialog over a pre-rendered, already shaded backdrop.
func Render(o Options) []string {
	if o.Height <= 0 {
		return nil
	}
	width := o.Width
	if o.Mode == Overlay {
		width = min(max(20, width), max(20, o.ScreenWidth-2))
	}
	if width <= 0 {
		width = max(1, o.Width)
	}
	maxBody := max(1, o.Height-8)
	if len(o.Body) > maxBody {
		o.Body = o.Body[:maxBody]
	}
	border, horizontal := "│", "─"
	tl, tr, bl, br := "┌", "┐", "└", "┘"
	leftRule, rightRule := "├", "┤"
	if o.ASCII {
		border, horizontal = "|", "-"
		tl, tr, bl, br = "+", "+", "+", "+"
		leftRule, rightRule = "+", "+"
	}
	title := o.Title
	if !o.Compact {
		title = strings.ToUpper(title)
	}
	footer := lowercaseShortcutLabels(o.Footer)
	background := make([]string, o.Height)
	for i := range background {
		if i < len(o.Backdrop) {
			background[i] = fit(o.Backdrop[i], o.ScreenWidth)
		}
	}

	if o.Mode == Overlay {
		return renderOverlay(o, background, width, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, footer)
	}
	return renderBox(o, background, width, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, footer)
}

func renderOverlay(o Options, background []string, width int, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, footer string) []string {
	wrap := func(text string, right bool) string {
		content := fit(text, width-4)
		if right {
			content = fitRight(text, width-4)
		}
		return o.Line.Render(border) + o.Base.Render(" "+content+" ") + o.Line.Render(border)
	}
	box := []string{o.Line.Render(tl + strings.Repeat(horizontal, width-2) + tr), wrap(o.Accent.Render(title), false)}
	if o.Compact {
		box = append(box, wrap(o.Muted.Render(strings.Repeat(horizontal, width-4)), false))
	} else {
		box = append(box, o.Line.Render(leftRule+strings.Repeat(horizontal, width-2)+rightRule))
	}
	for _, line := range o.Body {
		box = append(box, wrap(line, false))
	}
	if footer != "" {
		box = append(box, wrap(o.Muted.Render(footer), true))
	}
	box = append(box, o.Line.Render(bl+strings.Repeat(horizontal, width-2)+br))
	x := o.X
	if x < 0 {
		x = (o.ScreenWidth - width) / 2
	}
	x = min(max(0, x), max(0, o.ScreenWidth-width))
	y := o.Y
	if y < 0 {
		y = (o.Height - len(box)) / 2
	}
	y = min(max(0, y), max(0, o.Height-len(box)))
	for i, line := range box {
		row := y + i
		if row >= o.Height {
			break
		}
		background[row] = ansi.Cut(background[row], 0, x) + line + ansi.Cut(background[row], x+width, o.ScreenWidth)
	}
	return background
}

func renderBox(o Options, background []string, width int, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, footer string) []string {
	inner := width - 4
	content := []string{o.Accent.Render("  " + title), o.Line.Render(strings.Repeat(horizontal, width))}
	for _, line := range o.Body {
		content = append(content, "  "+fit(line, inner))
	}
	if footer != "" {
		content = append(content, o.Muted.Render("  "+fitRight(footer, inner)))
	}
	left := max(0, (o.ScreenWidth-width)/2)
	top := max(0, (o.Height-len(content))/2)
	frame := func(row int, text string) {
		if row >= 0 && row < len(background) {
			background[row] = ansi.Cut(background[row], 0, left) + text + ansi.Cut(background[row], left+width, o.ScreenWidth)
		}
	}
	frame(top-1, o.Line.Render(tl+strings.Repeat(horizontal, width-2)+tr))
	for i, line := range content {
		if i == 1 && !o.Compact {
			frame(top+i, o.Line.Render(leftRule+strings.Repeat(horizontal, width-2)+rightRule))
			continue
		}
		frame(top+i, o.Line.Render(border)+o.Base.Render(" "+fit(ansi.Cut(line, 2, width-2), inner)+" ")+o.Line.Render(border))
	}
	frame(top+len(content), o.Line.Render(bl+strings.Repeat(horizontal, width-2)+br))
	return background
}

func lowercaseShortcutLabels(text string) string {
	return strings.NewReplacer("Ctrl+", "ctrl+", "Enter", "enter", "Esc", "esc", "Tab", "tab", "Space", "space", "Delete", "delete", "PgUp", "pgup", "PgDn", "pgdn").Replace(text)
}

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

func fitRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	return strings.Repeat(" ", max(0, width-lipgloss.Width(s))) + s
}
