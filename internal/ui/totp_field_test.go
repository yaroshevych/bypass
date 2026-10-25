package ui

import (
	"strings"
	"testing"
	"time"

	"bypass/internal/vault"
)

func TestTOTPFieldMaskedRevealRefreshAndLock(t *testing.T) {
	for _, appearance := range []string{"Classic", "Compact"} {
		t.Run(appearance, func(t *testing.T) {
			m, _ := operationModel(t)
			m.settings.Appearance = appearance
			now := m.now()
			m.now = func() time.Time { return now }
			entry := &m.ws.ActiveSession().DB.Entries[0]
			applyEntryDetails(entry, []vault.Field{{Name: "TOTP", Protected: true}})
			m.searchCache = nil
			for i, name := range m.fieldNames() {
				if name == "TOTP" {
					m.ws.ActiveView().FieldIndex = i
				}
			}
			m.ws.ActiveView().Focus = 2
			if m.focusedField() != "TOTP" || visibleField(*entry, "TOTP") != "••••••••" {
				t.Fatal("TOTP field missing or unmasked")
			}
			execute(t, m, m.toggleReveal())
			if !strings.Contains(m.View(), "RESOLVED-SECRET") {
				t.Fatal("TOTP was not revealed inline")
			}
			deadline := m.revealUntil
			old := m.revealValue
			now = now.Add(time.Second)
			batch := m.onTick(now)
			if batch == nil || m.revealCancel == nil {
				t.Fatal("refresh did not start")
			}
			if !strings.Contains(m.View(), "RESOLVED-SECRET") {
				t.Fatal("TOTP disappeared while its refresh was pending")
			}
			m.Update(resolvedRevealMsg{id: m.revealGeneration, value: []byte("654321")})
			for _, b := range old {
				if b != 0 {
					t.Fatal("replaced code was not cleared")
				}
			}
			if !strings.Contains(m.View(), "654321") {
				t.Fatal("refreshed code was not displayed")
			}
			if !m.revealUntil.Equal(deadline) {
				t.Fatal("refresh extended reveal window")
			}
			now = deadline
			m.onTick(now)
			if m.revealField != "" || strings.Contains(m.View(), "654321") {
				t.Fatal("TOTP reveal did not expire")
			}
			pending := m.toggleReveal()
			msg := pending().(resolvedRevealMsg)
			m.lockCurrent(true)
			m.Update(msg)
			if len(m.revealValue) != 0 {
				t.Fatal("late TOTP survived lock")
			}
			for _, b := range msg.value {
				if b != 0 {
					t.Fatal("late TOTP was not cleared")
				}
			}
		})
	}
}
