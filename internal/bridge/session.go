package bridge

import (
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/history"
	"compositor-win/internal/render"
)

// errNoDocument is returned by every endpoint that needs an active tab.
var (
	errNoDocument               = errors.New("没有打开的文档")
	errDocumentDirty            = errors.New("DOCUMENT_DIRTY: 文档有未保存的更改，需要确认后重载")
	errDocumentBusy             = errors.New("DOCUMENT_BUSY: 文档有尚未完成的编辑，完成或取消后再重载")
	errDocumentIDConflict       = errors.New("DOCUMENT_ID_CONFLICT: 文档 ID 已被当前工作区使用")
	errExternalChangeNotPending = errors.New("EXTERNAL_CHANGE_NOT_PENDING: 没有待处理的外部变更")
)

// session is one open document's live state: the domain model, its undo
// history, the package path it was loaded from ("" until first save), and
// the in-memory bitmap library. Bitmaps are keyed by asset file name — the
// manifest derives ImageFile from the layer UUID (project.imageName), so
// asset name ↔ layer image is 1:1. Pixels never live in the domain model;
// every pixel read (render, save) goes through bitmaps.
type session struct {
	doc      *domain.Document
	hist     *history.History
	path     string
	bitmaps  map[string]*render.Bitmap
	rev      int // frontend revision: bumped on every observable change
	layerSeq int // names the next 图层 N
	// saving marks a package write in flight. Editing does not consult it: the
	// snapshot was already cloned under the lock, so the document stays editable
	// while the bytes go out (upstream ProjectController.save clears isProjectBusy
	// before the write). It only gates replacing the document — close, switch and
	// reload — which would otherwise race the write's completion.
	saving bool

	// Brush state: at most one stroke in flight per document. The stroke
	// paints into its own tile grid (render.Stroke never touches the base),
	// and EndStroke swaps the committed grid back into bitmaps.
	stroke    *render.Stroke
	strokeKey string

	// Filter session: one open filter/adjustment dialog (its preview bitmap
	// substitutes the layer's asset in the composite), plus the raster undo
	// journal pairing filter commits with history revisions.
	filter        *filterSession
	rasterJournal []rasterJournalEntry
	// A menu-created adjustment layer is marked provisional until its editor
	// commits. The marker is set only by the frontend's explicit provisional
	// LayerOp payload, so direct bridge callers retain normal layer history.
	provisionalAdjustment *provisionalAdjustmentCreation

	// In-flight spot-healing stroke.
	heal *healBrush

	// Render cache, invalidated by rev: renderPNG holds the composed PNG of
	// the document at renderRev. EncodePNG never returns empty bytes, so a
	// nil slice means "not cached yet".
	renderPNG       []byte
	renderRev       int
	renderFilterRev int

	// Clipboard (ticket 48): what Copy took — pixels with the document point
	// they came from, or the layer ids it took whole. Both carry the system
	// clipboard's sequence number at copy time; anything copied since (here or
	// in another app) invalidates them, exactly like the original compares
	// NSPasteboard.changeCount (SelectionClipboard.swift:4-18).
	clipPixels *clipPixels
	clipLayer  *clipLayer
}

type provisionalAdjustmentCreation struct {
	layerID         string
	historyRevision string
}

// newSession wraps a parsed/created document into a fresh session with an
// empty history and rev 0.
func newSession(path string, doc *domain.Document, bitmaps map[string]*render.Bitmap) *session {
	if bitmaps == nil {
		bitmaps = map[string]*render.Bitmap{}
	}
	return &session{
		doc:     doc,
		hist:    history.New(),
		path:    path,
		bitmaps: bitmaps,
	}
}

// 新建画布的底色（票 57，对等矩阵 U04）。空串与 "transparent" 都是透明，
// 与原版 NewCanvasBackground 的默认值一致。
const (
	canvasBackgroundTransparent = "transparent"
	canvasBackgroundWhite       = "white"
	canvasBackgroundBlack       = "black"
)

// parseCanvasBackground normalizes the background an RPC carried. Anything but
// the accepted spellings is a request error: the original's enum makes an
// unknown value unrepresentable, the wire makes it a string.
func parseCanvasBackground(background string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(background)) {
	case "", canvasBackgroundTransparent:
		return canvasBackgroundTransparent, nil
	case canvasBackgroundWhite:
		return canvasBackgroundWhite, nil
	case canvasBackgroundBlack:
		return canvasBackgroundBlack, nil
	}
	return "", fmt.Errorf("无效的画布底色 %q（可用：transparent、white、black）", background)
}

// newDocumentModel builds the model behind 新建文档 with the pre-ticket-57
// default: one opaque white 背景 layer at the full canvas. The 3-argument
// entry is kept for its existing callers (document_test/export_test/fill_test/
// text_test pin that fixture); the frontend creates documents through
// newDocumentModelWithBackground with a transparent default.
func newDocumentModel(width, height, resolution int) (*domain.Document, map[string]*render.Bitmap) {
	return buildDocumentModel(width, height, resolution, "背景", uint8Ptr(255))
}

// newDocumentModelWithBackground builds the model behind 新建文档 for the
// chosen background — it expects a value normalized by parseCanvasBackground.
// White/black fill an opaque Background layer with that color at full-canvas
// pixels (the original's firstLayer), transparent leaves a blank 图层 1. The original's transparent layer has no asset at all
// (asset == nil); here every paint target requires one
// (resolveRasterPaintTarget), so the blank layer carries a fully transparent
// bitmap — the composite is identical.
func newDocumentModelWithBackground(width, height, resolution int, background string) (*domain.Document, map[string]*render.Bitmap) {
	switch background {
	case canvasBackgroundWhite:
		return buildDocumentModel(width, height, resolution, "Background", uint8Ptr(255))
	case canvasBackgroundBlack:
		return buildDocumentModel(width, height, resolution, "Background", uint8Ptr(0))
	default: // transparent
		return buildDocumentModel(width, height, resolution, "图层 1", nil)
	}
}

// buildDocumentModel assembles a version-11 manifest document with one
// full-canvas pixel layer. The layer record follows the project-format
// contract — ImageFile derived from the layer UUID, identity transform at
// Nearest sampling. fill is the opaque color every pixel takes; nil leaves the
// bitmap fully transparent.
func buildDocumentModel(width, height, resolution int, layerName string, fill *uint8) (*domain.Document, map[string]*render.Bitmap) {
	res := resolution
	bg := newPixelLayer(layerName, width, height)
	active := bg.ID
	doc := &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    newUUID(),
		Width:         width,
		Height:        height,
		Resolution:    &res,
		ActiveLayerID: &active,
		Layers:        []domain.Layer{bg},
	}
	bmp := render.NewBitmap(width, height)
	if fill != nil {
		for i := 0; i < len(bmp.Pix); i += 4 { // premultiplied opaque fill: the color on RGB, 255 on alpha
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = *fill, *fill, *fill, 255
		}
	}
	return doc, map[string]*render.Bitmap{*bg.ImageFile: bmp}
}

// uint8Ptr returns a pointer to v (the fill color of a new canvas).
func uint8Ptr(v uint8) *uint8 { return &v }

// newPixelLayer builds a blank pixel layer: transparent bitmap asset named
// <UUID>.png (project.imageName requires exactly that spelling), visible,
// not a group, opacity 1, Normal, identity transform at Nearest.
func newPixelLayer(name string, width, height int) domain.Layer {
	id := newUUID()
	image := id + ".png"
	isGroup := false
	opacity := 1.0
	blend := domain.BlendNormal
	return domain.Layer{
		ID:        id,
		Name:      name,
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{float64(width), float64(height)},
			Sampling: domain.SamplingNearest,
		},
		ImageFile: &image,
		IsGroup:   &isGroup,
		Opacity:   &opacity,
		BlendMode: &blend,
	}
}

// pixelSource adapts the bitmap library to render.PixelSource.
func (sess *session) pixelSource() render.PixelSource {
	return func(name string) (*render.Bitmap, error) {
		if bmp, ok := sess.bitmaps[name]; ok && bmp != nil {
			return bmp, nil
		}
		return nil, fmt.Errorf("内存位图库缺少资产 %s", name)
	}
}

// findLayerIndex locates a layer by ID, or -1.
func findLayerIndex(doc *domain.Document, id string) int {
	for i := range doc.Layers {
		if doc.Layers[i].ID == id {
			return i
		}
	}
	return -1
}

// referencedAssetNames lists the distinct asset names the manifest points
// at (images and masks), in layer order.
func referencedAssetNames(doc *domain.Document) []string {
	seen := map[string]bool{}
	var out []string
	for i := range doc.Layers {
		for _, name := range []string{derefString(doc.Layers[i].ImageFile), derefString(doc.Layers[i].MaskFile)} {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// cloneDocForEdit deep-copies a document via a JSON round-trip — the same
// trick history.cloneDocument uses, as a rollback guard for failed edits.
func cloneDocForEdit(d *domain.Document) *domain.Document {
	b, err := json.Marshal(d)
	if err != nil { // unreachable for the JSON-only manifest model
		return d
	}
	var out domain.Document
	if err := json.Unmarshal(b, &out); err != nil {
		return d
	}
	return &out
}

// newUUID returns an uppercase dashed RFC 4122 v4 UUID — the on-disk spelling
// for document/layer IDs (project.parseUUID) and asset names.
func newUUID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func newAdjustmentSeed() uint32 {
	var b [4]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return binary.LittleEndian.Uint32(b[:])
}
