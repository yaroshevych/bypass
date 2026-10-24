package ui

import (
	"strings"

	"keepassui/internal/workspace"
)

type paneRect struct {
	Focus, X, Y, Width, Height int
}

type viewLayout struct {
	Mode       string
	Panes      []paneRect
	BodyY      int
	BodyHeight int
	Breadcrumb bool
	Zoomed     bool
}

func (m *Model) layout() viewLayout {
	w, h := max(1, m.width), max(1, m.height)
	header, footer := 4, 2
	if strings.EqualFold(m.settings.Appearance, "compact") {
		header, footer = 2, 1
	}
	l := viewLayout{Mode: "tiny", BodyY: header, BodyHeight: max(0, h-header-footer)}
	if w < 48 || h < 14 {
		return l
	}
	var v *workspace.View
	if m.ws != nil {
		v = m.ws.ActiveView()
	}
	focus := 1
	if v != nil {
		focus = min(2, max(0, v.Focus))
	}
	if m.editor != nil {
		focus = 2
	}
	zoomed := v != nil && v.Zoom
	l.Zoomed = zoomed
	if w >= 120 {
		l.Mode = "wide"
		nav, center, detail := m.widePaneWidths(w, v)
		if zoomed {
			l.Breadcrumb = true
			l.Panes = []paneRect{{Focus: focus, X: 0, Y: l.BodyY + 1, Width: w, Height: max(1, l.BodyHeight-1)}}
			return l
		}
		l.Panes = []paneRect{
			{Focus: 0, X: 0, Y: l.BodyY, Width: nav, Height: l.BodyHeight},
			{Focus: 1, X: nav + 1, Y: l.BodyY, Width: center, Height: l.BodyHeight},
			{Focus: 2, X: nav + center + 2, Y: l.BodyY, Width: detail, Height: l.BodyHeight},
		}
		return l
	}
	if w >= 80 {
		l.Mode = "medium"
		if zoomed {
			l.Breadcrumb = true
			l.Panes = []paneRect{{Focus: focus, X: 0, Y: l.BodyY + 1, Width: w, Height: max(1, l.BodyHeight-1)}}
			return l
		}
		nav := m.sidebarWidth(w, v)
		mainFocus := 1
		if focus == 2 {
			mainFocus = 2
		}
		l.Panes = []paneRect{{Focus: 0, X: 0, Y: l.BodyY, Width: nav, Height: l.BodyHeight}, {Focus: mainFocus, X: nav + 1, Y: l.BodyY, Width: w - nav - 1, Height: l.BodyHeight}}
		return l
	}
	l.Mode = "narrow"
	l.Breadcrumb = true
	l.Panes = []paneRect{{Focus: focus, X: 0, Y: l.BodyY + 1, Width: w, Height: max(1, l.BodyHeight-1)}}
	return l
}

func (m *Model) widePaneWidths(width int, v *workspace.View) (int, int, int) {
	// Keep a useful minimum for each pane while respecting user-resized widths.
	nav := max(18, width*23/100)
	detail := max(24, width*38/100)
	if v != nil {
		if v.NavWidth > 0 {
			nav = v.NavWidth
		}
		if v.DetailWidth > 0 {
			detail = v.DetailWidth
		}
	}
	maxNav := max(18, width*40/100)
	maxDetail := max(24, width*55/100)
	nav = min(maxNav, max(18, nav))
	detail = min(maxDetail, max(24, detail))
	for width-nav-detail-2 < 24 {
		if detail > 24 {
			detail--
		} else if nav > 18 {
			nav--
		} else {
			break
		}
	}
	center := max(24, width-nav-detail-2)
	return nav, center, detail
}

func (m *Model) sidebarWidth(width int, v *workspace.View) int {
	nav := max(18, width*25/100)
	if v != nil && v.NavWidth > 0 {
		nav = v.NavWidth
	}
	return min(width-25, max(18, nav))
}

func (m *Model) navRowHeight() int {
	if strings.EqualFold(m.settings.Appearance, "compact") {
		return 1
	}
	return 2
}

// viewportOffset keeps a selected row visible with a small scrolling margin.
// It does not mutate the model; callers can store the returned start offset.
func viewportOffset(index, total, capacity, previous int) int {
	if total <= 0 || capacity <= 0 {
		return 0
	}
	index = min(total-1, max(0, index))
	maxStart := max(0, total-capacity)
	start := min(maxStart, max(0, previous))
	margin := min(2, max(0, (capacity-1)/2))
	if index < start+margin {
		start = max(0, index-margin)
	} else if index >= start+capacity-margin {
		start = min(maxStart, index-capacity+margin+1)
	}
	if index < start {
		start = index
	}
	if index >= start+capacity {
		start = index - capacity + 1
	}
	return min(maxStart, max(0, start))
}
