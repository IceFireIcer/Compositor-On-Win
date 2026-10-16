package render

import (
	"fmt"
	"math"

	"compositor-win/internal/domain"
)

// strokeOf follows LayerEffects.visible: an omitted enabled field means that
// the effect is enabled. Even an inactive visible record contributes padding
// and selects the source-grid mask path, as LayerEffectsRenderer.render does.
func strokeOf(effects *domain.Effects) *domain.StrokeEffect {
	if effects == nil || effects.Stroke == nil {
		return nil
	}
	stroke := effects.Stroke
	if stroke.Enabled != nil && !*stroke.Enabled {
		return nil
	}
	return stroke
}

// colorOverlayOf follows LayerEffects.visible in Swift: old projects omit
// enabled, which means visible. Shadow and glow kernels are not implemented here.
func colorOverlayOf(effects *domain.Effects) *domain.OverlayEffect {
	if effects == nil || effects.ColorOverlay == nil {
		return nil
	}
	overlay := effects.ColorOverlay
	if (overlay.Enabled != nil && !*overlay.Enabled) || overlay.Opacity <= 0 {
		return nil
	}
	return overlay
}

// shadowOf / innerShadowOf / outerGlowOf / innerGlowOf follow LayerEffects.visible
// (LayerEffects.swift): a missing `enabled` field means the effect is visible, so
// old projects keep the effects they were saved with.
func shadowOf(effects *domain.Effects) *domain.ShadowEffect {
	if effects == nil || effects.Shadow == nil {
		return nil
	}
	s := effects.Shadow
	if s.Enabled != nil && !*s.Enabled {
		return nil
	}
	return s
}

func innerShadowOf(effects *domain.Effects) *domain.ShadowEffect {
	if effects == nil || effects.InnerShadow == nil {
		return nil
	}
	s := effects.InnerShadow
	if s.Enabled != nil && !*s.Enabled {
		return nil
	}
	return s
}

func outerGlowOf(effects *domain.Effects) *domain.GlowEffect {
	if effects == nil || effects.OuterGlow == nil {
		return nil
	}
	g := effects.OuterGlow
	if g.Enabled != nil && !*g.Enabled {
		return nil
	}
	return g
}

func innerGlowOf(effects *domain.Effects) *domain.GlowEffect {
	if effects == nil || effects.InnerGlow == nil {
		return nil
	}
	g := effects.InnerGlow
	if g.Enabled != nil && !*g.Enabled {
		return nil
	}
	return g
}

// effectsMargin ports LayerEffectsRenderer.margin (LayerEffects.swift:435-446):
// the room the effects need around the layer's pixels. An outside stroke asks for
// its width, a shadow for its distance plus three blur radii, an outer glow for
// three sizes; the largest wins, then ceil and two pixels of slack. An inside
// stroke and the recoloring effects (color overlay, inner glow, inner shadow)
// need no room but still take the two.
func effectsMargin(effects *domain.Effects) int {
	margin := 0.0
	needs := false
	if s := strokeOf(effects); s != nil {
		needs = true
		if !s.Inside {
			margin = math.Max(margin, s.Size)
		}
	}
	if s := shadowOf(effects); s != nil {
		needs = true
		margin = math.Max(margin, s.Distance+s.Blur*3)
	}
	if g := outerGlowOf(effects); g != nil {
		needs = true
		margin = math.Max(margin, g.Size*3)
	}
	if !needs {
		return 0
	}
	return int(math.Ceil(margin)) + 2
}

// applyColorOverlay ports LayerEffectsRenderer.render's coverage fill and
// MetalLayerEffects.effects_compose. The source is already masked, in layer
// pixels, before placement and layer/group opacity. This is source-over, not
// an RGB replacement: on a soft edge it also increases alpha. Keep this math
// in premultiplied sRGB, matching the original rather than linear light.
func applyColorOverlay(shown *Bitmap, overlay *domain.OverlayEffect) {
	for i := 0; i < len(shown.Pix); i += 4 {
		alpha := float64(shown.Pix[i+3])
		coverage := alpha / 255 * overlay.Opacity
		if coverage == 0 {
			continue
		}
		shown.Pix[i] = uint8(overlay.Red*coverage*255 + float64(shown.Pix[i])*(1-coverage) + 0.5)
		shown.Pix[i+1] = uint8(overlay.Green*coverage*255 + float64(shown.Pix[i+1])*(1-coverage) + 0.5)
		shown.Pix[i+2] = uint8(overlay.Blue*coverage*255 + float64(shown.Pix[i+2])*(1-coverage) + 0.5)
		shown.Pix[i+3] = uint8(coverage*255 + alpha*(1-coverage) + 0.5)
	}
}

// invertCoverage is the "1 - level" the inner effects read: strongest at the
// layer's edge and fading inward (LayerEffects.swift:518-523,560-566).
func invertCoverage(levels []uint8) []uint8 {
	if levels == nil {
		return nil
	}
	out := make([]uint8, len(levels))
	for i, v := range levels {
		out[i] = 255 - v
	}
	return out
}

// effectRecolorAtop is LayerEffectsRenderer.recolor (LayerEffects.swift:541-549):
// source-atop, so the color lands only where the layer shows and the pixels keep
// their own alpha. dst is the layer's own buffer, so its alpha is the gate.
func effectRecolorAtop(dst *[4]float64, red, green, blue, opacity float64, coverage uint8) {
	amount := opacity * float64(coverage) / 255 * dst[3]
	if amount <= 0 {
		return
	}
	dst[0] = red*amount + dst[0]*(1-amount)
	dst[1] = green*amount + dst[1]*(1-amount)
	dst[2] = blue*amount + dst[2]*(1-amount)
	// dst[3] is unchanged: source-atop keeps the destination's alpha.
}

// applyInPlaceEffects draws the effects that need no room around the layer: the
// color overlay and the two inner effects, in LayerEffectsRenderer.render's
// order. The inner effects read the alpha the layer had before the overlay
// touched it, as upstream's separate layer buffer does.
func applyInPlaceEffects(shown *Bitmap, overlay *domain.OverlayEffect, innerGlow *domain.GlowEffect, innerShadow *domain.ShadowEffect) {
	needsInner := (innerGlow != nil && innerGlow.Opacity > 0) || (innerShadow != nil && innerShadow.Opacity > 0)
	var shape []uint8
	if needsInner && shown.W > 0 && shown.H > 0 {
		shape = make([]uint8, shown.W*shown.H)
		for i := range shape {
			shape[i] = shown.Pix[i*4+3]
		}
	}
	if overlay != nil {
		applyColorOverlay(shown, overlay)
	}
	if innerGlow != nil && innerGlow.Opacity > 0 {
		plane := invertCoverage(effectCoverage(shape, shown.W, shown.H, 0, 0, innerGlow.Size))
		recolorAtopBitmap(shown, plane, innerGlow.Red, innerGlow.Green, innerGlow.Blue, innerGlow.Opacity)
	}
	if innerShadow != nil && innerShadow.Opacity > 0 {
		dx, dy := shadowOffset(innerShadow)
		plane := invertCoverage(effectCoverage(shape, shown.W, shown.H, dx, dy, innerShadow.Blur))
		recolorAtopBitmap(shown, plane, innerShadow.Red, innerShadow.Green, innerShadow.Blue, innerShadow.Opacity)
	}
}

func recolorAtopBitmap(shown *Bitmap, plane []uint8, red, green, blue, opacity float64) {
	if plane == nil {
		return
	}
	for i := range plane {
		j := i * 4
		var dst [4]float64
		for ch := 0; ch < 4; ch++ {
			dst[ch] = float64(shown.Pix[j+ch]) / 255
		}
		effectRecolorAtop(&dst, red, green, blue, opacity, plane[i])
		for ch := 0; ch < 4; ch++ {
			shown.Pix[j+ch] = uint8(max(0, min(1, dst[ch]))*255 + 0.5)
		}
	}
}

// renderLayerEffects ports LayerEffectsRenderer.render's CPU compose order:
// beneath the layer the drop shadow, the outer glow and an outside stroke; on
// the layer its own pixels, the color overlay, the inner glow and the inner
// shadow; on top an inside stroke. The morphology and coverage inputs are the
// shown source alpha before the overlay changes it. It returns the padded
// raster and the symmetric inset used to compensate the layer transform in
// document space.
//
// The effects beneath and the layer itself go into separate buffers because an
// inner effect is source-atop on the *layer's* alpha, not on the composite's:
// composing layer-over-under afterwards is the same arithmetic the single-pass
// form used, and it stops an inner glow from painting over the drop shadow.
func renderLayerEffects(shown *Bitmap, effects *domain.Effects) (*Bitmap, int, error) {
	overlay := colorOverlayOf(effects)
	stroke := strokeOf(effects)
	shadow := shadowOf(effects)
	glow := outerGlowOf(effects)
	innerGlow := innerGlowOf(effects)
	innerShadow := innerShadowOf(effects)
	inset := effectsMargin(effects)
	if inset == 0 {
		applyInPlaceEffects(shown, overlay, innerGlow, innerShadow)
		return shown, 0, nil
	}
	if shown.W <= 0 || shown.H <= 0 || shown.W > int(^uint(0)>>1)-2*inset || shown.H > int(^uint(0)>>1)-2*inset {
		return nil, 0, fmt.Errorf("特效 padding 尺寸溢出: %dx%d + %d", shown.W, shown.H, inset)
	}
	width, height := shown.W+2*inset, shown.H+2*inset
	if width <= 0 || height <= 0 || width > domain.MaxSurfacePixels/height {
		return nil, 0, fmt.Errorf("特效 surface 过大: %dx%d（上限 %d 像素）", width, height, domain.MaxSurfacePixels)
	}

	// Keep the original source alpha in the padded grid. This is also the
	// source used for the inside/outside ring, so an overlay cannot thicken or
	// shrink a later stroke.
	shape := make([]uint8, width*height)
	for y := 0; y < shown.H; y++ {
		for x := 0; x < shown.W; x++ {
			shape[(y+inset)*width+x+inset] = shown.Pix[(y*shown.W+x)*4+3]
		}
	}
	var ring []uint8
	if stroke != nil && stroke.Size > 0 && stroke.Opacity > 0 {
		ring = strokeRingCoverage(shape, width, height, max(1, int(math.Round(stroke.Size))), stroke.Inside)
	}
	// The coverage planes the effects beneath the layer need, all built from
	// the shown alpha before the overlay or an inside stroke can change it.
	var shadowPlane, glowPlane []uint8
	if shadow != nil && shadow.Opacity > 0 {
		dx, dy := shadowOffset(shadow)
		shadowPlane = effectCoverage(shape, width, height, dx, dy, shadow.Blur)
	}
	if glow != nil && glow.Opacity > 0 {
		// outerGlowCoverage: the softened shape with the interior taken out,
		// so the glow lights the room around the layer and not the layer itself.
		soft := effectCoverage(shape, width, height, 0, 0, glow.Size)
		glowPlane = make([]uint8, len(soft))
		for i := range soft {
			glowPlane[i] = uint8(float64(soft[i]) * (1 - float64(shape[i])/255))
		}
	}
	// innerGlowCoverage / innerCoverage: 1 - level, strongest at the layer's
	// edge and fading inward. They recolor the layer's own pixels, never the
	// effects beneath it.
	var innerGlowPlane, innerShadowPlane []uint8
	if innerGlow != nil && innerGlow.Opacity > 0 {
		soft := effectCoverage(shape, width, height, 0, 0, innerGlow.Size)
		innerGlowPlane = invertCoverage(soft)
	}
	if innerShadow != nil && innerShadow.Opacity > 0 {
		dx, dy := shadowOffset(innerShadow)
		innerShadowPlane = invertCoverage(effectCoverage(shape, width, height, dx, dy, innerShadow.Blur))
	}

	result := NewBitmap(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*width + x
			alpha := float64(shape[i]) / 255
			// Beneath the layer: the drop shadow, the outer glow, an outside stroke.
			var under [4]float64
			if shadowPlane != nil {
				effectColorOver(&under, shadow.Red, shadow.Green, shadow.Blue,
					float64(shadowPlane[i])/255*shadow.Opacity)
			}
			if glowPlane != nil {
				effectColorOver(&under, glow.Red, glow.Green, glow.Blue,
					float64(glowPlane[i])/255*glow.Opacity)
			}
			var strokeAlpha float64
			if ring != nil {
				strokeAlpha = float64(ring[i]) / 255 * stroke.Opacity
			}
			if stroke != nil && !stroke.Inside {
				effectColorOver(&under, stroke.Red, stroke.Green, stroke.Blue, strokeAlpha)
			}
			// The layer's own pixels, with the recoloring effects on them. Composing
			// them in their own buffer and drawing that over the effects is the same
			// arithmetic as the single-pass form it replaces, and it keeps an inner
			// effect gated on the layer's alpha rather than the composite's.
			var layer [4]float64
			if x >= inset && x < width-inset && y >= inset && y < height-inset {
				si := ((y-inset)*shown.W + x - inset) * 4
				for ch := range layer {
					layer[ch] = float64(shown.Pix[si+ch]) / 255
				}
			}
			if overlay != nil {
				// Overlay uses the shown alpha, not the accumulated outside
				// stroke/source coverage. Compose before quantizing, as Metal.
				effectColorOver(&layer, overlay.Red, overlay.Green, overlay.Blue, alpha*overlay.Opacity)
			}
			// The inner effects recolor the layer's own pixels (source-atop, so
			// they keep their alpha and a translucent pixel takes them fully).
			if innerGlowPlane != nil {
				effectRecolorAtop(&layer, innerGlow.Red, innerGlow.Green, innerGlow.Blue,
					innerGlow.Opacity, innerGlowPlane[i])
			}
			if innerShadowPlane != nil {
				effectRecolorAtop(&layer, innerShadow.Red, innerShadow.Green, innerShadow.Blue,
					innerShadow.Opacity, innerShadowPlane[i])
			}
			color := under
			for ch := range color {
				color[ch] = layer[ch] + color[ch]*(1-layer[3])
			}
			if stroke != nil && stroke.Inside {
				effectColorOver(&color, stroke.Red, stroke.Green, stroke.Blue, strokeAlpha)
			}
			for ch, value := range color {
				result.Pix[i*4+ch] = uint8(max(0, min(1, value))*255 + 0.5)
			}
		}
	}
	return result, inset, nil
}

// strokeRingCoverage is the byte-exact equivalent of Swift's two sliding
// window morphology passes. The deque makes wide strokes O(width*height),
// independent of the requested radius. Erosion treats a window that reaches
// outside the raster as zero; dilation naturally treats outside as zero.
func strokeRingCoverage(shape []uint8, width, height, reach int, inside bool) []uint8 {
	if width <= 0 || height <= 0 || len(shape) != width*height {
		return nil
	}
	moved := extremeCoverage(shape, width, height, reach, inside)
	ring := make([]uint8, len(shape))
	for i := range ring {
		if inside {
			if shape[i] > moved[i] {
				ring[i] = shape[i] - moved[i]
			}
		} else if moved[i] > shape[i] {
			ring[i] = moved[i] - shape[i]
		}
	}
	return ring
}

func extremeCoverage(source []uint8, width, height, reach int, smallest bool) []uint8 {
	if width <= 0 || height <= 0 || len(source) != width*height {
		return nil
	}
	radius := max(0, reach)
	pass := make([]uint8, len(source))
	result := make([]uint8, len(source))
	queue := make([]int, max(width, height))
	sweep := func(input, output []uint8, lines, count, lineStep, elementStep int) {
		for line := 0; line < lines; line++ {
			base := line * lineStep
			head, tail, next := 0, 0, 0
			for center := 0; center < count; center++ {
				for next <= min(count-1, center+radius) {
					value := input[base+next*elementStep]
					for tail > head {
						previous := input[base+queue[tail-1]*elementStep]
						if smallest && previous < value || !smallest && previous > value {
							break
						}
						tail--
					}
					queue[tail] = next
					tail++
					next++
				}
				for head < tail && queue[head] < center-radius {
					head++
				}
				if smallest && (center < radius || center+radius >= count) {
					output[base+center*elementStep] = 0
				} else {
					output[base+center*elementStep] = input[base+queue[head]*elementStep]
				}
			}
		}
	}
	sweep(source, pass, height, width, width, 1)
	sweep(pass, result, width, height, 1, width)
	return result
}

func effectColorOver(dst *[4]float64, red, green, blue, alpha float64) {
	dst[0] = red*alpha + dst[0]*(1-alpha)
	dst[1] = green*alpha + dst[1]*(1-alpha)
	dst[2] = blue*alpha + dst[2]*(1-alpha)
	dst[3] = alpha + dst[3]*(1-alpha)
}

func effectPlacedTransform(transform domain.Transform, sourceWidth, sourceHeight, inset int) domain.Transform {
	if inset <= 0 || sourceWidth <= inset*2 || sourceHeight <= inset*2 {
		return transform
	}
	grown := transform
	width, height := float64(sourceWidth), float64(sourceHeight)
	grown.Size = [2]float64{
		transform.Size[0] * width / (width - float64(inset*2)),
		transform.Size[1] * height / (height - float64(inset*2)),
	}
	grown.Origin = [2]float64{
		transform.Origin[0] + transform.Size[0]/2 - grown.Size[0]/2,
		transform.Origin[1] + transform.Size[1]/2 - grown.Size[1]/2,
	}
	return grown
}

// maskEffectSource mirrors LayerMask.clipImage + LayerEffectsRenderer.masked.
// A separately placed mask is sampled into the source layer's grid. Linking
// controls whether future edits move the placement; a stored placement applies
// regardless of the linked flag. Outside that placement Swift extends the
// mask with white or black according to the majority of its border pixels.
func maskEffectSource(shown *Bitmap, l *domain.Layer, src PixelSource) error {
	mask, err := src(*l.MaskFile)
	if err != nil {
		return fmt.Errorf("图层 %s 加载蒙版 %s 失败: %w", l.ID, *l.MaskFile, err)
	}
	cov := make([]uint8, shown.W*shown.H)
	if l.MaskPlacement == nil || sameEffectPlacement(*l.MaskPlacement, l.Transform) {
		local := domain.Transform{Size: [2]float64{float64(shown.W), float64(shown.H)}, Sampling: domain.SamplingNearest}
		placed := Place(mask, local, shown.W, shown.H)
		for i := range cov {
			cov[i] = placed.Pix[i*4]
		}
	} else {
		placement := *l.MaskPlacement
		background := effectMaskBackground(mask)
		lc, ls := math.Cos(l.Transform.Rotation*math.Pi/180), math.Sin(l.Transform.Rotation*math.Pi/180)
		mc, ms := math.Cos(placement.Rotation*math.Pi/180), math.Sin(placement.Rotation*math.Pi/180)
		for y := 0; y < shown.H; y++ {
			for x := 0; x < shown.W; x++ {
				// Layer pixel center -> document -> mask unit square.
				ux, uy := (float64(x)+0.5)/float64(shown.W)-0.5, (float64(y)+0.5)/float64(shown.H)-0.5
				if l.Transform.FlipX {
					ux = -ux
				}
				if l.Transform.FlipY {
					uy = -uy
				}
				dx, dy := ux*l.Transform.Size[0], uy*l.Transform.Size[1]
				px := l.Transform.Origin[0] + l.Transform.Size[0]/2 + lc*dx - ls*dy
				py := l.Transform.Origin[1] + l.Transform.Size[1]/2 + ls*dx + lc*dy
				dx, dy = px-placement.Origin[0]-placement.Size[0]/2, py-placement.Origin[1]-placement.Size[1]/2
				mx, my := (mc*dx+ms*dy)/placement.Size[0], (-ms*dx+mc*dy)/placement.Size[1]
				if placement.FlipX {
					mx = -mx
				}
				if placement.FlipY {
					my = -my
				}
				mx, my = mx+0.5, my+0.5
				c := background
				if mx >= 0 && mx < 1 && my >= 0 && my < 1 {
					c = sampleEffectMask(mask, mx*float64(mask.W), my*float64(mask.H))
				}
				cov[y*shown.W+x] = c
			}
		}
	}
	multiplyCoverage(shown, cov)
	return nil
}

func sameEffectPlacement(a, b domain.Transform) bool {
	return a.Origin == b.Origin && a.Size == b.Size && a.Rotation == b.Rotation && a.FlipX == b.FlipX && a.FlipY == b.FlipY
}

func effectMaskBackground(mask *Bitmap) uint8 {
	var total, count int
	for y := 0; y < mask.H; y++ {
		for x := 0; x < mask.W; x++ {
			if y == 0 || y == mask.H-1 || x == 0 || x == mask.W-1 {
				total += int(mask.Pix[(y*mask.W+x)*4])
				count++
			}
		}
	}
	if total*2 >= count*255 {
		return 255
	}
	return 0
}

// Smooth gray sampling with edge replication, since an opaque mask's edge
// must not acquire transparency when it is resampled into the layer grid.
func sampleEffectMask(mask *Bitmap, x, y float64) uint8 {
	x, y = x-0.5, y-0.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	var gray float64
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			sx, sy := max(0, min(mask.W-1, x0+dx)), max(0, min(mask.H-1, y0+dy))
			weight := (1 - math.Abs(fx-float64(dx))) * (1 - math.Abs(fy-float64(dy)))
			gray += float64(mask.Pix[(sy*mask.W+sx)*4]) * weight
		}
	}
	return uint8(gray + 0.5)
}
