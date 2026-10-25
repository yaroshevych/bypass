package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type listComponentRowKind uint8

const (
	listComponentItem listComponentRowKind = iota
	listComponentHeader
	listComponentSpacer
)

// listComponentRow is the shared row model for selectable lists. Text fields may
// already contain ANSI styling; the component owns one-cell insets, alignment,
// truncation, group rules, and count/shortcut placement.
type listComponentRow struct {
	kind         listComponentRowKind
	item         int
	text         string
	selectedText string
	right        string
	count        string
	indent       int
	selected     bool
	pillSelected bool
	activeFill   bool
	countLine    bool
	edgeToEdge   bool
	classicInset bool
	bold         bool
	divider      bool
}

type listComponentOptions struct {
	width, height int
	compact       bool
	ascii         bool
	focused       bool
	sidebar       bool
	ruleEndPad    int
}

type listComponentOutput struct {
	text string
	item int // -1 for group headings, spacers, and secondary/count rows.
}

func (m *Model) renderListComponent(rows []listComponentRow, opts listComponentOptions) []listComponentOutput {
	styles := m.colors()
	result := make([]listComponentOutput, 0, len(rows))
	rule := "─"
	if opts.ascii {
		rule = "-"
	}
	for _, row := range rows {
		switch row.kind {
		case listComponentSpacer:
			result = append(result, listComponentOutput{text: "", item: -2})
		case listComponentHeader:
			label := row.text
			if row.bold {
				label = styles.secondary.Bold(true).Render(label)
			} else {
				label = styles.secondary.Render(label)
			}
			if row.divider {
				endPad := opts.ruleEndPad
				if opts.compact && opts.sidebar {
					endPad = 1
				}
				label += styles.divider.Render(strings.Repeat(rule, max(0, opts.width-lipgloss.Width(row.text)-endPad)))
			}
			if opts.sidebar || row.divider {
				label = fit(label, opts.width)
			}
			result = append(result, listComponentOutput{text: label, item: -1})
		case listComponentItem:
			text := row.text
			if row.selected && row.selectedText != "" {
				text = row.selectedText
			}
			if row.countLine {
				style := styles.secondary
				if row.selected {
					selection := styles.inactive
					if opts.focused {
						selection = styles.active
					}
					style = style.Foreground(selection.GetForeground()).Background(selection.GetBackground()).Faint(true).Bold(false)
				}
				text = style.Render(fit(text, opts.width))
				result = append(result, listComponentOutput{text: text, item: row.item})
				continue
			}
			text = strings.Repeat("  ", min(row.indent, 5)) + text
			if row.edgeToEdge {
				text = fit(text, opts.width)
			} else if opts.compact && ((opts.sidebar && row.count != "") || row.pillSelected) {
				selected := row.selected
				selection := styles.inactive
				if selected && opts.focused {
					selection = styles.active
				}
				countStyle := styles.secondary
				if selected {
					countStyle = countStyle.Foreground(selection.GetForeground()).Background(selection.GetBackground()).Faint(true).Bold(false)
				}
				inner := max(0, opts.width-2)
				right := row.count
				if right == "" {
					right = row.right
				}
				rightText := countStyle.Render(right)
				labelWidth := inner
				if right != "" {
					labelWidth = max(0, inner-lipgloss.Width(rightText)-1)
				}
				body := fit(ansi.Truncate(text, labelWidth, "…"), labelWidth)
				if right != "" {
					body += " " + rightText
				}
				if selected && !opts.ascii {
					text = m.listPill(body, opts.width, opts.focused)
				} else if selected {
					text = selection.Render(fit("["+body+"]", opts.width))
				} else {
					text = fit(" "+body+" ", opts.width)
				}
			} else if row.activeFill && row.selected {
				rightText := styles.secondary.Foreground(styles.active.GetForeground()).Background(styles.active.GetBackground()).Render(row.right)
				available := max(0, opts.width-lipgloss.Width(rightText)-1)
				if row.classicInset {
					available = max(0, opts.width-lipgloss.Width(rightText)-3)
					if row.right == "" {
						available = max(0, opts.width-2)
					}
				}
				left := ansi.Truncate(text, available, "…")
				if row.right != "" {
					if row.classicInset {
						left = " " + fit(left, available) + " " + rightText + " "
					} else {
						left = fit(left, available) + " " + rightText
					}
				} else {
					if row.classicInset {
						left = fit(" "+left+" ", opts.width)
					} else {
						left = fit(left, opts.width)
					}
				}
				text = styles.active.Render(fit(left, opts.width))
			} else if row.right != "" {
				right := styles.secondary.Render(row.right)
				available := max(0, opts.width-lipgloss.Width(right)-1)
				if row.classicInset {
					available = max(0, opts.width-lipgloss.Width(right)-3)
					text = " " + fit(ansi.Truncate(text, available, "…"), available) + " " + right + " "
				} else {
					text = fit(ansi.Truncate(text, available, "…"), available) + " " + right
				}
			} else {
				if row.classicInset {
					text = fit(" "+text+" ", opts.width)
				} else {
					text = fit(text, opts.width)
				}
			}
			result = append(result, listComponentOutput{text: text, item: row.item})
		}
	}
	if opts.height > 0 {
		if len(result) > opts.height {
			result = result[:opts.height]
		}
		for len(result) < opts.height {
			result = append(result, listComponentOutput{text: strings.Repeat(" ", opts.width), item: -1})
		}
	}
	return result
}
