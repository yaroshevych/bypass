package ui

import (
	"reflect"
	"testing"
)

func TestLayoutResponsiveModesAndZoom(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/layout.kdbx"))
	tests := []struct {
		name       string
		width      int
		height     int
		focus      int
		zoom       bool
		mode       string
		paneCount  int
		mainFocus  int
		breadcrumb bool
	}{
		{"wide", 120, 35, 1, false, "wide", 3, 2, false},
		{"medium list", 100, 24, 1, false, "medium", 2, 1, false},
		{"medium details", 100, 24, 2, false, "medium", 2, 2, false},
		{"narrow", 70, 20, 0, false, "narrow", 1, 0, true},
		{"wide zoom", 140, 40, 2, true, "wide", 1, 2, true},
		{"tiny width", 47, 20, 1, false, "tiny", 0, 0, false},
		{"tiny height", 90, 13, 1, false, "tiny", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.width, m.height = tt.width, tt.height
			v := m.ws.ActiveView()
			v.Focus, v.Zoom = tt.focus, tt.zoom
			got := m.layout()
			if got.Mode != tt.mode || len(got.Panes) != tt.paneCount || got.Breadcrumb != tt.breadcrumb {
				t.Fatalf("layout = mode %q panes %d breadcrumb %v", got.Mode, len(got.Panes), got.Breadcrumb)
			}
			if len(got.Panes) > 0 && got.Panes[len(got.Panes)-1].Focus != tt.mainFocus {
				t.Fatalf("last pane focus = %d, want %d", got.Panes[len(got.Panes)-1].Focus, tt.mainFocus)
			}
			for _, pane := range got.Panes {
				if pane.Width <= 0 || pane.Height <= 0 || pane.X < 0 || pane.Y < got.BodyY || pane.X+pane.Width > tt.width || pane.Y+pane.Height > tt.height-2 {
					t.Fatalf("invalid pane geometry: %+v", pane)
				}
			}
		})
	}
}

func TestLayoutRespectsPaneWidthsWithoutMutatingView(t *testing.T) {
	m := New(Options{})
	m.ws.Open(fixture("/tmp/layout-widths.kdbx"))
	m.width, m.height = 140, 32
	v := m.ws.ActiveView()
	v.NavWidth, v.DetailWidth = 33, 41
	v.Query = "Alpha"
	before := *v
	l := m.layout()
	if len(l.Panes) != 3 || l.Panes[0].Width != 33 || l.Panes[2].Width != 41 {
		t.Fatalf("custom pane widths not applied: %+v", l.Panes)
	}
	_ = m.View()
	_ = m.View()
	if !reflect.DeepEqual(before, *v) {
		t.Fatal("rendering mutated workspace geometry")
	}
	if m.searchCache != nil {
		t.Fatal("rendering populated the search cache")
	}
}

func TestViewportOffsetKeepsIndexVisibleWithMargin(t *testing.T) {
	for _, tt := range []struct{ index, total, capacity, previous, want int }{
		{0, 100, 10, 0, 0},
		{50, 100, 10, 0, 43},
		{52, 100, 10, 43, 45},
		{99, 100, 10, 43, 90},
		{5, 100, 0, 20, 0},
		{4, 0, 10, 0, 0},
	} {
		if got := viewportOffset(tt.index, tt.total, tt.capacity, tt.previous); got != tt.want {
			t.Fatalf("viewportOffset(%d,%d,%d,%d)=%d want %d", tt.index, tt.total, tt.capacity, tt.previous, got, tt.want)
		}
	}
}

func TestTabSpansKeepActiveTabVisibleAndWithinWidth(t *testing.T) {
	m := New(Options{})
	for i := 0; i < 9; i++ {
		m.ws.Open(fixture("/tmp/vault-long-name-" + string(rune('a'+i)) + ".kdbx"))
	}
	m.width, m.height = 48, 18
	m.ws.Active = 8
	spans := m.tabSpans()
	if len(spans) < 2 || spans[len(spans)-2].Index != 8 || spans[len(spans)-1].Index != 9 {
		t.Fatalf("active tab not visible: %+v", spans)
	}
	for _, span := range spans {
		if span.Start < 0 || span.End > m.width || span.End <= span.Start {
			t.Fatalf("invalid tab span: %+v", span)
		}
	}
}
