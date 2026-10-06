package render

// HealStroke — the spot-healing brush's coverage accumulator. It reuses the
// brush interaction's dab geometry (BrushStroke.walk's even spacing,
// tipCoverage's hardness falloff, the string smoothing) but stamps gray
// coverage instead of color; EndStroke runs SpotHeal over the accumulated
// coverage.

import "math"

// HealStrokeSettings mirror the paint brush's stroke-shaping fields.
type HealStrokeSettings struct {
	Diameter  float64
	Hardness  float64 // 0–1
	Smoothing float64
	Zoom      float64
}

type HealStroke struct {
	coverage []uint8
	w, h     int
	settings HealStrokeSettings
	radius   float64
	spacing  float64

	prev      Point
	hasPrev   bool
	distToNext float64
	anchor    Point
	hasAnchor bool
	pointer   Point
	done      bool
}

// NewHealStroke starts a healing stroke over the layer's pixel grid.
func NewHealStroke(w, h int, s HealStrokeSettings) *HealStroke {
	radius := s.Diameter / 2
	if radius < 0.5 {
		radius = 0.5
	}
	spacing := math.Max(0.25, radius*spacingFraction(s.Hardness))
	return &HealStroke{
		coverage: make([]uint8, w*h),
		w:        w,
		h:        h,
		settings: s,
		radius:   radius,
		spacing:  spacing,
	}
}

// Append samples one pointer event (the brush's string smoothing).
func (st *HealStroke) Append(p Point) {
	if st.done || !isFinite(p.X) || !isFinite(p.Y) {
		return
	}
	st.pointer = p
	if !st.hasAnchor {
		st.anchor = p
		st.hasAnchor = true
		st.walk(p)
		return
	}
	if st.settings.Smoothing > 0 {
		radius := st.settings.Smoothing / math.Max(0.01, st.settings.Zoom)
		dx := p.X - st.anchor.X
		dy := p.Y - st.anchor.Y
		dist := math.Hypot(dx, dy)
		if dist <= radius {
			return
		}
		step := (dist - radius) / dist
		p = Point{X: st.anchor.X + dx*step, Y: st.anchor.Y + dy*step}
		st.anchor = p
	}
	st.walk(p)
}

// Finish ends the stroke, laying the raw pointer past the string.
func (st *HealStroke) Finish() {
	if st.done {
		return
	}
	if st.settings.Smoothing > 0 && st.hasAnchor && st.pointer != st.anchor {
		st.walk(st.pointer)
	}
	st.done = true
}

// Coverage returns the accumulated coverage grid (w×h gray).
func (st *HealStroke) Coverage() []uint8 { return st.coverage }

func (st *HealStroke) walk(to Point) {
	if st.hasPrev {
		dx := to.X - st.prev.X
		dy := to.Y - st.prev.Y
		dist := math.Hypot(dx, dy)
		for st.distToNext <= dist {
			f := st.distToNext / dist
			st.dab(Point{X: st.prev.X + dx*f, Y: st.prev.Y + dy*f})
			st.distToNext += st.spacing
		}
		st.distToNext -= dist
	} else {
		st.dab(to)
		st.distToNext = st.spacing
	}
	st.prev = to
	st.hasPrev = true
}

// dab stamps one soft disc of coverage, keeping the maximum already there.
func (st *HealStroke) dab(p Point) {
	x0 := max(0, int(math.Floor(p.X-st.radius))-1)
	x1 := min(st.w-1, int(math.Ceil(p.X+st.radius))+1)
	y0 := max(0, int(math.Floor(p.Y-st.radius))-1)
	y1 := min(st.h-1, int(math.Ceil(p.Y+st.radius))+1)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c := tipCoverage(st.settings.Hardness, st.radius,
				math.Hypot(float64(x)+0.5-p.X, float64(y)+0.5-p.Y))
			if c <= 0 {
				continue
			}
			v := uint8(clamp01(c)*255.0 + 0.5)
			i := y*st.w + x
			if v > st.coverage[i] {
				st.coverage[i] = v
			}
		}
	}
}
