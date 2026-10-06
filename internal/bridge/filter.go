package bridge

// Filter application — the Go port of Filters.swift's FilterJob /
// PixelFilter.run plus the FilterEdit lifecycle: one-shot destructive
// application (image menu, filter menu OK) and the live-preview session
// (≤2048px preview source, background worker with cancel/replace, full-size
// commit). Kernels live in internal/render; this file owns the layer
// plumbing: growing the grid for blur spread, trimming back to alpha bounds
// with the transform adjustment, blending through the selection, the raster
// undo journal, and the preview worker.

import (
	"encoding/json"
	"fmt"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// Filter kind names (Filters.swift FilterKind raw values plus the
// image-menu adjustments this pipeline also carries).
const (
	FilterGaussian         = "gaussianBlur"
	FilterMotion           = "motionBlur"
	FilterAddNoise         = "addNoise"
	FilterVignette         = "vignette"
	FilterBloom            = "bloomGlow"
	FilterTonal            = "tonalContrast"
	FilterLens             = "lensCorrection"
	FilterCameraRaw        = "cameraRaw"
	FilterInvert           = "invert"
	FilterInvertMask       = "invertMask"
	FilterContentFill      = "contentAwareFill"
	FilterDither           = "dither"
	FilterRemoveBackground = "removeBackground"
	FilterAdjustPrefix     = "adjust:"
)

// filterNames maps kinds to their history entry names (undo menu labels).
var filterNames = map[string]string{
	FilterGaussian:         "高斯模糊",
	FilterMotion:           "运动模糊",
	FilterAddNoise:         "添加杂色",
	FilterVignette:         "晕影",
	FilterBloom:            "辉光",
	FilterTonal:            "色调对比",
	FilterLens:             "镜头校正",
	FilterCameraRaw:        "Camera Raw",
	FilterInvert:           "反相",
	FilterInvertMask:       "反相蒙版",
	FilterContentFill:      "内容感知填充",
	FilterDither:           "抖动",
	FilterRemoveBackground: "移除背景",
}

// filterParams is the union of every filter's sliders; each kind reads its
// own and normalization clamps with the Filters.swift defaults.
type filterParams struct {
	Radius   float64 `json:"radius"`   // gaussian blur σ, 0.1–250
	Angle    float64 `json:"angle"`    // motion blur direction, −90–90
	Distance float64 `json:"distance"` // motion blur streak, 1–2000

	Amount        float64 `json:"amount"`        // add noise strength 0.1–400
	NoiseGaussian bool    `json:"gaussian"`      // noise distribution
	Monochromatic bool    `json:"monochromatic"` // noise brightness-only

	VignetteAmount     float64    `json:"vignetteAmount"`   // 0–100
	VignetteColor      [3]float64 `json:"vignetteColor"`    // 0–1 each
	VignetteMidpoint   float64    `json:"vignetteMidpoint"` // 0–100
	VignetteRoundness  float64    `json:"vignetteRoundness"`
	VignetteFeather    float64    `json:"vignetteFeather"`
	VignetteHighlights float64    `json:"vignetteHighlights"`

	BloomAmount float64 `json:"bloomAmount"` // 0–100
	BloomRadius float64 `json:"bloomRadius"` // 1–150

	TonalAmount     float64 `json:"tonalAmount"`     // 0–100
	TonalRadius     float64 `json:"tonalRadius"`     // 1–100
	TonalShadows    float64 `json:"tonalShadows"`    // −100–100
	TonalMidtones   float64 `json:"tonalMidtones"`   // −100–100
	TonalHighlights float64 `json:"tonalHighlights"` // −100–100

	Distortion float64 `json:"distortion"` // lens correction, −100–100

	Dither render.DitherSettings `json:"dither"` // dither filter
	// Remove Background (SubjectRemoval): Basic keeps the raw model mask;
	// Advanced runs the refine chain.
	BackgroundQuality string                   `json:"backgroundQuality"`
	RefineEdges       float64                  `json:"refineEdges"`
	MatteContrast     float64                  `json:"matteContrast"`
	ShiftEdge         float64                  `json:"shiftEdge"`
	CameraRaw         render.CameraRawSettings `json:"cameraRaw"` // Camera Raw filter
	// CameraRawShows carries the panel's per-group eyes (nil = all show);
	// the grade skips hidden groups but the panel keeps its slider values.
	CameraRawShows map[string]bool `json:"cameraRawShows,omitempty"`
	// CameraRawClipping is the Option-drag preview view (1 highlights,
	// 2 shadows); committing forces it back to 0.
	CameraRawClipping    *int               `json:"cameraRawClipping,omitempty"`
	CameraRawSharpenMask bool               `json:"cameraRawSharpenMask,omitempty"`
	Adjustment           *domain.Adjustment `json:"adjustment"` // image-menu adjustments
}

func clampF(v, lo, hi, fb float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fb
	}
	return math.Min(hi, math.Max(lo, v))
}

func (p *filterParams) normalized(kind string) filterParams {
	r := *p
	switch kind {
	case FilterGaussian:
		r.Radius = clampF(p.Radius, 0.1, 250, 1)
	case FilterMotion:
		r.Angle = clampF(p.Angle, -90, 90, 0)
		r.Distance = clampF(p.Distance, 1, 2000, 10)
	case FilterAddNoise:
		r.Amount = clampF(p.Amount, 0.1, 400, 10)
	case FilterVignette:
		r.VignetteAmount = clampF(p.VignetteAmount, 0, 100, 35)
		for i := range r.VignetteColor {
			r.VignetteColor[i] = clampF(p.VignetteColor[i], 0, 1, 0)
		}
		r.VignetteMidpoint = clampF(p.VignetteMidpoint, 0, 100, 50)
		r.VignetteRoundness = clampF(p.VignetteRoundness, -100, 100, 100)
		r.VignetteFeather = clampF(p.VignetteFeather, 0, 100, 60)
		r.VignetteHighlights = clampF(p.VignetteHighlights, 0, 100, 25)
	case FilterBloom:
		r.BloomAmount = clampF(p.BloomAmount, 0, 100, 40)
		r.BloomRadius = clampF(p.BloomRadius, 1, 150, 24)
	case FilterTonal:
		r.TonalAmount = clampF(p.TonalAmount, 0, 100, 50)
		r.TonalRadius = clampF(p.TonalRadius, 1, 100, 16)
		r.TonalShadows = clampF(p.TonalShadows, -100, 100, 40)
		r.TonalMidtones = clampF(p.TonalMidtones, -100, 100, 60)
		r.TonalHighlights = clampF(p.TonalHighlights, -100, 100, 30)
	case FilterLens:
		r.Distortion = clampF(p.Distortion, -100, 100, 0)
	case FilterCameraRaw:
		r.CameraRaw = p.CameraRaw.Normalized()
	}
	return r
}

// blurMargin mirrors Filters.swift: about three standard deviations of
// spread (or half a streak) in layer pixels.
func blurMargin(kind string, p *filterParams) int {
	var margin float64
	switch kind {
	case FilterGaussian:
		margin = p.Radius*3 + 2
	case FilterMotion:
		margin = p.Distance/2 + 2
	case FilterBloom:
		margin = p.BloomRadius*3 + 2
	default:
		return 0
	}
	return int(math.Ceil(margin))
}

// filterSelection carries the active selection in document pixels; Mask is
// the W×H gray coverage (Wails marshals []byte as base64).
type filterSelection struct {
	X    int     `json:"x"`
	Y    int     `json:"y"`
	W    int     `json:"w"`
	H    int     `json:"h"`
	Mask []uint8 `json:"mask"`
}

// parseFilterSelection decodes the optional selection payload.
func parseFilterSelection(raw string) (*filterSelection, error) {
	if raw == "" {
		return nil, nil
	}
	var sel filterSelection
	if err := json.Unmarshal([]byte(raw), &sel); err != nil {
		return nil, fmt.Errorf("无法解析选区: %w", err)
	}
	if sel.W <= 0 || sel.H <= 0 || len(sel.Mask) != sel.W*sel.H {
		return nil, fmt.Errorf("选区蒙版尺寸不符")
	}
	return &sel, nil
}

// ---------------------------------------------------------------------------
// Transform mapping (the affine behind render.Place)
// ---------------------------------------------------------------------------

// layerIndexToDoc maps a layer-pixel INDEX coordinate to document pixels
// through the transform (the affine the Swift pixelToDocument builds).
func layerIndexToDoc(t domain.Transform, gridW, gridH int, ix, iy float64) (float64, float64) {
	ux := ix / float64(gridW)
	uy := iy / float64(gridH)
	if t.FlipX {
		ux = 1 - ux
	}
	if t.FlipY {
		uy = 1 - uy
	}
	vx := (ux - 0.5) * t.Size[0]
	vy := (uy - 0.5) * t.Size[1]
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	return cx + c*vx - s*vy, cy + s*vx + c*vy
}

// docToLayerIndex inverts layerIndexToDoc for one point.
func docToLayerIndex(t domain.Transform, gridW, gridH int, dx, dy float64) (float64, float64) {
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	c, s := math.Cos(t.Rotation*math.Pi/180), math.Sin(t.Rotation*math.Pi/180)
	dx0, dy0 := dx-cx, dy-cy
	vx := c*dx0 + s*dy0
	vy := -s*dx0 + c*dy0
	ux := vx/t.Size[0] + 0.5
	uy := vy/t.Size[1] + 0.5
	if t.FlipX {
		ux = 1 - ux
	}
	if t.FlipY {
		uy = 1 - uy
	}
	return ux * float64(gridW), uy * float64(gridH)
}

// expandTransform mirrors FilterEdit.grow's transform math: the padded grid
// scales the size and re-centers the origin so the placed rect covers the
// padded area.
func expandTransform(t domain.Transform, oldW, oldH, newW, newH int) domain.Transform {
	out := t
	out.Size[0] = t.Size[0] * float64(newW) / float64(oldW)
	out.Size[1] = t.Size[1] * float64(newH) / float64(oldH)
	// The grid grew by the same margin on every side, so the OLD grid's
	// center sits at the NEW grid's center; map it through the old placement.
	cx, cy := layerIndexToDoc(t, oldW, oldH, float64(oldW)/2, float64(oldH)/2)
	out.Origin[0] = cx - out.Size[0]/2
	out.Origin[1] = cy - out.Size[1]/2
	return out
}

// trimTransform mirrors PixelFilter.trimmed: the cropped grid shrinks the
// size and moves the origin so the placed rect covers the crop.
func trimTransform(t domain.Transform, oldW, oldH int, bounds [4]int) domain.Transform {
	out := t
	cropW, cropH := bounds[2]-bounds[0], bounds[3]-bounds[1]
	out.Size[0] = t.Size[0] * float64(cropW) / float64(oldW)
	out.Size[1] = t.Size[1] * float64(cropH) / float64(oldH)
	mx, my := layerIndexToDoc(t, oldW, oldH, float64(bounds[0]+bounds[2])/2, float64(bounds[1]+bounds[3])/2)
	out.Origin[0] = mx - out.Size[0]/2
	out.Origin[1] = my - out.Size[1]/2
	return out
}

// padBitmap copies src into a grid grown by margin transparent pixels on
// every side.
func padBitmap(src *render.Bitmap, margin int) *render.Bitmap {
	if margin <= 0 {
		return src.Clone()
	}
	out := render.NewBitmap(src.W+2*margin, src.H+2*margin)
	for y := 0; y < src.H; y++ {
		copy(out.Pix[((y+margin)*out.W+margin)*4:], src.Pix[y*src.W*4:(y*src.W+src.W)*4])
	}
	return out
}

// cropBitmap cuts bounds (left, top, right, bottom exclusive) out of src.
func cropBitmap(src *render.Bitmap, bounds [4]int) *render.Bitmap {
	w, h := bounds[2]-bounds[0], bounds[3]-bounds[1]
	out := render.NewBitmap(w, h)
	for y := 0; y < h; y++ {
		copy(out.Pix[y*w*4:(y*w+w)*4], src.Pix[((y+bounds[1])*src.W+bounds[0])*4:((y+bounds[1])*src.W+bounds[2])*4])
	}
	return out
}

// ---------------------------------------------------------------------------
// PixelFilter.run — one kernel application on a working grid
// ---------------------------------------------------------------------------

// vignetteFrame is the rect the vignette shapes to, in the working grid's
// pixels, plus the fillsClear switch (canvas frame on an empty layer).
type vignetteFrame struct {
	x, y, w, h float64
	fillsClear bool
}

// vignetteFrameFor resolves the frame: the working grid itself for a layer
// with pixels, or the canvas mapped into layer pixels for an empty one.
func vignetteFrameFor(work *render.Bitmap, workT domain.Transform, docW, docH int, emptyLayer bool) vignetteFrame {
	if !emptyLayer {
		return vignetteFrame{x: 0, y: 0, w: float64(work.W), h: float64(work.H), fillsClear: false}
	}
	// Canvas rect corners mapped into layer index space; bounding box
	// (CGRect.applying semantics).
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{0, 0}, {float64(docW), 0}, {0, float64(docH)}, {float64(docW), float64(docH)}} {
		ix, iy := docToLayerIndex(workT, work.W, work.H, c[0], c[1])
		minX, minY = math.Min(minX, ix), math.Min(minY, iy)
		maxX, maxY = math.Max(maxX, ix), math.Max(maxY, iy)
	}
	return vignetteFrame{x: minX, y: minY, w: maxX - minX, h: maxY - minY, fillsClear: true}
}

// runFilterJob applies one filter kind to work in place at the given scale
// (PixelFilter.run). blur kernels receive the scaled slider so a
// downscaled preview blurs proportionally less.
func runFilterJob(kind string, work *render.Bitmap, p *filterParams, scale float64, seed uint32,
	docW, docH int, workT domain.Transform, emptyLayer bool, fillMask []uint8, subjectRaw *render.Bitmap) {
	switch kind {
	case FilterGaussian:
		render.ApplyGaussianBlur(work, p.Radius*scale)
	case FilterMotion:
		// CIMotionBlur's radius per streak pixel: an even streak of length d
		// spreads d/√12, so this radius matches its spread.
		render.ApplyMotionBlur(work, p.Angle, p.Distance*scale*render.MotionRadiusPerPixel)
	case FilterBloom:
		render.ApplyBloom(work, p.BloomRadius*scale, p.BloomAmount/50)
	case FilterAddNoise:
		// Noise previews at full size (scale 1); the raw Photoshop percentage.
		render.ApplyAddNoise(work, float32(p.Amount), p.NoiseGaussian, p.Monochromatic, seed)
	case FilterVignette:
		f := vignetteFrameFor(work, workT, docW, docH, emptyLayer)
		render.ApplyColoredVignette(work, f.x, f.y, f.w, f.h, f.fillsClear,
			p.VignetteAmount, p.VignetteMidpoint, p.VignetteRoundness, p.VignetteFeather,
			p.VignetteHighlights, p.VignetteColor[0], p.VignetteColor[1], p.VignetteColor[2])
	case FilterTonal:
		base := work.Clone()
		render.ApplyGaussianBlur(base, p.TonalRadius*scale)
		render.ApplyTonalContrast(work, base, p.TonalAmount, p.TonalShadows, p.TonalMidtones, p.TonalHighlights)
	case FilterLens:
		// The warp is relative to the image's own size, so a downscaled
		// preview bends the same way.
		src := work.Clone()
		render.LensDistort(src, work, p.Distortion/100*0.35)
	case FilterCameraRaw:
		clipping := 0
		if p.CameraRawClipping != nil {
			clipping = *p.CameraRawClipping
		}
		grade := p.CameraRaw.ApplyingGroups(p.CameraRawShows)
		*work = *render.ApplyCameraRawFilter(work, grade, render.CameraRawOptions{
			Scale: scale, Seed: seed, Clipping: clipping, SharpenMask: p.CameraRawSharpenMask,
		})
	case FilterInvert:
		render.ApplyInvert(work)
	case FilterContentFill:
		if fillMask != nil {
			render.ContentFill(work, fillMask)
		}
	case FilterDither:
		render.ApplyDitherFilter(work, p.Dither)
	case FilterRemoveBackground:
		if subjectRaw != nil {
			mask := render.RefineSubjectMask(subjectRaw, work, p.RefineEdges, p.ShiftEdge,
				p.MatteContrast, float64(max(work.W, work.H)))
			if p.BackgroundQuality != "advanced" {
				mask = subjectRaw
			}
			render.ApplySubjectMask(work, mask)
		}
	default:
		if _, ok := adjustmentKind(kind); ok && p.Adjustment != nil {
			render.ApplyAdjustment(p.Adjustment, work)
		}
	}
}

// adjustmentKind extracts "adjust:<AdjustmentKind>" payloads.
func adjustmentKind(kind string) (domain.AdjustmentKind, bool) {
	if len(kind) <= len(FilterAdjustPrefix) || kind[:len(FilterAdjustPrefix)] != FilterAdjustPrefix {
		return "", false
	}
	return domain.AdjustmentKind(kind[len(FilterAdjustPrefix):]), true
}

// selectionLayerMask rasterizes the doc-space selection into a layer-space
// gray coverage (the mask content_fill fills through).
func selectionLayerMask(sel *filterSelection, workT domain.Transform, w, h int) []uint8 {
	mask := make([]uint8, w*h)
	if sel == nil {
		return mask
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := layerIndexToDoc(workT, w, h, float64(x)+0.5, float64(y)+0.5)
			mx := int(math.Floor(dx)) - sel.X
			my := int(math.Floor(dy)) - sel.Y
			if mx < 0 || my < 0 || mx >= sel.W || my >= sel.H {
				continue
			}
			mask[y*w+x] = sel.Mask[my*sel.W+mx]
		}
	}
	return mask
}

// selectionLayerBounds maps the selection rect into layer pixel space
// (bounding box of the mapped corners, exclusive max).
func selectionLayerBounds(sel *filterSelection, workT domain.Transform, w, h int) [4]int {
	corners := [][2]float64{
		{float64(sel.X), float64(sel.Y)},
		{float64(sel.X + sel.W), float64(sel.Y)},
		{float64(sel.X), float64(sel.Y + sel.H)},
		{float64(sel.X + sel.W), float64(sel.Y + sel.H)},
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range corners {
		ix, iy := docToLayerIndex(workT, w, h, c[0], c[1])
		minX, minY = math.Min(minX, ix), math.Min(minY, iy)
		maxX, maxY = math.Max(maxX, ix), math.Max(maxY, iy)
	}
	return [4]int{
		int(math.Floor(minX)), int(math.Floor(minY)),
		int(math.Ceil(maxX)), int(math.Ceil(maxY)),
	}
}

// growBitmapTo copies src into a grid covering the union of the source and
// the given rect (layer px, exclusive max); offX/offY = where the source's
// (0,0) lands in the new grid.
func growBitmapTo(src *render.Bitmap, rx0, ry0, rx1, ry1 int) (*render.Bitmap, int, int) {
	x0 := min(min(rx0, 0), 0)
	y0 := min(min(ry0, 0), 0)
	x1 := max(max(rx1, src.W), 0)
	y1 := max(max(ry1, src.H), 0)
	out := render.NewBitmap(x1-x0, y1-y0)
	for y := 0; y < src.H; y++ {
		copy(out.Pix[((y-y0)*out.W+(0-x0))*4:((y-y0)*out.W+(0-x0))*4+src.W*4],
			src.Pix[y*src.W*4:(y*src.W+src.W)*4])
	}
	return out, -x0, -y0
}

// expandTransformOffset places the grown grid: the source's (0,0) lands at
// (offX, offY) in the new grid.
func expandTransformOffset(t domain.Transform, oldW, oldH, newW, newH, offX, offY int) domain.Transform {
	out := t
	out.Size[0] = t.Size[0] * float64(newW) / float64(oldW)
	out.Size[1] = t.Size[1] * float64(newH) / float64(oldH)
	cx, cy := layerIndexToDoc(t, oldW, oldH, float64(newW)/2-float64(offX), float64(newH)/2-float64(offY))
	out.Origin[0] = cx - out.Size[0]/2
	out.Origin[1] = cy - out.Size[1]/2
	return out
}

// blendThroughSelection overlays filtered with orig through the selection
// coverage (PixelAdjust.blend): inside the selection rect the filtered
// result shows with weight mask/255; everywhere else the original shows —
// the kernel runs on the whole grid, the selection decides what survives.
func blendThroughSelection(filtered, orig *render.Bitmap, sel *filterSelection, workT domain.Transform) {
	if sel == nil {
		return
	}
	for y := 0; y < filtered.H; y++ {
		for x := 0; x < filtered.W; x++ {
			i := (y*filtered.W + x) * 4
			dx, dy := layerIndexToDoc(workT, filtered.W, filtered.H, float64(x)+0.5, float64(y)+0.5)
			mx := int(math.Floor(dx)) - sel.X
			my := int(math.Floor(dy)) - sel.Y
			if mx < 0 || my < 0 || mx >= sel.W || my >= sel.H {
				copy(filtered.Pix[i:i+4], orig.Pix[i:i+4])
				continue
			}
			w := float64(sel.Mask[my*sel.W+mx]) / 255
			if w >= 1 {
				continue
			}
			for c := 0; c < 4; c++ {
				f := float64(filtered.Pix[i+c])
				o := float64(orig.Pix[i+c])
				filtered.Pix[i+c] = uint8(math.Round(f*w + o*(1-w)))
			}
		}
	}
}
