package ui

import (
	wipebuf "bypass/internal/secret"
	tea "github.com/charmbracelet/bubbletea"
)

// secretRunes handles typed and pasted secrets before shortcut formatting can
// convert them into immutable strings. Control keys still use normal routing.
func (m *Model) secretRunes(k tea.KeyMsg) bool {
	if k.Type != tea.KeyRunes && k.Type != tea.KeySpace {
		return false
	}
	switch m.activeLayer() {
	case layerPrivacy:
		if m.pinFullLock || m.pinFocus != 0 {
			return false
		}
		if k.Alt {
			return false
		}
		k, runes := normalizeSecretKey(k)
		defer wipebuf.Wipe(runes)
		for _, r := range runes {
			if r < '0' || r > '9' {
				return true
			}
		}
		if len(m.pinInput.text)+len(runes) <= 12 {
			m.pinInput.update(k)
		}
		return true
	case layerDialog:
		d := m.dialog
		if d == nil || (d.kind != "pin-setup" && d.kind != "pin-confirm") || d.selected != 0 {
			return false
		}
		if k.Alt {
			return false
		}
		k, runes := normalizeSecretKey(k)
		defer wipebuf.Wipe(runes)
		for _, r := range runes {
			if r < '0' || r > '9' {
				d.err = "Use digits only."
				return true
			}
		}
		d.input.update(k)
		d.err = ""
		return true
	case layerLifecycle:
		l := m.lifecycle
		if l == nil || l.busy || l.confirm || (l.kind != "create" && l.kind != "credentials") {
			return false
		}
		first := 0
		if l.kind == "create" {
			first = 1
		}
		if l.focus != first && l.focus != first+1 {
			return false
		}
		if k.Alt {
			return false
		}
		k, runes := normalizeSecretKey(k)
		defer wipebuf.Wipe(runes)
		l.inputs[l.focus].update(k)
		l.err = ""
		return true
	case layerWorkspace:
		if m.writing {
			return false
		}
		if m.editor != nil && m.editor.focus == editPassword {
			if k.Alt {
				return false
			}
			k, runes := normalizeSecretKey(k)
			defer wipebuf.Wipe(runes)
			m.hideReveal()
			if invalidPasswordRunes(runes) {
				m.editor.validationFocus = editPassword
				m.editor.validation = "Password contains an unsupported character."
				m.editor.err = m.editor.validation
			} else {
				m.editor.inputs[editPassword].update(k)
				m.editor.clearValidation()
			}
			return true
		}
		if m.screen == unlock && !m.busy && m.inputFocus == 0 {
			if k.Alt {
				return false
			}
			k, runes := normalizeSecretKey(k)
			defer wipebuf.Wipe(runes)
			for _, r := range runes {
				if r == '\n' || r == '\r' || r == 0 {
					m.err = "Password cannot contain line breaks; CLI transport does not support them."
					return true
				}
			}
			m.inputs[0].update(k)
			return true
		}
	}
	return false
}

func normalizeSecretKey(k tea.KeyMsg) (tea.KeyMsg, []rune) {
	runes := k.Runes
	if k.Type == tea.KeySpace {
		runes = []rune{' '}
	}
	k.Type, k.Runes = tea.KeyRunes, runes
	return k, runes
}
