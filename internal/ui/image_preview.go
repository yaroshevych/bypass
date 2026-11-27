package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"

	wipebuf "bypass/internal/secret"
	dialogrender "bypass/internal/ui/dialog"
	tea "github.com/charmbracelet/bubbletea"
)

const maxPreviewBytes = 32 << 20
const maxPreviewPixels = 16 << 20

type imagePreviewMsg struct {
	state         *extraState
	requestID     uint64
	png           []byte
	width, height int
	page, pages   int
	secret        *secretValues
	err           error
}

func (m *Model) openImagePreview() tea.Cmd {
	session, entry := m.extraTarget()
	if session == nil || entry == nil || !m.attachmentFocused() {
		return nil
	}
	index := m.ws.ActiveView().FieldIndex - len(m.fieldNames())
	if index < 0 || index >= len(entry.AttachmentNames) {
		return nil
	}
	name := entry.AttachmentNames[index]
	m.hideReveal()
	m.clearExtras()
	state := &extraState{mode: extraImagePreview, sessionID: session.ID, entryUUID: entry.UUID, groupUUID: entry.GroupUUID, targetName: name}
	m.extras = state
	if !m.previewTerminal.Supported() {
		state.err = "Preview requires a terminal with Kitty graphics support."
		return nil
	}
	for i, other := range entry.AttachmentNames {
		if i != index && other == name {
			state.err = "Cannot preview attachments with duplicate names."
			return nil
		}
	}
	if session.Core == nil || !session.Core.CanExportAttachment() {
		state.err = "Attachment preview is not supported by this backend."
		return nil
	}
	return m.requestPreviewPage(state, 1)
}

func (m *Model) requestPreviewPage(state *extraState, page int) tea.Cmd {
	session, entry := m.currentExtraEntry(state)
	if session == nil || entry == nil || session.Core == nil {
		return nil
	}
	if state.cancel != nil {
		state.cancel()
	}
	m.clearImagePreviewTerminal()
	wipebuf.Wipe(state.previewPNG)
	state.previewPNG = nil
	state.previewPage = page
	state.err = ""
	snapshot := session.Core.ReadSnapshot()
	if snapshot == nil {
		state.err = "Entry is no longer available."
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	state.cancel, state.loading = cancel, true
	state.requestID++
	requestID := state.requestID
	core, entryID, name := session.Core, entry.UUID, state.targetName
	if page < 1 {
		page = 1
	}
	resources := newJobResources(ctx, snapshot.Clear)
	return func() tea.Msg {
		msg := imagePreviewMsg{state: state, requestID: requestID}
		if !resources.start() {
			msg.err = ctx.Err()
			return msg
		}
		defer resources.done()
		data, err := core.ExportAttachment(ctx, snapshot, entryID, name)
		if err != nil {
			wipebuf.Wipe(data)
			msg.err = errors.New("Could not read attachment.")
			return msg
		}
		defer wipebuf.Wipe(data)
		if isPDF(data) {
			msg.page = page
			msg.png, msg.width, msg.height, msg.pages, msg.err = makePDFPreview(ctx, data, page)
		} else {
			msg.png, msg.width, msg.height, msg.err = makePreviewPNG(ctx, data)
		}
		msg.secret = holdSecret(ctx, msg.png)
		return msg
	}
}

func makePreviewPNG(ctx context.Context, data []byte) ([]byte, int, int, error) {
	if len(data) > maxPreviewBytes {
		return nil, 0, 0, errors.New("Image is too large to preview.")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, errors.New("This attachment is not a supported image.")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPreviewPixels/cfg.Height {
		return nil, 0, 0, errors.New("Image dimensions are too large to preview.")
	}
	if ctx.Err() != nil {
		return nil, 0, 0, ctx.Err()
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, errors.New("Could not decode image.")
	}
	defer clearDecodedImage(src)
	if ctx.Err() != nil {
		return nil, 0, 0, ctx.Err()
	}
	// Convert to RGBA so PNG encoding has a predictable format.
	rgba := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	defer wipebuf.Wipe(rgba.Pix)
	draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		wipebuf.Wipe(encoded.Bytes())
		return nil, 0, 0, errors.New("Could not prepare image preview.")
	}
	if ctx.Err() != nil {
		wipebuf.Wipe(encoded.Bytes())
		return nil, 0, 0, ctx.Err()
	}
	return encoded.Bytes(), cfg.Width, cfg.Height, nil
}

func clearDecodedImage(img image.Image) {
	switch v := img.(type) {
	case *image.RGBA:
		wipebuf.Wipe(v.Pix)
	case *image.NRGBA:
		wipebuf.Wipe(v.Pix)
	case *image.RGBA64:
		wipebuf.Wipe(v.Pix)
	case *image.NRGBA64:
		wipebuf.Wipe(v.Pix)
	case *image.CMYK:
		wipebuf.Wipe(v.Pix)
	case *image.Gray:
		wipebuf.Wipe(v.Pix)
	case *image.Gray16:
		wipebuf.Wipe(v.Pix)
	case *image.Alpha:
		wipebuf.Wipe(v.Pix)
	case *image.Alpha16:
		wipebuf.Wipe(v.Pix)
	case *image.Paletted:
		wipebuf.Wipe(v.Pix)
	case *image.YCbCr:
		wipebuf.Wipe(v.Y)
		wipebuf.Wipe(v.Cb)
		wipebuf.Wipe(v.Cr)
	case *image.NYCbCrA:
		wipebuf.Wipe(v.Y)
		wipebuf.Wipe(v.Cb)
		wipebuf.Wipe(v.Cr)
		wipebuf.Wipe(v.A)
	}
}

// imagePreviewGeometry returns the dialog frame and the image's interior area.
func (m *Model) imagePreviewGeometry() (dialogrender.OverlayGeometry, int, int, int, int) {
	width := max(20, m.width*90/100)
	height := max(5, m.height*90/100)
	chrome := 5 // classic: frame, title, divider, and status
	if m.compactAppearance() {
		chrome = 3
	}
	bodyRows := max(1, height-chrome)
	g := dialogrender.LayoutOverlay(width, m.width, m.height, bodyRows+1, m.compactAppearance(), -1, -1)
	return g, g.X + 2, g.Y + g.HeaderRows, max(1, g.Width-4), bodyRows
}

func (m *Model) imagePreviewLines(state *extraState) []string {
	g, _, _, _, bodyRows := m.imagePreviewGeometry()
	body := make([]string, bodyRows)
	if state.loading {
		body[0] = m.colors().secondary.Render(" Loading preview…")
	}
	if state.err != "" {
		body[0] = m.colors().error.Render(" " + safe(state.err))
	}
	opts := m.dialogOptions()
	footer := "esc close"
	if state.previewPages > 0 {
		arrows := "←→"
		if m.settings.ASCII {
			arrows = "left/right"
		}
		footer = fmt.Sprintf("page %d/%d · %s page · esc close", state.previewPage, state.previewPages, arrows)
	}
	opts.Mode, opts.Title, opts.Body, opts.StatusBar = dialogrender.Overlay, safe(state.targetName), body, footer
	opts.Width, opts.X, opts.Y, opts.Backdrop, opts.Fullscreen = g.Width, g.X, g.Y, m.modalBackdrop(), false
	return dialogrender.Render(opts)
}

// syncImagePreviewTerminal keeps terminal graphics aligned with the active modal.
// Clear is synchronous: a queued frame cannot restore an old generation.
func (m *Model) syncImagePreviewTerminal() {
	state := m.extras
	if m.previewTerminal == nil {
		return
	}
	if m.activeLayer() != layerExtras || state == nil || state.mode != extraImagePreview || len(state.previewPNG) == 0 {
		if m.previewMarker != "" {
			m.clearImagePreviewTerminal()
		}
		return
	}
	_, x, y, cols, rows := m.imagePreviewGeometry()
	cellWidth, cellHeight := previewCellSize(m.previewTerminal.Fd())
	x, y, cols, rows = fitPreviewImage(x, y, cols, rows, state.previewWidth, state.previewHeight, cellWidth, cellHeight)
	geometry := [4]int{x, y, cols, rows}
	if m.previewMarker != "" && geometry == m.previewGeometry {
		return
	}
	m.previewMarker = m.previewTerminal.SetPreview(state.previewPNG, x, y, cols, rows)
	m.previewGeometry = geometry
}

func (m *Model) clearImagePreviewTerminal() {
	if m.previewTerminal != nil {
		m.previewTerminal.Clear()
	}
	m.previewMarker = ""
	m.previewGeometry = [4]int{}
}

// Specify only one Kitty placement dimension so the terminal computes the other
// from the image's exact aspect ratio, including on older Ghostty versions.
func fitPreviewImage(x, y, cols, rows, imageWidth, imageHeight int, cellWidth, cellHeight float64) (int, int, int, int) {
	ratio := float64(imageWidth) / float64(imageHeight)
	cellRatio := cellWidth / cellHeight
	if float64(cols)*cellRatio/float64(rows) <= ratio {
		usedRows := min(rows, max(1, int(math.Ceil(float64(cols)*cellRatio/ratio))))
		return x, y + (rows-usedRows)/2, cols, 0
	}
	usedCols := min(cols, max(1, int(math.Ceil(float64(rows)*ratio/cellRatio))))
	return x + (cols-usedCols)/2, y, 0, rows
}
