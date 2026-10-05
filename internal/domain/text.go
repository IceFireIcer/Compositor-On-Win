package domain

// TextAlignment is the paragraph alignment. Raw values are capitalized.
type TextAlignment string

const (
	TextAlignmentLeft   TextAlignment = "Left"
	TextAlignmentCenter TextAlignment = "Center"
	TextAlignmentRight  TextAlignment = "Right"
)

// TextStyle keeps text layers editable. The PNG remains the display and
// export fallback; colorRuns/fontRuns (versions 10/11) carry per-run color
// and face overrides measured in UTF-16 units of content.
type TextStyle struct {
	Content   string         `json:"content"`
	FontName  string         `json:"fontName"`
	FontSize  float64        `json:"fontSize"`
	Red       float64        `json:"red"`
	Green     float64        `json:"green"`
	Blue      float64        `json:"blue"`
	Alignment TextAlignment  `json:"alignment"`
	Tracking  float64        `json:"tracking"`
	Leading   float64        `json:"leading"`
	BoxSize   *[2]float64    `json:"boxSize,omitempty"`
	ColorRuns []TextColorRun `json:"colorRuns,omitempty"`
	FontRuns  []TextFontRun  `json:"fontRuns,omitempty"`
}

// TextColorRun paints a span in another color.
type TextColorRun struct {
	Location int     `json:"location"`
	Length   int     `json:"length"`
	Red      float64 `json:"red"`
	Green    float64 `json:"green"`
	Blue     float64 `json:"blue"`
}

// TextFontRun sets a span in another face.
type TextFontRun struct {
	Location int    `json:"location"`
	Length   int    `json:"length"`
	FontName string `json:"fontName"`
}
