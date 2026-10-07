package psd

// Vector parsing (PSDVector, ticket 39): vector masks (`vmsk`/`vsms`) and
// origination data (`vogk`) map onto live shape layers when they are a
// plain filled rectangle/ellipse (rounded when all four radii agree); every
// other vector stays the stored raster, or — when Photoshop stored no
// pixels at all — is rasterized here from the path with fill and stroke.

import (
	"bytes"
	"encoding/binary"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/layerrender"
	"compositor-win/internal/render"
)

// PathSeg is one absolute path segment (vector.go PathSeg).
type PathSeg struct {
	Kind     byte // 0 move, 1 line, 3 cubic (control1, control2, to)
	X, Y     float64
	C1X, C1Y float64
	C2X, C2Y float64
}

// VectorStroke is the `vstk` stroke style.
type VectorStroke struct {
	Red, Green, Blue float64
	Width            float64
}

// ParsedVector carries what a layer's extras describe. Live layers keep
// their geometry; raster layers carry flattened segments to draw.
type ParsedVector struct {
	// Live is set for a plain filled rectangle/ellipse (vogk or sharp path).
	Live *LiveShape
	// Raster is set when Photoshop stored no pixels: draw these segments.
	Raster *RasterShape
}

// LiveShape is the editable shape-layer style (LayerShapeStyle).
type LiveShape struct {
	Kind             string // "Rectangle" | "Ellipse"
	Red, Green, Blue float64
	CornerRadius     float64
	// Bounds in document pixels (integral).
	X, Y, Width, Height float64
	Notes               []string
}

// RasterShape is a vector that imports as drawn pixels.
type RasterShape struct {
	Segments []PathSeg
	Fill     *[3]float64
	Stroke   *VectorStroke
	// Bounds in document pixels (integral), inset for the stroke.
	X, Y, Width, Height float64
}

// parseVector decodes a layer's vector extras against the canvas size.
// remainingPixels bounds the raster/live image size like the original.
func parseVector(extra map[string][]byte, canvasW, canvasH, remainingPixels int) *ParsedVector {
	vstk := extra["vstk"]
	fillEnabled := true
	if vstk != nil {
		if b, ok := vectorBool(vstk, "fillEnabled"); ok {
			fillEnabled = b
		}
	} else if extra["SoCo"] == nil {
		fillEnabled = false
	}
	strokeEnabled := false
	if vstk != nil {
		if b, ok := vectorBool(vstk, "strokeEnabled"); ok {
			strokeEnabled = b
		}
	}
	var fill *[3]float64
	if soco := extra["SoCo"]; soco != nil {
		fill = vectorRGB(soco)
	}
	if !fillEnabled || fill == nil {
		return nil
	}
	origin := origination(extra["vogk"])
	if origin == nil {
		origin = sharpRect(extra["vmsk"], canvasW, canvasH)
		if origin == nil {
			origin = sharpRect(extra["vsms"], canvasW, canvasH)
		}
	}
	if origin == nil {
		return nil
	}
	box := origin.bounds
	box.x, box.y = math.Floor(box.x), math.Floor(box.y)
	box.width, box.height = math.Ceil(box.width), math.Ceil(box.height)
	if math.IsNaN(box.x) || math.IsNaN(box.y) || math.IsInf(box.x, 0) || math.IsInf(box.y, 0) {
		return nil
	}
	w, h, ok := vectorPixelSize(box.width, box.height, remainingPixels)
	if !ok {
		return nil
	}
	var notes []string
	if strokeEnabled {
		notes = append(notes, "The Photoshop stroke isn’t supported on shape layers and was omitted.")
	}
	notes = append(notes, origin.notes...)
	return &ParsedVector{Live: &LiveShape{
		Kind:         origin.kind,
		Red:          fill[0],
		Green:        fill[1],
		Blue:         fill[2],
		CornerRadius: origin.cornerRadius,
		X:            box.x,
		Y:            box.y,
		Width:        float64(w),
		Height:       float64(h),
		Notes:        notes,
	}}
}

// parseVectorRaster decodes the fill/stroke raster path for layers with no
// stored pixels. Returns nil when nothing drawable is described.
func parseVectorRaster(extra map[string][]byte, canvasW, canvasH, remainingPixels int) *ParsedVector {
	mask := extra["vmsk"]
	if mask == nil {
		mask = extra["vsms"]
	}
	if mask == nil {
		return nil
	}
	segments := vectorPath(mask, canvasW, canvasH)
	if len(segments) == 0 {
		return nil
	}
	var fill *[3]float64
	if soco := extra["SoCo"]; soco != nil {
		fill = vectorRGB(soco)
	}
	vstk := extra["vstk"]
	fillEnabled := true
	if vstk != nil {
		if b, ok := vectorBool(vstk, "fillEnabled"); ok {
			fillEnabled = b
		}
	} else if fill == nil {
		fillEnabled = false
	}
	strokeEnabled := false
	if vstk != nil {
		if b, ok := vectorBool(vstk, "strokeEnabled"); ok {
			strokeEnabled = b
		}
	}
	var stroke *VectorStroke
	if vstk != nil {
		if rgb := vectorRGB(vstk); rgb != nil {
			width := 1.0
			if w, ok := vectorUnit(vstk, "strokeStyleLineWidth"); ok && !math.IsInf(w, 0) {
				width = w
			}
			stroke = &VectorStroke{Red: rgb[0], Green: rgb[1], Blue: rgb[2], Width: width}
		}
	}
	if !(fillEnabled && fill != nil) && !(strokeEnabled && stroke != nil) {
		return nil
	}
	if stroke != nil && math.IsNaN(stroke.Width) {
		return nil
	}
	if stroke != nil && strokeEnabled && !(0 <= stroke.Width && stroke.Width <= 30000) {
		return nil
	}
	// Bounding box of the flattened path, grown for the stroke.
	minX, minY, maxX, maxY := pathBounds(segments)
	if strokeEnabled && stroke != nil {
		grow := math.Ceil(stroke.Width/2 + 1)
		minX, minY, maxX, maxY = minX-grow, minY-grow, maxX+grow, maxY+grow
	}
	minX, minY = math.Floor(minX), math.Floor(minY)
	maxX, maxY = math.Ceil(maxX), math.Ceil(maxY)
	w, h, ok := vectorPixelSize(maxX-minX, maxY-minY, remainingPixels)
	if !ok {
		return nil
	}
	shape := &RasterShape{
		Segments: segments,
		Fill:     fill,
		Stroke:   stroke,
		X:        minX,
		Y:        minY,
		Width:    float64(w),
		Height:   float64(h),
	}
	if !fillEnabled {
		shape.Fill = nil
	}
	if !strokeEnabled {
		shape.Stroke = nil
	}
	return &ParsedVector{Raster: shape}
}

type vectorBounds struct {
	x, y, width, height float64
}

type vectorOrigination struct {
	kind         string
	bounds       vectorBounds
	cornerRadius float64
	notes        []string
}

// origination reads `vogk`: keyOriginType 1/2 = rectangle (2 rounded),
// 5 = ellipse; other kinds keep the layer raster.
func origination(data []byte) *vectorOrigination {
	if data == nil {
		return nil
	}
	typ, ok := vectorLong(data, "keyOriginType")
	if !ok {
		return nil
	}
	var kind string
	switch typ {
	case 1, 2:
		kind = "Rectangle"
	case 5:
		kind = "Ellipse"
	default:
		return nil
	}
	from := dataOffset(data, "keyOriginShapeBBox")
	left, okL := vectorUnit(data, "Left", from)
	top, okT := vectorUnit(data, "Top ", from)
	right, okR := vectorUnit(data, "Rght", from)
	bottom, okB := vectorUnit(data, "Btom", from)
	if !okL || !okT || !okR || !okB {
		return nil
	}
	bounds := vectorBounds{x: left, y: top, width: right - left, height: bottom - top}
	for _, v := range []float64{left, top, right, bottom} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
	}
	if !(bounds.width >= 1 && bounds.height >= 1) {
		return nil
	}
	origin := &vectorOrigination{kind: kind, bounds: bounds}
	if kind == "Rectangle" {
		if radiiAt, ok := dataOffsetOK(data, "keyOriginRRectRadii"); ok {
			var radii []float64
			for _, key := range []string{"topLeft", "topRight", "bottomRight", "bottomLeft"} {
				if r, ok := vectorUnit(data, key, radiiAt); ok {
					radii = append(radii, r)
				}
			}
			if len(radii) == 4 {
				lo, hi := radii[0], radii[0]
				for _, r := range radii {
					lo = math.Min(lo, r)
					hi = math.Max(hi, r)
				}
				if hi-lo > 0.5 {
					return nil
				}
				origin.cornerRadius = hi
			}
		}
	}
	return origin
}

// sharpRect accepts a vector path made of exactly four straight anchors —
// a sharp rectangle drawn with the pen (PSDVector.sharpRect).
func sharpRect(data []byte, canvasW, canvasH int) *vectorOrigination {
	if data == nil || canvasW <= 0 || canvasH <= 0 {
		return nil
	}
	segments := vectorPath(data, canvasW, canvasH)
	if segments == nil {
		return nil
	}
	offset := 8
	remaining := 0
	anchors := 0
	sharp := true
	for offset+26 <= len(data) {
		typ := int(int16(binary.BigEndian.Uint16(data[offset:])))
		body := data[offset+2 : offset+26]
		offset += 26
		switch typ {
		case 0, 3:
			if anchors != 0 {
				return nil
			}
			remaining = int(int16(binary.BigEndian.Uint16(body[0:])))
		case 1, 2, 4, 5:
			if remaining <= 0 {
				continue
			}
			remaining--
			inX, inY := vectorPoint(body, 0, canvasW, canvasH)
			anX, anY := vectorPoint(body, 8, canvasW, canvasH)
			outX, outY := vectorPoint(body, 16, canvasW, canvasH)
			if math.Hypot(inX-anX, inY-anY) > 0.5 || math.Hypot(outX-anX, outY-anY) > 0.5 {
				sharp = false
			}
			anchors++
		default:
			continue
		}
	}
	if !sharp || anchors != 4 {
		return nil
	}
	minX, minY, maxX, maxY := pathBounds(segments)
	box := vectorBounds{x: minX, y: minY, width: maxX - minX, height: maxY - minY}
	if !(box.width >= 1 && box.height >= 1) {
		return nil
	}
	return &vectorOrigination{kind: "Rectangle", bounds: box}
}

// vectorPath decodes the 26-byte-record path (Adobe spec): type 0/3 open/
// close a subpath of the recorded knot count; 1/2/4/5 are knots carrying
// incoming/anchor/outgoing points in 24.8 fixed units of the canvas.
func vectorPath(data []byte, canvasW, canvasH int) []PathSeg {
	if len(data) < 8 || canvasW <= 0 || canvasH <= 0 {
		return nil
	}
	var segments []PathSeg
	offset := 8
	remaining := 0
	closed := true
	first := true
	var previousOutX, previousOutY float64
	for offset+26 <= len(data) {
		typ := int(int16(binary.BigEndian.Uint16(data[offset:])))
		body := data[offset+2 : offset+26]
		offset += 26
		switch typ {
		case 0, 3:
			if !first && closed {
				segments = append(segments, PathSeg{Kind: 2}) // close
			}
			remaining = int(int16(binary.BigEndian.Uint16(body[0:])))
			closed = typ == 0
			first = true
		case 1, 2, 4, 5:
			if remaining <= 0 {
				continue
			}
			remaining--
			inX, inY := vectorPoint(body, 0, canvasW, canvasH)
			anX, anY := vectorPoint(body, 8, canvasW, canvasH)
			outX, outY := vectorPoint(body, 16, canvasW, canvasH)
			if first {
				segments = append(segments, PathSeg{Kind: 0, X: anX, Y: anY})
				first = false
			} else {
				segments = append(segments, PathSeg{Kind: 3, C1X: previousOutX, C1Y: previousOutY, C2X: inX, C2Y: inY, X: anX, Y: anY})
			}
			previousOutX, previousOutY = outX, outY
		default:
			continue
		}
	}
	if !first && closed {
		segments = append(segments, PathSeg{Kind: 2}) // close
	}
	if len(segments) == 0 {
		return nil
	}
	return segments
}

func pathBounds(segments []PathSeg) (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	touch := func(x, y float64) {
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x)
		maxY = math.Max(maxY, y)
	}
	for _, s := range segments {
		switch s.Kind {
		case 0, 1:
			touch(s.X, s.Y)
		case 3:
			touch(s.C1X, s.C1Y)
			touch(s.C2X, s.C2Y)
			touch(s.X, s.Y)
		}
	}
	if math.IsInf(minX, 1) {
		return 0, 0, 0, 0
	}
	return minX, minY, maxX, maxY
}

// vectorPixelSize rejects sizes beyond the side limit or pixel budget.
func vectorPixelSize(width, height float64, remainingPixels int) (int, int, bool) {
	if math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return 0, 0, false
	}
	if math.Abs(width) > 30000 || math.Abs(height) > 30000 {
		return 0, 0, false
	}
	budget := remainingPixels
	if budget < 0 {
		budget = 0
	}
	if width*height > float64(budget) {
		return 0, 0, false
	}
	w := int(width)
	h := int(height)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w*h > budget {
		return 0, 0, false
	}
	return w, h, true
}

// vectorRGB reads SoCo/vstk solid color ("doub" channels, 0–1 or 0–255).
func vectorRGB(data []byte) *[3]float64 {
	r, okR := vectorDouble(data, "Rd  ")
	g, okG := vectorDouble(data, "Grn ")
	b, okB := vectorDouble(data, "Bl  ")
	if !okR || !okG || !okB {
		return nil
	}
	channel := func(v float64) float64 {
		if v > 1 {
			return math.Min(255, math.Max(0, v)) / 255
		}
		return math.Min(1, math.Max(0, v))
	}
	return &[3]float64{channel(r), channel(g), channel(b)}
}

func vectorBool(data []byte, key string) (bool, bool) {
	start, ok := dataOffsetOK(data, key)
	if !ok {
		return false, false
	}
	typAt := start + len(key)
	if typAt+5 > len(data) || string(data[typAt:typAt+4]) != "bool" {
		return false, false
	}
	return data[typAt+4] != 0, true
}

func vectorUnit(data []byte, key string, from ...int) (float64, bool) {
	start := 0
	if len(from) > 0 {
		start = from[0]
	}
	keyAt, ok := dataOffsetOK(data, key, start)
	if !ok {
		return 0, false
	}
	unitAt, ok := dataOffsetOK(data, "UntF", keyAt)
	if !ok {
		return 0, false
	}
	if unitAt+16 > len(data) {
		return 0, false
	}
	return doubleAt(data, unitAt+8), true
}

func vectorLong(data []byte, key string) (int32, bool) {
	start, ok := dataOffsetOK(data, key)
	if !ok {
		return 0, false
	}
	typAt := start + len(key)
	if typAt+8 > len(data) || string(data[typAt:typAt+4]) != "long" {
		return 0, false
	}
	return int32(binary.BigEndian.Uint32(data[typAt+4:])), true
}

func vectorDouble(data []byte, key string) (float64, bool) {
	start, ok := dataOffsetOK(data, key)
	if !ok {
		return 0, false
	}
	typAt := start + len(key)
	if typAt+12 > len(data) || string(data[typAt:typAt+4]) != "doub" {
		return 0, false
	}
	return doubleAt(data, typAt+4), true
}

func doubleAt(data []byte, offset int) float64 {
	if offset+8 > len(data) {
		return math.NaN()
	}
	return math.Float64frombits(binary.BigEndian.Uint64(data[offset:]))
}

// vectorPoint reads one knot point: y then x, 24.8 fixed of the canvas.
func vectorPoint(body []byte, at int, canvasW, canvasH int) (float64, float64) {
	y := float64(int32(binary.BigEndian.Uint32(body[at:]))) / 0x1000000
	x := float64(int32(binary.BigEndian.Uint32(body[at+4:]))) / 0x1000000
	return x * float64(canvasW), y * float64(canvasH)
}

func dataOffset(data []byte, key string, from ...int) int {
	start := 0
	if len(from) > 0 {
		start = from[0]
	}
	at, _ := dataOffsetOK(data, key, start)
	return at
}

func dataOffsetOK(data []byte, key string, from ...int) (int, bool) {
	start := 0
	if len(from) > 0 {
		start = from[0]
	}
	if start < 0 || start >= len(data) {
		return 0, false
	}
	at := bytes.Index(data[start:], []byte(key))
	if at < 0 {
		return 0, false
	}
	return start + at, true
}

// liveShapeImage rasterizes a live shape layer (EditorSession.shapeImage).
func liveShapeImage(shape LiveShape) (*render.Bitmap, bool) {
	return layerrender.ShapeImage(domain.ShapeStyle{
		Kind:         shapeDomainKind(shape.Kind),
		Red:          shape.Red,
		Green:        shape.Green,
		Blue:         shape.Blue,
		CornerRadius: shape.CornerRadius,
	}, int(shape.Width), int(shape.Height))
}

// rasterVectorImage rasterizes an imported vector path with fill and stroke
// (PSDVector.raster).
func rasterVectorImage(shape RasterShape, extra map[string][]byte) (*render.Bitmap, bool) {
	segments := make([]layerrender.PathSeg, 0, len(shape.Segments))
	for _, seg := range shape.Segments {
		segments = append(segments, layerrender.PathSeg{
			Kind: seg.Kind, X: seg.X, Y: seg.Y,
			C1X: seg.C1X, C1Y: seg.C1Y, C2X: seg.C2X, C2Y: seg.C2Y,
		})
	}
	var stroke *layerrender.Stroke
	if shape.Stroke != nil {
		stroke = &layerrender.Stroke{
			Red: shape.Stroke.Red, Green: shape.Stroke.Green, Blue: shape.Stroke.Blue,
			Width: shape.Stroke.Width,
		}
	}
	return layerrender.VectorImage(segments, int(shape.X), int(shape.Y),
		int(shape.Width), int(shape.Height), shape.Fill, stroke)
}

func shapeDomainKind(kind string) domain.ShapeKind {
	switch kind {
	case "Ellipse":
		return domain.ShapeEllipse
	case "Line":
		return domain.ShapeLine
	}
	return domain.ShapeRectangle
}
