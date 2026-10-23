package ui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// input keeps secret input out of rendered text. Clear overwrites its rune buffer;
// Go strings and terminal input buffers cannot offer guaranteed memory erasure.
type input struct {
	text   []rune
	cursor int
}

func (i *input) value() string { return string(i.text) }

func (i *input) clear() {
	clear(i.text)
	i.text = nil
	i.cursor = 0
}

func (i *input) set(s string) {
	i.clear()
	i.text = []rune(s)
	i.cursor = len(i.text)
}

func (i *input) update(k tea.KeyMsg) {
	switch k.Type {
	case tea.KeyLeft:
		i.cursor = max(0, i.cursor-1)
	case tea.KeyRight:
		i.cursor = min(len(i.text), i.cursor+1)
	case tea.KeyHome, tea.KeyCtrlA:
		i.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		i.cursor = len(i.text)
	case tea.KeyCtrlU:
		i.clear()
	case tea.KeyCtrlW:
		start := i.cursor
		for start > 0 && unicode.IsSpace(i.text[start-1]) {
			start--
		}
		for start > 0 && !unicode.IsSpace(i.text[start-1]) {
			start--
		}
		i.erase(start, i.cursor)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if i.cursor > 0 {
			i.erase(i.cursor-1, i.cursor)
		}
	case tea.KeyDelete:
		if i.cursor < len(i.text) {
			i.erase(i.cursor, i.cursor+1)
		}
	case tea.KeyRunes:
		if k.Alt {
			return
		}
		var runes []rune
		for _, r := range k.Runes {
			if !unicode.IsControl(r) && r != '\u2028' && r != '\u2029' {
				runes = append(runes, r)
			}
		}
		// Bound paste size, including password paste, to keep rendering responsive.
		if len(i.text)+len(runes) > 4096 {
			return
		}
		tail := append([]rune(nil), i.text[i.cursor:]...)
		i.text = append(i.text[:i.cursor], runes...)
		i.text = append(i.text, tail...)
		i.cursor += len(runes)
		clear(tail)
		clear(runes)
	}
}

func (i *input) erase(start, end int) {
	n := copy(i.text[start:], i.text[end:])
	clear(i.text[start+n:])
	i.text = i.text[:len(i.text)-(end-start)]
	i.cursor = start
}

func (i *input) display(secret bool) string {
	if secret {
		return strings.Repeat("•", len(i.text))
	}
	return safe(i.value())
}
