package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
	m.settings.IdleTimeout = 15 * time.Minute
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

func TestFullLockTimeoutLocksSessionsWhilePINProtected(t *testing.T) {
	m, _ := operationModel(t)
	if err := m.gate.Configure([]byte("2468")); err != nil {
		t.Fatal(err)
	}
	first := m.ws.ActiveSession()
	m.ws.Open(fixture("/tmp/ui-privacy-full-lock.kdbx"))
	second := m.ws.ActiveSession()
	base := m.now()
	m.settings.IdleTimeout = 10 * time.Minute
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
