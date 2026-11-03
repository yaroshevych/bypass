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

// bodyRule marks a body row as a full-width rule joined to both dialog sides.
const bodyRule = "\x00bypass-dialog-body-rule"

// Action is a rendered button and its semantic target.
type Action struct {
	Text   string
	Target int
}

type Options struct {
	Mode                       Mode
	Title                      string
	Header                     Header
	Body                       []string
	Actions                    []Action
	Error                      string
	BodyRows, MaxBodyRows      int
	StatusBar                  string
	Backdrop                   []string
	Width, ScreenWidth         int
	Height, X, Y               int
	Compact, ASCII             bool
	Fullscreen                 bool
	HideHeaderRule             bool
	Surface, Accent, Secondary lipgloss.Style
	ErrorStyle                 lipgloss.Style
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

type BoxGeometry struct {
	X, Top, Width, BodyY int
	Body                 []string
	BodyIndices          []int
}

// LayoutBox uses the same body clipping and frame positions as Render.
func LayoutBox(o Options) BoxGeometry {
	body, indices := prepareBody(o)
	contentRows := len(body)
	headerRows := 0
	if !o.Compact {
		headerRows = 1
		if !o.HideHeaderRule {
			headerRows++
		}
	}
	contentRows += headerRows
	if lowercaseShortcutLabels(o.StatusBar) != "" {
		contentRows++
	}
	top := max(0, (o.Height-contentRows)/2)
	return BoxGeometry{X: max(0, (o.ScreenWidth-o.Width)/2), Top: top, Width: o.Width, BodyY: top + headerRows, Body: body, BodyIndices: indices}
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

// Hit identifies the semantic target at a screen position. Targets correspond to
// source body rows; a negative value marks a non-interactive row.
type Hit struct {
	Inside bool
	Target int
}

// HitTest uses the same clipping and frame geometry as Render.
func HitTest(o Options, x, y int, targets []int) Hit {
	body, indices := prepareBody(o)
	if o.Fullscreen {
		hit := Hit{Inside: x >= 0 && x < o.ScreenWidth && y >= 0 && y < o.Height, Target: -1}
		row := y - 1
		if hit.Inside && row >= 0 && row < len(indices) {
			hit.Target = targetAt(o, indices[row], targets)
		}
		return hit
	}
	var left, top, width, height, bodyY int
	if o.Mode == Overlay {
		footer := 0
		if lowercaseShortcutLabels(o.StatusBar) != "" {
			footer = 1
		}
		g := LayoutOverlay(o.Width, o.ScreenWidth, o.Height, len(body)+footer, o.Compact, o.X, o.Y)
		left, top, width, height, bodyY = g.X, g.Y, g.Width, g.Height, g.Y+g.HeaderRows
	} else {
		g := LayoutBox(o)
		left, top, width, bodyY = g.X, g.Top-1, g.Width, g.BodyY
		height = g.BodyY - g.Top + len(body) + 2
		if lowercaseShortcutLabels(o.StatusBar) != "" {
			height++
		}
	}
	hit := Hit{Inside: x >= left && x < left+width && y >= top && y < top+height, Target: -1}
	row := y - bodyY
	if hit.Inside && row >= 0 && row < len(indices) {
		hit.Target = targetAt(o, indices[row], targets)
	}
	return hit
}

func targetAt(o Options, source int, targets []int) int {
	if source < 0 {
		return -1
	}
	if source >= len(o.Body) {
		index := source - len(o.Body)
		if index < len(o.Actions) {
			return o.Actions[index].Target
		}
		return -1
	}
	if source < len(targets) {
		return targets[source]
	}
	return -1
}

// WrapMessage wraps long words as well as whitespace-separated text.
func WrapMessage(message string, width int) []string {
	width = max(1, width)
	return strings.Split(ansi.Hardwrap(ansi.Wrap(message, width, ""), width, false), "\n")
}

func prepareBody(o Options) ([]string, []int) {
	body := append([]string(nil), o.Body...)
	indices := make([]int, len(body))
	for i := range indices {
		indices[i] = i
	}
	if len(o.Actions) > 0 {
		body = append(body, bodyRule)
		indices = append(indices, -1)
		for i, action := range o.Actions {
			body = append(body, action.Text)
			indices = append(indices, len(o.Body)+i)
		}
	}
	statusBar := lowercaseShortcutLabels(o.StatusBar)
	chrome := 4
	if o.Compact {
		chrome = 2
	}
	if o.Fullscreen {
		chrome = 1
	}
	maxBody := max(1, o.Height-chrome)
	if statusBar != "" {
		maxBody = max(1, maxBody-1)
	}
	if o.MaxBodyRows > 0 {
		maxBody = min(maxBody, o.MaxBodyRows)
	}
	if o.BodyRows > 0 {
		bodyRows := o.BodyRows
		if statusBar != "" {
			bodyRows = max(1, bodyRows-1)
		}
		maxBody = min(bodyRows, maxBody)
	}
	var errorLines []string
	errorWidth := o.Width
	if o.Fullscreen {
		errorWidth = o.ScreenWidth + 4
	} else if o.Mode == Overlay {
		errorWidth = OverlayWidth(errorWidth, o.ScreenWidth)
	}
	errorWidth = max(1, errorWidth-6)
	if o.Error != "" {
		errorLines = WrapMessage(o.Error, errorWidth)
	}
	// Keep room for actions, then the error; clip ordinary content first.
	reserve := 0
	for i := len(body) - 1; i >= 0; i-- {
		if body[i] == bodyRule {
			reserve = len(body) - i
			break
		}
	}
	if reserve > maxBody {
		reserve = 0
	}
	available := maxBody - reserve
	errorCapacity := available
	if available > 1 {
		errorCapacity-- // blank line after the error
	}
	if len(errorLines) > errorCapacity {
		errorLines = errorLines[:max(0, errorCapacity)]
		if len(errorLines) > 0 {
			last := len(errorLines) - 1
			errorLines[last] = ansi.Truncate(errorLines[last]+"…", errorWidth, "…")
		}
	}
	var errors []string
	for _, line := range errorLines {
		errors = append(errors, o.ErrorStyle.Render(" "+line+" "))
	}
	if len(errors) > 0 && available > 1 {
		errors = append(errors, "")
	}
	maxBody -= len(errors)
	if len(body) > maxBody {
		// Keep stacked actions visible when a short terminal clips the body.
		rule := -1
		for i := len(body) - 1; i >= 0; i-- {
			if body[i] == bodyRule {
				rule = i
				break
			}
		}
		if rule >= 0 && len(body)-rule <= maxBody {
			prefix := maxBody - (len(body) - rule)
			body = append(append([]string(nil), body[:prefix]...), body[rule:]...)
			indices = append(append([]int(nil), indices[:prefix]...), indices[rule:]...)
		} else {
			body = body[:maxBody]
			indices = indices[:maxBody]
		}
	}
	if o.Fullscreen && len(body) < maxBody {
		for i := len(body) - 1; i >= 0; i-- {
			if body[i] == bodyRule {
				padding := maxBody - len(body)
				body = append(append(append([]string(nil), body[:i]...), make([]string, padding)...), body[i:]...)
				paddingIndices := make([]int, padding)
				for j := range paddingIndices {
					paddingIndices[j] = -1
				}
				indices = append(append(append([]int(nil), indices[:i]...), paddingIndices...), indices[i:]...)
				break
			}
		}
	}
	if o.BodyRows > 0 {
		for len(body) < maxBody {
			body = append(body, "")
			indices = append(indices, -1)
		}
	}
	for range errors {
		indices = append([]int{-1}, indices...)
	}
	return append(errors, body...), indices
}

// PrepareBody returns rendered body rows and their source indices; -1 marks an error or padding row.
func PrepareBody(o Options) ([]string, []int) { return prepareBody(o) }

// ContentCapacity reports how many ordinary body rows survive the frame's
// clipping after its error, status and actions have been reserved.
func ContentCapacity(o Options) int {
	o.Body = make([]string, max(1, o.Height))
	_, indices := prepareBody(o)
	capacity := 0
	for _, index := range indices {
		if index >= 0 && index < len(o.Body) {
			capacity++
		}
	}
	return capacity
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
	o.Body, _ = prepareBody(o)
	o.Actions = nil
	o.Error = ""
	statusBar := lowercaseShortcutLabels(o.StatusBar)
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

	if o.Fullscreen {
		return renderFullscreen(o, background, statusBar)
	}
	if o.Mode == Overlay {
		return renderOverlay(o, background, width, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, statusBar)
	}
	return renderBox(o, background, width, border, horizontal, tl, tr, bl, br, leftRule, rightRule, title, statusBar)
}

// renderFullscreen replaces the frame with a title row in a small pane.
func renderFullscreen(o Options, background []string, footer string) []string {
	width := max(1, o.ScreenWidth)
	header := o.Accent.Render(" " + strings.ToUpper(o.Title))
	if o.Header.Content != "" {
		header = o.Header.Content
	} else if o.Header.Accessory != "" {
		header += " " + o.Header.Accessory
	}
	lines := []string{fit(header, width)}
	for _, line := range o.Body {
		if line == bodyRule {
			line = strings.Repeat("─", width)
			if o.ASCII {
				line = strings.Repeat("-", width)
			}
			line = o.Divider.Render(line)
		}
		lines = append(lines, fit(line, width))
	}
	if footer != "" {
		lines = append(lines, o.Secondary.Render(fitRight(footer, width)))
	}
	for len(lines) < o.Height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	for i := 0; i < len(lines) && i < len(background); i++ {
		if o.RestoreResets != nil {
			lines[i] = o.RestoreResets(lines[i])
		}
		background[i] = o.Surface.Render(fit(lines[i], width))
	}
	return background
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
		box = []string{compactTopBorder(width, title, o.ASCII, horizontal, tl, tr, o.Divider, o.Pill, o.Surface, o.Header.Accessory, o.Header.AccessoryStyle, o.Header.Count)}
	} else {
		header := o.Accent.Render(" " + title + " ")
		if o.Header.Content != "" {
			header = o.Header.Content
		}
		topRule := tl + strings.Repeat(horizontal, width-2) + tr
		bottomRule := leftRule + strings.Repeat(horizontal, width-2) + rightRule
		if o.Header.Content != "" {
			topJoin, cross := "┬", "┴"
			if o.ASCII {
				topJoin, cross = "+", "+"
			}
			topRule = ruleWithDivider(width, tl, tr, horizontal, o.Header.Divider, topJoin)
			bottomRule = ruleWithDivider(width, leftRule, rightRule, horizontal, o.Header.Divider, cross)
		}
		box = []string{o.Divider.Render(topRule), wrap(header, false), o.Divider.Render(bottomRule)}
	}
	for _, line := range o.Body {
		if line == bodyRule {
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
		if o.Header.Content != "" {
			header = o.Header.Content
		}
		content = append(content, header)
		if !o.HideHeaderRule {
			headerRuleRow = len(content)
			content = append(content, o.Divider.Render(strings.Repeat(horizontal, width)))
		}
	}
	for _, line := range o.Body {
		if line == bodyRule {
			content = append(content, bodyRule)
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
	geometry := LayoutBox(o)
	left, top := geometry.X, geometry.Top
	frame := func(row int, text string) {
		if row >= 0 && row < len(background) {
			background[row] = ansi.Cut(background[row], 0, left) + text + ansi.Cut(background[row], left+width, o.ScreenWidth)
		}
	}
	topBorder := o.Divider.Render(tl + strings.Repeat(horizontal, width-2) + tr)
	if o.Compact {
		topBorder = compactTopBorder(width, title, o.ASCII, horizontal, tl, tr, o.Divider, o.Pill, o.Surface, "", lipgloss.NewStyle(), "")
	} else if o.Header.Content != "" {
		join := "┬"
		if o.ASCII {
			join = "+"
		}
		topBorder = o.Divider.Render(ruleWithDivider(width, tl, tr, horizontal, o.Header.Divider, join))
	}
	frame(top-1, topBorder)
	for i, line := range content {
		if line == bodyRule {
			frame(top+i, o.Divider.Render(leftRule+strings.Repeat(horizontal, max(0, width-2))+rightRule))
			continue
		}
		if i == headerRuleRow {
			rule := leftRule + strings.Repeat(horizontal, width-2) + rightRule
			if o.Header.Content != "" {
				join := "┴"
				if o.ASCII {
					join = "+"
				}
				rule = ruleWithDivider(width, leftRule, rightRule, horizontal, o.Header.Divider, join)
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
	return fit(edge.Render(glyphs.Left())+style.Render(fit(title, pillWidth-2))+edge.Render(glyphs.Right()), pillWidth)
}

func renderPill(text string, width int, style, surface lipgloss.Style) string {
	if width < 4 {
		return style.Render(fit(text, width))
	}
	center := style.Render(" ") + style.Render(fit(text, max(0, width-4))) + style.Render(" ")
	edge := surface.Foreground(style.GetBackground())
	return fit(edge.Render(glyphs.Left())+center+edge.Render(glyphs.Right()), width)
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
