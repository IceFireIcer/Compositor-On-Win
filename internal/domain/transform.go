package domain

// Sampling is the resampling quality of a layer transform. Raw values are
// the macOS LayerSampling spellings ("High quality" has a lowercase q).
type Sampling string

const (
	SamplingNearest     Sampling = "Nearest"
	SamplingSmooth      Sampling = "Smooth"
	SamplingHighQuality Sampling = "High quality"
)

// Transform places a layer in document pixels: Origin is its top-left
// corner, Size its width and height, Rotation in degrees clockwise.
// Serialized as compact arrays per the manifest contract:
// origin [x, y], size [w, h].
type Transform struct {
	Origin   [2]float64 `json:"origin"`
	Size     [2]float64 `json:"size"`
	Rotation float64    `json:"rotation"`
	FlipX    bool       `json:"flipX"`
	FlipY    bool       `json:"flipY"`
	Sampling Sampling   `json:"sampling"`
}
