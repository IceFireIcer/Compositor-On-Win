package domain

// Manifest identity constants (project-format.md).
const (
	FormatID       = "com.compositor.project"
	ColorSpaceSRGB = "sRGB"
	// FormatVersion is written by new saves; versions 1–10 remain readable.
	FormatVersion = 11
)

// GuideAxis is the orientation of an alignment guide.
type GuideAxis string

const (
	GuideAxisHorizontal GuideAxis = "horizontal"
	GuideAxisVertical   GuideAxis = "vertical"
)

// Guide is one alignment guide. Position runs in document pixels and may be
// negative; |position| ≤ 1,000,000 and at most 1,000 guides are stored.
type Guide struct {
	ID       string    `json:"id"`
	Axis     GuideAxis `json:"axis"`
	Position float64   `json:"position"`
}

// Layer is one entry of the flat bottom-to-top layer list. Hierarchy is by
// pointer: ParentID refers to a group layer; renderers traverse contiguous
// subtrees. Optional manifest fields are pointers so absent fields marshal
// as absent — the version-gating reader (ticket 05) depends on that.
type Layer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsVisible bool   `json:"isVisible"`
	Transform `json:"transform"`

	ImageFile *string `json:"imageFile,omitempty"`

	// Version 2: hierarchy.
	ParentID *string `json:"parentID,omitempty"`
	IsGroup  *bool   `json:"isGroup,omitempty"`

	// Version 3: appearance.
	Opacity   *float64   `json:"opacity,omitempty"`
	BlendMode *BlendMode `json:"blendMode,omitempty"`

	// Version 4 (layer masks) / 6 (group masks).
	MaskFile    *string `json:"maskFile,omitempty"`
	MaskEnabled *bool   `json:"maskEnabled,omitempty"`

	// Version 5: clipping masks.
	MaskSourceID *string `json:"maskSourceID,omitempty"`

	// Version 7+: adjustment layers.
	Adjustment *Adjustment `json:"adjustment,omitempty"`

	// Ungated additive fields.
	MaskPlacement *Transform  `json:"maskPlacement,omitempty"`
	MaskLinked    *bool       `json:"maskLinked,omitempty"`
	Shape         *ShapeStyle `json:"shape,omitempty"`
	Effects       *Effects    `json:"effects,omitempty"`
	Text          *TextStyle  `json:"text,omitempty"`
}

// IsGroupLayer reports whether the layer is a folder.
func (l *Layer) IsGroupLayer() bool { return l.IsGroup != nil && *l.IsGroup }

// Document is the parsed manifest: the model-level counterpart of
// CanvasDocument. File-level concerns (version gating, package validation,
// PNG assets) live in ticket 05's ProjectStore.
type Document struct {
	Format        string   `json:"format"`
	Version       int      `json:"version"`
	ColorSpace    string   `json:"colorSpace"`
	DocumentID    string   `json:"documentID"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	ActiveLayerID *string  `json:"activeLayerID,omitempty"`
	Resolution    *int     `json:"resolution,omitempty"`
	Layers        []Layer  `json:"layers"`
	GuidesList    *[]Guide `json:"guides,omitempty"`
}

// Guides returns the guide list, or nil when the manifest has none
// (versions 1–7 or guides removed).
func (d *Document) Guides() []Guide {
	if d.GuidesList == nil {
		return nil
	}
	return *d.GuidesList
}
