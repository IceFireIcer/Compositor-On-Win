package bridge

// 选区命令（票 16/23/47）：填充前景/背景色、清除选区像素、把选区内的像素按
// 方向键搬运。语义真值是 reference/Swift/Compositor/Document/：
//
//	SelectionEdits.swift:53-72   fillSelection / clearSelectedPixels
//	SelectionEdits.swift:150-211 beginPixelMove / movePixels / nudgePixels
//	BrushStroke.swift:706-718    fill / clearPixels（render.FillColor 已移植）
//
// 两条命令都只动**活动像素图层**的位图，且都在 history 事务里：像素由栅格
// 日志撤销（EditActiveRaster 的那一套）。选区在前端，搬运后的选区随回复一起
// 回去，和原版写 document.selection 一样（SelectionEdits.swift:193）。
//
// 差异（诚实标注）：原版在画布空间抬起像素再透过图层变换落回，这里直接在图层
// 像素网格里做同一件事 —— 文档位移经 docToLayerIndex 换成图层网格位移，选区
// 覆盖度复用 filter.go 的 selectionLayerMask（与 clearSelectionOnLayer 同一套
// 映射）。整数文档位移 + 1:1 图层时逐位精确；缩放/旋转的图层走双线性重采样。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// FillMode names accepted by FillSelection — the menu commands
// (Menus:Fill with Foreground / Fill with Background) plus Delete's clear.
const (
	FillModeForeground = "foreground"
	FillModeBackground = "background"
	FillModeClear      = "clear"
)

// fillColorPayload is the palette color the frontend passes: straight sRGB
// components in 0–1 (the engine premultiplies into the 8-bit RGBA store).
type fillColorPayload struct {
	R float64  `json:"r"`
	G float64  `json:"g"`
	B float64  `json:"b"`
	A *float64 `json:"a"`
}

// FillSelection fills the active layer with a palette color inside the
// selection — the whole layer without one, nothing with an empty one — or
// clears it to transparency (mode "clear").
//
// selectionJSON is the frontend's document-space coverage payload
// (parseFilterSelection); "" means no selection. colorJSON is
// {"r":0..1,"g":0..1,"b":0..1,"a":0..1} and is ignored for "clear"; "" falls
// back to the original's palette defaults (foreground black, background
// white — BrushStroke.swift:13-15, EditorSession.swift:228).
//
// Returns the fresh document envelope; a request that changes no pixel
// records no history and leaves rev alone.
func (s *Service) FillSelection(selectionJSON string, mode string, colorJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	var fillMode render.FillMode
	switch mode {
	case FillModeForeground:
		fillMode = render.FillForeground
	case FillModeBackground:
		fillMode = render.FillBackground
	case FillModeClear:
		fillMode = render.FillClear
	default:
		return "", fmt.Errorf("不支持的填充模式 %q", mode)
	}
	color := render.Color{A: 1}
	if fillMode != render.FillClear {
		color, err = parseFillColor(colorJSON, mode)
		if err != nil {
			return "", err
		}
	}
	name := "填充"
	if fillMode == render.FillClear {
		name = "清除"
	}
	return s.ws.EditActiveRaster(name, func(sess *session) (*rasterChange, error) {
		layer, err := activePixelLayer(sess)
		if err != nil {
			return nil, err
		}
		bmp := sess.bitmaps[*layer.ImageFile]
		if bmp == nil {
			return nil, fmt.Errorf("内存位图库缺少资产 %s", *layer.ImageFile)
		}
		// nil mask = the whole layer (SelectionEdits.swift:51); a selection
		// that misses this layer rasterizes to all zeros, which changes nothing.
		var mask []uint8
		if sel != nil {
			mask = selectionLayerMask(sel, layer.Transform, bmp.W, bmp.H)
		}
		before := bmp.Clone()
		if err := render.FillColor(bmp, mask, bmp.W, bmp.H, color, fillMode); err != nil {
			return nil, fmt.Errorf("填充失败: %w", err)
		}
		if bytes.Equal(before.Pix, bmp.Pix) {
			return nil, nil // an empty (or already-clear) region is not an undo step
		}
		key := *layer.ImageFile
		return &rasterChange{
			before: map[string]*render.Bitmap{key: before},
			after:  map[string]*render.Bitmap{key: bmp},
		}, nil
	})
}

// parseFillColor decodes the palette color, clamping out-of-range components
// the way CGColor would.
func parseFillColor(colorJSON, mode string) (render.Color, error) {
	if strings.TrimSpace(colorJSON) == "" {
		if mode == FillModeBackground {
			return render.Color{R: 1, G: 1, B: 1, A: 1}, nil
		}
		return render.Color{A: 1}, nil
	}
	var p fillColorPayload
	if err := json.Unmarshal([]byte(colorJSON), &p); err != nil {
		return render.Color{}, fmt.Errorf("无法解析填充颜色: %w", err)
	}
	a := 1.0
	if p.A != nil {
		a = *p.A
	}
	for _, v := range []float64{p.R, p.G, p.B, a} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return render.Color{}, fmt.Errorf("填充颜色分量必须是有限数值")
		}
	}
	return render.Color{R: clampUnit(p.R), G: clampUnit(p.G), B: clampUnit(p.B), A: clampUnit(a)}, nil
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// selectionHasCoverage reports whether any pixel of a document-space
// selection is selected at all (Swift's !selection.isEmpty).
func selectionHasCoverage(sel *filterSelection) bool {
	for _, v := range sel.Mask {
		if v != 0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Moving selected pixels (Cmd-arrow / nudgePixels)

// movePixelsReply is the document envelope plus the moved selection: the
// original writes document.selection on commit (SelectionEdits.swift:193), and
// the selection lives in the frontend here, so it travels back with the
// snapshot. Embedding keeps it applyDocumentSnapshot-compatible.
type movePixelsReply struct {
	documentEnvelope
	Selection *filterSelection `json:"selection"`
}

// MoveSelectedPixels moves the selected pixels of the active layer by (dx, dy)
// document pixels and moves the selection with them, as one "移动像素" undo
// step (SelectionEdits.swift:206-211 nudgePixels). Without a selection, with
// an empty one, or with a selection that misses the layer's pixels it refuses
// with a Chinese error (the original beeps).
//
// The reply is {"rev","filterRev","doc","selection"}; selection is the moved
// coverage as a full-canvas document mask, clipped to the canvas, or null when
// there was none.
func (s *Service) MoveSelectedPixels(selectionJSON string, dx, dy int) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	sel, err := parseFilterSelection(selectionJSON)
	if err != nil {
		return "", err
	}
	if sel == nil {
		return "", fmt.Errorf("没有选区，无法移动像素")
	}
	if !selectionHasCoverage(sel) {
		return "", fmt.Errorf("选区为空，无法移动像素")
	}
	return s.ws.editActiveRasterSelection("移动像素", func(sess *session) (*rasterChange, *filterSelection, error) {
		layer, err := activePixelLayer(sess)
		if err != nil {
			return nil, nil, err
		}
		bmp := sess.bitmaps[*layer.ImageFile]
		if bmp == nil {
			return nil, nil, fmt.Errorf("内存位图库缺少资产 %s", *layer.ImageFile)
		}
		moved := translateSelection(sess.doc, sel, dx, dy)
		if dx == 0 && dy == 0 {
			return nil, moved, nil
		}
		before := bmp.Clone()
		changed, err := moveLayerSelection(bmp, layer.Transform, sel, dx, dy)
		if err != nil {
			return nil, nil, err
		}
		if !changed {
			return nil, nil, fmt.Errorf("选区内没有图层像素，无法移动")
		}
		key := *layer.ImageFile
		return &rasterChange{
			before: map[string]*render.Bitmap{key: before},
			after:  map[string]*render.Bitmap{key: bmp},
		}, moved, nil
	})
}

// translateSelection shifts a document-space selection by whole pixels and
// clips it to the canvas, returning a full-canvas coverage mask (the shape the
// frontend's currentSelection holds).
func translateSelection(doc *domain.Document, sel *filterSelection, dx, dy int) *filterSelection {
	out := &filterSelection{X: 0, Y: 0, W: doc.Width, H: doc.Height, Mask: make([]uint8, doc.Width*doc.Height)}
	if doc.Width < 1 || doc.Height < 1 {
		return out
	}
	for y := 0; y < sel.H; y++ {
		ty := y + sel.Y + dy
		if ty < 0 || ty >= doc.Height {
			continue
		}
		row := y * sel.W
		outRow := ty * doc.Width
		for x := 0; x < sel.W; x++ {
			cov := sel.Mask[row+x]
			if cov == 0 {
				continue
			}
			tx := x + sel.X + dx
			if tx < 0 || tx >= doc.Width {
				continue
			}
			out.Mask[outRow+tx] = cov
		}
	}
	return out
}

// moveLayerSelection cuts the selected pixels out of their old place and
// deposits them (dx, dy) document pixels away, in place. It mirrors
// BrushStroke.liftSelection + moveLifted: the clear is destination-out by the
// coverage, the deposit is source-over with the same coverage. Returns false
// when the selection does not touch the layer's pixel grid.
func moveLayerSelection(bmp *render.Bitmap, t domain.Transform, sel *filterSelection, dx, dy int) (bool, error) {
	if bmp == nil || bmp.W < 1 || bmp.H < 1 {
		return false, fmt.Errorf("图层没有像素，无法移动")
	}
	if t.Size[0] <= 0 || t.Size[1] <= 0 {
		return false, fmt.Errorf("图层变换无效，无法移动像素")
	}
	// The document offset as a layer-grid offset: the affine's translation.
	ax, ay := docToLayerIndex(t, bmp.W, bmp.H, 0, 0)
	bx, by := docToLayerIndex(t, bmp.W, bmp.H, float64(dx), float64(dy))
	dix, diy := bx-ax, by-ay

	mask := selectionLayerMask(sel, t, bmp.W, bmp.H)
	bounds := selectionLayerBounds(sel, t, bmp.W, bmp.H)
	padX := int(math.Ceil(math.Abs(dix))) + 2
	padY := int(math.Ceil(math.Abs(diy))) + 2
	x0 := clampInt(bounds[0]-padX, 0, bmp.W)
	y0 := clampInt(bounds[1]-padY, 0, bmp.H)
	x1 := clampInt(bounds[2]+padX, 0, bmp.W)
	y1 := clampInt(bounds[3]+padY, 0, bmp.H)
	if x1 <= x0 || y1 <= y0 {
		return false, nil
	}
	integral := math.Abs(dix-math.Round(dix)) < 1e-9 && math.Abs(diy-math.Round(diy)) < 1e-9

	// 1) Lift: destination-out by the coverage, exactly clearSelectionOnLayer's
	// arithmetic, restricted to the region the selection can reach.
	out := bmp.Clone()
	changed := false
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cov := mask[y*bmp.W+x]
			i := (y*bmp.W + x) * 4
			if cov == 0 || out.Pix[i+3] == 0 {
				continue // nothing selected, or already transparent
			}
			keep := 255 - int(cov)
			for c := 0; c < 4; c++ {
				out.Pix[i+c] = uint8(int(out.Pix[i+c]) * keep / 255)
			}
			changed = true
		}
	}

	// 2) Deposit: the lifted pixels (sampled from the ORIGINAL bitmap) land
	// offset by the layer-space delta, source-over with their coverage.
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			u := float64(x) - dix
			v := float64(y) - diy
			cov := sampleMask(mask, bmp.W, bmp.H, u, v, integral)
			if cov <= 0 {
				continue
			}
			var r, g, b, a float64
			if integral {
				r, g, b, a = pixelPremul(bmp, int(math.Round(u)), int(math.Round(v)))
			} else {
				r, g, b, a = samplePremulBilinear(bmp, u, v)
			}
			w := cov / 255
			inv := 1 - w
			i := (y*bmp.W + x) * 4
			// Only a deposit that actually lands a pixel counts as a change:
			// dragging an empty region must not leave an undo step behind.
			next := [4]uint8{
				roundByte(r*w + float64(out.Pix[i])*inv),
				roundByte(g*w + float64(out.Pix[i+1])*inv),
				roundByte(b*w + float64(out.Pix[i+2])*inv),
				roundByte(a*w + float64(out.Pix[i+3])*inv),
			}
			for c := 0; c < 4; c++ {
				if next[c] != out.Pix[i+c] {
					changed = true
				}
				out.Pix[i+c] = next[c]
			}
		}
	}
	if !changed {
		return false, nil
	}
	copy(bmp.Pix, out.Pix)
	return true, nil
}

// sampleMask reads a layer-space coverage mask at a (possibly fractional)
// layer-grid position: nearest when the move lands on whole layer pixels (the
// exact 1:1 case), bilinear otherwise.
func sampleMask(mask []uint8, w, h int, u, v float64, integral bool) float64 {
	at := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= w || y >= h {
			return 0
		}
		return float64(mask[y*w+x])
	}
	if integral {
		return at(int(math.Round(u)), int(math.Round(v)))
	}
	x0 := int(math.Floor(u - 0.5))
	y0 := int(math.Floor(v - 0.5))
	fx := (u - 0.5) - float64(x0)
	fy := (v - 0.5) - float64(y0)
	top := at(x0, y0) + (at(x0+1, y0)-at(x0, y0))*fx
	bottom := at(x0, y0+1) + (at(x0+1, y0+1)-at(x0, y0+1))*fx
	return top + (bottom-top)*fy
}

// pixelPremul reads one premultiplied pixel, transparent outside the grid.
func pixelPremul(bmp *render.Bitmap, x, y int) (float64, float64, float64, float64) {
	if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
		return 0, 0, 0, 0
	}
	i := (y*bmp.W + x) * 4
	return float64(bmp.Pix[i]), float64(bmp.Pix[i+1]), float64(bmp.Pix[i+2]), float64(bmp.Pix[i+3])
}

// samplePremulBilinear samples the premultiplied bitmap at a fractional
// pixel-index coordinate (0,0 = the first pixel's centre), clamping at the
// edges.
func samplePremulBilinear(bmp *render.Bitmap, u, v float64) (float64, float64, float64, float64) {
	x0 := int(math.Floor(u))
	y0 := int(math.Floor(v))
	fx := u - float64(x0)
	fy := v - float64(y0)
	at := func(x, y int) [4]float64 {
		if x < 0 {
			x = 0
		} else if x >= bmp.W {
			x = bmp.W - 1
		}
		if y < 0 {
			y = 0
		} else if y >= bmp.H {
			y = bmp.H - 1
		}
		i := (y*bmp.W + x) * 4
		return [4]float64{float64(bmp.Pix[i]), float64(bmp.Pix[i+1]), float64(bmp.Pix[i+2]), float64(bmp.Pix[i+3])}
	}
	p00, p10 := at(x0, y0), at(x0+1, y0)
	p01, p11 := at(x0, y0+1), at(x0+1, y0+1)
	var out [4]float64
	for c := 0; c < 4; c++ {
		top := p00[c] + (p10[c]-p00[c])*fx
		bottom := p01[c] + (p11[c]-p01[c])*fx
		out[c] = top + (bottom-top)*fy
	}
	return out[0], out[1], out[2], out[3]
}

// roundByte rounds a 0–255 float to a byte, clamping.
func roundByte(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return uint8(math.Round(v))
	}
}

// editActiveRasterSelection mirrors EditActiveRaster for a command that also
// moves the selection: the same history transaction and raster journal, plus
// the moved selection in the reply. fn must validate before touching
// sess.bitmaps (an error rolls the document back but not the bitmap library,
// exactly like EditActiveRaster).
func (w *Workspace) editActiveRasterSelection(name string, fn func(*session) (*rasterChange, *filterSelection, error)) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	before := cloneDocForEdit(sess.doc)
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	change, selection, err := fn(sess)
	if err != nil {
		sess.doc = before
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
		return "", err
	}
	if change == nil {
		// Nothing changed: closing the transaction records nothing (History.End
		// skips an unchanged document), so no undo step appears.
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	} else {
		sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
		sess.journalRasters(sess.hist.Revision(), change.before, change.after)
		sess.rev++
		w.setTabDirtyLocked(w.active, true)
	}
	filterRev := 0
	if sess.filter != nil {
		filterRev = sess.filter.previewRev
	}
	b, err := json.Marshal(movePixelsReply{
		documentEnvelope: documentEnvelope{Rev: sess.rev, FilterRev: filterRev, Doc: sess.doc},
		Selection:        selection,
	})
	if err != nil {
		return "", fmt.Errorf("无法编码文档快照: %w", err)
	}
	return string(b), nil
}
