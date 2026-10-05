package domain

// ShapeKind is the shape-tool geometry. A rectangle becomes a rounded
// rectangle (or pill) through cornerRadius; lines are stroked, not filled.
type ShapeKind string

const (
	ShapeRectangle ShapeKind = "Rectangle"
	ShapeEllipse   ShapeKind = "Ellipse"
	ShapeLine      ShapeKind = "Line"
)

// ShapeStyle keeps shape-tool layers editable: the style rides alongside the
// ordinary raster imageFile and is dropped once anything else rasterizes the
// pixels. Line endpoints are fractions of the layer box; lineWidth is in
// document pixels.
type ShapeStyle struct {
	Kind         ShapeKind   `json:"kind"`
	Red          float64     `json:"red"`
	Green        float64     `json:"green"`
	Blue         float64     `json:"blue"`
	CornerRadius float64     `json:"cornerRadius"`
	LineWidth    *float64    `json:"lineWidth,omitempty"`
	Start        *[2]float64 `json:"start,omitempty"`
	End          *[2]float64 `json:"end,omitempty"`
}
