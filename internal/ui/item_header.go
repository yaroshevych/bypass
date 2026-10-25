package ui

import (
	"fmt"
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
	if !h.exists {
		return nil
	}
	title := "  " + h.title
	titleStyle := styles.secondary
	if accented {
		titleStyle = styles.accent
		if dirty {
			title += " *"
		}
	}
	lines := []string{titleStyle.Render(title)}
	if !h.modified.IsZero() {
		lines = append(lines, styles.secondary.Render("  Modified "+h.modified.Local().Format("2006-01-02")))
	}
	lines = append(lines, styles.secondary.Render("  "+safe(h.groupPath)))
	if showNonPasswordVaultSecurity && h.hasTOTP {
		lines = append(lines, styles.secondary.Render("  TOTP configured · commands to view/copy"))
	}
	if h.attachmentCount > 0 {
		lines = append(lines, styles.secondary.Render(fmt.Sprintf("  Attachments: %d", h.attachmentCount)))
	}
	if showNonPasswordVaultSecurity && h.historyCount > 0 {
		lines = append(lines, styles.secondary.Render(fmt.Sprintf("  History snapshots: %d", h.historyCount)))
	}
	if h.expiryEnabled && !h.expires.IsZero() {
		label := "  Expires: "
		if !now.Before(h.expires) {
			label = "  Expired: "
		}
		lines = append(lines, styles.secondary.Render(label+h.expires.Local().Format("2006-01-02 15:04")))
	}
	lines = append(lines, styles.rule(width))
	return lines
}
