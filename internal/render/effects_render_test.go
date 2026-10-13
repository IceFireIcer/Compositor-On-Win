package render

import (
	"bytes"
	"testing"

	"compositor-win/internal/domain"
)

func TestRenderStrokeOutsideDefaultsToEnabled(t *testing.T) {
	// Swift uses a square max-window, so even the diagonal corners are
	// covered. Padding must extend beyond the original 3x3 layer rectangle.
	image := solid(3, 3, 0, 0, 255, 255)
	original := append([]uint8(nil), image.Pix...)
	doc := testDoc(7, 7, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{2, 2}
		l.Transform.Size = [2]float64{3, 3}
		l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1}}
	}))
	out := renderDoc(t, doc, map[string]*Bitmap{"image.png": image})
	assertEffectGrid(t, out, []string{
		".......", ".RRRRR.", ".RBBBR.", ".RBBBR.", ".RBBBR.", ".RRRRR.", ".......",
	})
	if !bytes.Equal(image.Pix, original) {
		t.Fatal("stroke render modified the source asset")
	}
}

func TestRenderStrokeInsideAndOverlayOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		inside  bool
		overlay bool
		rows    []string
	}{
		{"inside", true, false, []string{".......", ".......", "..RRR..", "..RBR..", "..RRR..", ".......", "......."}},
		{"outside then source then overlay", false, true, []string{".......", ".RRRRR.", ".RGGGR.", ".RGGGR.", ".RGGGR.", ".RRRRR.", "......."}},
		{"inside after overlay", true, true, []string{".......", ".......", "..RRR..", "..RGR..", "..RRR..", ".......", "......."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := testDoc(7, 7, testLayer("image", "image.png", func(l *domain.Layer) {
				l.Transform.Origin = [2]float64{2, 2}
				l.Transform.Size = [2]float64{3, 3}
				l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1, Inside: tc.inside}}
				if tc.overlay {
					l.Effects.ColorOverlay = &domain.OverlayEffect{Green: 1, Opacity: 1}
				}
			}))
			assertEffectGrid(t, renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255)}), tc.rows)
		})
	}
}

func TestRenderStrokeDisabledAndInactive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled *bool
		size    float64
		opacity float64
		active  bool
	}{
		{"missing enabled", nil, 1, 1, true},
		{"explicit enabled", ptr(true), 1, 1, true},
		{"disabled", ptr(false), 1, 1, false},
		{"zero size", nil, 0, 1, false},
		{"zero opacity", nil, 1, 0, false},
	} {
		for _, withOverlay := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "", true: " with overlay"}[withOverlay], func(t *testing.T) {
				doc := testDoc(5, 5, testLayer("image", "image.png", func(l *domain.Layer) {
					l.Transform.Origin = [2]float64{2, 2}
					l.Transform.Size = [2]float64{1, 1}
					l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Enabled: tc.enabled, Size: tc.size, Red: 1, Opacity: tc.opacity}}
					if withOverlay {
						l.Effects.ColorOverlay = &domain.OverlayEffect{Green: 1, Opacity: 1}
					}
				}))
				out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(1, 1, 0, 0, 255, 255)})
				wantRing := [4]uint8{}
				if tc.active {
					wantRing = [4]uint8{255, 0, 0, 255}
				}
				if got := px(out, 1, 1); got != wantRing {
					t.Fatalf("ring = %v, want %v", got, wantRing)
				}
				wantSource := [4]uint8{0, 0, 255, 255}
				if withOverlay {
					wantSource = [4]uint8{0, 255, 0, 255}
				}
				if got := px(out, 2, 2); got != wantSource {
					t.Fatalf("source = %v, want %v", got, wantSource)
				}
			})
		}
	}
}

func TestRenderStrokeFractionalReachAndWideStroke(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  float64
		reach int
	}{
		{"positive subpixel", 0.1, 1}, {"below half", 0.49, 1}, {"half", 0.5, 1},
		{"below next half", 1.49, 1}, {"round half up", 1.5, 2}, {"maximum reach", 500, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := testDoc(9, 9, testLayer("image", "image.png", func(l *domain.Layer) {
				l.Transform.Origin = [2]float64{4, 4}
				l.Transform.Size = [2]float64{1, 1}
				l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: tc.size, Red: 1, Opacity: 1}}
			}))
			out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(1, 1, 0, 0, 255, 255)})
			for y := 0; y < 9; y++ {
				for x := 0; x < 9; x++ {
					want := [4]uint8{}
					if x == 4 && y == 4 {
						want = [4]uint8{0, 0, 255, 255}
					} else if x >= 4-tc.reach && x <= 4+tc.reach && y >= 4-tc.reach && y <= 4+tc.reach {
						want = [4]uint8{255, 0, 0, 255}
					}
					if got := px(out, x, y); got != want {
						t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
	// The eroded window reaches outside the padded raster for a wide inside
	// stroke. Swift uses zero outside, so the whole tiny source is outlined.
	doc := testDoc(3, 3, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Size = [2]float64{3, 3}
		l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 500, Red: 1, Opacity: 1, Inside: true}}
	}))
	assertEffectGrid(t, renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255)}), []string{"RRR", "RRR", "RRR"})
}

func TestRenderStrokeSoftAlphaOpacityAndOverlay(t *testing.T) {
	// Literal source-over results in premultiplied sRGB. The second source
	// pixel is half-opaque next to an opaque pixel: its outside ring is 127,
	// and its inside ring is 128. Overlay must use original alpha 128.
	for _, tc := range []struct {
		name     string
		inside   bool
		opacity  float64
		overlay  bool
		want     [4]uint8
		wantRing [4]uint8
	}{
		{"outside soft source", false, 1, false, [4]uint8{63, 0, 128, 191}, [4]uint8{128, 0, 0, 128}},
		{"outside half stroke", false, 0.5, false, [4]uint8{32, 0, 128, 160}, [4]uint8{64, 0, 0, 64}},
		{"outside overlay", false, 1, true, [4]uint8{32, 128, 64, 223}, [4]uint8{128, 0, 0, 128}},
		{"inside soft source", true, 1, false, [4]uint8{128, 0, 64, 192}, [4]uint8{}},
		{"inside half stroke", true, 0.5, false, [4]uint8{64, 0, 96, 160}, [4]uint8{}},
		{"inside overlay", true, 1, true, [4]uint8{128, 64, 32, 223}, [4]uint8{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := NewBitmap(2, 1)
			copy(image.Pix, []uint8{0, 0, 255, 255, 0, 0, 128, 128})
			doc := testDoc(6, 5, testLayer("image", "image.png", func(l *domain.Layer) {
				l.Transform.Origin = [2]float64{2, 2}
				l.Transform.Size = [2]float64{2, 1}
				l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: tc.opacity, Inside: tc.inside}}
				if tc.overlay {
					l.Effects.ColorOverlay = &domain.OverlayEffect{Green: 1, Opacity: 1}
				}
			}))
			out := renderDoc(t, doc, map[string]*Bitmap{"image.png": image})
			if got := px(out, 3, 2); got != tc.want {
				t.Fatalf("soft source = %v, want %v", got, tc.want)
			}
			if got := px(out, 4, 2); got != tc.wantRing {
				t.Fatalf("soft ring = %v, want %v", got, tc.wantRing)
			}
		})
	}
}

func TestRenderStrokeMaskedAndScaled(t *testing.T) {
	for _, inside := range []bool{false, true} {
		for _, disableMask := range []bool{false, true} {
			t.Run(map[bool]string{false: "outside", true: "inside"}[inside]+map[bool]string{false: " masked", true: " mask disabled"}[disableMask], func(t *testing.T) {
				image := solid(2, 2, 0, 0, 255, 255)
				mask := solid(2, 2, 0, 0, 0, 255)
				copy(mask.Pix, []uint8{255, 255, 255, 255})
				original := append([]uint8(nil), image.Pix...)
				originalMask := append([]uint8(nil), mask.Pix...)
				doc := testDoc(12, 12, testLayer("image", "image.png", func(l *domain.Layer) {
					l.Transform.Origin = [2]float64{4, 4}
					l.Transform.Size = [2]float64{4, 4} // Each source pixel is 2x2 on the canvas.
					l.MaskFile = ptr("mask.png")
					l.MaskEnabled = ptr(!disableMask)
					l.Effects = &domain.Effects{
						Stroke:       &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1, Inside: inside},
						ColorOverlay: &domain.OverlayEffect{Green: 1, Opacity: 1},
					}
				}))
				assets := map[string]*Bitmap{"image.png": image, "mask.png": mask}
				out := renderDoc(t, doc, assets)
				end := 6
				if disableMask {
					end = 8
				}
				for y := 0; y < 12; y++ {
					for x := 0; x < 12; x++ {
						want := [4]uint8{}
						shown := x >= 4 && x < end && y >= 4 && y < end
						if inside && shown {
							want = [4]uint8{255, 0, 0, 255}
						} else if !inside && shown {
							want = [4]uint8{0, 255, 0, 255}
						} else if !inside && x >= 2 && x < end+2 && y >= 2 && y < end+2 {
							want = [4]uint8{255, 0, 0, 255}
						}
						if got := px(out, x, y); got != want {
							t.Fatalf("scaled masked pixel (%d,%d) = %v, want %v", x, y, got, want)
						}
					}
				}
				if again := renderDoc(t, doc, assets); !bytes.Equal(out.Pix, again.Pix) {
					t.Fatal("repeated stroke render changed")
				}
				if !bytes.Equal(image.Pix, original) || !bytes.Equal(mask.Pix, originalMask) {
					t.Fatal("stroke render changed shared source/mask assets")
				}
			})
		}
	}
}

func TestRenderStrokePaddingRotationAndFlips(t *testing.T) {
	// The asymmetric source has one blue pixel and one transparent pixel.
	// Its shown shape is a 2x2 canvas block after scaling; the square reach
	// adds two canvas pixels on each side. These literal boxes also verify
	// anisotropic transform compensation (2x1 source stretched to 4x2).
	for _, tc := range []struct {
		name              string
		rotation          float64
		flipX, flipY      bool
		blueX, blueY      int
		ringLeft, ringTop int
	}{
		{"scaled", 0, false, false, 3, 4, 1, 2},
		{"flip x", 0, true, false, 5, 4, 3, 2},
		{"flip y", 0, false, true, 3, 4, 1, 2},
		{"rotate clockwise", 90, false, false, 4, 3, 2, 1},
		{"rotate flipped", 90, true, true, 4, 5, 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := NewBitmap(2, 1)
			copy(image.Pix, []uint8{0, 0, 255, 255})
			doc := testDoc(10, 10, testLayer("image", "image.png", func(l *domain.Layer) {
				l.Transform.Origin = [2]float64{3, 4}
				l.Transform.Size = [2]float64{4, 2}
				l.Transform.Rotation = tc.rotation
				l.Transform.FlipX, l.Transform.FlipY = tc.flipX, tc.flipY
				l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1}}
			}))
			out := renderDoc(t, doc, map[string]*Bitmap{"image.png": image})
			for y := 0; y < 10; y++ {
				for x := 0; x < 10; x++ {
					want := [4]uint8{}
					if x >= tc.blueX && x < tc.blueX+2 && y >= tc.blueY && y < tc.blueY+2 {
						want = [4]uint8{0, 0, 255, 255}
					} else if x >= tc.ringLeft && x < tc.ringLeft+6 && y >= tc.ringTop && y < tc.ringTop+6 {
						want = [4]uint8{255, 0, 0, 255}
					}
					if got := px(out, x, y); got != want {
						t.Fatalf("transformed pixel (%d,%d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestRenderStrokeSoftMaskAndGroupOpacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inside bool
		want   [4]uint8
		ring   [4]uint8
	}{
		{"outside", false, [4]uint8{0, 0, 16, 16}, [4]uint8{8, 0, 0, 8}},
		{"inside", true, [4]uint8{8, 0, 12, 20}, [4]uint8{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folder := testLayer("folder", "", func(l *domain.Layer) {
				l.IsGroup = ptr(true)
				l.Transform.Size = [2]float64{5, 5}
				l.MaskFile = ptr("folder.mask.png")
				l.Opacity = ptr(0.5)
				// Group effects do not produce strokes around descendants.
				l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 500, Green: 1, Opacity: 1}}
			})
			child := testLayer("image", "image.png", func(l *domain.Layer) {
				l.ParentID = &folder.ID
				l.Transform.Origin = [2]float64{2, 2}
				l.Transform.Size = [2]float64{1, 1}
				l.MaskFile = ptr("own.mask.png")
				l.Opacity = ptr(0.5)
				l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 0.5, Inside: tc.inside}}
			})
			doc := testDoc(5, 5, folder, child)
			out := renderDoc(t, doc, map[string]*Bitmap{
				"image.png":       solid(1, 1, 0, 0, 255, 255),
				"own.mask.png":    solid(1, 1, 128, 128, 128, 255),
				"folder.mask.png": solid(1, 1, 128, 128, 128, 255),
			})
			if got := px(out, 2, 2); got != tc.want {
				t.Fatalf("attenuated source = %v, want %v", got, tc.want)
			}
			if got := px(out, 1, 1); got != tc.ring {
				t.Fatalf("attenuated stroke = %v, want %v", got, tc.ring)
			}
		})
	}
}

func TestRenderStrokeSeparateMaskPlacement(t *testing.T) {
	for _, linked := range []bool{false, true} {
		for _, inside := range []bool{false, true} {
			t.Run(map[bool]string{false: "unlinked", true: "linked"}[linked]+map[bool]string{false: " outside", true: " inside"}[inside], func(t *testing.T) {
				// A black-bordered mask with a white central pixel moves that
				// single shown pixel to the right, independent of mask linkage.
				mask := solid(3, 3, 0, 0, 0, 255)
				copy(mask.Pix[16:20], []uint8{255, 255, 255, 255})
				doc := testDoc(7, 7, testLayer("image", "image.png", func(l *domain.Layer) {
					l.Transform.Origin = [2]float64{2, 2}
					l.Transform.Size = [2]float64{3, 3}
					l.MaskFile = ptr("mask.png")
					l.MaskLinked = &linked
					l.MaskPlacement = &domain.Transform{Origin: [2]float64{3, 2}, Size: [2]float64{3, 3}}
					l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1, Inside: inside}}
				}))
				rows := []string{".......", ".......", "...RRR.", "...RBR.", "...RRR.", ".......", "......."}
				if inside {
					rows = []string{".......", ".......", ".......", "....R..", ".......", ".......", "......."}
				}
				assertEffectGrid(t, renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(3, 3, 0, 0, 255, 255), "mask.png": mask}), rows)
			})
		}
	}
}

func TestRenderStrokeClippingAfterEffects(t *testing.T) {
	base := testLayer("base", "base.png", func(l *domain.Layer) {
		l.Transform.Size = [2]float64{5, 5}
	})
	image := testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{2, 2}
		l.Transform.Size = [2]float64{1, 1}
		l.MaskSourceID = &base.ID
		l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1}}
	})
	basePixels := NewBitmap(5, 5)
	for y := 0; y < 5; y++ {
		for x := 0; x < 2; x++ {
			copy(basePixels.Pix[(y*5+x)*4:], []uint8{0, 0, 255, 255})
		}
	}
	out := renderDoc(t, testDoc(5, 5, base, image), map[string]*Bitmap{"base.png": basePixels, "image.png": solid(1, 1, 0, 0, 255, 255)})
	assertEffectGrid(t, out, []string{"BB...", "BR...", "BR...", "BR...", "BB..."})
}

func TestRenderStrokeTransparentAndLiveShape(t *testing.T) {
	for _, inside := range []bool{false, true} {
		doc := testDoc(5, 5, testLayer("image", "image.png", func(l *domain.Layer) {
			l.Transform.Origin = [2]float64{2, 2}
			l.Transform.Size = [2]float64{1, 1}
			l.Effects = &domain.Effects{
				Stroke:       &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1, Inside: inside},
				ColorOverlay: &domain.OverlayEffect{Green: 1, Opacity: 1},
			}
		}))
		assertEffectGrid(t, renderDoc(t, doc, map[string]*Bitmap{"image.png": NewBitmap(1, 1)}), []string{".....", ".....", ".....", ".....", "....."})
	}
	doc := testDoc(5, 5, testLayer("shape", "", func(l *domain.Layer) {
		l.Transform.Origin = [2]float64{2, 2}
		l.Transform.Size = [2]float64{1, 1}
		l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Blue: 1}
		l.Effects = &domain.Effects{Stroke: &domain.StrokeEffect{Size: 1, Red: 1, Opacity: 1}}
	}))
	out, err := Render(doc, func(string) (*Bitmap, error) {
		t.Fatal("a live shape should not need an asset")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEffectGrid(t, out, []string{".....", ".RRR.", ".RBR.", ".RRR.", "....."})
}

func assertEffectGrid(t *testing.T, out *Bitmap, rows []string) {
	t.Helper()
	colors := map[byte][4]uint8{'.': {}, 'R': {255, 0, 0, 255}, 'B': {0, 0, 255, 255}, 'G': {0, 255, 0, 255}}
	if out.H != len(rows) {
		t.Fatalf("height = %d, want %d", out.H, len(rows))
	}
	for y, row := range rows {
		if out.W != len(row) {
			t.Fatalf("width = %d, want %d", out.W, len(row))
		}
		for x := range row {
			want, ok := colors[row[x]]
			if !ok {
				t.Fatalf("unknown effect grid color %q", row[x])
			}
			if got := px(out, x, y); got != want {
				t.Fatalf("effect pixel (%d,%d) = %v, want %v (%c)", x, y, got, want, row[x])
			}
		}
	}
}

func TestRenderColorOverlayDefaultsToEnabled(t *testing.T) {
	image := solid(2, 2, 0, 0, 255, 255)
	doc := testDoc(2, 2, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
	}))
	out, err := Render(doc, func(string) (*Bitmap, error) { return image, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := px(out, 0, 0); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("Color Overlay with omitted enabled = %v, want opaque red", got)
	}
}

func TestRenderColorOverlayCoverage(t *testing.T) {
	// Literal expectations from the source-over coverage fill in Swift
	// LayerEffects.swift and MetalLayerEffects.swift (no linear-light blend).
	for _, tc := range []struct {
		name    string
		enabled *bool
		opacity float64
		pixel   [4]uint8
		want    [4]uint8
	}{
		{"disabled", ptr(false), 1, [4]uint8{0, 0, 255, 255}, [4]uint8{0, 0, 255, 255}},
		{"explicit enabled", ptr(true), 1, [4]uint8{0, 0, 255, 255}, [4]uint8{255, 0, 0, 255}},
		{"zero opacity", nil, 0, [4]uint8{0, 0, 128, 128}, [4]uint8{0, 0, 128, 128}},
		{"opaque half overlay", nil, 0.5, [4]uint8{0, 0, 255, 255}, [4]uint8{128, 0, 128, 255}},
		{"soft edge half overlay", nil, 0.5, [4]uint8{0, 0, 128, 128}, [4]uint8{64, 0, 96, 160}},
		{"transparent", nil, 1, [4]uint8{}, [4]uint8{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			image := solid(2, 2, tc.pixel[0], tc.pixel[1], tc.pixel[2], tc.pixel[3])
			original := append([]uint8(nil), image.Pix...)
			doc := testDoc(2, 2, testLayer("image", "image.png", func(l *domain.Layer) {
				l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Enabled: tc.enabled, Red: 1, Opacity: tc.opacity}}
			}))
			out, err := Render(doc, func(string) (*Bitmap, error) { return image, nil })
			if err != nil {
				t.Fatal(err)
			}
			if got := px(out, 0, 0); got != tc.want {
				t.Fatalf("render = %v, want %v", got, tc.want)
			}
			if !bytes.Equal(image.Pix, original) {
				t.Fatal("render modified the source asset")
			}
		})
	}
}

func TestRenderColorOverlayMaskBeforeEffectAndOpacity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mask        uint8
		maskEnabled *bool
		opacity     float64
		want        [4]uint8
	}{
		{"soft mask", 128, nil, 1, [4]uint8{128, 0, 64, 192}},
		{"soft mask and layer opacity", 128, nil, 0.5, [4]uint8{64, 0, 32, 96}},
		{"hidden", 0, nil, 1, [4]uint8{}},
		{"disabled mask", 0, ptr(false), 1, [4]uint8{255, 0, 0, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := testDoc(2, 2, testLayer("image", "image.png", func(l *domain.Layer) {
				l.MaskFile = ptr("mask.png")
				l.MaskEnabled = tc.maskEnabled
				l.Opacity = &tc.opacity
				l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
			}))
			assets := map[string]*Bitmap{"image.png": solid(2, 2, 0, 0, 255, 255), "mask.png": solid(1, 1, tc.mask, tc.mask, tc.mask, 255)}
			out, err := Render(doc, func(name string) (*Bitmap, error) { return assets[name], nil })
			if err != nil {
				t.Fatal(err)
			}
			if got := px(out, 0, 0); got != tc.want {
				t.Fatalf("masked overlay = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderColorOverlayGroupPassThrough(t *testing.T) {
	// Swift FolderMaskClip clips each child, and effectiveOpacity multiplies
	// ancestor opacity per child. It does not flatten the group first.
	for _, tc := range []struct {
		name    string
		opacity float64
		mask    uint8
		want    [4]uint8
	}{
		{"group opacity", 0.5, 255, [4]uint8{192, 0, 0, 192}},
		{"group mask", 1, 128, [4]uint8{192, 0, 0, 192}},
		{"group opacity and mask", 0.5, 128, [4]uint8{112, 0, 0, 112}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folder := testLayer("folder", "", func(l *domain.Layer) {
				l.IsGroup = ptr(true)
				l.Opacity = &tc.opacity
				l.MaskFile = ptr("folder.mask.png")
				// Swift does not apply a group's own effects to its children.
				l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Green: 1, Opacity: 1}}
			})
			child := func(id string) domain.Layer {
				return testLayer(id, "image.png", func(l *domain.Layer) {
					l.ParentID = &folder.ID
					l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
				})
			}
			doc := testDoc(2, 2, folder, child("one"), child("two"))
			assets := map[string]*Bitmap{"image.png": solid(2, 2, 0, 0, 255, 255), "folder.mask.png": solid(1, 1, tc.mask, tc.mask, tc.mask, 255)}
			out, err := Render(doc, func(name string) (*Bitmap, error) { return assets[name], nil })
			if err != nil {
				t.Fatal(err)
			}
			if got := px(out, 0, 0); got != tc.want {
				t.Fatalf("grouped overlays = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderColorOverlaySourceGridBeforeSmoothPlacement(t *testing.T) {
	// A half-opaque source gets alpha 192 from the overlay *before* the
	// interpolation with its transparent neighbor, giving 144 at x=1.
	// Applying the overlay after interpolation would instead give alpha 156.
	image := NewBitmap(2, 1)
	copy(image.Pix, []uint8{0, 0, 128, 128, 0, 0, 0, 0})
	doc := testDoc(4, 1, testLayer("image", "image.png", func(l *domain.Layer) {
		l.Transform.Size = [2]float64{4, 1}
		l.Transform.Sampling = domain.SamplingSmooth
		l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
	}))
	out := renderDoc(t, doc, map[string]*Bitmap{"image.png": image})
	if got := px(out, 1, 0); got != [4]uint8{96, 0, 48, 144} {
		t.Fatalf("scaled source-grid overlay = %v, want [96 0 48 144]", got)
	}
}

func TestRenderColorOverlayLiveShape(t *testing.T) {
	doc := testDoc(2, 2, testLayer("shape", "", func(l *domain.Layer) {
		l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Blue: 1}
		l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 0.2, Green: 0.4, Blue: 0.6, Opacity: 1}}
	}))
	out, err := Render(doc, func(string) (*Bitmap, error) {
		t.Fatal("a live shape must not need its saved raster")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := px(out, 0, 0); got != [4]uint8{51, 102, 153, 255} {
		t.Fatalf("live shape overlay = %v, want [51 102 153 255]", got)
	}
}

func TestRenderColorOverlaySeparateMaskPlacement(t *testing.T) {
	for _, tc := range []struct {
		name     string
		linked   bool
		flip     bool
		rotation float64
		offset   bool
		white    bool
		want     [][4]uint8
	}{
		{"unlinked offset", false, false, 0, true, false, [][4]uint8{{}, {255, 0, 0, 255}, {}, {}}},
		{"linked stored offset", true, false, 0, true, false, [][4]uint8{{}, {255, 0, 0, 255}, {}, {}}},
		{"flipped mask", false, true, 0, false, false, [][4]uint8{{}, {}, {255, 0, 0, 255}, {}}},
		{"rotated mask", false, false, 180, false, false, [][4]uint8{{}, {}, {255, 0, 0, 255}, {}}},
		{"white border extends", false, false, 0, true, true, [][4]uint8{{255, 0, 0, 255}, {255, 0, 0, 255}, {255, 0, 0, 255}, {255, 0, 0, 255}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mask := solid(4, 1, 0, 0, 0, 255)
			copy(mask.Pix[4:8], []uint8{255, 255, 255, 255})
			placement := domain.Transform{Size: [2]float64{4, 1}, FlipX: tc.flip, Rotation: tc.rotation}
			if tc.offset {
				placement.Origin = [2]float64{1, 0}
				placement.Size = [2]float64{2, 1}
				// Black top/bottom rows make outside coverage black while the
				// center row supplies white at x=1 and black at x=2.
				mask = solid(2, 3, 0, 0, 0, 255)
				copy(mask.Pix[8:12], []uint8{255, 255, 255, 255})
			}
			if tc.white {
				mask = solid(2, 1, 255, 255, 255, 255)
			}
			doc := testDoc(4, 1, testLayer("image", "image.png", func(l *domain.Layer) {
				l.Transform.Size = [2]float64{4, 1}
				l.MaskFile = ptr("mask.png")
				l.MaskLinked = &tc.linked
				l.MaskPlacement = &placement
				l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
			}))
			out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(4, 1, 0, 0, 255, 255), "mask.png": mask})
			for x, want := range tc.want {
				if got := px(out, x, 0); got != want {
					t.Fatalf("placed mask x=%d = %v, want %v", x, got, want)
				}
			}
		})
	}
}

func TestRenderColorOverlayGroupBackdrop(t *testing.T) {
	folder := testLayer("folder", "", func(l *domain.Layer) { l.IsGroup = ptr(true) })
	child := testLayer("child", "image.png", func(l *domain.Layer) {
		l.ParentID = &folder.ID
		l.BlendMode = ptr(domain.BlendMultiply)
		l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
	})
	doc := testDoc(2, 2, testLayer("below", "below.png", nil), folder, child)
	out := renderDoc(t, doc, map[string]*Bitmap{"below.png": solid(2, 2, 128, 128, 128, 255), "image.png": solid(2, 2, 0, 0, 255, 255)})
	if got := px(out, 0, 0); got != [4]uint8{128, 0, 0, 255} {
		t.Fatalf("group child multiply against external backdrop = %v, want [128 0 0 255]", got)
	}
	// Adjustment-only folders still act on the shared backdrop; the folder
	// opacity/mask controls the adjustment's crossfade, not backdrop alpha.
	folder.Opacity = ptr(0.5)
	folder.MaskFile = ptr("mask.png")
	adj := adjustmentLayer("adjust", levelsAdj(func(s *domain.LevelsSettings) {
		s.Ranges[0].OutputWhite = 0
	}), func(l *domain.Layer) { l.ParentID = &folder.ID })
	doc = testDoc(2, 2, testLayer("below", "below.png", nil), folder, adj)
	out = renderDoc(t, doc, map[string]*Bitmap{"below.png": solid(2, 2, 128, 128, 128, 255), "mask.png": solid(2, 2, 128, 128, 128, 255)})
	if got := px(out, 0, 0); got != [4]uint8{96, 96, 96, 255} {
		t.Fatalf("group adjustment on external backdrop = %v, want [96 96 96 255]", got)
	}
}

func TestRenderColorOverlayNestedFolderMasks(t *testing.T) {
	outer := testLayer("outer", "", func(l *domain.Layer) {
		l.IsGroup = ptr(true)
		l.Opacity = ptr(0.5)
		l.MaskFile = ptr("mask.png")
	})
	inner := testLayer("inner", "", func(l *domain.Layer) {
		l.IsGroup = ptr(true)
		l.ParentID = &outer.ID
		l.Opacity = ptr(0.5)
		l.MaskFile = ptr("mask.png")
	})
	child := func(id, parent string) domain.Layer {
		return testLayer(id, "image.png", func(l *domain.Layer) {
			l.ParentID = &parent
			l.Effects = &domain.Effects{ColorOverlay: &domain.OverlayEffect{Red: 1, Opacity: 1}}
		})
	}
	doc := testDoc(2, 2, outer, inner, child("inner-one", inner.ID), child("inner-two", inner.ID), child("outer-child", outer.ID))
	out := renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(2, 2, 0, 0, 255, 255), "mask.png": solid(2, 2, 128, 128, 128, 255)})
	// Inner children each have coverage 16. Their composite has coverage 31;
	// the outer sibling has coverage 64, giving 87. The inner mask/opacity
	// must not leak to that sibling when recursion returns.
	if got := px(out, 0, 0); got != [4]uint8{87, 0, 0, 87} {
		t.Fatalf("nested folder overlay = %v, want [87 0 0 87]", got)
	}
	inner.MaskEnabled = ptr(false)
	doc = testDoc(2, 2, outer, inner, child("inner-one", inner.ID), child("inner-two", inner.ID))
	out = renderDoc(t, doc, map[string]*Bitmap{"image.png": solid(2, 2, 0, 0, 255, 255), "mask.png": solid(2, 2, 128, 128, 128, 255)})
	if got := px(out, 0, 0); got != [4]uint8{60, 0, 0, 60} {
		t.Fatalf("disabled inner folder mask = %v, want [60 0 0 60]", got)
	}
}
