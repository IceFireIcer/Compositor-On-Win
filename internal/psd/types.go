// Package psd reads Photoshop .psd/.psb files — the Go port of the macOS
// IO/PSD reader (PSDReader/PSDChannelCoder/PSDDocumentBuilder), written from
// Adobe's Photoshop File Formats Specification. 8-bit RGB only; CMYK and
// other variants are rejected with actionable errors. Compositor does not
// write PSD.
//
// Scope note (ticket 38): the original's live-text (PSDText) and vector-shape
// (PSDVector) re-rendering are not ported — those layers import as the
// raster pixels Photoshop stores for every layer, with a conversion note.
package psd

import (
	"errors"
	"fmt"

	"compositor-win/internal/render"
)

// Errors mirror PSDError: each is actionable, not a crash.
var (
	ErrTruncated            = errors.New("无法读取该 Photoshop 文件：文件可能损坏或不完整")
	ErrUnsupportedVersion   = errors.New("此 Photoshop 文件的格式版本无法读取")
	ErrUnsupportedColorMode = errors.New("仅支持导入 8 位 RGB 的 Photoshop 文件")
	ErrUnsupportedDepth     = errors.New("仅支持导入 8 位 RGB 的 Photoshop 文件")
	ErrUnsupportedCompress  = errors.New("此 Photoshop 文件使用了不支持的图层压缩方式")
	ErrTooLarge             = errors.New("文件超出文档限制")
	ErrNotPhotoshop         = errors.New("无法读取该文件")
	ErrNoFillSource         = errors.New("选区周围没有足够的不透明像素可供填充")
)

// Conversion is one import note (PSDConversion): what changed and why.
type Conversion struct {
	LayerName string `json:"layerName"`
	Message   string `json:"message"`
}

// Document is the parsed file (PSDDocument): layers bottom to top.
type Document struct {
	Width      int
	Height     int
	Resolution float64
	Layers     []Record
}

// LayerKind is the coarse layer classification (PSDLayerKind).
type LayerKind int

const (
	KindRaster LayerKind = iota
	KindGroup
	KindAdjustment
	KindText
	KindSmartObject
	KindEffects
	KindVector
	KindOther
)

// Record is one parsed layer (PSDRecord). Image is premultiplied RGBA
// (the render.Bitmap format); Mask is 8-bit gray. Text carries a parsed
// editable type layer (ticket 39) and Shape a live shape style; both are
// nil when the layer imports as its stored pixels.
type Record struct {
	ID                    string
	ParentID              string
	Name                  string
	IsGroup               bool
	IsVisible             bool
	Opacity               float64
	BlendKey              string
	Clipping              bool
	CroppedToCanvas       bool
	Left, Top             int
	Width, Height         int
	Image                 *render.Bitmap
	Mask                  []uint8
	MaskLeft, MaskTop     int
	MaskWidth, MaskHeight int
	MaskDefault           uint8
	MaskEnabled           bool
	MaskLinked            bool
	// Adjustment is the parsed levl/curv/hue2 record, nil for other kinds.
	Adjustment interface{ AdjustmentKind() string }
	// Text is the parsed TySh type layer (KindText), Shape a live shape
	// style parsed from vogk (KindVector); both feed the builder's editable
	// metadata, nil otherwise.
	Text  *ParsedText
	Shape *LiveShape

	// OriginX/OriginY is the transform's top-left in document pixels
	// (fractional for text layers); Rotation and FlipY ride with it.
	OriginX, OriginY float64
	Rotation         float64
	FlipY            bool

	Kind LayerKind
}

// BlendModeFromPSD maps the Photoshop blend key onto the domain's modes
// (LayerBlendMode.fromPSD). Dissolve, Darker Color and Lighter Color are
// deliberately absent — they fall through to Normal and say so in the
// conversion report.
func BlendModeFromPSD(key string) string {
	switch key {
	case "norm":
		return "Normal"
	case "mul ":
		return "Multiply"
	case "scrn":
		return "Screen"
	case "over":
		return "Overlay"
	case "sLit":
		return "Soft Light"
	case "dark":
		return "Darken"
	case "lite":
		return "Lighten"
	case "diff":
		return "Difference"
	case "div ":
		return "Color Dodge"
	case "idiv":
		return "Color Burn"
	case "hue ":
		return "Hue"
	case "sat ":
		return "Saturation"
	case "colr":
		return "Color"
	case "lum ":
		return "Luminosity"
	case "lbrn":
		return "Linear Burn"
	case "lddg":
		return "Linear Dodge (Add)"
	case "hLit":
		return "Hard Light"
	case "vLit":
		return "Vivid Light"
	case "lLit":
		return "Linear Light"
	case "pLit":
		return "Pin Light"
	case "hMix":
		return "Hard Mix"
	case "smud":
		return "Exclusion"
	case "fsub":
		return "Subtract"
	case "fdiv":
		return "Divide"
	default:
		return ""
	}
}

// Matches reports whether the data starts with the Photoshop magic.
func Matches(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == "8BPS"
}

var adjustmentKeys = map[string]bool{
	"levl": true, "curv": true, "hue2": true, "hue ": true, "expA": true,
	"grdm": true, "brit": true, "blnc": true, "nvrt": true, "thrs": true,
	"post": true, "mixr": true, "selc": true, "blwh": true, "phfl": true, "vibA": true,
}

func formatErr(err error, context string) error {
	return fmt.Errorf("%s: %w", context, err)
}
