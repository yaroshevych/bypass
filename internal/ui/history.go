package ui

import (
	"bypass/internal/vault"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"sort"
	"strings"
	"time"
)

func (m *Model) openHistory() tea.Cmd {
	m.recordItemUse()
	m.hideReveal()
	session, entry := m.extraTarget()
	if session == nil || entry == nil {
		m.setNotice("Select an entry first.", true)
		return nil
	}
	if len(entry.History) == 0 {
		m.setNotice("This entry has no history snapshots.", false)
		return nil
	}
	m.clearExtras()
	history := append([]vault.Entry(nil), entry.History...)
	for i := range history {
		history[i].Fields = append([]vault.Field(nil), history[i].Fields...)
		rank := map[string]int{"Title": 0, "UserName": 1, "Password": 2, "URL": 3, "Notes": 4}
		priority := func(name string) int {
			if value, ok := rank[name]; ok {
				return value
			}
			return 5
		}
		sort.SliceStable(history[i].Fields, func(a, b int) bool { return priority(history[i].Fields[a].Name) < priority(history[i].Fields[b].Name) })
	}
	m.extras = &extraState{
		mode:         extraHistory,
		sessionID:    session.ID,
		entryUUID:    entry.UUID,
		groupUUID:    entry.GroupUUID,
		history:      history,
		historyIndex: len(history) - 1,
	}
	return nil
}

func (m *Model) historyLines(state *extraState) []string {
	body := []string{m.colors().secondary.Render("Read-only history · snapshots cannot be restored from this view."), ""}
	capacity := max(1, m.height-12)
	start := viewportOffset(state.historyIndex, len(state.history), capacity, state.offset)
	rows := make([]listComponentRow, 0, capacity)
	for i := start; i < len(state.history) && i < start+capacity; i++ {
		snapshot := state.history[i]
		title := historyTitle(snapshot)
		when := snapshot.Modified
		if when.IsZero() {
			when = snapshot.Created
		}
		label := fmt.Sprintf("%s  %s", title, historyDate(when))
		rows = append(rows, listComponentRow{kind: listComponentItem, item: i, text: label, selected: i == state.historyIndex, activeFill: true, pillSelected: true})
	}
	if strings.EqualFold(m.settings.Appearance, "compact") {
		for _, row := range m.renderListComponent(rows, listComponentOptions{
			width: max(1, min(74, m.width-8)), compact: true, ascii: m.settings.ASCII,
			focused: true, sidebar: true,
		}) {
			body = append(body, row.text)
		}
	} else {
		for _, row := range rows {
			label := row.text
			if row.item == state.historyIndex {
				label = m.colors().active.Render("› " + label)
			}
			body = append(body, label)
		}
	}
	if len(state.history) == 0 {
		body = append(body, "No history snapshots")
	}
	return m.overlay("History", body, "", min(78, m.width-4), -1, -1)
}

func (m *Model) historyEntryLines(state *extraState) []string {
	entry := m.extraHistorySnapshot(state)
	if entry == nil {
		return m.overlay("History Snapshot", []string{m.colors().error.Render("Snapshot is no longer available.")}, "", min(78, m.width-4), -1, -1)
	}
	when := entry.Modified
	if when.IsZero() {
		when = entry.Created
	}
	body := []string{m.colors().secondary.Render(historyDate(when)), "", m.colors().secondary.Render("Read-only snapshot · no restore action")}
	if state.fieldIndex >= 0 && state.fieldIndex < len(entry.Fields) {
		field := entry.Fields[state.fieldIndex]
		label := field.Name
		if field.Name == "UserName" {
			label = "Username"
		}
		body = append(body, m.colors().active.Render("› "+safe(label)))
		lines := m.historyValueLines(*entry, field, state)
		capacity := m.historyValueCapacity()
		start := min(max(0, state.valueOffset), max(0, len(lines)-capacity))
		end := min(len(lines), start+capacity)
		body = append(body, lines[start:end]...)
	}
	return m.overlay("History Snapshot", body, "PgUp/PgDn value · c copy · v reveal", min(78, m.width-4), -1, -1)
}

func (m *Model) historyValueCapacity() int {
	return max(1, max(1, m.height-8)-5)
}

func (m *Model) historyValueLines(entry vault.Entry, field vault.Field, state *extraState) []string {
	value := m.historyFieldValue(entry, field.Name, state)
	if value == "" {
		value = "—"
	}
	boxWidth := min(max(20, min(78, m.width-4)), max(20, m.width-2))
	wrapWidth := max(1, boxWidth-4)
	var lines []string
	for _, paragraph := range strings.Split(value, "\n") {
		wrapped := ansi.Wrap(safe(paragraph), wrapWidth, "")
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	if len(lines) == 0 {
		return []string{"—"}
	}
	return lines
}

func historyTitle(entry vault.Entry) string {
	if entry.IsProtected("Title") {
		return "(protected title)"
	}
	return safe(entry.Get("Title"))
}

func historyDate(value time.Time) string {
	if value.IsZero() {
		return "Date unavailable"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func (m *Model) historyFieldValue(entry vault.Entry, name string, state *extraState) string {
	if state.revealedField == name && len(state.revealedValue) != 0 && m.now().Before(state.revealedUntil) {
		return string(state.revealedValue)
	}
	if name == "Password" || entry.IsProtected(name) {
		return "••••••••"
	}
	return entry.Get(name)
}

func (m *Model) extraHistorySnapshot(state *extraState) *vault.Entry {
	if state == nil || state.historyIndex < 0 || state.historyIndex >= len(state.history) {
		return nil
	}
	return &state.history[state.historyIndex]
}

func (m *Model) clearHistoryReveal(state *extraState) {
	clear(state.revealedValue)
	state.revealedValue = nil
	state.revealedField = ""
	state.revealedUntil = time.Time{}
}

func (m *Model) copyHistoryField(state *extraState) tea.Cmd {
	entry := m.extraHistorySnapshot(state)
	if entry == nil || state.fieldIndex < 0 || state.fieldIndex >= len(entry.Fields) {
		return nil
	}
	field := entry.Fields[state.fieldIndex]
	if isTOTPSeedField(field.Name) {
		m.setNotice("TOTP seeds stay hidden; use Show / Copy TOTP for a current code.", false)
		return nil
	}
	if strings.Contains(field.Value, "{REF:") {
		m.setNotice("Historical field references cannot be resolved here.", false)
		return nil
	}
	return m.copyValue([]byte(field.Value), field.Name+" from history")
}

func (m *Model) revealHistoryField(state *extraState) tea.Cmd {
	entry := m.extraHistorySnapshot(state)
	if entry == nil || state.fieldIndex < 0 || state.fieldIndex >= len(entry.Fields) {
		return nil
	}
	field := entry.Fields[state.fieldIndex]
	if isTOTPSeedField(field.Name) {
		m.setNotice("TOTP seeds stay hidden; use Show / Copy TOTP for a current code.", false)
		return nil
	}
	if strings.Contains(field.Value, "{REF:") {
		m.setNotice("Historical field references cannot be resolved here.", false)
		return nil
	}
	if state.revealedField == field.Name && len(state.revealedValue) != 0 {
		m.clearHistoryReveal(state)
		return nil
	}
	m.clearHistoryReveal(state)
	state.revealedField = field.Name
	state.revealedValue = []byte(field.Value)
	state.revealedUntil = m.now().Add(5 * time.Second)
	return nil
}

func isTOTPSeedField(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "otp", "totp seed", "timeotp-secret-base32":
		return true
	default:
		return false
	}
}
