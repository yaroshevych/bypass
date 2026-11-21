package ui

import (
	"fmt"

	runpkg "bypass/internal/run"
	wipebuf "bypass/internal/secret"
	"bypass/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func runSessionPrincipal(identity runpkg.Identity, _ []runTarget) string {
	if identity.Digest == "" {
		return ""
	}
	return "identity:" + identity.Digest
}

func runTargetsHaveSessionGrants(grants map[runSessionGrant]string, principal string, targets []runTarget) bool {
	if principal == "" || len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if _, ok := grants[runSessionGrant{target.session, principal, target.entry, target.field}]; !ok {
			return false
		}
	}
	return true
}

// runGrantJob resolves fields in private snapshots. Session permissions are
// checked on the UI goroutine and only committed after successful resolution.
func (m *Model) runGrantJob(p *runpkg.Pending, targets []runTarget, mode string) tea.Cmd {
	type vaultRead struct {
		core *session.Session
		data *session.Snapshot
	}
	snapshots := make(map[string]vaultRead)
	allowed := mode != "check"
	if mode == "check" && m.runs != nil {
		allowed = runTargetsHaveSessionGrants(m.runs.grants, runSessionPrincipal(p.Identity, targets), targets)
	}
	for _, target := range targets {
		if _, exists := snapshots[target.session]; exists {
			continue
		}
		if current := m.ws.Sessions[target.session]; current != nil && current.Database() == target.database && current.Core != nil {
			snapshots[target.session] = vaultRead{current.Core, current.Core.Snapshot()}
		}
	}
	epoch := uint64(0)
	if m.runs != nil {
		epoch = m.runs.permissionEpoch
	}
	resources := newJobResources(p.Context, func() {
		for _, snapshot := range snapshots {
			snapshot.data.Clear()
		}
	})
	return func() tea.Msg {
		secrets := newSecretValues(p.Context)
		result := runResult{pending: p, targets: targets, secrets: secrets, permissionEpoch: epoch, mode: mode, newIdentity: p.Identity.Digest != ""}
		if !resources.start() {
			secrets.Clear()
			result.err = p.Context.Err()
			return result
		}
		defer resources.done()
		defer func() {
			if result.err != nil || result.needsApproval || p.Context.Err() != nil {
				secrets.Clear()
			}
		}()
		if mode == "s" && runSessionPrincipal(p.Identity, targets) == "" {
			result.err = fmt.Errorf("identity required for session approval")
			return result
		}
		if mode == "check" && !allowed {
			result.needsApproval = true
			return result
		}
		for _, target := range targets {
			snapshot := snapshots[target.session]
			if snapshot.core == nil || !snapshot.core.CanResolveField() {
				result.err = fmt.Errorf("field resolution unavailable")
				return result
			}
			value, err := snapshot.core.ResolveField(p.Context, snapshot.data, target.entry, target.field)
			if err != nil {
				wipebuf.Wipe(value)
				result.err = err
				return result
			}
			secrets.Put(target.ref, value)
		}
		return result
	}
}
