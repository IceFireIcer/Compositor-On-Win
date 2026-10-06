package render

import (
	"errors"
	"math"
)

// Brush engine core: the CPU port of the macOS brush raster
// (reference/Swift/Compositor/Document/BrushStroke.swift, the semantic
// authority). One stroke accumulates per-tile grayscale coverage buffers; on
// every read each touched tile is recomposed from its original pixels plus
// color × coverage × opacity, which is what caps a stroke at the brush
// opacity however many dabs overlap (BrushStroke.swift:180-183). Drawing and
// erasing share the one pipeline: erase swaps the source-over recompose for
// destination-out (BrushStroke.swift:534-539).
//
// Not ported here (they live above this layer or in later tickets): the layer
// transform (pixelToDocument — the Go stroke grid is already the canvas),
// mask painting, Clone Stamp / Blur / Spot Healing, the Metal coverage path,
// the provisional straight tail, and the Catmull-Rom input spline
// (BrushStroke.swift:442-458) — the Go port lays final dabs along straight
// runs; the spline belongs to the input-sampling layer.
//
// Pixel invariants (AGENTS.md): premultiplied 8-bit RGBA throughout, sRGB;
// coverage is 8-bit grayscale like BrushRaster's mask contexts
// (BrushStroke.swift:36-44).

// ErrInvalidBrushSettings mirrors the ProjectError.tooLarge guard in
// BrushStroke.init (BrushStroke.swift:223-227).
var ErrInvalidBrushSettings = errors.New("render: brush settings out of range")

// TileSize is BrushStroke.tileSize (BrushStroke.swift:186): only touched
// 256px tiles allocate writable pixels; wider tiles measured no faster for
// wide brushes and slower for narrow ones.
const TileSize = 256

// maxBrushDiameter mirrors BrushStroke.maxDiameter (BrushStroke.swift:147):
// the options bar stops Size at 2000; strokes the app lays itself run a
// little wider.
const maxBrushDiameter = 2100

// BrushSettings mirrors BrushSettings (BrushStroke.swift:9-27). Colors are
// straight sRGB in [0,1]; opacity caps the whole stroke, as in Photoshop:
// overlapping dabs never exceed it (BrushStroke.swift:15-16).
type BrushSettings struct {
	Diameter  float64 // document-pixel diameter of the tip
	Hardness  float64 // 0–1; 1 is a hard circle, 0 feathers across the whole radius
	R, G, B   float64 // paint color
	Opacity   float64 // stroke-wide alpha cap, 0.01–1
	Smoothing float64 // 0–100; the brush trails the pointer on a string of this length, in screen points
	// Spacing overrides the dab spacing as a fraction of the diameter. The
	// original exposes no spacing control — it derives the spacing from
	// hardness (spacingFraction, BrushStroke.swift:462) — so 0 (the default)
	// keeps that derived spacing. The field exists for the parity tests and a
	// future options-bar control.
	Spacing float64
	Erase   bool // the stroke clears the layer's pixels instead of painting color on them
}

// Point is a document-space coordinate.
type Point struct{ X, Y float64 }

// TileGrid is a raster stored as a grid of TileSize squares, edge tiles
// clipped (allocateTile's min() clamp, BrushStroke.swift:646). A nil tile was
// never touched and reads as fully transparent — the Go counterpart of
// BrushStroke's lazily allocated tile contexts (BrushStroke.swift:119-121,
// 643-683). Slice sharing is deliberate: a snapshot reuses the base grid's
// untouched tile slices verbatim, so committing a stroke copies at most the
// touched tiles, never the entire layer on a mouse-move event. Treat a grid
// as immutable once handed to NewStroke or returned from Stroke.Commit;
// mutate a tile only through a fresh copy.
type TileGrid struct {
	W, H       int
	Cols, Rows int
	Tiles      [][]uint8 // len Cols*Rows; each len tileW*tileH*4 premultiplied, or nil
}

// NewTileGrid returns an all-transparent grid of w×h document pixels, or nil
// for degenerate extents.
func NewTileGrid(w, h int) *TileGrid {
	if w < 1 || h < 1 {
		return nil
	}
	cols := (w + TileSize - 1) / TileSize
	rows := (h + TileSize - 1) / TileSize
	return &TileGrid{W: w, H: h, Cols: cols, Rows: rows, Tiles: make([][]uint8, cols*rows)}
}

// TileGridFromBitmap splits a contiguous premultiplied bitmap into tile
// slices. Each tile gets its own copy: a 256px column of a strided bitmap is
// not addressable as one contiguous slice.
func TileGridFromBitmap(b *Bitmap) *TileGrid {
	if b == nil || b.W < 1 || b.H < 1 {
		return nil
	}
	g := NewTileGrid(b.W, b.H)
	for ty := 0; ty < g.Rows; ty++ {
		for tx := 0; tx < g.Cols; tx++ {
			tw, th := g.tileSize(tx, ty)
			pix := make([]uint8, tw*th*4)
			for y := 0; y < th; y++ {
				src := (ty*TileSize+y)*b.W*4 + tx*TileSize*4
				copy(pix[y*tw*4:(y+1)*tw*4], b.Pix[src:src+tw*4])
			}
			g.Tiles[ty*g.Cols+tx] = pix
		}
	}
	return g
}

// tileSize returns the clipped extents of one tile (BrushStroke.swift:646).
func (g *TileGrid) tileSize(tx, ty int) (w, h int) {
	return min(TileSize, g.W-tx*TileSize), min(TileSize, g.H-ty*TileSize)
}

// Pixel reads one premultiplied pixel; nil tiles and out-of-bounds reads are
// transparent.
func (g *TileGrid) Pixel(x, y int) (r, gc, b, a uint8) {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return
	}
	tx, ty := x/TileSize, y/TileSize
	pix := g.Tiles[ty*g.Cols+tx]
	if pix == nil {
		return
	}
	tw, _ := g.tileSize(tx, ty)
	i := ((y-ty*TileSize)*tw + (x - tx*TileSize)) * 4
	return pix[i], pix[i+1], pix[i+2], pix[i+3]
}

// Bitmap assembles the grid into one contiguous premultiplied bitmap.
func (g *TileGrid) Bitmap() *Bitmap {
	out := NewBitmap(g.W, g.H)
	for ty := 0; ty < g.Rows; ty++ {
		for tx := 0; tx < g.Cols; tx++ {
			pix := g.Tiles[ty*g.Cols+tx]
			if pix == nil {
				continue
			}
			tw, th := g.tileSize(tx, ty)
			for y := 0; y < th; y++ {
				dst := (ty*TileSize+y)*g.W*4 + tx*TileSize*4
				copy(out.Pix[dst:dst+tw*4], pix[y*tw*4:(y+1)*tw*4])
			}
		}
	}
	return out
}

// Stroke is one in-progress brush stroke over a base grid. The stroke never
// writes into the base grid: touched tiles accumulate coverage in private
// buffers and the base plays the role of BrushStroke's per-tile "original
// content" (tile.base, BrushStroke.swift:175), so discarding the stroke is an
// undo for free and committing is a snapshot, not a mutation.
type Stroke struct {
	base     *TileGrid
	settings BrushSettings
	radius   float64 // diameter / 2
	spacing  float64 // document pixels between dab centers

	// Zoom is the viewport zoom, which scales the smoothing string
	// (EditorSession+Brush.swift:105: the string's length is in screen
	// points, so it feels the same however far the canvas is zoomed in). Set
	// it before the first Append; it defaults to 1.
	Zoom float64

	// String smoothing state (EditorSession+Brush.swift:103-113).
	anchor     Point
	hasAnchor  bool
	pointer    Point // last raw pointer sample
	hasPointer bool

	// Walk state (BrushStroke.swift:173, 465-483): where the last dab run
	// stopped and how far until the next dab.
	prev       Point
	hasPrev    bool
	distToNext float64

	// Per-tile stroke state, keyed ty*Cols+tx: grayscale coverage accumulated
	// dab by dab, and the tile recomposed from base × coverage. Allocated
	// together, lazily (BrushStroke.swift:605-609).
	cov map[int][]uint8
	pix map[int][]uint8

	// dabs records dab centers for the parity tests; the original keeps no
	// such list.
	dabs []Point

	done bool
}

// NewStroke validates the settings against BrushStroke.init's guards
// (BrushStroke.swift:223-227) and returns a stroke that paints over base.
func NewStroke(base *TileGrid, s BrushSettings) (*Stroke, error) {
	switch {
	case base == nil || base.W < 1 || base.H < 1:
		return nil, ErrInvalidBrushSettings
	case !isFinite(s.Diameter) || s.Diameter < 1 || s.Diameter > maxBrushDiameter:
		return nil, ErrInvalidBrushSettings
	case !isFinite(s.Hardness) || s.Hardness < 0 || s.Hardness > 1:
		return nil, ErrInvalidBrushSettings
	case !isFinite(s.Opacity) || s.Opacity < 0.01 || s.Opacity > 1:
		return nil, ErrInvalidBrushSettings
	}
	st := &Stroke{
		base:     base,
		settings: s,
		radius:   s.Diameter / 2,
		cov:      make(map[int][]uint8),
		pix:      make(map[int][]uint8),
		Zoom:     1,
	}
	if s.Spacing > 0 {
		st.spacing = max(0.25, s.Diameter*s.Spacing)
	} else {
		st.spacing = max(0.25, s.Diameter*spacingFraction(s.Hardness))
	}
	// The options bar clamps smoothing to 0–100 (BrushControls.swift:88-95).
	st.settings.Smoothing = min(100, max(0, st.settings.Smoothing))
	return st, nil
}

// spacingFraction ports BrushStroke.spacingFraction (BrushStroke.swift:462):
// soft tips lay dabs at 2.5% of the diameter and hard ones at 1.5% — dense
// enough that the stroke still reads as solid along its length, never beading
// (BrushTests.swift:336-352).
func spacingFraction(hardness float64) float64 {
	if hardness >= 1 {
		return 0.015
	}
	return 0.025
}

// Append samples one pointer event. With smoothing on, samples within the
// string's reach never reach the stroke (EditorSession.continueBrush →
// smoothed, EditorSession+Brush.swift:91-113). Port of BrushStroke.append
// (BrushStroke.swift:274-290) minus the GPU path and the provisional straight
// tail: the Go port lays final dabs immediately, so there is no tail to erase
// and re-lay.
func (st *Stroke) Append(p Point) {
	if st.done || !isFinite(p.X) || !isFinite(p.Y) {
		return // :275
	}
	st.pointer = p
	st.hasPointer = true
	if !st.hasAnchor {
		// beginBrush anchors the string at the first sample and paints it at
		// once (EditorSession+Brush.swift:84-85).
		st.anchor = p
		st.hasAnchor = true
		st.walk(p)
		return
	}
	moved, ok := st.smoothed(p)
	if !ok {
		return // the string is still slack; those jitters never reach the stroke
	}
	st.walk(moved)
}

// smoothed ports the string model (EditorSession+Brush.swift:103-113): the
// brush trails the pointer on a string of length smoothing/zoom and only
// moves once the pointer pulls that string taut — and then just far enough to
// take up the slack. Returns ok=false while the string is slack.
func (st *Stroke) smoothed(p Point) (Point, bool) {
	if st.settings.Smoothing <= 0 {
		return p, true
	}
	radius := st.settings.Smoothing / math.Max(0.01, st.Zoom)
	dx := p.X - st.anchor.X
	dy := p.Y - st.anchor.Y
	dist := math.Hypot(dx, dy)
	if dist <= radius {
		return Point{}, false
	}
	step := (dist - radius) / dist
	moved := Point{X: st.anchor.X + dx*step, Y: st.anchor.Y + dy*step}
	st.anchor = moved
	return moved, true
}

// Line paints a straight run between two points at the walk's dab spacing —
// the Shift-click line (EditorSession.shiftLineStart,
// EditorSession+Brush.swift:114-118, which starts a fresh stroke at the end
// of the last one). It bypasses smoothing: Shift-click geometry is exact.
// Call it on a fresh stroke.
func (st *Stroke) Line(from, to Point) {
	if st.done {
		return
	}
	st.hasPrev = false
	st.walk(from)
	st.walk(to)
}

// Finish ends the stroke. Smoothing can leave the brush short of the pointer;
// the stroke ends where the hand did (EditorSession.finishBrushImmediately,
// EditorSession+Brush.swift:138-142, which appends the raw pointer past the
// string). flush()'s curve finalization has no counterpart here: dabs were
// final when laid.
func (st *Stroke) Finish() {
	if st.done {
		return
	}
	if st.settings.Smoothing > 0 && st.hasPointer && st.pointer != st.anchor {
		st.walk(st.pointer)
	}
}

// walk lays evenly spaced dabs along a straight run from the previous dab
// position — an exact port of BrushStroke.walk (BrushStroke.swift:465-483).
// distToNext carries the phase across segments, so dab placement depends on
// distance traveled, not on how many pointer events arrived
// (BrushIntersectionTests.swift:55-67).
func (st *Stroke) walk(to Point) {
	if st.hasPrev {
		dx := to.X - st.prev.X
		dy := to.Y - st.prev.Y
		length := math.Hypot(dx, dy)
		if length > 0 {
			distance := st.distToNext
			for distance <= length {
				st.dab(Point{X: st.prev.X + dx*distance/length, Y: st.prev.Y + dy*distance/length})
				distance += st.spacing
			}
			st.distToNext = distance - length
		}
	} else {
		st.dab(to)
		st.distToNext = st.spacing
	}
	st.prev = to
	st.hasPrev = true
}

// dab stamps the tip once into the per-tile coverage buffers. Port of
// BrushStroke.dab (BrushStroke.swift:576-641) reduced to document space (the
// Go stroke grid is the canvas, so pixelToDocument drops out), with the tip
// evaluated directly per pixel instead of blitting a pre-rendered tip image
// (BrushStroke.tip, BrushStroke.swift:251-268) — the same profile, with
// nothing for a resampler to soften.
func (st *Stroke) dab(p Point) {
	// The dab's pixel box, with a pixel of room for the hard tip's
	// antialiasing ramp; the soft profile is exactly zero past the rim.
	x0 := max(0, int(math.Floor(p.X-st.radius))-1)
	x1 := min(st.base.W-1, int(math.Ceil(p.X+st.radius))+1)
	y0 := max(0, int(math.Floor(p.Y-st.radius))-1)
	y1 := min(st.base.H-1, int(math.Ceil(p.Y+st.radius))+1)
	if x0 > x1 || y0 > y1 {
		return // the dab misses the canvas (BrushStroke.swift:579-580)
	}
	hard := st.settings.Hardness >= 1
	for y := y0; y <= y1; y++ {
		ty := y / TileSize
		yOff := y - ty*TileSize
		for x := x0; x <= x1; x++ {
			c := tipCoverage(st.settings.Hardness, st.radius,
				math.Hypot(float64(x)+0.5-p.X, float64(y)+0.5-p.Y))
			if c <= 0 {
				continue
			}
			tx := x / TileSize
			key := ty*st.base.Cols + tx
			cov := st.coverageFor(key, tx, ty)
			tw, _ := st.base.tileSize(tx, ty)
			i := yOff*tw + (x - tx*TileSize)
			// Soft tips accumulate paint within the stroke (screen: each dab
			// adds what the others leave, capped at full); hard tips keep
			// their antialiased silhouette (lighten: the strongest rim wins).
			// BrushStroke.swift:180-183; blend modes chosen at :614 and :624.
			old := float64(cov[i]) / 255
			if hard {
				if c > old {
					cov[i] = u8round(c * 255)
				}
			} else {
				cov[i] = u8round((old + c - old*c) * 255)
			}
		}
	}
	st.dabs = append(st.dabs, p)
}

// tipCoverage returns the tip's grayscale coverage at distance d from the dab
// center — BrushStroke.tip (BrushStroke.swift:251-268) evaluated
// analytically. A hard tip is the filled ellipse with an antialiased rim (the
// ramp spans one pixel, standing in for CoreGraphics's coverage filter); a
// soft tip holds full strength inside radius·hardness and feathers across the
// band to the rim with the radial gradient built from brushFalloff
// (BrushStroke.swift:231-234, 263-265).
func tipCoverage(hardness, radius, d float64) float64 {
	if hardness >= 1 {
		return clamp01(radius + 0.5 - d)
	}
	inner := radius * hardness
	if d <= inner {
		return 1
	}
	if d >= radius {
		return 0
	}
	return brushFalloff((d - inner) / (radius - inner))
}

// brushFalloff ports BrushRaster.falloff (BrushStroke.swift:104-109): a
// normalized Gaussian, k = 2.5, that fades across the band between the
// hardness radius and the rim and reaches zero at the rim — about half
// strength at u = 0.5 (BrushTests.swift:149-153).
func brushFalloff(u float64) float64 {
	const k = 2.5
	return math.Max(0, (math.Exp(-k*u*u)-math.Exp(-k))/(1-math.Exp(-k)))
}

// coverageFor lazily allocates a touched tile's coverage and pixel buffers —
// the Go counterpart of allocateTile (BrushStroke.swift:643-683); the base
// grid plays the role of the tile's drawn-in source pixels.
func (st *Stroke) coverageFor(key, tx, ty int) []uint8 {
	if cov, ok := st.cov[key]; ok {
		return cov
	}
	tw, th := st.base.tileSize(tx, ty)
	cov := make([]uint8, tw*th)
	st.cov[key] = cov
	st.pix[key] = make([]uint8, tw*th*4)
	return cov
}

// recompose rebuilds one touched tile's premultiplied pixels from the tile's
// original content and the accumulated coverage: original + color × coverage
// × opacity (BrushStroke.publish, BrushStroke.swift:485-551). Opacity folds
// in exactly once here, which is what preserves the stroke-wide cap however
// many dabs overlap; rebuilding from the original on every read is what makes
// moving back and forth over one pixel idempotent within the stroke
// (BrushTests.swift:306-319).
func (st *Stroke) recompose(key, tx, ty int) []uint8 {
	tw, th := st.base.tileSize(tx, ty)
	pix := st.pix[key]
	base := st.base.Tiles[key]
	cov := st.cov[key]
	s := st.settings
	for i := 0; i < tw*th; i++ {
		srcA := float64(cov[i]) / 255 * s.Opacity
		o := i * 4
		var br, bg, bb, ba float64
		if base != nil {
			br, bg, bb, ba = float64(base[o]), float64(base[o+1]), float64(base[o+2]), float64(base[o+3])
		}
		if s.Erase {
			// destination-out (BrushStroke.swift:534-539): the coverage takes
			// the layer's alpha out, leaving the pixels under it transparent;
			// premultiplied channels all scale by the same factor.
			f := 1 - srcA
			pix[o] = u8round(br * f)
			pix[o+1] = u8round(bg * f)
			pix[o+2] = u8round(bb * f)
			pix[o+3] = u8round(ba * f)
			continue
		}
		// Source-over on premultiplied pixels: out = src + dst·(1−srcA), with
		// src = paintColor premultiplied by coverage × opacity — the Go form
		// of BrushRaster.fill's masked fill at setAlpha(opacity)
		// (BrushStroke.swift:91-103, 541).
		inv := 1 - srcA
		pix[o] = u8round(s.R*srcA*255 + br*inv)
		pix[o+1] = u8round(s.G*srcA*255 + bg*inv)
		pix[o+2] = u8round(s.B*srcA*255 + bb*inv)
		pix[o+3] = u8round(srcA*255 + ba*inv)
	}
	return pix
}

// snapshot assembles the stroke's state as a grid: touched tiles recomposed,
// every untouched tile sharing the base grid's slice verbatim — "snapshots
// copy at most those tiles, never the entire layer on a mouse-move event"
// (BrushStroke.swift:119-121).
func (st *Stroke) snapshot() *TileGrid {
	g := &TileGrid{
		W:     st.base.W,
		H:     st.base.H,
		Cols:  st.base.Cols,
		Rows:  st.base.Rows,
		Tiles: make([][]uint8, len(st.base.Tiles)),
	}
	for key := range g.Tiles {
		if _, touched := st.pix[key]; touched {
			g.Tiles[key] = st.recompose(key, key%st.base.Cols, key/st.base.Cols)
		} else {
			g.Tiles[key] = st.base.Tiles[key]
		}
	}
	return g
}

// View returns the live stroke as a grid — the counterpart of the stroke's
// patches during preview (BrushStroke.swift:189). The touched tiles are the
// stroke's own buffers: treat them as read-only, and know their contents keep
// changing until Commit. Preview and commit are byte-identical by
// construction.
func (st *Stroke) View() *TileGrid { return st.snapshot() }

// Commit freezes the stroke into an immutable snapshot and retires it:
// further Append/Line/Finish calls are no-ops. Undo is the base grid — the
// stroke never wrote into it — and redo is the returned grid.
func (st *Stroke) Commit() *TileGrid {
	g := st.snapshot()
	st.done = true
	return g
}

// isFinite reports whether v is a usable coordinate or setting; NaN and the
// infinities are rejected as in BrushStroke.swift:275 and :225-227.
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// u8round rounds a [0,255] float into a byte, matching the +0.5 rounding of
// premul (blend.go).
func u8round(v float64) uint8 { return uint8(min(255, max(0, v)) + 0.5) }
