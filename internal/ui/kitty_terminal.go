package ui

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// KittyTerminal intercepts preview markers in Bubble Tea's output. Image bytes
// never enter a View string or Bubble Tea's cached frames. Its mutex serializes
// deletion with rendering so a late frame cannot reveal a closed preview.
type KittyTerminal struct {
	mu               sync.Mutex
	out              io.Writer
	supported        bool
	generation       uint64
	imageID          uint32
	png              []byte
	x, y, cols, rows int
}

const kittyMarkerPrefix = "\x1b_bypass-preview="
const kittyMarkerEnd = "\x1b\\"

var kittyImageFallback atomic.Uint32

// KittyGraphicsAvailable conservatively identifies terminals known to support
// direct Kitty graphics. tmux requires an opt-in passthrough setting, so it is
// not assumed to work based on the outer terminal's environment.
func KittyGraphicsAvailable() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	term, program := strings.ToLower(os.Getenv("TERM")), strings.ToLower(os.Getenv("TERM_PROGRAM"))
	return strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") || program == "ghostty" || os.Getenv("KITTY_WINDOW_ID") != ""
}

func NewKittyTerminal(out io.Writer, supported bool) *KittyTerminal {
	var idBytes [4]byte
	id := uint32(0)
	if _, err := rand.Read(idBytes[:]); err == nil {
		id = binary.BigEndian.Uint32(idBytes[:])
	}
	if id == 0 {
		id = uint32(os.Getpid()) ^ kittyImageFallback.Add(1) | 1
	}
	return &KittyTerminal{out: out, supported: supported, imageID: id}
}

func (t *KittyTerminal) Supported() bool { return t != nil && t.supported }

// Bubble Tea checks term.File to obtain the terminal size. Forward the
// descriptor while keeping ownership of stdout with the caller.
func (t *KittyTerminal) Fd() uintptr {
	if f, ok := t.out.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}
func (t *KittyTerminal) Read(p []byte) (int, error) {
	if r, ok := t.out.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, io.EOF
}
func (t *KittyTerminal) Close() error { t.Clear(); return nil }

// SetPreview replaces the image and returns the zero-width marker to put at
// the beginning of the last View row. Positions are zero-based terminal cells.
func (t *KittyTerminal) SetPreview(png []byte, x, y, cols, rows int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLocked()
	if !t.supported || len(png) == 0 || cols < 0 || rows < 0 || (cols == 0 && rows == 0) {
		return ""
	}
	t.png = append([]byte(nil), png...)
	t.x, t.y, t.cols, t.rows = max(0, x), max(0, y), cols, rows
	return kittyMarkerPrefix + strconv.FormatUint(t.generation, 10) + kittyMarkerEnd
}

// Clear synchronously removes the image even if no further frame is rendered.
func (t *KittyTerminal) Clear() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLocked()
}

func (t *KittyTerminal) clearLocked() {
	t.generation++
	if len(t.png) > 0 {
		_, _ = fmt.Fprintf(t.out, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", t.imageID)
		clear(t.png)
		t.png = nil
	}
}

type kittyStrictWriter struct{ io.Writer }

func (w kittyStrictWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

// Bubble Tea's standard renderer writes each entire frame in one Write call.
// Markers are inserted only in those frames, never in standalone terminal
// control writes. Unknown or stale markers are removed without displaying data.
func (t *KittyTerminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	original := len(p)
	out := kittyStrictWriter{t.out}
	for {
		i := bytes.Index(p, []byte(kittyMarkerPrefix))
		if i < 0 {
			if _, err := out.Write(p); err != nil {
				return 0, err
			}
			return original, nil
		}
		if _, err := out.Write(p[:i]); err != nil {
			return 0, err
		}
		rest := p[i+len(kittyMarkerPrefix):]
		end := bytes.Index(rest, []byte(kittyMarkerEnd))
		if end < 0 { // A malformed marker must never leak to the terminal.
			return original, nil
		}
		generation, err := strconv.ParseUint(string(rest[:end]), 10, 64)
		if err == nil && generation == t.generation && len(t.png) > 0 {
			if err := t.writePreviewLocked(); err != nil {
				return 0, err
			}
		}
		p = rest[end+len(kittyMarkerEnd):]
	}
}

func (t *KittyTerminal) writePreviewLocked() error {
	// Save/restore the renderer's cursor. C=1 prevents Kitty moving it.
	out := kittyStrictWriter{t.out}
	if _, err := fmt.Fprintf(out, "\x1b7\x1b[%d;%dH", t.y+1, t.x+1); err != nil {
		return err
	}
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(t.png)))
	defer clear(encoded)
	base64.StdEncoding.Encode(encoded, t.png)
	for pos := 0; pos < len(encoded); {
		end := min(pos+4096, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		var err error
		if pos == 0 {
			size := ""
			if t.cols > 0 {
				size += fmt.Sprintf(",c=%d", t.cols)
			}
			if t.rows > 0 {
				size += fmt.Sprintf(",r=%d", t.rows)
			}
			_, err = fmt.Fprintf(out, "\x1b_Ga=T,t=d,f=100,i=%d,q=2,C=1%s,m=%d;", t.imageID, size, more)
		} else {
			_, err = fmt.Fprintf(out, "\x1b_Gm=%d;", more)
		}
		if err != nil {
			return err
		}
		if _, err = out.Write(encoded[pos:end]); err != nil {
			return err
		}
		if _, err = io.WriteString(out, "\x1b\\"); err != nil {
			return err
		}
		pos = end
	}
	_, err := io.WriteString(out, "\x1b8")
	return err
}
