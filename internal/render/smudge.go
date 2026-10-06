package render

import (
	"math"
)

// Smudge, blur and liquify — the blur tool's three modes — ported from the
// original's WarpStroke (reference/Swift/Compositor/Document/SmudgeLiquify.swift)
// and its GPU working copy MetalWarp (reference/Swift/Compositor/Rendering/
// MetalWarp.swift). A stroke deforms a working copy of the active layer dab by
// dab; the pixels come back to the CPU once, at pen-up, as one undoable bitmap.
//
// The heart of the port is liquify's offset field (MetalWarp.swift:15-18): a
// per-pixel source offset that each dab moves — never the pixels. A pixel is
// drawn afresh from the untouched original through its offset, sampled once at
// pen-up (Commit); resampling the pixels themselves at every dab, as the
// original's CPU path did, softens them a little each time, where Photoshop's
// Liquify keeps them sharp. Smudge instead drags pixels directly through a
// carried (2r+1)² color block, and blur paints a stroke-start softened
// snapshot of the layer through the tip (BlurTool.blurSample,
// reference/Swift/Compositor/Document/BlurTool.swift:10-17).
//
// Pixel invariants (AGENTS.md): the bitmaps here are the package's standard
// premultiplied RGBA8; averaging and bilinearly mixing premultiplied samples
// stays compositing-correct, and all math runs in sRGB like the original.

// WarpMode is the blur tool's mode, one of "liquify", "blur" or "smudge".
type WarpMode string

const (
	WarpModeLiquify WarpMode = "liquify"
	WarpModeBlur    WarpMode = "blur"
	WarpModeSmudge  WarpMode = "smudge"
)

// CycleWarpMode advances the blur tool's mode the way the R key does: through
// BlurToolMode's case order — liquify, blur, smudge — and around
// (SmudgeLiquify.swift:10-14; EditorSession.cycleToolMode,
// EditorSession.swift:393-404).
func CycleWarpMode(m WarpMode) WarpMode {
	switch m {
	case WarpModeLiquify:
		return WarpModeBlur
	case WarpModeBlur:
		return WarpModeSmudge
	default:
		return WarpModeLiquify
	}
}

// WarpSettings carries the brush settings a warp stroke runs with. Diameter
// and hardness are clamped here exactly as WarpStroke.init clamps them
// (SmudgeLiquify.swift:52-53). Strength is the dab's pressure: the original
// clamps it to 0.01…1 for the options bar, but the engine keeps 0 legal so a
// pressure curve can bottom out — a zero-strength dab moves nothing.
type WarpSettings struct {
	Diameter   float64
	Hardness   float64
	Strength   float64
	BlurRadius float64 // blur mode only: the options bar's radius, canvas pixels
}

// WarpPoint is one dab's center, in canvas pixels.
type WarpPoint struct {
	X, Y float64
}

// WarpStroke is a smudge/blur/liquify stroke in progress: the working copy
// plus whatever the mode's dabs accumulate. Dabs go in through Append (which
// spaces them along the path) or the per-mode dab calls; Commit returns the
// result. All coordinates are canvas pixels, top-left rows.
type WarpStroke struct {
	w, h     int
	mode     WarpMode
	diameter float64
	hardness float64
	strength float64
	radius   int

	// Mask is the optional selection clip: 8-bit coverage, one byte per pixel,
	// that every dab's weight is scaled by. nil (or a wrong-sized mask) means
	// the whole canvas.
	Mask []uint8

	// src is the layer as the stroke found it — liquify's untouched original
	// (MetalWarp.original), from which every pixel is resampled at pen-up.
	src *Bitmap
	// work is the working copy: smudge and blur dabs write it directly;
	// liquify dabs leave it alone and move the offsets instead.
	work *Bitmap
	// blur is blur mode's stroke-start softened snapshot (BlurTool.blurSample).
	blur *Bitmap
	// offsets is liquify's field: a source offset per pixel, in pixels
	// (MetalWarp.offsets). A pixel is drawn from src where its offset points.
	offsets []float32
	// scratch is the dab area's offsets as they were before a dab, which the
	// dab reads (MetalWarp.scratch).
	scratch []float32
	// carried is smudge's color block, a (2r+1)² RGBA square in 0…255
	// (MetalWarp.carried, WarpStroke.carried).
	carried []float32

	hasLast      bool
	lastX, lastY float64
	points       []WarpPoint
}

// NewWarpStroke starts a stroke on src (which the caller must leave unchanged
// while the stroke runs — it is the readback's resampling source). An unknown
// mode falls back to liquify, the tool's default (EditorSession.swift:207).
func NewWarpStroke(src *Bitmap, mode WarpMode, settings WarpSettings) *WarpStroke {
	if mode != WarpModeBlur && mode != WarpModeSmudge {
		mode = WarpModeLiquify
	}
	diameter := math.Max(2, settings.Diameter)
	hardness := math.Min(0.98, math.Max(0, settings.Hardness))
	strength := math.Max(0, settings.Strength)
	s := &WarpStroke{
		w:        src.W,
		h:        src.H,
		mode:     mode,
		diameter: diameter,
		hardness: hardness,
		strength: strength,
		radius:   int(math.Ceil(diameter / 2)),
		src:      src,
		work:     src.Clone(),
	}
	switch mode {
	case WarpModeBlur:
		// The softened sample is taken when the stroke starts, so going over an
		// area again in a new stroke softens it further, as in Photoshop
		// (BlurTool.swift:8-9). Core Image applies a true Gaussian; three box
		// passes are the standard separable approximation. Its sigma follows
		// BlurTool.swift:17's radius formula — sigma = min(50, max(0.5, radius))
		// mapped into the layer's pixels; at document scale perPixel is 1, and a
		// whole-canvas snapshot needs no side/2 cap.
		s.blur = blurSnapshot(src, math.Min(50, math.Max(0.5, settings.BlurRadius)))
	case WarpModeLiquify:
		// The first push keeps the layer as it is, and starts every offset at
		// nothing (MetalWarp.swift:132-140; warp_clear).
		s.offsets = make([]float32, src.W*src.H*2)
	}
	return s
}

// warpWeight is how much a dab acts on a pixel u (0 center, 1 rim) from its
// center: full inside the hardness plateau, a smoothstep to zero at the rim
// (MetalWarp.swift:179-184, WarpStroke.weight at SmudgeLiquify.swift:67-73).
// The math stays in float32 like both originals — the Swift CPU path runs it
// in Float and the Metal kernel in float, and the trail shape is sensitive
// enough at its apex that widening it moves bytes.
func warpWeight(u, hardness float32) float32 {
	if u >= 1 {
		return 0
	}
	if u <= hardness {
		return 1
	}
	t := (1 - u) / (1 - hardness)
	return t * t * (3 - 2*t)
}

// maskAt is the selection's coverage at a pixel: 1 without a clip, else the
// 8-bit mask over 255.
func (s *WarpStroke) maskAt(x, y int) float64 {
	if len(s.Mask) != s.w*s.h {
		return 1
	}
	return float64(s.Mask[y*s.w+x]) / 255
}

// Append continues the stroke to a point, dabbing along the way. Dabs space at
// max(1, diameter × 0.005) for smudge and × 0.025 otherwise — spaced widely
// each step left a faint echo of what it dragged; a pixel apart the steps run
// together into one smear (SmudgeLiquify.swift:84-101). The stroke's first
// point primes the state only: smudge picks its color up there, the others
// just remember where the stroke starts.
func (s *WarpStroke) Append(x, y float64) {
	if !s.hasLast {
		s.hasLast, s.lastX, s.lastY = true, x, y
		if s.mode == WarpModeSmudge {
			s.pickUp(x, y)
		}
		return
	}
	distance := math.Hypot(x-s.lastX, y-s.lastY)
	spacing := math.Max(1, s.diameter*0.025)
	if s.mode == WarpModeSmudge {
		spacing = math.Max(1, s.diameter*0.005)
	}
	if distance < spacing {
		return
	}
	steps := int(math.Ceil(distance / spacing))
	prevX, prevY := s.lastX, s.lastY
	for step := 1; step <= steps; step++ {
		t := float64(step) / float64(steps)
		nx := s.lastX + (x-s.lastX)*t
		ny := s.lastY + (y-s.lastY)*t
		switch s.mode {
		case WarpModeSmudge:
			s.SmudgeDab(nx, ny, s.radius, s.strength)
		case WarpModeBlur:
			s.BlurDab(nx, ny, s.radius, s.strength)
		default:
			s.PushDab(prevX, prevY, nx, ny, s.radius, s.strength)
		}
		s.points = append(s.points, WarpPoint{X: nx, Y: ny})
		prevX, prevY = nx, ny
	}
	s.lastX, s.lastY = x, y
}

// Points reports every dab's center in order — the path the finished warp
// covers (WarpStroke.points, SmudgeLiquify.swift:29-30).
func (s *WarpStroke) Points() []WarpPoint {
	out := make([]WarpPoint, len(s.points))
	copy(out, s.points)
	return out
}

// pickUp takes the (2r+1)² block under the brush into carried, zeros outside
// the canvas (WarpStroke.pickUp, SmudgeLiquify.swift:111-125; warp_pick_up,
// MetalWarp.swift:186-194).
func (s *WarpStroke) pickUp(x, y float64) {
	r := s.radius
	side := 2*r + 1
	s.carried = make([]float32, side*side*4)
	cx, cy := int(math.Round(x)), int(math.Round(y))
	for dy := -r; dy <= r; dy++ {
		y := cy + dy
		if y < 0 || y >= s.h {
			continue
		}
		for dx := -r; dx <= r; dx++ {
			x := cx + dx
			if x < 0 || x >= s.w {
				continue
			}
			p, c := (y*s.w+x)*4, ((dy+r)*side+(dx+r))*4
			for k := 0; k < 4; k++ {
				s.carried[c+k] = float32(s.work.Pix[p+k])
			}
		}
	}
}

// SmudgeDab lays what the brush carried down under it at the dab's strength,
// weighted by the falloff, and the brush then carries what it just left — and
// nothing older: holding on to what it picked up at the start stamped it again
// at every dab, a trail of ghost copies (WarpStroke.smudge,
// SmudgeLiquify.swift:127-152; warp_smudge, MetalWarp.swift:196-211). A first
// dab on a stroke that never picked anything up picks up here. The falloff
// measures distance against the dab's own radius, matching the original's
// inverseRadius = 1/(diameter/2) exactly for even diameters.
func (s *WarpStroke) SmudgeDab(x, y float64, radius int, strength float64) {
	if radius < 1 {
		radius = 1
	}
	if len(s.carried) == 0 {
		s.pickUp(x, y)
	}
	side := 2*radius + 1
	cx, cy := int(math.Round(x)), int(math.Round(y))
	keep := float32(strength)
	invR := float32(1) / float32(radius)
	hardness := float32(s.hardness)
	for dy := -radius; dy <= radius; dy++ {
		y := cy + dy
		if y < 0 || y >= s.h {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			x := cx + dx
			if x < 0 || x >= s.w {
				continue
			}
			w := warpWeight(float32(math.Sqrt(float64(dx*dx+dy*dy)))*invR, hardness) *
				float32(s.maskAt(x, y))
			if w <= 0 {
				continue // outside the selection the brush neither lays down nor picks up
			}
			p, c := (y*s.w+x)*4, ((dy+radius)*side+(dx+radius))*4
			for k := 0; k < 4; k++ {
				under := float32(s.work.Pix[p+k])
				// What was under the brush at the last dab, laid down here at
				// the smudge's strength.
				painted := under + (s.carried[c+k]-under)*w*keep
				s.work.Pix[p+k] = clampByte(float64(painted))
				s.carried[c+k] = painted
			}
		}
	}
}

// BlurDab paints the stroke-start softened snapshot through the tip: the same
// falloff and strength as a smudge dab, sourced from the snapshot instead of
// the carried block. The original routes this through its brush pipeline (a
// BrushStroke whose clone holds the layer blurred, painted in place —
// EditorSession+Brush.swift:76-82, BrushStroke.swift:162-164); the tip-weight
// paint against the snapshot reproduces it at dab level. Dabs over one spot
// converge on the same snapshot instead of compounding the blur.
func (s *WarpStroke) BlurDab(x, y float64, radius int, strength float64) {
	if radius < 1 {
		radius = 1
	}
	if s.blur == nil {
		return
	}
	cx, cy := int(math.Round(x)), int(math.Round(y))
	invR := float32(1) / float32(radius)
	hardness := float32(s.hardness)
	for dy := -radius; dy <= radius; dy++ {
		y := cy + dy
		if y < 0 || y >= s.h {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			x := cx + dx
			if x < 0 || x >= s.w {
				continue
			}
			w := float64(warpWeight(float32(math.Sqrt(float64(dx*dx+dy*dy)))*invR, hardness)) *
				s.maskAt(x, y)
			if w <= 0 {
				continue
			}
			p := (y*s.w + x) * 4
			for k := 0; k < 4; k++ {
				under := float64(s.work.Pix[p+k])
				painted := under + (float64(s.blur.Pix[p+k])-under)*w*strength
				s.work.Pix[p+k] = clampByte(painted)
			}
		}
	}
}

// PushDab is liquify's forward warp, as WarpStroke.push: what's under the
// brush moves with it, most at its center, fading to none at its rim — worked
// on the offsets, with the pixels under the dab drawn again from the untouched
// ones (MetalWarp.push, MetalWarp.swift:120-148; warp_push, lines 229-259).
// The dab only ever writes the offset field: the pixels are resampled once, at
// Commit, which is what keeps repeated dabs from softening them.
func (s *WarpStroke) PushDab(fromX, fromY, toX, toY float64, radius int, strength float64) {
	if radius < 1 {
		radius = 1
	}
	moveX, moveY := (toX-fromX)*strength, (toY-fromY)*strength
	margin := int(math.Ceil(math.Max(math.Abs(moveX), math.Abs(moveY)))) + 2
	cx, cy := int(math.Round(toX)), int(math.Round(toY))
	// A copy of the area as it was before this dab, which the dab samples from
	// (SmudgeLiquify.swift:160-165).
	clampI := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	x0 := clampI(cx-radius-margin, 0, s.w-1)
	x1 := clampI(cx+radius+margin, 0, s.w-1)
	y0 := clampI(cy-radius-margin, 0, s.h-1)
	y1 := clampI(cy+radius+margin, 0, s.h-1)
	if x0 > x1 || y0 > y1 {
		return
	}
	cw, ch := x1-x0+1, y1-y0+1
	// warp_copy on the offsets: the field over the dab's area, as it was.
	need := cw * ch * 2
	if cap(s.scratch) < need {
		s.scratch = make([]float32, need)
	} else {
		s.scratch = s.scratch[:need]
	}
	for y := 0; y < ch; y++ {
		base := ((y+y0)*s.w + x0) * 2
		copy(s.scratch[y*cw*2:(y+1)*cw*2], s.offsets[base:base+cw*2])
	}
	invR := float32(1) / float32(radius)
	hardness := float32(s.hardness)
	for dy := -radius; dy <= radius; dy++ {
		y := cy + dy
		if y < y0 || y > y1 {
			continue
		}
		for dx := -radius; dx <= radius; dx++ {
			x := cx + dx
			if x < x0 || x > x1 {
				continue
			}
			w := float64(warpWeight(float32(math.Sqrt(float64(dx*dx+dy*dy)))*invR, hardness)) *
				s.maskAt(x, y)
			if w <= 0 {
				continue // masked-out pixels keep their offset — and stay untouched
			}
			// Bilinear sample of the offsets as they were, from behind the
			// brush's travel (MetalWarp.swift:241-249). The displacement is
			// scaled by the mask like the weight itself.
			sx := math.Min(float64(cw-1), math.Max(0, float64(x-x0)-moveX*w))
			sy := math.Min(float64(ch-1), math.Max(0, float64(y-y0)-moveY*w))
			ix, iy := int(sx), int(sy)
			if ix > cw-2 {
				ix = cw - 2
			}
			if iy > ch-2 {
				iy = ch - 2
			}
			if ix < 0 || iy < 0 {
				continue
			}
			fx, fy := sx-float64(ix), sy-float64(iy)
			b := (iy*cw + ix) * 2
			o00x, o00y := float64(s.scratch[b]), float64(s.scratch[b+1])
			o10x, o10y := float64(s.scratch[b+2]), float64(s.scratch[b+3])
			o01x, o01y := float64(s.scratch[b+cw*2]), float64(s.scratch[b+cw*2+1])
			o11x, o11y := float64(s.scratch[b+cw*2+2]), float64(s.scratch[b+cw*2+3])
			topX, topY := o00x+(o10x-o00x)*fx, o00y+(o10y-o00y)*fx
			botX, botY := o01x+(o11x-o01x)*fx, o01y+(o11y-o01y)*fx
			o := (y*s.w + x) * 2
			s.offsets[o] = float32(topX + (botX-topX)*fy - moveX*w)
			s.offsets[o+1] = float32(topY + (botY-topY)*fy - moveY*w)
		}
	}
}

// Commit is the pen-up readback: the working copy's pixels as they stand once
// every dab has run (MetalWarp.read, MetalWarp.swift:47-52). For liquify that
// is the untouched original drawn once more through the offset field — each
// pixel is the original bilinearly sampled where its offset points, held to
// the canvas edges (warp_push's per-dab write, MetalWarp.swift:251-258, run
// once over the whole canvas; identical bytes, because every dab write also
// sampled only the original through the then-current field). The result is a
// fresh bitmap and Commit is pure — call it twice, get equal bytes — so the
// session can store it as one undo step the way the original's finishWarp
// commits the stroke as a single paint snapshot (SmudgeLiquify.swift:216-246);
// the Go history takes the bitmap directly, no per-dab replay.
func (s *WarpStroke) Commit() *Bitmap {
	out := NewBitmap(s.w, s.h)
	if s.mode != WarpModeLiquify {
		copy(out.Pix, s.work.Pix)
		return out
	}
	if len(s.offsets) != s.w*s.h*2 {
		copy(out.Pix, s.work.Pix)
		return out
	}
	applyWarpField(s.src, s.offsets, out)
	return out
}

// applyWarpField draws src through the offset field into out (both canvas
// sized; offsets is w*h float32 pairs, zero meaning "as it was").
func applyWarpField(src *Bitmap, offsets []float32, out *Bitmap) {
	w, h := src.W, src.H
	parallelFor(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				// The untouched layer where the offset points, held to its edges.
				sx := math.Min(float64(w-1), math.Max(0, float64(x)+float64(offsets[(y*w+x)*2])))
				sy := math.Min(float64(h-1), math.Max(0, float64(y)+float64(offsets[(y*w+x)*2+1])))
				ix, iy := int(sx), int(sy)
				if ix > w-2 {
					ix = w - 2
				}
				if iy > h-2 {
					iy = h - 2
				}
				if ix < 0 {
					ix = 0
				}
				if iy < 0 {
					iy = 0
				}
				fx, fy := sx-float64(ix), sy-float64(iy)
				i00 := (iy*w + ix) * 4
				o := (y*w + x) * 4
				for k := 0; k < 4; k++ {
					c00 := float64(src.Pix[i00+k])
					c10 := float64(src.Pix[i00+4+k])
					c01 := float64(src.Pix[i00+w*4+k])
					c11 := float64(src.Pix[i00+w*4+4+k])
					top := c00 + (c10-c00)*fx
					bottom := c01 + (c11-c01)*fx
					v := top + (bottom-top)*fy
					if v < 0 {
						v = 0
					}
					if v > 255 {
						v = 255
					}
					out.Pix[o+k] = uint8(math.Round(v))
				}
			}
		}
	})
}

// clampByte rounds a 0…255 color to its byte, as the warp kernels' final
// clamp(round(color * 255), 0, 255) does (MetalWarp.swift:209, 258).
func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}

// blurSnapshot makes the stroke-start softened copy the blur mode paints
// through the tip: three edge-clamped box passes sized to match a Gaussian of
// sigma (the boxes-for-sigma formula behind fast-Gaussian implementations).
// The original lets the blur spread past the sample's edges by blurring a 3σ
// larger region (BlurTool.swift:18-19); a whole-canvas snapshot gets the same
// effect from the edge clamp at the document border. The original also budgets
// the sample's resolution (BlurTool.swift:22-26); this runs at full document
// resolution, where the CPU fallback's O(w·h·r) passes are the cost the GPU
// fast path exists to avoid.
func blurSnapshot(src *Bitmap, sigma float64) *Bitmap {
	out := src.Clone()
	for _, r := range boxRadiiForSigma(sigma) {
		if r > 0 {
			boxBlur(out, r)
		}
	}
	return out
}

// boxRadiiForSigma sizes three box windows whose successive averages match a
// Gaussian of the given sigma.
func boxRadiiForSigma(sigma float64) [3]int {
	wIdeal := math.Sqrt(12*sigma*sigma/3 + 1)
	wl := int(math.Floor(wIdeal))
	if wl%2 == 0 {
		wl--
	}
	if wl < 1 {
		wl = 1
	}
	wu := wl + 2
	m := (12*sigma*sigma - 3*float64(wl*wl) - 4*float64(wl) - 3) / (-4*float64(wl) - 4)
	mi := int(math.Round(m))
	if mi < 0 {
		mi = 0
	}
	if mi > 3 {
		mi = 3
	}
	var radii [3]int
	for i := range radii {
		if i < mi {
			radii[i] = (wl - 1) / 2
		} else {
			radii[i] = (wu - 1) / 2
		}
	}
	return radii
}

// boxBlur runs one box pass of the given radius over all four channels,
// horizontally then vertically, each pixel the integer average of its clamped
// window (premultiplied RGBA, so averaging stays compositing-correct).
func boxBlur(bm *Bitmap, radius int) {
	w, h := bm.W, bm.H
	tmp := make([]uint8, len(bm.Pix))
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			lo, hi := x-radius, x+radius
			if lo < 0 {
				lo = 0
			}
			if hi > w-1 {
				hi = w - 1
			}
			var s0, s1, s2, s3 uint32
			for xx := lo; xx <= hi; xx++ {
				i := (row + xx) * 4
				s0 += uint32(bm.Pix[i])
				s1 += uint32(bm.Pix[i+1])
				s2 += uint32(bm.Pix[i+2])
				s3 += uint32(bm.Pix[i+3])
			}
			n := uint32(hi - lo + 1)
			o := (row + x) * 4
			tmp[o] = uint8((s0 + n/2) / n)
			tmp[o+1] = uint8((s1 + n/2) / n)
			tmp[o+2] = uint8((s2 + n/2) / n)
			tmp[o+3] = uint8((s3 + n/2) / n)
		}
	}
	for y := 0; y < h; y++ {
		lo, hi := y-radius, y+radius
		if lo < 0 {
			lo = 0
		}
		if hi > h-1 {
			hi = h - 1
		}
		n := uint32(hi - lo + 1)
		for x := 0; x < w; x++ {
			var s0, s1, s2, s3 uint32
			for yy := lo; yy <= hi; yy++ {
				i := (yy*w + x) * 4
				s0 += uint32(tmp[i])
				s1 += uint32(tmp[i+1])
				s2 += uint32(tmp[i+2])
				s3 += uint32(tmp[i+3])
			}
			o := (y*w + x) * 4
			bm.Pix[o] = uint8((s0 + n/2) / n)
			bm.Pix[o+1] = uint8((s1 + n/2) / n)
			bm.Pix[o+2] = uint8((s2 + n/2) / n)
			bm.Pix[o+3] = uint8((s3 + n/2) / n)
		}
	}
}
