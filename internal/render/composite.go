package render

import (
	"fmt"

	"compositor-win/internal/domain"
)

// PixelSource supplies layer/mask pixels for an asset file name, already
// decoded into the premultiplied working format. The bridge wires this to
// project.Store + PNG decode; tests use in-memory bitmaps.
type PixelSource func(imageFile string) (*Bitmap, error)

// scope holds per-group clip coverage rasters, chained to enclosing groups —
// a clipping mask's source is its lower contiguous sibling, found in the
// innermost scope that rendered it.
type scope struct {
	cov    map[string][]uint8
	parent *scope
}

func (s *scope) lookup(id string) ([]uint8, bool) {
	for sc := s; sc != nil; sc = sc.parent {
		if c, ok := sc.cov[id]; ok {
			return c, true
		}
	}
	return nil, false
}

// Render composites the whole document bottom-to-top into a premultiplied
// RGBA raster sized to the canvas. Group semantics follow project-format.md:
// folders are pass-through (Normal), their opacity multiplies the composite
// of their subtree, their mask covers the folder rectangle; layer masks and
// clipping-mask chains multiply coverage. Adjustment layers recolor the
// composite below them through their own LUT/cube kernels (ticket 10); the
// spatial M5 kinds (blurs, Grain, Add Noise) pass through until ticket 26.
func Render(doc *domain.Document, src PixelSource) (*Bitmap, error) {
	children := make(map[string][]int)
	var roots []int
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.ParentID == nil {
			roots = append(roots, i)
		} else {
			children[*l.ParentID] = append(children[*l.ParentID], i)
		}
	}
	out := NewBitmap(doc.Width, doc.Height)
	if err := renderScope(doc, roots, out, src, 1, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// renderScope composites one sibling run (a group's children, or the roots)
// into a scratch buffer, applies the group's mask and opacity, then blends
// the result onto target.
func renderScope(doc *domain.Document, idxs []int, target *Bitmap, src PixelSource, groupOpacity float64, group *domain.Layer) error {
	scratch := NewBitmap(doc.Width, doc.Height)
	sc := &scope{cov: map[string][]uint8{}}
	for _, i := range idxs {
		l := &doc.Layers[i]
		// An invisible layer hides itself; an invisible folder hides its
		// whole subtree (its children are only rendered through it).
		if !l.IsVisible {
			continue
		}
		if l.Adjustment != nil {
			// The Swift layer path skips an adjustment layer that takes its
			// mask from another layer's coverage entirely.
			if l.MaskSourceID != nil {
				continue
			}
			if err := renderAdjustment(doc, l, scratch, src); err != nil {
				return err
			}
			continue
		}
		if l.IsGroupLayer() {
			if err := renderScope(doc, children(doc, l.ID), scratch, src, opacityOf(l), l); err != nil {
				return err
			}
			continue
		}
		if err := renderLayer(doc, l, scratch, sc, src); err != nil {
			return err
		}
	}
	if group != nil && group.MaskFile != nil && maskEnabled(group) {
		cov, err := maskCoverage(doc, group, src)
		if err != nil {
			return err
		}
		multiplyCoverage(scratch, cov)
	}
	if groupOpacity < 1 {
		scaleAlpha(scratch, groupOpacity)
	}
	BlendBitmap(domain.BlendNormal, target, scratch, 1)
	return nil
}

// renderAdjustment ports the Swift adjusted(below:by:adjustment:) layer
// pass: the adjustment recolors the composite below it, then comes back
// through its own mask at its opacity. In a non-normal blend mode the
// adjusted straight colors blend with the below ones at full coverage and
// the original coverage is restored afterwards — as LiveMaskRenderer does,
// so soft edges aren't thickened.
func renderAdjustment(doc *domain.Document, l *domain.Layer, below *Bitmap, src PixelSource) error {
	changed := below.Clone()
	if !applyAdjustment(l.Adjustment, changed) {
		return nil // M5 kernel kinds pass through until their tickets land.
	}
	mode := domain.BlendNormal
	if l.BlendMode != nil {
		mode = *l.BlendMode
	}
	if mode != domain.BlendNormal {
		blendOpaque(mode, changed, below)
	}
	cov := make([]uint8, below.W*below.H)
	for i := range cov {
		cov[i] = 255
	}
	if l.MaskFile != nil && maskEnabled(l) {
		mcov, err := maskCoverage(doc, l, src)
		if err != nil {
			return err
		}
		for i := range cov {
			cov[i] = uint8(uint32(cov[i]) * uint32(mcov[i]) / 255)
		}
	}
	if op := opacityOf(l); op < 1 {
		k := uint32(op*255 + 0.5)
		for i := range cov {
			cov[i] = uint8(uint32(cov[i]) * k / 255)
		}
	}
	// The result fades linearly between the below composite and the adjusted
	// one — CIBlendWithRedMask is a premultiplied-domain crossfade, not an
	// alpha composite (fading the adjusted raster's own alpha would turn its
	// straight color white at partial coverage).
	for i := 0; i < len(below.Pix); i += 4 {
		c := uint32(cov[i/4])
		switch c {
		case 0:
			continue // below already holds the original
		case 255:
			below.Pix[i], below.Pix[i+1], below.Pix[i+2], below.Pix[i+3] =
				changed.Pix[i], changed.Pix[i+1], changed.Pix[i+2], changed.Pix[i+3]
		default:
			for ch := 0; ch < 4; ch++ {
				v := (float64(changed.Pix[i+ch])*float64(c) + float64(below.Pix[i+ch])*float64(255-c)) / 255
				below.Pix[i+ch] = uint8(v + 0.5)
			}
		}
	}
	return nil
}

// blendOpaque runs one blend-mode pass at full coverage: both rasters'
// straight colors blend mode-wise, and the result is premultiplied back by
// the below pixel's own alpha (its coverage comes back after the blend).
func blendOpaque(mode domain.BlendMode, changed, below *Bitmap) {
	for i := 0; i < len(below.Pix); i += 4 {
		a := below.Pix[i+3]
		if a == 0 {
			changed.Pix[i], changed.Pix[i+1], changed.Pix[i+2], changed.Pix[i+3] = 0, 0, 0, 0
			continue
		}
		dR, dG, dB := unpremul(below.Pix[i], below.Pix[i+1], below.Pix[i+2], a)
		sA := changed.Pix[i+3]
		var sR, sG, sB float64
		if sA == 0 {
			// The adjustment left nothing here; the below color blends with itself.
			sR, sG, sB = dR, dG, dB
		} else {
			sR, sG, sB = unpremul(changed.Pix[i], changed.Pix[i+1], changed.Pix[i+2], sA)
		}
		oR := BlendChannel(mode, dR, sR)
		oG := BlendChannel(mode, dG, sG)
		oB := BlendChannel(mode, dB, sB)
		changed.Pix[i], changed.Pix[i+1], changed.Pix[i+2], changed.Pix[i+3] = premul(oR, oG, oB, float64(a)/255)
	}
}

func children(doc *domain.Document, parentID string) []int {
	var out []int
	for i := range doc.Layers {
		if doc.Layers[i].ParentID != nil && *doc.Layers[i].ParentID == parentID {
			out = append(out, i)
		}
	}
	return out
}

func renderLayer(doc *domain.Document, l *domain.Layer, target *Bitmap, sc *scope, src PixelSource) error {
	var placed *Bitmap
	if l.ImageFile != nil {
		pixels, err := src(*l.ImageFile)
		if err != nil {
			return fmt.Errorf("图层 %s 加载 %s 失败: %w", l.ID, *l.ImageFile, err)
		}
		placed = Place(pixels, l.Transform, doc.Width, doc.Height)
	} else {
		placed = NewBitmap(doc.Width, doc.Height)
	}
	scaleAlpha(placed, opacityOf(l))

	if l.MaskFile != nil && maskEnabled(l) {
		cov, err := maskCoverage(doc, l, src)
		if err != nil {
			return err
		}
		multiplyCoverage(placed, cov)
	}
	if l.MaskSourceID != nil {
		if sourceCov, ok := sc.lookup(*l.MaskSourceID); ok {
			multiplyCoverage(placed, sourceCov)
		} else {
			// The source exists (validated) but rendered nothing — a blank
			// base clips the target away entirely.
			placed = NewBitmap(doc.Width, doc.Height)
		}
	}
	sc.cov[l.ID] = placed.coverage()

	mode := domain.BlendNormal
	if l.BlendMode != nil {
		mode = *l.BlendMode
	}
	BlendBitmap(mode, target, placed, 1)
	return nil
}

// maskCoverage renders a mask into document space. Linked masks (the
// default) follow the layer transform; unlinked masks keep their own
// maskPlacement. Coverage is the mask's grayscale value, not its alpha.
func maskCoverage(doc *domain.Document, l *domain.Layer, src PixelSource) ([]uint8, error) {
	mt := l.Transform
	if l.MaskPlacement != nil && (l.MaskLinked != nil && !*l.MaskLinked) {
		mt = *l.MaskPlacement
	}
	pixels, err := src(*l.MaskFile)
	if err != nil {
		return nil, fmt.Errorf("图层 %s 加载蒙版 %s 失败: %w", l.ID, *l.MaskFile, err)
	}
	placed := Place(pixels, mt, doc.Width, doc.Height)
	cov := make([]uint8, doc.Width*doc.Height)
	for i := range cov {
		cov[i] = placed.Pix[i*4] // grayscale PNG: R == luminance, alpha opaque
	}
	return cov, nil
}

func opacityOf(l *domain.Layer) float64 {
	if l.Opacity == nil {
		return 1
	}
	return *l.Opacity
}

func maskEnabled(l *domain.Layer) bool {
	return l.MaskEnabled == nil || *l.MaskEnabled
}

// multiplyCoverage scales a bitmap's premultiplied pixels by a coverage
// raster (0–255 per pixel).
func multiplyCoverage(b *Bitmap, cov []uint8) {
	if len(cov) < b.W*b.H {
		return
	}
	for i := 0; i < len(b.Pix); i += 4 {
		c := uint32(cov[i/4])
		if c == 255 {
			continue
		}
		if c == 0 {
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 0, 0, 0, 0
			continue
		}
		b.Pix[i] = uint8(uint32(b.Pix[i]) * c / 255)
		b.Pix[i+1] = uint8(uint32(b.Pix[i+1]) * c / 255)
		b.Pix[i+2] = uint8(uint32(b.Pix[i+2]) * c / 255)
		b.Pix[i+3] = uint8(uint32(b.Pix[i+3]) * c / 255)
	}
}

// scaleAlpha folds a layer's opacity into its premultiplied pixels.
func scaleAlpha(b *Bitmap, opacity float64) {
	if opacity >= 1 {
		return
	}
	k := uint32(opacity*255 + 0.5)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i] = uint8(uint32(b.Pix[i]) * k / 255)
		b.Pix[i+1] = uint8(uint32(b.Pix[i+1]) * k / 255)
		b.Pix[i+2] = uint8(uint32(b.Pix[i+2]) * k / 255)
		b.Pix[i+3] = uint8(uint32(b.Pix[i+3]) * k / 255)
	}
}
