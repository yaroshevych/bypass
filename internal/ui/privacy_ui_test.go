package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPINIdleProtectionUsesDefaultAndConfiguredTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{5 * time.Minute, time.Minute, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			m, _ := operationModel(t)
			if m.settings.PINIdleTimeout != 5*time.Minute {
				t.Fatal("PIN idle timeout should default to five minutes")
			}
			m.settings.PINIdleTimeout = timeout
			if err := m.gate.Configure([]byte("2468")); err != nil {
				t.Fatal(err)
			}
			session := m.ws.ActiveSession()
			base := m.now()
			session.LastActivity = base
			// The separate no-PIN timeout must not determine PIN protection.
			m.settings.IdleTimeout = 24 * time.Hour
			m.onTick(base.Add(timeout - time.Second))
			if m.gate.Protected() {
				t.Fatal("PIN screen protected before its deadline")
			}
			m.onTick(base.Add(timeout))
			if !m.gate.Protected() || session.DB == nil {
				t.Fatal("PIN deadline should cover the UI and keep the vault unlocked")
			}
		})
	}
}

func TestConfiguredPINProtectsOnlyWhenEveryUnlockedSessionIsIdle(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	first := m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/ui-privacy-second.kdbx"))
	second := m.ws.ActiveSession()
	base := m.now()
	now := base.Add(20 * time.Minute)
	m.settings.PINIdleTimeout = 15 * time.Minute
	first.LastActivity = now.Add(-16 * time.Minute)
	second.LastActivity = now.Add(-time.Minute)

	m.onTick(now)
	if m.gate.Protected() || first.DB == nil || second.DB == nil {
		t.Fatal("an active session did not keep the global screen unprotected")
	}

	second.LastActivity = now.Add(-16 * time.Minute)
	m.onTick(now)
	if !m.gate.Protected() || first.DB == nil || second.DB == nil {
		t.Fatal("idle sessions were not screen protected while remaining unlocked")
	}
}

func TestInputResetsPINIdleDeadline(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	base := m.now()
	session := m.ws.ActiveSession()
	session.LastActivity = base
	m.now = func() time.Time { return base.Add(4 * time.Minute) }
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.onTick(base.Add(5 * time.Minute))
	if m.gate.Protected() {
		t.Fatal("recent input did not postpone PIN protection")
	}
	m.onTick(base.Add(9 * time.Minute))
	if !m.gate.Protected() {
		t.Fatal("PIN protection did not follow the last input")
	}
}

func TestPickerInputResetsPINIdleDeadline(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	base := m.now()
	m.ws.ActiveSession().LastActivity = base
	m.screen = chooseFile
	m.now = func() time.Time { return base.Add(4 * time.Minute) }
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m.onTick(base.Add(5 * time.Minute))
	if m.gate.Protected() {
		t.Fatal("typing in the vault picker did not postpone PIN protection")
	}
	m.onTick(base.Add(9 * time.Minute))
	if !m.gate.Protected() {
		t.Fatal("PIN protection did not follow picker activity")
	}
}

func TestFullLockTimeoutLocksSessionsWhilePINProtected(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	first := m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/ui-privacy-full-lock.kdbx"))
	second := m.ws.ActiveSession()
	base := m.now()
	m.settings.PINIdleTimeout = 10 * time.Minute
	m.settings.FullLockTimeout = 30 * time.Minute
	first.LastActivity = base
	second.LastActivity = base

	m.onTick(base.Add(11 * time.Minute))
	if !m.gate.Protected() || first.DB == nil || second.DB == nil {
		t.Fatal("idle timeout should protect the screen and leave sessions unlocked")
	}
	m.onTick(base.Add(31 * time.Minute))
	if first.DB != nil || second.DB != nil || m.gate.Protected() || m.gate.Configured() {
		t.Fatal("full lock timeout did not clear sessions and synchronize privacy state")
	}
	if len(first.Credentials.Password) != 0 || len(second.Credentials.Password) != 0 {
		t.Fatal("full lock timeout retained session credentials")
	}
}

func TestMouseActivityDoesNotRefreshProtectedSession(t *testing.T) {
	m, _ := operationModel(t)
	session := m.ws.ActiveSession()
	activity := session.LastActivity
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	m.gate.Protect()

	m.Update(tea.MouseMsg{})
	if !session.LastActivity.Equal(activity) {
		t.Fatal("mouse input while screen protected refreshed session activity")
	}
	if session.DB == nil {
		t.Fatal("mouse input while protected unexpectedly locked the vault")
	}
}

func TestNoPINIdleTimeoutStillFullLocks(t *testing.T) {
	m, _ := operationModel(t)
	session := m.ws.ActiveSession()
	activity := session.LastActivity
	m.settings.IdleTimeout = time.Minute

	m.onTick(activity.Add(time.Minute))
	if session.DB != nil {
		t.Fatal("without a configured PIN, idle timeout did not full lock the session")
	}
	if len(session.Credentials.Password) != 0 {
		t.Fatal("idle full lock retained credentials")
	}
}
