package ui

import (
	"bypass/internal/vault"
	"bypass/internal/workspace"
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const attachmentInputLimit = 32 << 20

func (m *Model) openAttachments() tea.Cmd {
	m.hideReveal()
	session, entry := m.extraTarget()
	if session == nil || entry == nil {
		m.setNotice("Select an entry first.", true)
		return nil
	}
	m.clearExtras()
	m.extras = &extraState{mode: extraAttachments, sessionID: session.ID, entryUUID: entry.UUID, groupUUID: entry.GroupUUID}
	return nil
}

func (m *Model) attachmentLines(state *extraState) []string {
	session, entry := m.currentExtraEntry(state)
	if session == nil || entry == nil {
		return m.overlay("Attachments", []string{m.colors().errorStyle.Render("Entry is no longer available.")}, "Esc close", min(72, m.width-4), -1, -1)
	}
	var body []string
	if state.phase == "path" {
		label := "Import source path"
		if state.action == "export" {
			label = "Plaintext export destination"
		}
		body = []string{m.colors().muted.Render(label), m.colors().accent.Render(state.input.display(false) + "▏"), ""}
	} else if strings.HasPrefix(state.phase, "confirm-") {
		body = m.attachmentConfirmation(state)
	} else {
		writable := m.extrasWritable(session)
		body = append(body, m.colors().muted.Render("a add · r replace · d remove · e export · Esc close"), "")
		capacity := m.attachmentListCapacity()
		items := len(entry.AttachmentNames) + 1
		start := viewportOffset(state.selected, items, capacity, state.offset)
		for i := start; i < items && i < start+capacity; i++ {
			label := ""
			if i == 0 {
				label = "  Add Attachment…"
			} else {
				label = "  " + safe(entry.AttachmentNames[i-1])
			}
			if state.selected == i {
				label = m.colors().active.Render("› " + strings.TrimPrefix(label, "  "))
			}
			body = append(body, label)
		}
		if !writable {
			body = append(body, "", m.colors().muted.Render("Read only · R enables writes"))
		}
	}
	if state.err != "" {
		body = append(body, "", m.colors().errorStyle.Render(state.err))
	}
	footer := "↑↓ select · a add · r replace · d remove · e export · Esc close"
	if state.phase == "path" {
		footer = "Enter continue · Esc back"
	} else if strings.HasPrefix(state.phase, "confirm-") {
		footer = "←→ choose · Enter confirm · Esc cancel"
	} else if state.loading {
		footer = "Working… · Esc close"
	}
	return m.overlay("Attachments", body, footer, min(76, m.width-4), -1, -1)
}

func (m *Model) attachmentListCapacity() int {
	return max(1, m.height-14)
}

func (m *Model) attachmentConfirmation(state *extraState) []string {
	switch state.phase {
	case "confirm-replace":
		return m.confirmationChoice(state, []string{fmt.Sprintf("Replace attachment %q with %q?", safe(state.targetName), safe(filepath.Base(state.path))), "The vault is backed up before the change."})
	case "confirm-remove":
		return m.confirmationChoice(state, []string{fmt.Sprintf("Remove attachment %q?", safe(state.targetName)), "This change creates an encrypted backup."})
	case "confirm-export":
		return m.confirmationChoice(state, []string{"Export attachment as plaintext to:", safe(state.path), "Confirm only if this destination is appropriate."})
	default:
		return nil
	}
}

func (m *Model) extrasWritable(session *workspace.Session) bool {
	_, writer := m.backend.(vault.Writer)
	return session != nil && session.DB != nil && !session.ReadOnly && !session.Stale && !m.writing && m.settings.BackupDir != "" && writer
}

func (m *Model) attachmentListKey(k tea.KeyMsg) tea.Cmd {
	state := m.extras
	session, entry := m.currentExtraEntry(state)
	if session == nil || entry == nil {
		m.clearExtras()
		return nil
	}
	switch k.String() {
	case "esc":
		m.clearExtras()
	case "up", "k":
		state.selected = max(0, state.selected-1)
		state.offset = viewportOffset(state.selected, len(entry.AttachmentNames)+1, m.attachmentListCapacity(), state.offset)
	case "down", "j":
		state.selected = min(len(entry.AttachmentNames), state.selected+1)
		state.offset = viewportOffset(state.selected, len(entry.AttachmentNames)+1, m.attachmentListCapacity(), state.offset)
	case "a":
		if !m.extrasWritable(session) {
			state.err = "Attachment changes require write access and an encrypted backup location."
			return nil
		}
		state.action, state.targetName, state.phase = "add", "", "path"
		state.confirmProceed = false
		state.input.clear()
		state.err = ""
	case "r":
		if state.selected == 0 || !m.extrasWritable(session) {
			state.err = "Select an attachment and enable writes first."
			return nil
		}
		state.action, state.targetName, state.phase = "replace", entry.AttachmentNames[state.selected-1], "path"
		state.confirmProceed = false
		state.input.clear()
		state.err = ""
	case "d", "delete":
		if state.selected == 0 || !m.extrasWritable(session) {
			state.err = "Select an attachment and enable writes first."
			return nil
		}
		state.action, state.targetName, state.phase = "remove", entry.AttachmentNames[state.selected-1], "confirm-remove"
		state.confirmProceed = false
		state.err = ""
	case "e":
		if state.selected == 0 {
			state.err = "Select an attachment to export."
			return nil
		}
		state.action, state.targetName, state.phase = "export", entry.AttachmentNames[state.selected-1], "path"
		state.confirmProceed = false
		state.input.clear()
		state.err = ""
	}
	return nil
}

func (m *Model) attachmentPathKey(k tea.KeyMsg) tea.Cmd {
	state := m.extras
	switch k.String() {
	case "esc":
		state.phase, state.action, state.path, state.targetName = "", "", "", ""
		state.input.clear()
		state.err = ""
	case "enter":
		path := strings.TrimSpace(state.input.value())
		if path == "" {
			state.err = "Enter a file path."
			return nil
		}
		path = expandPath(path)
		if !filepath.IsAbs(path) {
			path, _ = filepath.Abs(path)
		}
		if state.action == "export" {
			if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
				state.err = "Destination already exists or cannot be checked; choose another path."
				return nil
			}
			state.path = path
			state.phase = "confirm-export"
			state.confirmProceed = false
			state.err = ""
			return nil
		}
		name := filepath.Base(path)
		if name == "." || name == string(filepath.Separator) || name == "" {
			state.err = "Choose a regular file."
			return nil
		}
		if _, err := attachmentFileSize(path); err != nil {
			state.err = "Source must be a readable regular file no larger than 32 MiB."
			return nil
		}
		state.path = path
		if state.action == "replace" || attachmentExists(m, state, name) {
			state.targetName = nameIf(state.action == "replace", state.targetName, name)
			state.phase = "confirm-replace"
			state.confirmProceed = false
		} else {
			state.targetName = name
			return m.commitAttachmentImport(state)
		}
		state.err = ""
		return nil
	default:
		state.input.update(k)
		state.err = ""
	}
	return nil
}

func nameIf(condition bool, a, b string) string {
	if condition {
		return a
	}
	return b
}

func attachmentExists(m *Model, state *extraState, name string) bool {
	_, entry := m.currentExtraEntry(state)
	if entry == nil {
		return false
	}
	for _, existing := range entry.AttachmentNames {
		if existing == name {
			return true
		}
	}
	return false
}

func attachmentFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > attachmentInputLimit {
		return 0, errors.New("invalid attachment file")
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > attachmentInputLimit {
		return 0, errors.New("invalid attachment file")
	}
	return info.Size(), nil
}

func (m *Model) commitAttachmentImport(state *extraState) tea.Cmd {
	session, _ := m.currentExtraEntry(state)
	if !m.extrasWritable(session) {
		state.err = "Attachment changes require write access and an encrypted backup location."
		return nil
	}
	if _, err := attachmentFileSize(state.path); err != nil {
		state.err = "Source must be a readable regular file no larger than 32 MiB."
		return nil
	}
	file, err := os.Open(state.path)
	if err != nil {
		state.err = "Could not read attachment source."
		return nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, attachmentInputLimit+1))
	if err != nil || len(data) > attachmentInputLimit {
		clear(data)
		state.err = "Source must be a readable regular file no larger than 32 MiB."
		return nil
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		clear(data)
		state.err = "Source must be a readable regular file no larger than 32 MiB."
		return nil
	}
	kind := vault.AddAttachment
	if state.phase == "confirm-replace" || state.action == "replace" {
		kind = vault.ReplaceAttachment
	}
	changeData := append([]byte(nil), data...)
	clear(data)
	change := vault.Change{Kind: kind, EntryUUID: state.entryUUID, GroupUUID: state.groupUUID, AttachmentName: state.targetName, AttachmentData: changeData}
	state.phase = "working"
	state.loading = true
	state.err = ""
	return m.apply(change)
}

func (m *Model) attachmentConfirmKey(k tea.KeyMsg) tea.Cmd {
	state := m.extras
	switch k.String() {
	case "esc":
		state.phase, state.action, state.path, state.targetName = "", "", "", ""
		state.confirmProceed = false
		state.loading = false
		state.err = ""
	case "left", "right", "tab", "shift+tab":
		state.confirmProceed = !state.confirmProceed
	case "enter":
		if !state.confirmProceed {
			state.phase, state.action, state.path, state.targetName = "", "", "", ""
			state.input.clear()
			state.err = ""
			return nil
		}
		switch state.phase {
		case "confirm-replace":
			return m.commitAttachmentImport(state)
		case "confirm-remove":
			session, _ := m.currentExtraEntry(state)
			if !m.extrasWritable(session) {
				state.err = "Attachment changes require write access and an encrypted backup location."
				return nil
			}
			state.phase = "working"
			state.loading = true
			return m.apply(vault.Change{Kind: vault.RemoveAttachment, EntryUUID: state.entryUUID, GroupUUID: state.groupUUID, AttachmentName: state.targetName})
		case "confirm-export":
			return m.startAttachmentExport(state)
		}
	}
	return nil
}

func (m *Model) startAttachmentExport(state *extraState) tea.Cmd {
	api, ok := m.backend.(interface {
		ExportAttachment(context.Context, *vault.Database, vault.Credentials, string, string) ([]byte, error)
	})
	if !ok {
		state.err = "Attachment export is not supported by this backend."
		return nil
	}
	session, entry := m.currentExtraEntry(state)
	if session == nil || entry == nil {
		state.err = "Entry is no longer available."
		return nil
	}
	if _, err := os.Lstat(state.path); err == nil || !errors.Is(err, os.ErrNotExist) {
		state.err = "Destination already exists or cannot be checked; choose another path."
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	state.cancel = cancel
	state.loading = true
	state.err = ""
	db := session.DB.Clone()
	creds := session.Credentials.Clone()
	entryID, name, path := entry.UUID, state.targetName, state.path
	return func() tea.Msg {
		defer db.Clear()
		defer creds.Clear()
		data, err := api.ExportAttachment(ctx, db, creds, entryID, name)
		if err == nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = writePrivateExclusive(ctx, path, data)
			}
		}
		clear(data)
		return extrasExportMsg{state: state, err: err}
	}
}

func writePrivateExclusive(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	created := true
	defer func() {
		if created {
			_ = os.Remove(path)
		}
	}()
	for start := 0; start < len(data); {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return err
		}
		end := min(len(data), start+64*1024)
		n, err := file.Write(data[start:end])
		if err != nil {
			_ = file.Close()
			return err
		}
		if n != end-start {
			_ = file.Close()
			return io.ErrShortWrite
		}
		start = end
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	created = false
	return nil
}
