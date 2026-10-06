package render

import (
	"errors"
	"math"
)

// Clone Stamp and the fill command family — the CPU port of the macOS
// tools (semantic authorities, read-only):
//
//   - Clone Stamp: EditorSession's clone state and CloneStamp.swift
//     (reference/Swift/Compositor/Document/CloneStamp.swift), painting through
//     the clone branch of BrushStroke.publish (BrushStroke.swift:506-525).
//   - Fill: EditorSession.fillSelection and clearSelectedPixels
//     (SelectionEdits.swift:53-72) painting through BrushStroke.fill and
//     clearPixels (BrushStroke.swift:706-718).
//
// The tip and blend reuse the brush engine's parts (brush.go): tipCoverage
// for the profile, u8round for the byte rounding, isFinite for the guards.
// Coordinates are canvas pixels — the Go grid is the canvas, as in brush.go,
// so the original's layer-transform plumbing (pixelToDocument,
// cloneSample's gridRect placement) drops out.
//
// Pixel invariants (AGENTS.md): bitmaps are the package's premultiplied
// 8-bit RGBA in sRGB; masks are 8-bit grayscale. Stamping and filling mix
// premultiplied pixels channel-uniformly, which stays compositing-correct;
// all math runs in sRGB like the original.

// ErrInvalidStampFill reports stamp or fill parameters the engine refuses:
// nil or empty bitmaps, a zero/negative or non-finite radius, non-finite
// points, colors or settings, a wrong-sized selection mask, or an unknown
// fill mode. The callers get the bitmap back untouched.
var ErrInvalidStampFill = errors.New("render: stamp/fill parameters out of range")

// Color is a straight (non-premultiplied) sRGB color, components in [0,1].
// Out-of-range components clamp like CGColor's would.
type Color struct{ R, G, B, A float64 }

// FillMode is the fill command's paint (feature parity C15/C16). The
// foreground fill (⌥⌫), the background fill (⌘⌫) and clearing the
// selection's pixels (Delete).
type FillMode string

const (
	FillForeground FillMode = "foreground"
	FillBackground FillMode = "background"
	FillClear      FillMode = "clear"
)

// CloneStamp carries the clone stamp tool's source state across ticks and
// strokes: the ⌥-click source, and the first-tick anchor that aligned mode's
// fixed offset hangs from — EditorSession's cloneSource and cloneOffset
// (EditorSession.swift:218-226, CloneStamp.swift:12-48).
//
// Sample All Layers needs no engine support: the original composites the live
// document and uses that composite as the sample, single-layer sampling uses
// the layer's own pixels (cloneSample, CloneStamp.swift:39-48) — either way
// StampTick only sees src, so the choice belongs to the caller, which passes
// the canvas composite or the layer bitmap accordingly.
type CloneStamp struct {
	// Opacity caps the stamp's strength, 0–1, like the options bar's 1–0
	// keys (EditorSession.typeOpacityDigit). The zero value means the tool's
	// full-strength default — the options bar's floor is 1%, so 0 never
	// arrives from the UI; a negative value clamps to an inert stamp.
	Opacity float64

	// Hardness is the tip profile, 0–1: 0 is the clone stamp's own soft
	// default — feathered across the whole radius with the brush falloff —
	// and 1 a hard circle with an antialiased rim (the tool keeps its own
	// tip: CloneStampTests.cloneStampKeepsItsOwnSoftBrushTip). The zero
	// value is the tool's default.
	Hardness float64

	// srcPoint is the ⌥-click source, kept for Source(); the tick call
	// passes it through so a tick is self-describing.
	srcPoint  Point
	hasSource bool
	// anchor is the first tick's brush point of the current alignment; the
	// aligned offset fixes as srcPoint − anchor (the counterpart of
	// cloneStrokeOffset's stored offset, CloneStamp.swift:23-27).
	anchor    Point
	hasAnchor bool
}

// SetCloneSource is ⌥-click (EditorSession.setCloneSource, CloneStamp.swift:
// 14-18): records the point stamps copy from and starts a new alignment —
// the offset re-fixes at the next tick. Non-finite points are ignored, as in
// the original's guard, and change neither the source nor the alignment.
func (cs *CloneStamp) SetCloneSource(p Point) {
	if !isFinite(p.X) || !isFinite(p.Y) {
		return
	}
	cs.srcPoint = p
	cs.hasSource = true
	cs.hasAnchor = false
}

// Source reports the point SetCloneSource recorded — the crosshair's anchor
// (cloneSamplePoint) and the session's "⌥-click first" refusal both read it.
// False before the first ⌥-click.
func (cs *CloneStamp) Source() (Point, bool) {
	if !cs.hasSource {
		return Point{}, false
	}
	return cs.srcPoint, true
}

// StampTick copies one brush-tip dab from src onto dst: the sample, shifted
// by the whole-pixel source offset, painted through the tip's coverage — the
// CPU port of the clone branch of BrushStroke.publish (BrushStroke.swift:
// 506-525), reduced to canvas pixels like brush.go's dab.
//
// srcPoint is the ⌥-click source (what SetCloneSource recorded); dstPoint is
// the brush; radius is half the tip diameter. aligned picks the offset:
//
//   - aligned: the offset fixes at this alignment's first tick — offset =
//     srcPoint − first dstPoint — and the source translates with the brush:
//     the sample sits at srcPoint + (dst − firstDst). Later ticks and strokes
//     keep that offset, the way the original's aligned strokes keep
//     cloneStrokeOffset's stored one (CloneStamp.swift:23-27); a
//     SetCloneSource resets it. (One session-layer nuance: the original also
//     rewrites cloneOffset at every stroke start, aligned or not
//     (EditorSession+Brush.swift:73), so aligned mode resumes from the last
//     stroke's offset; a session reproduces that by calling SetCloneSource
//     with the same source at the stroke start.)
//   - unaligned: srcPoint is the sample center itself — every tick copies
//     from the source point, so every stroke starts again at the source
//     (CloneStampTests' third stroke). The original additionally tracks the
//     stroke-start offset within one stroke; a session wanting that passes a
//     stroke-start-adjusted srcPoint per tick.
//
// The offset rounds to whole pixels once per alignment (CloneStamp.swift:26's
// .rounded()), so samples land 1:1 on the grid with nothing to resample.
// Where the sample doesn't reach — a source position outside src — nothing
// paints (BrushStroke.swift:507-508).
//
// mask (nil = stamp everywhere) is the selection clip, dst-sized 8-bit
// grayscale in dst pixel order: its 0–255 value scales the dab's strength,
// so a stamp inside a feathered selection lands proportionally. Unlike
// WarpStroke's lenient fallback, a wrong-sized mask is an error: a silently
// ignored clip would stamp outside the selection.
//
// The edit is in place: dst holds the result on nil return. Undo wiring is
// the caller's, per stroke rather than per tick: snapshot dst (Bitmap.Clone)
// before the stroke's first tick and register the pair as one history step
// at pen-up — the Go counterpart of commitPaintSnapshot naming the edit
// "Clone Stamp" (EditorSession+Brush.swift:154-167). The engine touches no
// history itself.
func (cs *CloneStamp) StampTick(dst *Bitmap, mask []uint8, src *Bitmap, srcPoint, dstPoint Point, radius float64, aligned bool) error {
	switch {
	case dst == nil || src == nil || dst.W < 1 || dst.H < 1 || src.W < 1 || src.H < 1:
		return ErrInvalidStampFill
	case !isFinite(radius) || radius <= 0:
		return ErrInvalidStampFill
	case !isFinite(srcPoint.X) || !isFinite(srcPoint.Y),
		!isFinite(dstPoint.X) || !isFinite(dstPoint.Y),
		!isFinite(cs.Hardness) || !isFinite(cs.Opacity):
		return ErrInvalidStampFill
	case mask != nil && len(mask) != dst.W*dst.H:
		return ErrInvalidStampFill
	}
	hardness := min(1, max(0, cs.Hardness))
	opacity := cs.stampOpacity()

	// The whole-pixel offset, fixed per alignment (aligned) or per tick
	// (unaligned): source − brush, rounded (CloneStamp.swift:23-27).
	var offX, offY float64
	if aligned {
		if !cs.hasAnchor {
			cs.anchor = dstPoint
			cs.hasAnchor = true
		}
		offX = math.Round(srcPoint.X - cs.anchor.X)
		offY = math.Round(srcPoint.Y - cs.anchor.Y)
	} else {
		offX = math.Round(srcPoint.X - dstPoint.X)
		offY = math.Round(srcPoint.Y - dstPoint.Y)
	}

	// The dab's pixel box, with a pixel of room for the hard tip's
	// antialiasing ramp; the soft profile is exactly zero past the rim.
	x0 := max(0, int(math.Floor(dstPoint.X-radius))-1)
	x1 := min(dst.W-1, int(math.Ceil(dstPoint.X+radius))+1)
	y0 := max(0, int(math.Floor(dstPoint.Y-radius))-1)
	y1 := min(dst.H-1, int(math.Ceil(dstPoint.Y+radius))+1)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c := tipCoverage(hardness, radius,
				math.Hypot(float64(x)+0.5-dstPoint.X, float64(y)+0.5-dstPoint.Y))
			if c <= 0 {
				continue
			}
			if mask != nil {
				w := float64(mask[y*dst.W+x]) / 255
				if w <= 0 {
					continue // the selection leaves this pixel alone
				}
				c *= w
			}
			a := c * opacity
			sx, sy := x+int(offX), y+int(offY)
			if sx < 0 || sy < 0 || sx >= src.W || sy >= src.H {
				continue // where the sample doesn't reach, nothing paints
			}
			// Source-over on premultiplied pixels: out = src·a + dst·(1−a),
			// channel-uniform because src is already premultiplied — the Go
			// form of drawing the sample through the coverage at
			// setAlpha(opacity) (BrushStroke.swift:514-525).
			di := (y*dst.W + x) * 4
			si := (sy*src.W + sx) * 4
			inv := 1 - a
			dst.Pix[di] = u8round(float64(src.Pix[si])*a + float64(dst.Pix[di])*inv)
			dst.Pix[di+1] = u8round(float64(src.Pix[si+1])*a + float64(dst.Pix[di+1])*inv)
			dst.Pix[di+2] = u8round(float64(src.Pix[si+2])*a + float64(dst.Pix[di+2])*inv)
			dst.Pix[di+3] = u8round(float64(src.Pix[si+3])*a + float64(dst.Pix[di+3])*inv)
		}
	}
	return nil
}

// stampOpacity resolves the Opacity field: 0 means unset (the tool's
// full-strength default), negatives clamp inert, anything above 1 clamps.
func (cs *CloneStamp) stampOpacity() float64 {
	switch {
	case cs.Opacity == 0:
		return 1
	case cs.Opacity < 0:
		return 0
	default:
		return min(1, cs.Opacity)
	}
}

// FillColor ports the fill command family (feature parity C15/C16): the
// foreground fill (⌥⌫), the background fill (⌘⌫) and clearing the
// selection's pixels (Delete) — EditorSession.fillSelection and
// clearSelectedPixels (SelectionEdits.swift:53-72) painting through
// BrushStroke.fill and clearPixels (BrushStroke.swift:706-718).
//
// mode picks the paint. FillForeground and FillBackground both fill with
// color — the original resolves the palette's foreground or background color
// in fillSelection before the engine sees it, so the two differ only in the
// color the caller passes. The fill is an opaque source-over: it replaces
// what was there, including fully transparent pixels
// (BrushStroke.fill's plain setFillColor + fill). FillClear is
// destination-out: existing pixels scale toward transparent by the coverage,
// full coverage clearing to zero (clearPixels's .destinationOut); where
// nothing was, nothing is.
//
// mask is the selection clip in bmp pixel order, 8-bit grayscale: only
// mask > 0 pixels change, and the 0–255 value scales the operation, so an
// antialiased or feathered edge lands proportionally — exactly what the
// original's selectionClip.apply does (BrushStroke.paintCanvas). nil fills
// the whole layer: "With no selection it fills the whole layer; an empty
// selection fills nothing" (SelectionEdits.swift:51) — the empty selection
// is an all-zero mask. w and h are the raster's extents, passed explicitly
// like WandMask's; they must match bmp.
//
// The edit is in place: bmp holds the result on nil return. Undo wiring is
// the caller's: snapshot bmp (Bitmap.Clone) BEFORE the call and register the
// pair as one history step after — the Go counterpart of applyPixelEdit →
// commitRasterEdit wrapping beginEdit("Fill"/"Fill Mask"/"Clear") … endEdit
// (SelectionEdits.swift:62, 71, 213-223). One call is one undo step. The
// engine touches no history itself.
func FillColor(bmp *Bitmap, mask []uint8, w, h int, color Color, mode FillMode) error {
	switch {
	case bmp == nil || w < 1 || h < 1 || w != bmp.W || h != bmp.H:
		return ErrInvalidStampFill
	case mask != nil && len(mask) != w*h:
		return ErrInvalidStampFill
	case !isFinite(color.R) || !isFinite(color.G) || !isFinite(color.B) || !isFinite(color.A):
		return ErrInvalidStampFill
	}
	switch mode {
	case FillForeground, FillBackground:
		// Opaque source-over, the fill's coverage folded into the source
		// alpha: out = color·cov + dst·(1−cov) in straight terms, computed on
		// premultiplied bytes exactly as brush.go's recompose does
		// (BrushRaster.fill at setAlpha 1 through the selection clip).
		a := clamp01(color.A)
		r := clamp01(color.R) * a
		g := clamp01(color.G) * a
		b := clamp01(color.B) * a
		for y := 0; y < h; y++ {
			row := y * w
			for x := 0; x < w; x++ {
				cov := 1.0
				if mask != nil {
					cov = float64(mask[row+x]) / 255
					if cov <= 0 {
						continue // outside the selection: untouched
					}
				}
				srcA := a * cov
				inv := 1 - srcA
				i := (row + x) * 4
				bmp.Pix[i] = u8round(r*srcA*255 + float64(bmp.Pix[i])*inv)
				bmp.Pix[i+1] = u8round(g*srcA*255 + float64(bmp.Pix[i+1])*inv)
				bmp.Pix[i+2] = u8round(b*srcA*255 + float64(bmp.Pix[i+2])*inv)
				bmp.Pix[i+3] = u8round(srcA*255 + float64(bmp.Pix[i+3])*inv)
			}
		}
	case FillClear:
		// destination-out (BrushStroke.swift:714-718): every premultiplied
		// channel scales by (1 − coverage); full coverage clears to zero,
		// and transparent pixels stay transparent.
		for y := 0; y < h; y++ {
			row := y * w
			for x := 0; x < w; x++ {
				cov := 1.0
				if mask != nil {
					cov = float64(mask[row+x]) / 255
					if cov <= 0 {
						continue
					}
				}
				f := 1 - cov
				i := (row + x) * 4
				bmp.Pix[i] = u8round(float64(bmp.Pix[i]) * f)
				bmp.Pix[i+1] = u8round(float64(bmp.Pix[i+1]) * f)
				bmp.Pix[i+2] = u8round(float64(bmp.Pix[i+2]) * f)
				bmp.Pix[i+3] = u8round(float64(bmp.Pix[i+3]) * f)
			}
		}
	default:
		return ErrInvalidStampFill
	}
	return nil
}
