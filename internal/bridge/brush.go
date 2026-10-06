package bridge

import (
	"compositor-win/internal/render"
)

// Brush endpoint defaults, per the M2 binding contract: size 30, hardness
// 0.5, black paint, full opacity, no smoothing (pointer samples go
// straight into the stroke). Settings beyond (options bar, eraser) arrive
// with later tickets.
func defaultBrushSettings() render.BrushSettings {
	return render.BrushSettings{
		Diameter:  30,
		Hardness:  0.5,
		R:         0,
		G:         0,
		B:         0,
		Opacity:   1,
		Smoothing: 0,
		Erase:     false,
	}
}

// BeginStroke starts a brush stroke at document-space (x, y) on the active
// layer's bitmap and returns the document envelope. Pixels change only in
// the stroke's own buffers, so rev stays put until EndStroke.
func (s *Service) BeginStroke(x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.BeginStroke(x, y)
}

// StrokePoint extends the in-flight stroke.
func (s *Service) StrokePoint(x, y float64) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.StrokePoint(x, y)
}

// EndStroke commits the stroke into the layer's bitmap, bumps the revision
// (which invalidates the render cache) and returns the envelope.
func (s *Service) EndStroke() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.EndStroke()
}
