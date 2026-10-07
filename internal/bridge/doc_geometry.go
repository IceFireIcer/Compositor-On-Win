package bridge

// Document-level geometry endpoints (ticket 43): Canvas Size (⌥⌘C, nine-cell
// anchor, optional Canvas Extension fill layer), Image Size (⌥⌘I, resample
// with quality choice) and Trim (auto-crop by transparency or pixel colour).
// All three run inside one history transaction and land in the manifest.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// canvasSizeRequest is CanvasSize's payload (CanvasSizeOptions + the dialog
// fields).
type canvasSizeRequest struct {
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Anchor  int      `json:"anchor"` // 0–8, row-major; 4 is centre
	FillHex string   `json:"fill"`
	OffsetX *float64 `json:"offsetX,omitempty"` // explicit content offset (trim)
	OffsetY *float64 `json:"offsetY,omitempty"`
}

// imageSizeRequest is ImageSize's payload (ImageSizeOptions).
type imageSizeRequest struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Resolution int    `json:"resolution"`
	Sampling   string `json:"sampling"` // "Nearest" | "Smooth" | "High quality"
}

// trimRequest is Trim's payload (TrimOptions).
type trimRequest struct {
	BasedOn   string `json:"basedOn"` // "transparent" | "topLeft" | "bottomRight"
	Top       bool   `json:"top"`
	Bottom    bool   `json:"bottom"`
	Left      bool   `json:"left"`
	Right     bool   `json:"right"`
	Tolerance int    `json:"tolerance"`
}

// CanvasSize resizes the canvas: every layer and guide translates by the
// anchor offset, and an expanded canvas may receive a coloured Canvas
// Extension layer under everything.
func (s *Service) CanvasSize(payload string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	var req canvasSizeRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", fmt.Errorf("无法解析画布尺寸参数: %w", err)
	}
	return s.ws.EditActive("画布大小", func(sess *session) error {
		return applyCanvasSize(sess, req)
	})
}

// applyCanvasSize is the CanvasResizer port: validate, translate, expand.
func applyCanvasSize(sess *session, req canvasSizeRequest) error {
	old := sess.doc
	if req.Width < 1 || req.Height < 1 || req.Width > domain.MaxSide || req.Height > domain.MaxSide {
		return fmt.Errorf("画布尺寸超出 %d 像素边长限制", domain.MaxSide)
	}
	if req.Anchor < 0 || req.Anchor > 8 {
		return fmt.Errorf("锚点非法: %d", req.Anchor)
	}
	offsetX, offsetY := 0.0, 0.0
	if req.OffsetX != nil && req.OffsetY != nil {
		offsetX, offsetY = *req.OffsetX, *req.OffsetY
	} else {
		offsetX, offsetY = render.AnchorOffset(req.Width, req.Height, old.Width, old.Height, render.Anchor(req.Anchor))
	}
	if offsetX == 0 && offsetY == 0 && req.Width == old.Width && req.Height == old.Height {
		return nil // nothing to do
	}
	// Translate every layer (and its unlinked mask placement) and guide.
	for i := range old.Layers {
		l := &old.Layers[i]
		l.Transform.Origin[0] += offsetX
		l.Transform.Origin[1] += offsetY
		if !transformFinite(l.Transform) {
			return fmt.Errorf("图层变换超出范围")
		}
		if l.MaskPlacement != nil {
			l.MaskPlacement.Origin[0] += offsetX
			l.MaskPlacement.Origin[1] += offsetY
		}
	}
	if guides := old.Guides(); guides != nil {
		for i := range guides {
			guides[i].Position += guideShift(guides[i], offsetX, offsetY)
		}
		old.GuidesList = &guides
	}
	// Expanding with a fill colour: a Canvas Extension layer under everything.
	if req.FillHex != "" && (req.Width > old.Width || req.Height > old.Height) {
		r, g, b, err := parseFillHex(req.FillHex)
		if err != nil {
			return err
		}
		ceiling := domain.DocumentPixelBudget()
		used := 0
		for _, bmp := range sess.bitmaps {
			if bmp != nil {
				used += bmp.W * bmp.H
			}
		}
		if req.Width*req.Height > ceiling-used {
			return fmt.Errorf("扩展超出文档像素预算")
		}
		if len(old.Layers) >= 10_000 {
			return fmt.Errorf("图层数量超出上限")
		}
		bmp := render.CanvasExtension(req.Width, req.Height, old.Width, old.Height, offsetX, offsetY, r, g, b)
		l := newPixelLayer("画布扩展", req.Width, req.Height)
		active := l.ID
		old.Layers = append([]domain.Layer{l}, old.Layers...) // bottom-most
		sess.bitmaps[*l.ImageFile] = bmp
		old.ActiveLayerID = &active
	}
	old.Width, old.Height = req.Width, req.Height
	return nil
}

// ImageSize resamples the whole document: every layer re-rasterizes into its
// scaled box with the chosen quality (rotation and flips bake into the
// pixels), masks follow, and the resolution lands in the manifest.
func (s *Service) ImageSize(payload string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	var req imageSizeRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", fmt.Errorf("无法解析图像尺寸参数: %w", err)
	}
	return s.ws.EditActive("图像大小", func(sess *session) error {
		return applyImageSize(sess, req)
	})
}

func applyImageSize(sess *session, req imageSizeRequest) error {
	old := sess.doc
	if req.Width < 1 || req.Height < 1 || req.Width > domain.MaxSide || req.Height > domain.MaxSide {
		return fmt.Errorf("图像尺寸超出 %d 像素边长限制", domain.MaxSide)
	}
	if req.Width*req.Height > domain.MaxSurfacePixels {
		return fmt.Errorf("图像尺寸超出单表面 %.0f 百万像素上限", float64(domain.MaxSurfacePixels)/1e6)
	}
	if req.Resolution < domain.MinResolution || req.Resolution > domain.MaxResolution {
		return fmt.Errorf("分辨率超出 %d–%d ppi", domain.MinResolution, domain.MaxResolution)
	}
	sampling := domain.Sampling(req.Sampling)
	switch sampling {
	case domain.SamplingNearest, domain.SamplingSmooth, domain.SamplingHighQuality:
	default:
		sampling = domain.SamplingHighQuality
	}
	res := req.Resolution
	old.Resolution = &res
	if req.Width == old.Width && req.Height == old.Height {
		return nil
	}
	sx := float64(req.Width) / float64(old.Width)
	sy := float64(req.Height) / float64(old.Height)
	// Guides scale with the canvas.
	if guides := old.Guides(); guides != nil {
		for i := range guides {
			if guides[i].Axis == domain.GuideAxisHorizontal {
				guides[i].Position *= sy
			} else {
				guides[i].Position *= sx
			}
		}
		old.GuidesList = &guides
	}
	budget := domain.DocumentPixelBudget()
	usedImages, usedMasks := 0, 0
	for i := range old.Layers {
		l := &old.Layers[i]
		if l.ImageFile == nil {
			l.Transform = scaledTransform(l.Transform, sx, sy)
			continue
		}
		src := sess.bitmaps[*l.ImageFile]
		if src == nil {
			return fmt.Errorf("图层 %s 缺少位图资产", l.ID)
		}
		left, top, w, h := render.LayerBox(l.Transform, sx, sy)
		if w > domain.MaxSide || h > domain.MaxSide || w*h > budget-usedImages {
			return fmt.Errorf("图层 %s 缩放后超出文档像素预算", l.Name)
		}
		usedImages += w * h
		origTransform := l.Transform // masks re-rasterize through the same mapping
		dst := render.NewBitmap(w, h)
		render.DrawTransformed(src, origTransform, sx, sy, dst, left, top, sampling)
		// The pixels now carry rotation/flips; the transform is plain.
		l.Transform = domain.Transform{
			Origin:   [2]float64{left, top},
			Size:     [2]float64{float64(w), float64(h)},
			Sampling: sampling,
		}
		if !transformFinite(l.Transform) {
			return fmt.Errorf("图层变换超出范围")
		}
		newName := newUUID() + ".png"
		if err := renameAsset(sess, *l.ImageFile, newName, dst); err != nil {
			return err
		}
		l.ImageFile = &newName
		// Masks: uniform 1×1 and unlinked placements are resolution
		// independent; linked masks re-rasterize onto the new layer grid
		// through the original transform (ImageResizer's drawCoverage leg).
		if l.MaskFile != nil && l.MaskPlacement == nil {
			if gray, mw, mh, ok := maskGrid(sess, *l.MaskFile); ok && !(mw == 1 && mh == 1) {
				if w*h > budget-usedMasks {
					return fmt.Errorf("图层 %s 的蒙版超出文档像素预算", l.Name)
				}
				usedMasks += w * h
				mask := render.DrawMaskTransformed(gray, mw, mh, origTransform, sx, sy, w, h, left, top, sampling)
				maskName := newUUID() + ".mask.png"
				if err := renameMaskAsset(sess, *l.MaskFile, maskName, mask, w, h); err != nil {
					return err
				}
				l.MaskFile = &maskName
			}
		}
	}
	old.Width, old.Height = req.Width, req.Height
	return nil
}

// Trim crops the canvas to the rendered content's bounding box (ImageTrim):
// the composite raster decides the rect, then a Canvas Size with an explicit
// content offset applies it.
func (s *Service) Trim(payload string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	var req trimRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", fmt.Errorf("无法解析修剪参数: %w", err)
	}
	return s.ws.EditActive("修剪", func(sess *session) error {
		raster, err := render.Render(sess.doc, sess.pixelSource())
		if err != nil {
			return err
		}
		options := render.TrimOptions{
			Top: req.Top, Bottom: req.Bottom, Left: req.Left, Right: req.Right,
			Tolerance: uint8(clampInt(req.Tolerance, 0, 255)),
		}
		switch req.BasedOn {
		case "topLeft":
			options.BasedOn = render.TrimTopLeftPixelColor
		case "bottomRight":
			options.BasedOn = render.TrimBottomRightPixelColor
		default:
			options.BasedOn = render.TrimTransparentPixels
		}
		x, y, w, h, ok := render.TrimRect(raster, options)
		if !ok {
			return fmt.Errorf("修剪后没有剩余内容")
		}
		if w == sess.doc.Width && h == sess.doc.Height && x == 0 && y == 0 {
			return nil
		}
		return applyCanvasSize(sess, canvasSizeRequest{
			Width: w, Height: h,
			OffsetX: ptrFloat(-float64(x)), OffsetY: ptrFloat(-float64(y)),
		})
	})
}

// scaledTransform maps a transform through the document scale without
// touching pixels (groups and adjustment layers have no asset).
func scaledTransform(t domain.Transform, sx, sy float64) domain.Transform {
	t.Origin[0] *= sx
	t.Origin[1] *= sy
	t.Size[0] *= sx
	t.Size[1] *= sy
	return t
}

func transformFinite(t domain.Transform) bool {
	for _, v := range []float64{t.Origin[0], t.Origin[1], t.Size[0], t.Size[1], t.Rotation} {
		if v != v || v > 1e15 || v < -1e15 {
			return false
		}
	}
	return true
}

// guideShift is the translation one guide takes under a canvas-size offset.
func guideShift(g domain.Guide, offsetX, offsetY float64) float64 {
	if g.Axis == domain.GuideAxisHorizontal {
		return offsetY
	}
	return offsetX
}

func parseFillHex(hex string) (uint8, uint8, uint8, error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("填充色应为 #rrggbb")
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("填充色应为 #rrggbb")
	}
	return uint8(value >> 16), uint8(value >> 8), uint8(value), nil
}

// renameAsset stores the resampled bitmap under a fresh name and drops the
// old one, so the .comp round-trip writes exactly the live assets.
func renameAsset(sess *session, oldName, newName string, bmp *render.Bitmap) error {
	if _, ok := sess.bitmaps[oldName]; !ok {
		return fmt.Errorf("缺少位图资产 %s", oldName)
	}
	delete(sess.bitmaps, oldName)
	sess.bitmaps[newName] = bmp
	return nil
}

// maskGrid returns a mask asset as a gray plane (brightness in red, the
// .comp convention).
func maskGrid(sess *session, name string) ([]uint8, int, int, bool) {
	bmp, ok := sess.bitmaps[name]
	if !ok || bmp == nil {
		return nil, 0, 0, false
	}
	gray := make([]uint8, bmp.W*bmp.H)
	for i := range gray {
		gray[i] = bmp.Pix[i*4]
	}
	return gray, bmp.W, bmp.H, true
}

// renameMaskAsset stores a resampled mask as gray-in-RGBA.
func renameMaskAsset(sess *session, oldName, newName string, gray []uint8, w, h int) error {
	if _, ok := sess.bitmaps[oldName]; !ok {
		return fmt.Errorf("缺少蒙版资产 %s", oldName)
	}
	delete(sess.bitmaps, oldName)
	out := render.NewBitmap(w, h)
	for i := 0; i < w*h && i < len(gray); i++ {
		out.Pix[i*4] = gray[i]
		out.Pix[i*4+1] = gray[i]
		out.Pix[i*4+2] = gray[i]
		out.Pix[i*4+3] = 255
	}
	sess.bitmaps[newName] = out
	return nil
}

func ptrFloat(v float64) *float64 { return &v }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
