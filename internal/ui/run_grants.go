package ui

import (
	wipebuf "bypass/internal/secret"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	runpkg "bypass/internal/run"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func runSessionPrincipal(identity runpkg.Identity, targets []runTarget) string {
	if identity.Name != "" {
		return "agent:" + identity.Name
	}
	items := make([]string, 0, len(targets))
	for _, target := range targets {
		items = append(items, target.entry+"\x00"+target.field)
	}
	sort.Strings(items)
	unique := items[:0]
	for _, item := range items {
		if len(unique) == 0 || unique[len(unique)-1] != item {
			unique = append(unique, item)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(unique, "\x00")))
	return "items:" + hex.EncodeToString(sum[:])
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
	snapshots := map[string]*vault.Database{}
	allowed := mode != "check"
	if mode == "check" && m.runs != nil {
		allowed = runTargetsHaveSessionGrants(m.runs.grants, runSessionPrincipal(p.Identity, targets), targets)
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
