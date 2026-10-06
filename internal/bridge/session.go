package bridge

import (
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"compositor-win/internal/domain"
	"compositor-win/internal/history"
	"compositor-win/internal/render"
)

// errNoDocument is returned by every endpoint that needs an active tab.
var errNoDocument = errors.New("没有打开的文档")

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

	// Brush state: at most one stroke in flight per document. The stroke
	// paints into its own tile grid (render.Stroke never touches the base),
	// and EndStroke swaps the committed grid back into bitmaps.
	stroke    *render.Stroke
	strokeKey string

	// Render cache, invalidated by rev: renderPNG holds the composed PNG of
	// the document at renderRev. EncodePNG never returns empty bytes, so a
	// nil slice means "not cached yet".
	renderPNG []byte
	renderRev int
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

// newDocumentModel builds the model behind 新建文档: a version-11 manifest
// document with one opaque white 背景 layer at the full canvas. The layer
// record follows the project-format contract — ImageFile derived from the
// layer UUID, identity transform at Nearest sampling.
func newDocumentModel(width, height, resolution int) (*domain.Document, map[string]*render.Bitmap) {
	res := resolution
	bg := newPixelLayer("背景", width, height)
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
	white := render.NewBitmap(width, height)
	for i := range white.Pix { // premultiplied white opaque: 255 on every channel
		white.Pix[i] = 255
	}
	return doc, map[string]*render.Bitmap{*bg.ImageFile: white}
}

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
