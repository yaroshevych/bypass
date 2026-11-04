package ui

import (
	wipebuf "bypass/internal/secret"
	"context"
	"fmt"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

// runGrantJob resolves fields in private snapshots. Session permissions are
// checked on the UI goroutine and only committed after successful resolution.
func (m *Model) runGrantJob(p *runpkg.Pending, targets []runTarget, mode string) tea.Cmd {
	snapshots := map[string]*vault.Database{}
	allowed := mode != "check"
	if mode == "check" && p.Identity.Digest != "" && m.runs != nil {
		allowed = true
		for _, target := range targets {
			if _, ok := m.runs.grants[runSessionGrant{target.session, p.Identity.Digest, target.entry, target.field}]; !ok {
				allowed = false
				break
			}
		}
	}
	for _, target := range targets {
		if snapshots[target.session] == nil {
			snapshots[target.session] = target.database.Clone()
		}
	}
	backend := m.backend
	epoch := uint64(0)
	if m.runs != nil {
		epoch = m.runs.permissionEpoch
	}
	return func() tea.Msg {
		secrets := newSecretValues(p.Context)
		result := runResult{pending: p, targets: targets, secrets: secrets, permissionEpoch: epoch, mode: mode, newIdentity: p.Identity.Digest != ""}
		defer func() {
			for _, db := range snapshots {
				db.Clear()
			}
		}()
		defer func() {
			if result.err != nil || result.needsApproval || p.Context.Err() != nil {
				secrets.Clear()
			}
		}()
		if mode == "check" && !allowed {
			result.needsApproval = true
			return result
		}
		if mode == "s" && p.Identity.Digest == "" {
			result.err = fmt.Errorf("identity required for session approval")
			return result
		}
		resolver, ok := backend.(interface {
			ResolveField(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
		})
		if !ok {
			result.err = fmt.Errorf("field resolution unavailable")
			return result
		}
		for _, target := range targets {
			value, err := resolver.ResolveField(p.Context, snapshots[target.session], vault.Credentials{}, target.entry, target.field)
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
