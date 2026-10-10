package bridge

// 剪贴板（票 48）—— SelectionClipboard.swift 的移植：像素出/入系统剪贴板、
// 剪切留洞、粘贴（原位/居中）、整层复制粘贴（同标签复制、跨标签搬运）。
//
// 与原版的对应关系：
//   selectionCopyRegion / renderSelectedPixels  SelectionClipboard.swift:24-62
//   copySelection / store                       :101-129
//   cutSelection                                :132-136
//   canPaste / paste                            :138-157
//   layerViaCopy                                :161-170
//   copiedLayerIDs                              :173-177
// 差异（诚实标注）：原版把"选中的蒙版"也当作可拷贝的像素，这里只处理图层像素
// （本应用还没有蒙版选区入口）；原版用 CoreGraphics 的软边蒙版裁切像素，这里对
// 预乘数据逐通道乘覆盖度（等价）；粘贴出的新层命名按仓库既有的"图层 N"
// （原版是 "Layer N"）。

import (
	"encoding/json"
	"fmt"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
	"compositor-win/internal/winclip"
)

// systemClipboard is the seam over the Windows clipboard: production is
// winclip, tests substitute an in-memory fake so the suite never reads or
// clobbers the user's clipboard.
type systemClipboard interface {
	Put(w, h int, pix []uint8) error
	Get() (*winclip.ClipBitmap, error)
	Sequence() uint32
}

// winclipSystem is the real clipboard.
type winclipSystem struct{}

func (winclipSystem) Put(w, h int, pix []uint8) error   { return winclip.PutBitmap(w, h, pix) }
func (winclipSystem) Get() (*winclip.ClipBitmap, error) { return winclip.GetImage() }
func (winclipSystem) Sequence() uint32                  { return winclip.Sequence() }

var clipboard systemClipboard = winclipSystem{}

// clipPixels is what Copy took with a selection (or the whole canvas): the
// pixels and the document point their top-left sits at, so Paste puts them
// back exactly where they came from.
type clipPixels struct {
	bmp    *render.Bitmap
	origin [2]float64
	seq    uint32
}

// clipLayer is a whole layer Copy took (no selection): the subtree roots and
// the clipboard sequence that keeps it valid.
type clipLayer struct {
	ids []string
	seq uint32
}

// copyReply is CopySelection's answer. Kind is "pixels" when the clipboard got
// an image, "none" when nothing was copied. Layer is true when a whole-layer
// copy was remembered beside the pixels (no selection — Swift's canCopyLayer
// branch).
type copyReply struct {
	Kind      string `json:"kind"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Clipboard bool   `json:"clipboard"`
	Layer     bool   `json:"layer"`
}

// CopySelection (⌘C) copies the active layer's pixels clipped to the selection
// — or the whole canvas without one — to the app's clipboard and to the system
// clipboard as a bitmap. With no selection the layer is also remembered whole,
// so Paste can bring it back complete in this project or another.
func (s *Service) CopySelection(selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	s.ws.mu.Lock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		s.ws.mu.Unlock()
		return "", errNoDocument
	}
	reply, err := copySelectionLocked(sess, sel)
	s.ws.mu.Unlock()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("无法编码拷贝结果: %w", err)
	}
	return string(b), nil
}

// copySelectionLocked is the shared body of Copy and the copy half of Cut.
//
// Without a selection the layer is copied WHOLE (Swift's canCopyLayer branch):
// folders and adjustment layers have no pixels of their own but still travel,
// and a pixel layer additionally goes to the system clipboard as an image. With
// a selection only the selected pixels are copied, so the layer must own
// pixels.
func copySelectionLocked(sess *session, sel *filterSelection) (copyReply, error) {
	if sess.doc.ActiveLayerID == nil {
		return copyReply{Kind: "none"}, fmt.Errorf("没有活动图层")
	}
	layer, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return copyReply{Kind: "none"}, err
	}

	if sel == nil {
		reply := copyReply{Kind: "layer", Layer: true}
		if layer.ImageFile != nil {
			if bmp, origin, err := renderSelectedPixels(sess, layer, nil); err == nil {
				reply.Kind = "pixels"
				reply.Width, reply.Height = bmp.W, bmp.H
				if err := clipboard.Put(bmp.W, bmp.H, bmp.Pix); err != nil {
					// The pixels still live in memory; a refusing system
					// clipboard (or a non-Windows build) is reported, not
					// fatal — same as Copy Merged.
					fmt.Printf("拷贝：剪贴板不可用 %v\n", err)
				} else {
					reply.Clipboard = true
				}
				// The sequence is read AFTER our own write: writing is what
				// advances it, so capturing it first would make the payload
				// look stale to Paste (SelectionClipboard.swift:127).
				sess.clipPixels = &clipPixels{bmp: bmp, origin: origin, seq: clipboard.Sequence()}
			}
		}
		// Only the ROOT id: both paste paths expand the subtree themselves
		// (duplicateLayersInSession with a single root id, copyLayersInto with
		// ids ∪ descendants). Storing the whole subtree here made the same-tab
		// paste treat every descendant as an independent root and duplicate it
		// twice. A layer that could not be rendered still copies whole.
		sess.clipLayer = &clipLayer{ids: []string{layer.ID}, seq: clipboard.Sequence()}
		return reply, nil
	}

	if layer.ImageFile == nil {
		return copyReply{Kind: "none"}, fmt.Errorf("图层 %s 没有像素", layer.ID)
	}
	bmp, origin, err := renderSelectedPixels(sess, layer, sel)
	if err != nil {
		return copyReply{Kind: "none"}, err
	}
	reply := copyReply{Kind: "pixels", Width: bmp.W, Height: bmp.H}
	if err := clipboard.Put(bmp.W, bmp.H, bmp.Pix); err != nil {
		fmt.Printf("拷贝：剪贴板不可用 %v\n", err)
	} else {
		reply.Clipboard = true
	}
	sess.clipPixels = &clipPixels{bmp: bmp, origin: origin, seq: clipboard.Sequence()}
	sess.clipLayer = nil
	return reply, nil
}

// CutSelection (⌘X) copies the selected pixels and clears them from the layer,
// leaving a hole. Copy first, then clear, exactly as the original does; the
// clear is one 剪切 history entry whose pixels the raster journal restores on
// undo.
func (s *Service) CutSelection(selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	if sel == nil {
		return "", fmt.Errorf("没有选区，无法剪切")
	}
	if _, err := s.CopySelection(selectionJSON); err != nil {
		return "", err
	}
	return s.ws.EditActiveRaster("剪切", func(sess *session) (*rasterChange, error) {
		layer, err := activePixelLayer(sess)
		if err != nil {
			return nil, err
		}
		bmp := sess.bitmaps[*layer.ImageFile]
		if bmp == nil {
			return nil, fmt.Errorf("内存位图库缺少资产 %s", *layer.ImageFile)
		}
		cleared := bmp.Clone()
		if !clearSelectionOnLayer(cleared, layer.Transform, sel) {
			return nil, nil // the selection misses this layer: nothing to undo
		}
		key := *layer.ImageFile
		sess.bitmaps[key] = cleared // the erase itself; the journal is undo only
		return &rasterChange{
			before: map[string]*render.Bitmap{key: bmp},
			after:  map[string]*render.Bitmap{key: cleared},
		}, nil
	})
}

// Paste (⌘V). Three sources, in the original's order: a whole layer copied in
// this workspace (duplicated here, or brought into the tab Paste runs in), the
// pixels this app put on the clipboard (back in place), or an image another
// app copied (centered).
func (s *Service) Paste() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	seq := clipboard.Sequence()
	if src, ids := s.ws.copiedLayerFor(seq); ids != nil {
		if src == s.ws.activeSession() {
			return s.ws.EditActive("粘贴", func(sess *session) error {
				return duplicateLayersInSession(sess, ids[0], nil)
			})
		}
		// Another tab copied it: bring the layers over (the cross-tab transfer).
		if err := s.ws.copyLayersInto(ids, src, s.ws.activeSession(), "粘贴"); err != nil {
			return "", err
		}
		return s.ws.ActiveDocumentJSON()
	}

	s.ws.mu.Lock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		s.ws.mu.Unlock()
		return "", errNoDocument
	}
	clip := sess.clipPixels
	s.ws.mu.Unlock()
	if clip != nil && clip.seq == seq {
		return s.ws.EditActive("粘贴", func(sess *session) error {
			return addPixelLayer(sess, clip.bmp.Clone(), clip.origin)
		})
	}

	// An image from another app: centered on the canvas
	// (SelectionClipboard.swift:151-155).
	external, err := clipboard.Get()
	if err != nil {
		return "", fmt.Errorf("剪贴板里没有可粘贴的图像")
	}
	bmp := &render.Bitmap{W: external.W, H: external.H, Pix: external.Pix}
	return s.ws.EditActive("粘贴", func(sess *session) error {
		origin := [2]float64{
			math.Floor(float64(sess.doc.Width-bmp.W) / 2),
			math.Floor(float64(sess.doc.Height-bmp.H) / 2),
		}
		return addPixelLayer(sess, bmp, origin)
	})
}

// LayerViaCopy (⌘J) with a selection: the selected pixels become a new layer
// in place, as one 通过拷贝的图层 step. Without a selection it is the ordinary
// layer duplicate, which the ⌘J action already routes to the duplicateLayer op.
func (s *Service) LayerViaCopy(selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	if sel == nil {
		return "", fmt.Errorf("没有选区")
	}
	s.ws.mu.Lock()
	sess := s.ws.activeSessionLocked()
	if sess == nil {
		s.ws.mu.Unlock()
		return "", errNoDocument
	}
	layer, err := activePixelLayer(sess)
	if err != nil {
		s.ws.mu.Unlock()
		return "", err
	}
	bmp, origin, err := renderSelectedPixels(sess, layer, sel)
	s.ws.mu.Unlock()
	if err != nil {
		return "", err
	}
	return s.ws.EditActive("通过拷贝的图层", func(sess *session) error {
		return addPixelLayer(sess, bmp, origin)
	})
}

// ---------------------------------------------------------------------------
// Pixel plumbing
// ---------------------------------------------------------------------------

// activePixelLayer is the layer Copy/Cut work on: the active one, and it must
// own pixels (folders and adjustment layers have none — Swift canCopyPixels).
func activePixelLayer(sess *session) (*domain.Layer, error) {
	if sess.doc.ActiveLayerID == nil {
		return nil, fmt.Errorf("没有活动图层")
	}
	layer, err := layerByID(sess.doc, *sess.doc.ActiveLayerID)
	if err != nil {
		return nil, err
	}
	if layer.IsGroupLayer() {
		return nil, fmt.Errorf("编组没有自己的像素")
	}
	if layer.ImageFile == nil {
		return nil, fmt.Errorf("图层 %s 没有像素", layer.ID)
	}
	return layer, nil
}

// clipRegion is the whole-pixel rectangle Copy takes.
type clipRegion struct{ x, y, w, h int }

// selectionRegion is the selection's bounds clipped to the canvas — the whole
// canvas without a selection (SelectionClipboard.swift:24-34).
func selectionRegion(doc *domain.Document, sel *filterSelection) *clipRegion {
	if sel == nil {
		return &clipRegion{0, 0, doc.Width, doc.Height}
	}
	x0 := max(0, sel.X)
	y0 := max(0, sel.Y)
	x1 := min(doc.Width, sel.X+sel.W)
	y1 := min(doc.Height, sel.Y+sel.H)
	if x1 <= x0 || y1 <= y0 {
		return nil
	}
	return &clipRegion{x0, y0, x1 - x0, y1 - y0}
}

// renderSelectedPixels composites the layer as the canvas shows it (transform,
// opacity, its own masks — through the same CPU compositor every other path
// uses), crops to the region, then scales every premultiplied channel by the
// selection's coverage, so soft edges survive. Returns the pixels and their
// document origin.
func renderSelectedPixels(sess *session, layer *domain.Layer, sel *filterSelection) (*render.Bitmap, [2]float64, error) {
	region := selectionRegion(sess.doc, sel)
	if region == nil {
		return nil, [2]float64{}, fmt.Errorf("选区不在画布内")
	}
	// Only this layer, composited on transparency: blend math over nothing
	// keeps the layer's own pixels.
	temp := cloneDocForEdit(sess.doc)
	temp.Layers = []domain.Layer{*layer}
	id := layer.ID
	temp.ActiveLayerID = &id
	full, err := render.Render(temp, sess.pixelSource())
	if err != nil {
		return nil, [2]float64{}, fmt.Errorf("拷贝像素失败: %w", err)
	}
	out := render.NewBitmap(region.w, region.h)
	for y := 0; y < region.h; y++ {
		dy := region.y + y
		for x := 0; x < region.w; x++ {
			dx := region.x + x
			coverage := 255
			if sel != nil {
				mx, my := dx-sel.X, dy-sel.Y
				if mx < 0 || my < 0 || mx >= sel.W || my >= sel.H {
					coverage = 0
				} else {
					coverage = int(sel.Mask[my*sel.W+mx])
				}
			}
			if coverage == 0 {
				continue
			}
			si := (dy*full.W + dx) * 4
			di := (y*region.w + x) * 4
			if coverage == 255 {
				copy(out.Pix[di:di+4], full.Pix[si:si+4])
				continue
			}
			for c := 0; c < 4; c++ {
				out.Pix[di+c] = uint8(int(full.Pix[si+c]) * coverage / 255)
			}
		}
	}
	return out, [2]float64{float64(region.x), float64(region.y)}, nil
}

// clearSelectionOnLayer erases the selected pixels from a layer bitmap
// (premultiplied channels scale by 1 − coverage, which is transparent inside
// the selection). Returns false when no pixel changed — an empty overlap is
// not an undo step.
func clearSelectionOnLayer(bmp *render.Bitmap, t domain.Transform, sel *filterSelection) bool {
	changed := false
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			if bmp.Pix[i+3] == 0 {
				continue // already transparent
			}
			dx, dy := layerIndexToDoc(t, bmp.W, bmp.H, float64(x)+0.5, float64(y)+0.5)
			mx := int(math.Floor(dx)) - sel.X
			my := int(math.Floor(dy)) - sel.Y
			if mx < 0 || my < 0 || mx >= sel.W || my >= sel.H {
				continue
			}
			coverage := int(sel.Mask[my*sel.W+mx])
			if coverage == 0 {
				continue
			}
			keep := 255 - coverage
			for c := 0; c < 4; c++ {
				bmp.Pix[i+c] = uint8(int(bmp.Pix[i+c]) * keep / 255)
			}
			changed = true
		}
	}
	return changed
}

// addPixelLayer lands pasted/copied pixels as a new layer above the active
// one — inside its folder when the active layer is a folder — exactly as the
// importing path does (SelectionClipboard.swift:251-265). Callers run inside
// an EditActive transaction, so the whole paste is one undo step and the new
// layer becomes active.
func addPixelLayer(sess *session, bmp *render.Bitmap, origin [2]float64) error {
	if bmp == nil || bmp.W <= 0 || bmp.H <= 0 {
		return fmt.Errorf("没有可粘贴的像素")
	}
	if len(sess.doc.Layers) >= domain.MaxLayers {
		return fmt.Errorf("图层数超过 %d 上限", domain.MaxLayers)
	}
	l := newPixelLayer(nextFreeName(sess.doc, "图层"), bmp.W, bmp.H)
	l.Transform.Origin = origin
	if parent := pixelParentForNewLayer(sess); parent != nil {
		l.ParentID = parent
	}
	sess.bitmaps[*l.ImageFile] = bmp
	sess.doc.Layers = append(sess.doc.Layers, l)
	id := l.ID
	sess.doc.ActiveLayerID = &id
	return nil
}

// pixelParentForNewLayer is the parent a pasted layer lands in: the active
// layer's folder when the active layer is a folder, otherwise the active
// layer's own parent (commitImportedLayers semantics).
func pixelParentForNewLayer(sess *session) *string {
	if sess.doc.ActiveLayerID == nil {
		return nil
	}
	idx := findLayerIndex(sess.doc, *sess.doc.ActiveLayerID)
	if idx < 0 {
		return nil
	}
	active := &sess.doc.Layers[idx]
	if active.IsGroupLayer() {
		id := active.ID
		return &id
	}
	return active.ParentID
}

// rasterChange is one pixel mutation for EditActiveRaster: the bitmaps the
// command replaced and what replaced them. nil means "no pixel changed", and
// then no history entry is forced.
type rasterChange struct {
	before map[string]*render.Bitmap
	after  map[string]*render.Bitmap
}

// EditActiveRaster runs one pixel-only command on the active session. Unlike
// EditActive the document usually does not change, so the history entry is
// forced (hist.EndForced) and the pixels are paired with the revision in the
// raster journal — undo and redo restore them, exactly as a filter commit's
// pixels are restored.
//
// fn must validate before it touches sess.bitmaps: an error rolls the document
// back but not the bitmap library (the same contract filter commits have).
func (w *Workspace) EditActiveRaster(name string, fn func(*session) (*rasterChange, error)) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	before := cloneDocForEdit(sess.doc)
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	change, err := fn(sess)
	if err != nil {
		sess.doc = before
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
		return "", err
	}
	if change == nil {
		// Nothing changed: closing the transaction records nothing (History.End
		// skips an unchanged document), so no undo step appears.
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
		return marshalEnvelope(sess.rev, sess.doc)
	}
	sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
	sess.journalRasters(sess.hist.Revision(), change.before, change.after)
	sess.rev++
	w.setTabDirtyLocked(w.active, true)
	return marshalEnvelope(sess.rev, sess.doc)
}

// copiedLayerFor finds the tab holding a whole-layer clipboard written against
// the current system clipboard sequence (ProjectWorkspace.pasteCopiedLayer,
// ProjectWorkspace.swift:193-207). Returns (nil, nil) when no tab does.
func (w *Workspace) copiedLayerFor(seq uint32) (*session, []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, sess := range w.sessions {
		if sess.clipLayer != nil && sess.clipLayer.seq == seq && len(sess.clipLayer.ids) > 0 {
			return sess, sess.clipLayer.ids
		}
	}
	return nil, nil
}

// activeSession returns the active tab's session without holding the lock.
func (w *Workspace) activeSession() *session {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.activeSessionLocked()
}
