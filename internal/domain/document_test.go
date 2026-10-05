package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The golden manifest exercises every schema field from project-format.md
// v1–v11 (except hsvSettings internals, which ride as raw JSON). Round-trip
// equality is compared as parsed JSON trees so key order never matters while
// key names, optionality and values must match exactly.
const goldenManifest = `{
  "format": "com.compositor.project",
  "version": 11,
  "colorSpace": "sRGB",
  "documentID": "0C5E7A91-3B2D-4F6A-8E1C-9D0B7A6F5E4D",
  "width": 1920,
  "height": 1080,
  "resolution": 72,
  "activeLayerID": "L0000000-0000-0000-0000-00000000000A",
  "guides": [
    {"id": "G0000000-0000-0000-0000-000000000001", "axis": "horizontal", "position": 128.5},
    {"id": "G0000000-0000-0000-0000-000000000002", "axis": "vertical", "position": -320}
  ],
  "layers": [
    {
      "id": "L0000000-0000-0000-0000-00000000000A",
      "name": "Base",
      "imageFile": "L0000000-0000-0000-0000-00000000000A.png",
      "isVisible": true,
      "isGroup": false,
      "opacity": 0.8,
      "blendMode": "Multiply",
      "transform": {
        "origin": [0, 0],
        "size": [1920, 1080],
        "rotation": 15.5,
        "flipX": true,
        "flipY": false,
        "sampling": "High quality"
      },
      "maskFile": "L0000000-0000-0000-0000-00000000000A.mask.png",
      "maskEnabled": true,
      "maskPlacement": {"origin": [4, 8], "size": [640, 480], "rotation": 0, "flipX": false, "flipY": false, "sampling": "Nearest"},
      "maskLinked": false,
      "effects": {
        "stroke": {"enabled": true, "size": 6, "red": 1, "green": 0.5, "blue": 0, "opacity": 0.9, "inside": true},
        "shadow": {"enabled": false, "angle": 135, "distance": 12, "blur": 8, "red": 0, "green": 0, "blue": 0, "opacity": 0.5},
        "colorOverlay": {"red": 1, "green": 1, "blue": 1, "opacity": 0.25},
        "innerShadow": {"angle": 90, "distance": 4, "blur": 6, "red": 0, "green": 0, "blue": 0.2, "opacity": 0.4},
        "outerGlow": {"size": 10, "red": 1, "green": 1, "blue": 0.8, "opacity": 0.75},
        "innerGlow": {"enabled": false, "size": 4, "red": 0.9, "green": 0.9, "blue": 1, "opacity": 0.6}
      }
    },
    {
      "id": "L0000000-0000-0000-0000-00000000000B",
      "name": "Folder",
      "isVisible": true,
      "isGroup": true,
      "opacity": 0.9,
      "blendMode": "Normal",
      "transform": {"origin": [100, 100], "size": [800, 600], "rotation": 0, "flipX": false, "flipY": false, "sampling": "Smooth"},
      "maskFile": "L0000000-0000-0000-0000-00000000000B.mask.png",
      "maskEnabled": false
    },
    {
      "id": "L0000000-0000-0000-0000-00000000000C",
      "name": "Clipped",
      "imageFile": "L0000000-0000-0000-0000-00000000000C.png",
      "isVisible": true,
      "isGroup": false,
      "opacity": 1,
      "blendMode": "Normal",
      "transform": {"origin": [120, 120], "size": [400, 300], "rotation": 0, "flipX": false, "flipY": false, "sampling": "High quality"},
      "parentID": "L0000000-0000-0000-0000-00000000000B",
      "maskSourceID": "L0000000-0000-0000-0000-00000000000A"
    },
    {
      "id": "L0000000-0000-0000-0000-00000000000D",
      "name": "Grade",
      "isVisible": true,
      "isGroup": false,
      "opacity": 1,
      "blendMode": "Color Dodge",
      "transform": {"origin": [0, 0], "size": [1920, 1080], "rotation": 0, "flipX": false, "flipY": false, "sampling": "High quality"},
      "parentID": "L0000000-0000-0000-0000-00000000000B",
      "adjustment": {
        "kind": "Curves",
        "hue": 10.5,
        "saturation": 20,
        "lightness": -5,
        "colorize": true,
        "hsvSettings": {"range": "Master", "colorize": true},
        "levels": {
          "channel": "RGB",
          "ranges": [
            {"black": 0, "gamma": 1, "white": 255, "outputBlack": 0, "outputWhite": 255},
            {"black": 10, "gamma": 1.1, "white": 250, "outputBlack": 5, "outputWhite": 250},
            {"black": 0, "gamma": 0.9, "white": 240, "outputBlack": 0, "outputWhite": 245},
            {"black": 5, "gamma": 1, "white": 255, "outputBlack": 2, "outputWhite": 255}
          ]
        },
        "curves": {
          "channel": "RGB",
          "channels": [
            [{"x": 0, "y": 0}, {"x": 255, "y": 255}],
            [{"x": 0, "y": 0}, {"x": 120, "y": 147}, {"x": 255, "y": 255}],
            [{"x": 0, "y": 0}, {"x": 100, "y": 114}, {"x": 255, "y": 255}],
            [{"x": 0, "y": 0}, {"x": 115, "y": 97}, {"x": 255, "y": 238}]
          ]
        },
        "exposureSettings": {"exposure": 0.5, "offset": 0.01, "gamma": 1.05},
        "gradientMapSettings": {"shadows": {"red": 0.1, "green": 0, "blue": 0.2}, "highlights": {"red": 1, "green": 0.9, "blue": 0.8}, "reversed": true},
        "grainSettings": {"amount": 25, "size": 1.5, "roughness": 50, "seed": 424242},
        "blackWhiteSettings": {"reds": 40, "yellows": 60, "greens": 40, "cyans": 60, "blues": 20, "magentas": 80, "tint": true, "tintHue": 40, "tintSaturation": 20},
        "colorBalanceSettings": {
          "shadowCyanRed": -10, "shadowMagentaGreen": 10, "shadowYellowBlue": 20,
          "midCyanRed": 0, "midMagentaGreen": -5, "midYellowBlue": 5,
          "highlightCyanRed": 15, "highlightMagentaGreen": 0, "highlightYellowBlue": -15,
          "preserveLuminosity": true
        },
        "blurRadius": 12.5,
        "motionAngle": -30,
        "motionDistance": 88,
        "noiseAmount": 33.3,
        "noiseGaussian": true,
        "noiseMonochromatic": false,
        "noiseSeed": 424242
      }
    },
    {
      "id": "L0000000-0000-0000-0000-00000000000E",
      "name": "Caption",
      "imageFile": "L0000000-0000-0000-0000-00000000000E.png",
      "isVisible": true,
      "isGroup": false,
      "opacity": 1,
      "blendMode": "Normal",
      "transform": {"origin": [40, 960], "size": [360, 120], "rotation": 0, "flipX": false, "flipY": false, "sampling": "High quality"},
      "text": {
        "content": "Hello, world",
        "fontName": "Helvetica-Bold",
        "fontSize": 48,
        "red": 0.1,
        "green": 0.2,
        "blue": 0.3,
        "alignment": "Center",
        "tracking": 2.5,
        "leading": 56,
        "boxSize": [320, 120],
        "colorRuns": [{"location": 0, "length": 5, "red": 1, "green": 0, "blue": 0}],
        "fontRuns": [{"location": 7, "length": 5, "fontName": "Menlo-Regular"}]
      }
    },
    {
      "id": "L0000000-0000-0000-0000-00000000000F",
      "name": "Stroke",
      "imageFile": "L0000000-0000-0000-0000-00000000000F.png",
      "isVisible": true,
      "isGroup": false,
      "opacity": 1,
      "blendMode": "Normal",
      "transform": {"origin": [10, 20], "size": [200, 80], "rotation": 0, "flipX": false, "flipY": false, "sampling": "High quality"},
      "shape": {"kind": "Line", "red": 0, "green": 0, "blue": 0, "cornerRadius": 0, "lineWidth": 3, "start": [10, 20], "end": [200, 80]}
    }
  ]
}`

func mustParseTree(t *testing.T, data string) map[string]any {
	t.Helper()
	var tree map[string]any
	if err := json.Unmarshal([]byte(data), &tree); err != nil {
		t.Fatalf("golden JSON is not valid: %v", err)
	}
	return tree
}

func TestDocumentRoundTrip(t *testing.T) {
	var doc Document
	if err := json.Unmarshal([]byte(goldenManifest), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Version != 11 || doc.Width != 1920 || doc.Height != 1080 || doc.Resolution == nil || *doc.Resolution != 72 {
		t.Fatalf("document header wrong: %+v", doc)
	}
	if len(doc.Layers) != 6 || len(doc.Guides()) != 2 {
		t.Fatalf("layers=%d guides=%d", len(doc.Layers), len(doc.Guides()))
	}
	base := doc.Layers[0]
	if base.ImageFile == nil || *base.ImageFile != "L0000000-0000-0000-0000-00000000000A.png" {
		t.Fatalf("base imageFile = %v", base.ImageFile)
	}
	if base.Transform.Origin != [2]float64{0, 0} || base.Transform.Size != [2]float64{1920, 1080} {
		t.Fatalf("base transform = %+v", base.Transform)
	}
	if base.Effects == nil || base.Effects.Stroke == nil || !base.Effects.Stroke.Inside {
		t.Fatalf("base effects = %+v", base.Effects)
	}
	adj := doc.Layers[3].Adjustment
	if adj == nil || adj.Kind != AdjustmentCurves || adj.BlurRadius == nil || *adj.BlurRadius != 12.5 {
		t.Fatalf("adjustment = %+v", adj)
	}
	if len(adj.Curves.Channels) != 4 || len(adj.Curves.Channels[0]) != 2 {
		t.Fatalf("curves = %+v", adj.Curves)
	}

	remarshaled, err := json.Marshal(&doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !reflect.DeepEqual(mustParseTree(t, goldenManifest), mustParseTree(t, string(remarshaled))) {
		t.Fatalf("round-trip changed the document:\nwant %s\ngot  %s", goldenManifest, remarshaled)
	}
}

func TestOptionalFieldsAreOmitted(t *testing.T) {
	doc := Document{
		Format:     FormatID,
		Version:    FormatVersion,
		ColorSpace: ColorSpaceSRGB,
		DocumentID: "D",
		Width:      10,
		Height:     10,
		Layers: []Layer{{
			ID:        "L",
			Name:      "Blank",
			IsVisible: true,
			Transform: Transform{Sampling: SamplingHighQuality},
		}},
	}
	data, err := json.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"activeLayerID", "resolution", "guides", "imageFile", "parentID", "isGroup", "opacity", "blendMode", "maskFile", "maskEnabled", "maskSourceID", "adjustment", "maskPlacement", "maskLinked", "shape", "effects", "text"} {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("empty optional %q must be omitted, got %s", key, data)
		}
	}
	if !strings.Contains(string(data), `"isVisible"`) || !strings.Contains(string(data), `"transform"`) {
		t.Fatalf("required fields missing: %s", data)
	}
}
