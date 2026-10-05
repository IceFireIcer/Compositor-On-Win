package render

import (
	"testing"

	"compositor-win/internal/domain"
)

// Adjustment layers in the compositor: the adjustment recolors the
// composite below it, then comes back through its own mask at its opacity
// (the Swift adjusted(below:by:adjustment:) pass).

func levelsAdj(mutate func(*domain.LevelsSettings)) *domain.Adjustment {
	s := domain.LevelsSettings{Ranges: [4]domain.LevelRange{identRange(), identRange(), identRange(), identRange()}}
	if mutate != nil {
		mutate(&s)
	}
	return &domain.Adjustment{Kind: domain.AdjustmentLevels, Levels: s}
}

func adjustmentLayer(id string, adj *domain.Adjustment, mutate func(*domain.Layer)) domain.Layer {
	return testLayer(id, "", func(l *domain.Layer) {
		l.Adjustment = adj
		if mutate != nil {
			mutate(l)
		}
	})
}

func renderDoc(t *testing.T, doc *domain.Document, assets map[string]*Bitmap) *Bitmap {
	t.Helper()
	out, err := Render(doc, func(name string) (*Bitmap, error) { return assets[name], nil })
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRenderAdjustmentLevels(t *testing.T) {
	base := solid(2, 2, 128, 128, 128, 255)
	// black 51, white 204, output white 128: straight 128/255 →
	// (128−51)/153 × 128/255 → byte 64.
	doc := testDoc(2, 2,
		testLayer("base", "base.png", nil),
		adjustmentLayer("adj", levelsAdj(func(s *domain.LevelsSettings) {
			s.Ranges[0] = domain.LevelRange{Black: 51, Gamma: 1, White: 204, OutputWhite: 128}
		}), nil))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{64, 64, 64, 255} {
		t.Fatalf("levels adjustment render = %v, want (64, 64, 64, 255)", px(out, 0, 0))
	}
	// The source asset is untouched — the adjustment is non-destructive.
	if px(base, 0, 0) != [4]uint8{128, 128, 128, 255} {
		t.Fatalf("base asset mutated: %v", px(base, 0, 0))
	}
}

func TestRenderAdjustmentIdentityUntouched(t *testing.T) {
	base := solid(2, 2, 128, 100, 60, 255)
	doc := testDoc(2, 2, testLayer("base", "base.png", nil), adjustmentLayer("adj", levelsAdj(nil), nil))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{128, 100, 60, 255} {
		t.Fatalf("identity levels changed pixels: %v", px(out, 0, 0))
	}
}

func TestRenderAdjustmentRecompute(t *testing.T) {
	// The same document re-renders with new adjustment settings at any time.
	base := solid(2, 2, 128, 128, 128, 255)
	adj := levelsAdj(func(s *domain.LevelsSettings) {
		s.Ranges[0] = domain.LevelRange{Black: 51, Gamma: 1, White: 204, OutputWhite: 128}
	})
	doc := testDoc(2, 2, testLayer("base", "base.png", nil), adjustmentLayer("adj", adj, nil))
	assets := map[string]*Bitmap{"base.png": base}
	if got := renderDoc(t, doc, assets); px(got, 0, 0)[0] != 64 {
		t.Fatalf("first render = %v", px(got, 0, 0))
	}
	adj.Kind = domain.AdjustmentLevels
	adj.Levels = domain.LevelsSettings{Ranges: [4]domain.LevelRange{identRange(), identRange(), identRange(), identRange()}}
	adj.Levels.Ranges[0].OutputBlack = 102
	if got := renderDoc(t, doc, assets); px(got, 0, 0)[0] != 179 {
		t.Fatalf("re-render after settings change = %v, want 179", px(got, 0, 0))
	}
}

func TestRenderAdjustmentMaskAndOpacity(t *testing.T) {
	base := solid(2, 2, 128, 128, 128, 255)
	assets := map[string]*Bitmap{
		"base.png": base,
		// Mask: top row white (adjustment applies), bottom row black (it doesn't).
		"mask.png": func() *Bitmap {
			m := NewBitmap(2, 2)
			for x := 0; x < 2; x++ {
				m.Pix[x*4], m.Pix[x*4+3] = 255, 255
				i := (1*2 + x) * 4
				m.Pix[i+3] = 255 // black mask row, opaque
			}
			return m
		}(),
	}
	adj := levelsAdj(func(s *domain.LevelsSettings) {
		s.Ranges[0] = domain.LevelRange{Black: 51, Gamma: 1, White: 204, OutputWhite: 128}
	})
	doc := testDoc(2, 2, testLayer("base", "base.png", nil),
		adjustmentLayer("adj", adj, func(l *domain.Layer) { l.MaskFile = ptr("mask.png") }))
	out := renderDoc(t, doc, assets)
	if px(out, 0, 0) != [4]uint8{64, 64, 64, 255} {
		t.Errorf("masked top row = %v, want adjusted 64", px(out, 0, 0))
	}
	if px(out, 0, 1) != [4]uint8{128, 128, 128, 255} {
		t.Errorf("masked bottom row = %v, want original 128", px(out, 0, 1))
	}

	// At half opacity the adjusted result fades over the original: the
	// premultiplied crossfade lands on 96.
	dom := ptr(0.5)
	doc = testDoc(2, 2, testLayer("base", "base.png", nil),
		adjustmentLayer("adj", adj, func(l *domain.Layer) { l.Opacity = dom }))
	out = renderDoc(t, doc, assets)
	if px(out, 0, 0) != [4]uint8{96, 96, 96, 255} {
		t.Errorf("half-opacity adjustment = %v, want 96", px(out, 0, 0))
	}
}

func TestRenderAdjustmentBlendModePass(t *testing.T) {
	// In Multiply the adjusted straight colors blend with the below ones at
	// full coverage: 128/255 × 64/255 premultiplied back → 32.
	mode, _ := domain.ParseBlendMode("Multiply")
	base := solid(2, 2, 128, 128, 128, 255)
	doc := testDoc(2, 2, testLayer("base", "base.png", nil),
		adjustmentLayer("adj", levelsAdj(func(s *domain.LevelsSettings) {
			s.Ranges[0] = domain.LevelRange{Black: 51, Gamma: 1, White: 204, OutputWhite: 128}
		}), func(l *domain.Layer) { l.BlendMode = &mode }))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{32, 32, 32, 255} {
		t.Fatalf("multiply adjustment = %v, want (32, 32, 32, 255)", px(out, 0, 0))
	}
}

func TestRenderAdjustmentHSV(t *testing.T) {
	// Red 200 shifted +180° lands on cyan of the same tone: (0, 200, 200).
	base := solid(2, 2, 200, 0, 0, 255)
	doc := testDoc(2, 2, testLayer("base", "base.png", nil),
		adjustmentLayer("adj", &domain.Adjustment{Kind: domain.AdjustmentHueSaturation, Hue: 180}, nil))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{0, 200, 200, 255} {
		t.Fatalf("hue +180 render = %v, want (0, 200, 200, 255)", px(out, 0, 0))
	}
}

func TestRenderAdjustmentInsideGroup(t *testing.T) {
	// An adjustment layer inside a group recolors the group's own composite;
	// the group's opacity still applies to the whole subtree afterwards.
	base := solid(2, 2, 128, 128, 128, 255)
	group := testLayer("group", "", func(l *domain.Layer) { l.IsGroup = ptr(true) })
	doc := testDoc(2, 2, group,
		testLayer("base", "base.png", func(l *domain.Layer) { l.ParentID = ptr("group") }),
		adjustmentLayer("adj", levelsAdj(func(s *domain.LevelsSettings) {
			s.Ranges[0] = domain.LevelRange{Black: 51, Gamma: 1, White: 204, OutputWhite: 128}
		}), func(l *domain.Layer) { l.ParentID = ptr("group") }))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{64, 64, 64, 255} {
		t.Fatalf("grouped adjustment render = %v, want 64", px(out, 0, 0))
	}
}

func TestRenderAdjustmentUnsupportedPassesThrough(t *testing.T) {
	// M5 kernel kinds (Grain and friends) leave the composite untouched.
	base := solid(2, 2, 128, 100, 60, 255)
	doc := testDoc(2, 2, testLayer("base", "base.png", nil),
		adjustmentLayer("adj", &domain.Adjustment{Kind: domain.AdjustmentGrain}, nil))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{128, 100, 60, 255} {
		t.Fatalf("grain pass-through changed pixels: %v", px(out, 0, 0))
	}
}

func TestRenderAdjustmentMaskSourcedSkipped(t *testing.T) {
	// An adjustment layer taking its mask from another layer's coverage is
	// skipped by the layer path entirely, as in the original.
	base := solid(2, 2, 128, 128, 128, 255)
	doc := testDoc(2, 2, testLayer("base", "base.png", nil),
		adjustmentLayer("adj", levelsAdj(func(s *domain.LevelsSettings) {
			s.Ranges[0] = domain.LevelRange{Black: 51, Gamma: 1, White: 204, OutputWhite: 128}
		}), func(l *domain.Layer) { l.MaskSourceID = ptr("base") }))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{128, 128, 128, 255} {
		t.Fatalf("mask-sourced adjustment rendered: %v", px(out, 0, 0))
	}
}

func TestRenderInvisibleLayersSkipped(t *testing.T) {
	// An invisible layer hides itself; an invisible folder hides its whole
	// subtree (found while wiring adjustment layers — ticket 09 gap).
	base := solid(2, 2, 128, 128, 128, 255)
	top := solid(2, 2, 255, 0, 0, 255)
	doc := testDoc(2, 2,
		testLayer("base", "base.png", nil),
		testLayer("top", "top.png", func(l *domain.Layer) { l.IsVisible = false }))
	out := renderDoc(t, doc, map[string]*Bitmap{"base.png": base, "top.png": top})
	if px(out, 0, 0) != [4]uint8{128, 128, 128, 255} {
		t.Fatalf("invisible layer still rendered: %v", px(out, 0, 0))
	}
	group := testLayer("group", "", func(l *domain.Layer) { l.IsGroup = ptr(true); l.IsVisible = false })
	doc = testDoc(2, 2, group,
		testLayer("base", "base.png", func(l *domain.Layer) { l.ParentID = ptr("group") }))
	out = renderDoc(t, doc, map[string]*Bitmap{"base.png": base})
	if px(out, 0, 0) != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("invisible group subtree still rendered: %v", px(out, 0, 0))
	}
}
