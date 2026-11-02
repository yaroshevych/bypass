// Package dialog renders the shared frame and layout for Bypass dialogs.
// It does not own dialog content, behavior, or modal backdrop generation.
package dialog

import (
	"strings"

	"bypass/internal/ui/glyphs"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type Mode uint8

const (
	Box Mode = iota
	Overlay
)

type Header struct {
	Content        string
	Divider        int
	Accessory      string
	AccessoryStyle lipgloss.Style
	Count          string
}

// BodyRule marks a body row as a full-width rule joined to both dialog sides.
const BodyRule = "\x00bypass-dialog-body-rule"

type Options struct {
	Mode                       Mode
	Title                      string
	HeaderContent              string
	HeaderDivider              int
	CompactAccessory           string
	CompactAccessoryStyle      lipgloss.Style
	CompactCount               string
	Body                       []string
	BodyRows                   int
	StatusBar                  string
	Backdrop                   []string
	Width, ScreenWidth         int
	Height, X, Y               int
	Compact, ASCII             bool
	HideHeaderRule             bool
	Surface, Accent, Secondary lipgloss.Style
	Pill                       lipgloss.Style
	Divider                    lipgloss.Style
	RestoreResets              func(string) string
}

// OverlayWidth returns the actual width used by an overlay frame.
func OverlayWidth(width, screenWidth int) int {
	return min(max(20, width), max(20, screenWidth-2))
}

// OverlayGeometry describes the frame used for rendering and hit testing.
type OverlayGeometry struct {
	X, Y, Width, Height, HeaderRows int
}

func LayoutOverlay(width, screenWidth, screenHeight, bodyRows int, compact bool, x, y int) OverlayGeometry {
	width = OverlayWidth(width, screenWidth)
	header := 3
	if compact {
		header = 1
	}
	height := header + bodyRows + 1
	if x < 0 {
		x = (screenWidth - width) / 2
	}
	if y < 0 {
		y = (screenHeight - height) / 2
	}
	return OverlayGeometry{X: min(max(0, x), max(0, screenWidth-width)), Y: min(max(0, y), max(0, screenHeight-height)), Width: width, Height: height, HeaderRows: header}
}

// Render places a framed dialog over a pre-rendered, already shaded backdrop.
func Render(o Options) []string {
	if o.Height <= 0 {
		return nil
	}
	width := o.Width
	if o.Mode == Overlay {
		width = OverlayWidth(width, o.ScreenWidth)
	}
	if width <= 0 {
		width = max(1, o.Width)
	}
	statusBar := lowercaseShortcutLabels(o.StatusBar)
	chrome := 4
	if o.Compact {
		chrome = 2
	}
	maxBody := max(1, o.Height-chrome)
	if statusBar != "" {
		maxBody = max(1, maxBody-1)
	}
	if o.BodyRows > 0 {
		maxBody = o.BodyRows
		if statusBar != "" {
			maxBody = max(1, maxBody-1)
		}
	}
	if len(o.Body) > maxBody {
		// Keep stacked actions visible when a short terminal clips the body.
		rule := -1
		for i := len(o.Body) - 1; i >= 0; i-- {
			if o.Body[i] == BodyRule {
				rule = i
				break
			}
		}
		if rule >= 0 && len(o.Body)-rule <= maxBody {
			prefix := maxBody - (len(o.Body) - rule)
			o.Body = append(append([]string(nil), o.Body[:prefix]...), o.Body[rule:]...)
		} else {
			o.Body = o.Body[:maxBody]
		}
	}
	if o.BodyRows > 0 {
		for len(o.Body) < maxBody {
			o.Body = append(o.Body, "")
		}
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
	background := make([]string, o.Height)
	for i := range background {
		if i < len(o.Backdrop) {
			background[i] = fit(o.Backdrop[i], o.ScreenWidth)
		}
	}

	if o.Mode == Overlay {
		return renderOverlay(o, background, width, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, statusBar)
	}
	return renderBox(o, background, width, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, statusBar)
}

// BodyCapacity returns the number of dialog rows available for content after
// reserving one row for a visible status bar.
func BodyCapacity(height int, statusBar string) int {
	capacity := max(1, height-8)
	if statusBar != "" {
		capacity = max(1, capacity-1)
	}
	return capacity
}

func renderOverlay(o Options, background []string, width int, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, footer string) []string {
	wrap := func(text string, right bool) string {
		if right && o.Compact {
			content := fitRight(text, width-3)
			return o.Divider.Render(border) + o.Surface.Render(" "+content) + o.Divider.Render(border)
		}
		content := fit(text, width-4)
		if right {
			content = fitRight(text, width-4)
		}
		return o.Divider.Render(border) + o.Surface.Render(" "+content+" ") + o.Divider.Render(border)
	}
	var box []string
	if o.Compact {
		box = []string{compactTopBorder(width, title, o.ASCII, horizontal, tl, tr, o.Divider, o.Pill, o.Surface, o.CompactAccessory, o.CompactAccessoryStyle, o.CompactCount)}
	} else {
		header := o.Accent.Render(" " + title + " ")
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
		box = []string{o.Divider.Render(topRule), wrap(header, false), o.Divider.Render(bottomRule)}
	}
	for _, line := range o.Body {
		if line == BodyRule {
			box = append(box, o.Divider.Render(leftRule+strings.Repeat(horizontal, max(0, width-2))+rightRule))
			continue
		}
		box = append(box, wrap(line, false))
	}
	if footer != "" {
		box = append(box, wrap(o.Secondary.Render(footer), true))
	}
	box = append(box, o.Divider.Render(bl+strings.Repeat(horizontal, width-2)+br))
	for i := range box {
		if o.RestoreResets != nil {
			box[i] = o.RestoreResets(box[i])
		}
	}
	headerRows := 3
	if o.Compact {
		headerRows = 1
	}
	g := LayoutOverlay(width, o.ScreenWidth, o.Height, len(box)-headerRows-1, o.Compact, o.X, o.Y)
	x, y := g.X, g.Y
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
	footerRow := -1
	headerRuleRow := -1
	if !o.Compact {
		header := o.Accent.Render("   " + title + " ")
		if o.HeaderContent != "" {
			header = o.HeaderContent
		}
		content = append(content, header)
		if !o.HideHeaderRule {
			headerRuleRow = len(content)
			content = append(content, o.Divider.Render(strings.Repeat(horizontal, width)))
		}
	}
	for _, line := range o.Body {
		if line == BodyRule {
			content = append(content, BodyRule)
		} else {
			content = append(content, "  "+fit(line, inner))
		}
	}
	if footer != "" {
		footerRow = len(content)
		content = append(content, o.Secondary.Render("  "+fitRight(footer, inner)))
	}
	for i := range content {
		if o.RestoreResets != nil {
			content[i] = o.RestoreResets(content[i])
		}
	}
	left := max(0, (o.ScreenWidth-width)/2)
	top := max(0, (o.Height-len(content))/2)
	frame := func(row int, text string) {
		if row >= 0 && row < len(background) {
			background[row] = ansi.Cut(background[row], 0, left) + text + ansi.Cut(background[row], left+width, o.ScreenWidth)
		}
	}
	topBorder := o.Divider.Render(tl + strings.Repeat(horizontal, width-2) + tr)
	if o.Compact {
		topBorder = compactTopBorder(width, title, o.ASCII, horizontal, tl, tr, o.Divider, o.Pill, o.Surface, "", lipgloss.NewStyle(), "")
	} else if o.HeaderContent != "" {
		join := "┬"
		if o.ASCII {
			join = "+"
		}
		topBorder = o.Divider.Render(ruleWithDivider(width, tl, tr, horizontal, o.HeaderDivider, join))
	}
	frame(top-1, topBorder)
	for i, line := range content {
		if line == BodyRule {
			frame(top+i, o.Divider.Render(leftRule+strings.Repeat(horizontal, max(0, width-2))+rightRule))
			continue
		}
		if i == headerRuleRow {
			rule := leftRule + strings.Repeat(horizontal, width-2) + rightRule
			if o.HeaderContent != "" {
				join := "┴"
				if o.ASCII {
					join = "+"
				}
				rule = ruleWithDivider(width, leftRule, rightRule, horizontal, o.HeaderDivider, join)
			}
			frame(top+i, o.Divider.Render(rule))
			continue
		}
		if o.Compact && i == footerRow {
			footerContent := o.Secondary.Render(fitRight(footer, inner+1))
			if o.RestoreResets != nil {
				footerContent = o.RestoreResets(footerContent)
			}
			frame(top+i, o.Divider.Render(border)+o.Surface.Render(" "+footerContent)+o.Divider.Render(border))
			continue
		}
		frame(top+i, o.Divider.Render(border)+o.Surface.Render(" "+fit(ansi.Cut(line, 2, width-2), inner)+" ")+o.Divider.Render(border))
	}
	frame(top+len(content), o.Divider.Render(bl+strings.Repeat(horizontal, width-2)+br))
	return background
}

func compactTopBorder(width int, title string, ascii bool, horizontal, leftCorner, rightCorner string, line, pillStyle, surface lipgloss.Style, accessory string, accessoryStyle lipgloss.Style, count string) string {
	inner := max(0, width-2)
	if accessory != "" || count != "" {
		pill := renderTitlePillWithRightSurface(title, inner, ascii, pillStyle, surface, accessoryStyle)
		prefix := line.Render(leftCorner+horizontal) + pill
		suffix := surface.Render(" ") + count + line.Render(horizontal+rightCorner)
		budget := max(0, width-lipgloss.Width(prefix)-lipgloss.Width(suffix))
		contentBudget := max(0, budget-1)
		accessory = ansi.Truncate(accessory, contentBudget, "…")
		if ascii {
			accessory += accessoryStyle.Render("]")
		} else {
			edge := surface.Foreground(accessoryStyle.GetBackground())
			accessory += edge.Render(glyphs.Right())
		}
		remaining := max(0, budget-lipgloss.Width(accessory))
		return prefix + accessory + line.Render(strings.Repeat(horizontal, remaining)) + suffix
	}
	pill := renderTitlePill(title, inner, ascii, pillStyle, surface)
	pillWidth := min(inner, lipgloss.Width(pill))
	remaining := max(0, inner-pillWidth)
	left := min(1, remaining)
	right := remaining - left
	return line.Render(leftCorner+strings.Repeat(horizontal, left)) + pill + line.Render(strings.Repeat(horizontal, right)+rightCorner)
}

func renderTitlePillWithRightSurface(title string, width int, ascii bool, style, leftSurface, rightSurface lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	pillWidth := min(width, lipgloss.Width(title)+2)
	if ascii {
		return style.Render("[" + fit(title, max(0, pillWidth-2)) + "]")
	}
	center := style.Render(fit(title, max(0, pillWidth-2)))
	left := leftSurface.Foreground(style.GetBackground()).Render(glyphs.Left())
	right := rightSurface.Foreground(style.GetBackground()).Render(glyphs.Right())
	return fit(left+center+right, pillWidth)
}

func renderTitlePill(title string, width int, ascii bool, style, surface lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	pillWidth := min(width, lipgloss.Width(title)+2)
	if ascii {
		label := fit(title, max(0, pillWidth-2))
		return style.Render("[" + label + "]")
	}
	if pillWidth < 2 {
		return style.Render(fit(title, pillWidth))
	}
	edge := surface.Foreground(style.GetBackground())
	return fit(edge.Render("")+style.Render(fit(title, pillWidth-2))+edge.Render(""), pillWidth)
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
