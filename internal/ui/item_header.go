package ui

import (
	"fmt"
	"strings"
	"time"

	"bypass/internal/vault"
)

type itemHeader struct {
	exists          bool
	title           string
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
	lines := []string{titleStyle.Render(title)}
	if !h.modified.IsZero() {
		lines = append(lines, styles.secondary.Render(padding+"Modified "+h.modified.Local().Format("2006-01-02")))
	}
	lines = append(lines, styles.secondary.Render(padding+safe(h.groupPath)))
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
