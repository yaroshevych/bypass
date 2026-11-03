package ui

import tea "github.com/charmbracelet/bubbletea"

// layerHandlers groups the input and rendering behavior of one active modal.
// Layer priority remains in activeLayer; workspace and terminal-size handling
// are owned by the application rather than a modal.
type layerHandlers struct {
	key     func(tea.KeyMsg) tea.Cmd
	pointer func(tea.MouseMsg) tea.Cmd
	view    func() []string
}

func (m *Model) handlersFor(layer uiLayer) layerHandlers {
	switch layer {
	case layerPrivacy:
		return layerHandlers{m.pinKey, m.pinPointer, m.pinLines}
	case layerRequests:
		return layerHandlers{m.runLayerKey, m.runPointer, m.runDialogView}
	case layerDialog:
		return layerHandlers{m.dialogKey, m.dialogLayerPointer, m.dialogLines}
	case layerLifecycle:
		return layerHandlers{m.lifecycleKey, m.lifecyclePointer, m.lifecycleLines}
	case layerExtras:
		return layerHandlers{m.extrasLayerKey, m.extrasLayerPointer, m.extrasLines}
	case layerGrand:
		return layerHandlers{m.grandKey, m.grandPointer, m.grandLines}
	case layerPalette:
		return layerHandlers{m.paletteKey, m.palettePointer, m.paletteLines}
	case layerJump:
		return layerHandlers{key: m.jumpKey, view: m.jumpLines}
	case layerHelp:
		return layerHandlers{m.helpKey, m.helpPointer, m.helpLines}
	default:
		return layerHandlers{}
	}
}

func (m *Model) extrasLayerKey(msg tea.KeyMsg) tea.Cmd {
	cmd, _ := m.extrasKey(msg)
	return cmd
}

func (m *Model) extrasLayerPointer(msg tea.MouseMsg) tea.Cmd {
	cmd, _ := m.extrasPointer(msg)
	return cmd
}

func (m *Model) dialogLayerPointer(msg tea.MouseMsg) tea.Cmd {
	m.dragging = 0
	if !m.writing && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		return m.dialogPointer(msg)
	}
	return nil
}

func (m *Model) helpPointer(msg tea.MouseMsg) tea.Cmd {
	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollHelp(-1)
		case tea.MouseButtonWheelDown:
			m.scrollHelp(1)
		}
	}
	return nil
}

func (m *Model) runLayerKey(msg tea.KeyMsg) tea.Cmd {
	cmd, _ := m.updateRun(msg)
	return cmd
}

func (m *Model) runPointer(msg tea.MouseMsg) tea.Cmd {
	r := m.runs
	if r == nil || msg.Action != tea.MouseActionPress || r.busy || r.resetBusy {
		return nil
	}
	if !r.resetConfirm && !r.bulkConfirm {
		delta := 0
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -1
		} else if msg.Button == tea.MouseButtonWheelDown {
			delta = 1
		}
		if delta != 0 {
			if r.review && r.choosing {
				r.choiceIndex = min(max(0, len(r.choices)-1), max(0, r.choiceIndex+delta))
			} else if r.review {
				r.fieldOffset = min(max(0, len(r.targets)-m.runFieldPageSize()), max(0, r.fieldOffset+delta))
			} else if len(r.pending) > 0 {
				r.index = min(len(r.pending)-1, max(0, r.index+delta))
				r.buttonsFocused = false
			}
			return nil
		}
	}
	if msg.Button == tea.MouseButtonLeft {
		if action, ok := m.runActionAt(msg.X, msg.Y); ok {
			return m.activateRunAction(action)
		}
		if index := m.runRequestAt(msg.X, msg.Y); index >= 0 && index < len(r.pending) {
			r.index, r.buttonsFocused = index, false
		}
	}
	return nil
}

func (m *Model) jumpLines() []string {
	body := []string{"p Password   u Username   w URL   n Notes"}
	if e := m.selected(); e != nil {
		for _, field := range e.Fields {
			if field.Name == "Email" {
				body = append(body, "e Email")
				break
			}
		}
	}
	return m.overlay("Jump to Field", body, "", min(66, m.width-4), -1, -1)
}
