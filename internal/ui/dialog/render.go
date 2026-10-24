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

type Header struct {
	Content string
	Divider int
}

type Options struct {
	Mode                Mode
	Title               string
	HeaderContent       string
	HeaderDivider       int
	Body                []string
	Footer              string
	Backdrop            []string
	Width, ScreenWidth  int
	Height, X, Y        int
	Compact, ASCII      bool
	Base, Accent, Muted lipgloss.Style
	Pill                lipgloss.Style
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
	title = strings.ToUpper(title)
	if o.Compact && o.ASCII {
		tl, tr, bl, br = "+", "+", "+", "+"
	} else if o.Compact {
		tl, tr, bl, br = "╭", "╮", "╰", "╯"
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
	var box []string
	if o.Compact {
		box = []string{compactTopBorder(width, title, o.ASCII, horizontal, tl, tr, o.Line, o.Pill, o.Base)}
	} else {
		header := o.Accent.Render(title)
		if o.HeaderContent != "" {
			header = o.HeaderContent
		}
		topRule := tl + strings.Repeat(horizontal, width-2) + tr
		bottomRule := leftRule + strings.Repeat(horizontal, width-2) + rightRule
		if o.HeaderContent != "" {
			topJoin, cross := "┬", "┴"
			if o.ASCII {
				topJoin, cross = "+", "+"
			}
			topRule = ruleWithDivider(width, tl, tr, horizontal, o.HeaderDivider, topJoin)
			bottomRule = ruleWithDivider(width, leftRule, rightRule, horizontal, o.HeaderDivider, cross)
		}
		box = []string{o.Line.Render(topRule), wrap(header, false), o.Line.Render(bottomRule)}
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

func ruleWithDivider(width int, left, right, horizontal string, divider int, join string) string {
	runes := []rune(strings.Repeat(horizontal, max(0, width-2)))
	if divider > 0 && divider < width-1 {
		runes[divider-1] = []rune(join)[0]
	}
	return left + string(runes) + right
}

func renderBox(o Options, background []string, width int, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, footer string) []string {
	inner := width - 4
	var content []string
	if !o.Compact {
		header := o.Accent.Render("  " + title)
		if o.HeaderContent != "" {
			header = o.HeaderContent
		}
		content = append(content, header, o.Line.Render(strings.Repeat(horizontal, width)))
	}
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
	topBorder := o.Line.Render(tl + strings.Repeat(horizontal, width-2) + tr)
	if o.Compact {
		topBorder = compactTopBorder(width, title, o.ASCII, horizontal, tl, tr, o.Line, o.Pill, o.Base)
	} else if o.HeaderContent != "" {
		join := "┬"
		if o.ASCII {
			join = "+"
		}
		topBorder = o.Line.Render(ruleWithDivider(width, tl, tr, horizontal, o.HeaderDivider, join))
	}
	frame(top-1, topBorder)
	for i, line := range content {
		if i == 1 && !o.Compact {
			rule := leftRule + strings.Repeat(horizontal, width-2) + rightRule
			if o.HeaderContent != "" {
				join := "┴"
				if o.ASCII {
					join = "+"
				}
				rule = ruleWithDivider(width, leftRule, rightRule, horizontal, o.HeaderDivider, join)
			}
			frame(top+i, o.Line.Render(rule))
			continue
		}
		frame(top+i, o.Line.Render(border)+o.Base.Render(" "+fit(ansi.Cut(line, 2, width-2), inner)+" ")+o.Line.Render(border))
	}
	frame(top+len(content), o.Line.Render(bl+strings.Repeat(horizontal, width-2)+br))
	return background
}

func compactTopBorder(width int, title string, ascii bool, horizontal, leftCorner, rightCorner string, line, pillStyle, surface lipgloss.Style) string {
	inner := max(0, width-2)
	pill := renderTitlePill(title, inner, ascii, pillStyle, surface)
	pillWidth := min(inner, lipgloss.Width(pill))
	remaining := max(0, inner-pillWidth)
	left := min(1, remaining)
	right := remaining - left
	return line.Render(leftCorner+strings.Repeat(horizontal, left)) + pill + line.Render(strings.Repeat(horizontal, right)+rightCorner)
}

func renderTitlePill(title string, width int, ascii bool, style, surface lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	pillWidth := min(width, lipgloss.Width(title)+4)
	if ascii {
		label := fit(title, max(0, pillWidth-4))
		return style.Render("[ " + label + " ]")
	}
	return renderPill(title, pillWidth, style, surface)
}

func renderPill(text string, width int, style, surface lipgloss.Style) string {
	if width < 4 {
		return style.Render(fit(text, width))
	}
	center := style.Render(" ") + style.Render(fit(text, max(0, width-4))) + style.Render(" ")
	edge := surface.Foreground(style.GetBackground())
	return fit(edge.Render("")+center+edge.Render(""), width)
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
