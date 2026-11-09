package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"bypass/internal/vault"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type itemHeader struct {
	exists          bool
	title           string
	tags            []string
	hoveredTag      string
	groupPath       string
	modified        time.Time
	expires         time.Time
	expiryEnabled   bool
	hasTOTP         bool
	attachmentCount int
	historyCount    int
}

func newItemHeader(entry *vault.Entry) itemHeader {
	if entry == nil {
		return itemHeader{}
	}
	return itemHeader{
		exists:          true,
		title:           visibleField(*entry, "Title"),
		tags:            append([]string(nil), entry.Tags...),
		groupPath:       entry.GroupPath,
		modified:        entry.Modified,
		expires:         entry.Expires,
		expiryEnabled:   entry.ExpiryEnabled,
		hasTOTP:         entry.HasTOTP,
		attachmentCount: len(entry.AttachmentNames),
		historyCount:    entry.HistoryCount,
	}
}

func (h itemHeader) render(styles *styleSet, width int, now time.Time, accented, dirty bool) []string {
	return h.renderWithIndent(styles, width, now, accented, dirty, 2)
}

func (h itemHeader) renderWithIndent(styles *styleSet, width int, now time.Time, accented, dirty bool, indent int) []string {
	if !h.exists {
		return nil
	}
	padding := strings.Repeat(" ", max(0, indent))
	title := padding + h.title
	titleStyle := styles.secondary
	if accented {
		titleStyle = styles.accent
		if dirty {
			title += " *"
		}
	}
	title, _ = tagChipLayout(title, h.tags, width, styles.inactive, styles.active, h.hoveredTag)
	groupPath := strings.Trim(h.groupPath, "/")
	if groupPath == "" {
		groupPath = "Passwords"
	}
	groupPath = strings.Join(strings.Split(groupPath, "/"), " / ")
	lines := []string{titleStyle.Render(title), styles.secondary.Render(padding + safe(groupPath))}
	if !h.modified.IsZero() {
		lines = append(lines, styles.secondary.Render(padding+"Modified "+h.modified.Local().Format("2006-01-02")))
	}
	if showNonPasswordVaultSecurity && h.hasTOTP {
		lines = append(lines, styles.secondary.Render(padding+"TOTP configured · commands to view/copy"))
	}
	if h.attachmentCount > 0 {
		lines = append(lines, styles.secondary.Render(fmt.Sprintf("%sAttachments: %d", padding, h.attachmentCount)))
	}
	if showNonPasswordVaultSecurity && h.historyCount > 0 {
		lines = append(lines, styles.secondary.Render(fmt.Sprintf("%sHistory snapshots: %d", padding, h.historyCount)))
	}
	if h.expiryEnabled && !h.expires.IsZero() {
		label := padding + "Expires: "
		if !now.Before(h.expires) {
			label = padding + "Expired: "
		}
		lines = append(lines, styles.secondary.Render(label+h.expires.Local().Format("2006-01-02 15:04")))
	}
	lines = append(lines, styles.rule(width))
	return lines
}

type tagChipSpan struct {
	tag        string
	start, end int
}

func tagChipLayout(title string, tags []string, width int, chipStyle, hoverStyle lipgloss.Style, hovered string) (string, []tagChipSpan) {
	if width <= 0 || len(tags) == 0 {
		return title, nil
	}
	titleWidth := ansi.StringWidth(title)
	available := max(0, width-titleWidth-1) // Keep one cell at the right edge.
	tags = append([]string(nil), tags...)
	sort.Slice(tags, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(safe(tags[i]))) < strings.ToLower(strings.TrimSpace(safe(tags[j])))
	})
	tagNames := make([]string, 0, len(tags))
	chips := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(safe(tag))
		if tag != "" {
			tagNames = append(tagNames, tag)
			chips = append(chips, chipStyle.Render(" "+tag+" "))
		}
	}
	if len(chips) == 0 {
		return title, nil
	}
	total := 0
	for i, chip := range chips {
		if i > 0 {
			total++
		}
		total += ansi.StringWidth(chip)
	}
	if total+3 <= available {
		return renderTagChipCluster(title, tagNames, width, chips, "", chipStyle, hoverStyle, hovered)
	}
	ellipsis := chipStyle.Render(" … ")
	ellipsisWidth := ansi.StringWidth(ellipsis)
	cluster := make([]string, 0, len(chips)+1)
	used := ellipsisWidth
	for _, chip := range chips {
		chipWidth := ansi.StringWidth(chip)
		gap := 0
		if len(cluster) > 0 {
			gap = 1
		}
		if used+gap+chipWidth+4 > available {
			break
		}
		cluster = append(cluster, chip)
		used += gap + chipWidth
	}
	if used+4 <= available {
		selectedTags := tagNames[:len(cluster)]
		return renderTagChipCluster(title, selectedTags, width, chips[:len(cluster)], ellipsis, chipStyle, hoverStyle, hovered)
	}
	return title, nil
}

func renderTagChipCluster(title string, tags []string, width int, chips []string, ellipsis string, chipStyle, hoverStyle lipgloss.Style, hovered string) (string, []tagChipSpan) {
	clusterWidth := 0
	for i, chip := range chips {
		if i > 0 {
			clusterWidth++
		}
		clusterWidth += ansi.StringWidth(chip)
	}
	if ellipsis != "" {
		if len(chips) > 0 {
			clusterWidth++
		}
		clusterWidth += ansi.StringWidth(ellipsis)
	}
	titleWidth := ansi.StringWidth(title)
	padding := max(3, width-titleWidth-1-clusterWidth)
	result := title + strings.Repeat(" ", padding)
	spans := make([]tagChipSpan, 0, len(chips))
	position := titleWidth + padding
	for i, chip := range chips {
		if i > 0 {
			result += " "
			position++
		}
		style := chipStyle
		if strings.EqualFold(tags[i], hovered) {
			style = hoverStyle
		}
		result += style.Render(" " + tags[i] + " ")
		chipWidth := ansi.StringWidth(chip)
		spans = append(spans, tagChipSpan{tag: tags[i], start: position, end: position + chipWidth})
		position += chipWidth
	}
	if ellipsis != "" {
		if len(chips) > 0 {
			result += " "
			position++
		}
		result += ellipsis
	}
	return result, spans
}
