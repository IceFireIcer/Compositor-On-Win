package bridge

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// Start with a compact reveal-all mask, without seeding any history. The
// explicit target is deliberately different from the active image layer.
func maskPaintingFixture(t *testing.T) (*Service, string, *session, string, string) {
	t.Helper()
	svc, tab := newTestService(t, 16, 12)
	sess := activeSessionOf(t, svc)
	l := &sess.doc.Layers[0]
	id, key := l.ID, l.ID+".mask.png"
	l.MaskFile = &key
	sess.bitmaps[key] = newLayerMaskBitmap(1, 1, []uint8{255})
	other := newPixelLayer("Other active layer", 16, 12)
	sess.bitmaps[*other.ImageFile] = render.NewBitmap(16, 12)
	sess.doc.Layers = append(sess.doc.Layers, other)
	sess.doc.ActiveLayerID = idPtr(other.ID)
	return svc, tab, sess, id, key
}

func maskBrushJSON(t *testing.T, id string, extra map[string]any) string {
	t.Helper()
	p := map[string]any{"layerID": id, "targetMask": true, "diameter": 2, "hardness": 1, "opacity": 1, "zoom": 1}
	for k, v := range extra {
		p[k] = v
	}
	return payloadJSON(t, p)
}

func maskGradientJSON(t *testing.T, id string) string {
	t.Helper()
	return payloadJSON(t, map[string]any{"layerID": id, "targetMask": true, "style": "foregroundToBackground", "foreground": map[string]float64{"r": 0}, "background": map[string]float64{"r": 1, "g": 1, "b": 1}})
}

func maskPreview(t *testing.T, svc *Service, token string) *render.Bitmap {
	t.Helper()
	r := httptest.NewRecorder()
	svc.serveStrokePreview(r, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("mask preview status=%d body=%s", r.Code, r.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(r.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return render.BitmapFromImage(img)
}

func assertOpaqueMask(t *testing.T, bmp *render.Bitmap) {
	t.Helper()
	for i := 0; i < len(bmp.Pix); i += 4 {
		if bmp.Pix[i] != bmp.Pix[i+1] || bmp.Pix[i] != bmp.Pix[i+2] || bmp.Pix[i+3] != 255 {
			t.Fatalf("mask pixel %d is not opaque gray: %v", i/4, bmp.Pix[i:i+4])
		}
	}
}

func TestMaskPaintingVisibleRenderAndUniformUndoRedo(t *testing.T) {
	for _, tool := range []string{"brush", "gradient"} {
		t.Run(tool, func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			original := sess.bitmaps[key]
			l := mustLayerByID(t, sess.doc, id)
			image := sess.bitmaps[*l.ImageFile]
			imageBytes := append([]byte(nil), image.Pix...)
			l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
			l.Text = &domain.TextStyle{Content: "Editable text"}
			textBefore := payloadJSON(t, l.Text)
			// The fallback image differs from the procedural shape, so clearing
			// Shape in a preview would change the unpainted area's visible color.
			if tool == "brush" {
				token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, id, nil), "", 4.5, 5.5)
				if err != nil {
					t.Fatal(err)
				}
				preview := maskPreview(t, svc, token)
				if got := pixelAt(t, preview, 4, 5); got != [4]uint8{} {
					t.Fatalf("mask dab preview=%v", got)
				}
				if got := pixelAt(t, preview, 12, 5); got != [4]uint8{255, 0, 0, 255} {
					t.Fatalf("preview lost procedural shape: %v", got)
				}
				if sess.bitmaps[key] != original || sess.hist.UndoCount() != 0 {
					t.Fatal("preview committed expanded mask")
				}
				if _, err := svc.EndPaintStroke(token, false); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := svc.DrawGradient(tab, 4.5, 5.5, 12.5, 5.5, maskGradientJSON(t, id), ""); err != nil {
					t.Fatal(err)
				}
			}
			committed := sess.bitmaps[key]
			if committed.W != 16 || committed.H != 12 {
				t.Fatalf("uniform mask did not expand: %dx%d", committed.W, committed.H)
			}
			assertOpaqueMask(t, committed)
			assertDrawingPixel(t, svc, tab, 4, 5, [4]uint8{})
			assertDrawingPixel(t, svc, tab, 12, 5, [4]uint8{255, 0, 0, 255})
			if sess.bitmaps[*l.ImageFile] != image || !bytes.Equal(image.Pix, imageBytes) || l.Shape == nil || payloadJSON(t, l.Text) != textBefore {
				t.Fatal("mask edit changed image pixels or shape metadata")
			}
			if sess.hist.UndoCount() != 1 {
				t.Fatal("mask edit must be one undo step")
			}
			if _, err := svc.Undo(); err != nil {
				t.Fatal(err)
			}
			if sess.bitmaps[key] != original || original.W != 1 || original.Pix[0] != 255 {
				t.Fatal("undo did not restore original compact mask pointer")
			}
			assertDrawingPixel(t, svc, tab, 4, 5, [4]uint8{255, 0, 0, 255})
			if _, err := svc.Redo(); err != nil {
				t.Fatal(err)
			}
			if sess.bitmaps[key] != committed {
				t.Fatal("redo did not restore expanded raster")
			}
			assertDrawingPixel(t, svc, tab, 4, 5, [4]uint8{})
		})
	}
}

func TestMaskPaintingCancelNoopPreservesCompactMaskAndRedo(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, id, nil), "", 4.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	original, revision, rev := sess.bitmaps[key], sess.hist.Revision(), sess.rev
	dirty := svc.ws.Snapshot().Tabs[0].Dirty
	for _, tc := range []struct {
		name      string
		cancel    bool
		x         float64
		extra     map[string]any
		selection string
	}{
		{name: "cancel", cancel: true, x: 4.5},
		{name: "outside", x: -100},
		{name: "same color", x: 4.5, extra: map[string]any{"r": 1, "g": 1, "b": 1}},
		{name: "empty selection", x: 4.5, selection: solidSelPayload(0, 0, 16, 12, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, id, tc.extra), tc.selection, tc.x, 5.5)
			if err != nil {
				t.Fatal(err)
			}
			maskPreview(t, svc, token)
			if _, err := svc.EndPaintStroke(token, tc.cancel); err != nil {
				t.Fatal(err)
			}
			if sess.bitmaps[key] != original || sess.hist.Revision() != revision || sess.rev != rev || !sess.hist.CanRedo() || sess.hist.UndoCount() != 0 || svc.ws.Snapshot().Tabs[0].Dirty != dirty {
				t.Fatal("cancel/noop changed compact asset, history, revision, redo, or dirty state")
			}
		})
	}
	if _, err := svc.DrawGradient(tab, 0.5, 5.5, 15.5, 5.5, maskGradientJSON(t, id), solidSelPayload(0, 0, 16, 12, 0)); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[key] != original || sess.hist.Revision() != revision || !sess.hist.CanRedo() {
		t.Fatal("empty gradient committed mask expansion")
	}
}

func TestMaskPaintingRejectsAbsentDisabledAndMissingPinnedTargets(t *testing.T) {
	for _, problem := range []string{"absent", "disabled", "missing asset", "missing layer"} {
		for _, tool := range []string{"brush", "gradient"} {
			t.Run(problem+"/"+tool, func(t *testing.T) {
				svc, tab, sess, id, key := maskPaintingFixture(t)
				l := mustLayerByID(t, sess.doc, id)
				switch problem {
				case "absent":
					l.MaskFile = nil
				case "disabled":
					disabled := false
					l.MaskEnabled = &disabled
				case "missing asset":
					delete(sess.bitmaps, key)
				case "missing layer":
					id = "missing"
				}
				var err error
				if tool == "brush" {
					_, err = svc.BeginPaintStroke(tab, maskBrushJSON(t, id, nil), "", 4.5, 5.5)
				} else {
					_, err = svc.DrawGradient(tab, 0.5, 5.5, 15.5, 5.5, maskGradientJSON(t, id), "")
				}
				if err == nil || strings.Contains(err.Error(), "unknown field") {
					t.Fatalf("want target validation error, got %v", err)
				}
				if sess.stroke != nil || sess.hist.UndoCount() != 0 || sess.rev != 0 {
					t.Fatal("invalid target changed state")
				}
			})
		}
	}
}

func TestMaskPaintingPlacedMaskUsesOwnTransformAndSelection(t *testing.T) {
	for _, tool := range []string{"brush", "gradient"} {
		t.Run(tool, func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			l := mustLayerByID(t, sess.doc, id)
			linked := false
			l.MaskLinked = &linked
			l.MaskPlacement = &domain.Transform{Origin: [2]float64{4, 2}, Size: [2]float64{8, 8}, FlipX: true, Rotation: 90, Sampling: domain.SamplingNearest}
			sess.bitmaps[key] = newLayerMaskBitmap(8, 8, bytes.Repeat([]byte{255}, 64))
			placement := *l.MaskPlacement
			x, y := layerIndexToDoc(placement, 8, 8, 2.5, 3.5)
			selection := solidSelPayload(int(x), int(y), 1, 1, 128)
			if tool == "brush" {
				token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, id, nil), selection, x, y)
				if err != nil {
					t.Fatal(err)
				}
				if got := pixelAt(t, maskPreview(t, svc, token), int(x), int(y)); got != [4]uint8{127, 127, 127, 127} {
					t.Fatalf("placed preview=%v", got)
				}
				if _, err := svc.EndPaintStroke(token, false); err != nil {
					t.Fatal(err)
				}
			} else {
				settings := payloadJSON(t, map[string]any{"layerID": id, "targetMask": true, "style": "foregroundToBackground"})
				if _, err := svc.DrawGradient(tab, x, y, x+4, y, settings, selection); err != nil {
					t.Fatal(err)
				}
			}
			if got := pixelAt(t, sess.bitmaps[key], 2, 3); got != [4]uint8{127, 127, 127, 255} {
				t.Fatalf("placed selected mask pixel=%v", got)
			}
			if got := pixelAt(t, sess.bitmaps[key], 5, 3); got != [4]uint8{255, 255, 255, 255} {
				t.Fatalf("unselected mask pixel=%v", got)
			}
			if *mustLayerByID(t, sess.doc, id).MaskPlacement != placement {
				t.Fatal("painting changed mask placement")
			}
			assertDrawingPixel(t, svc, tab, int(x), int(y), [4]uint8{127, 127, 127, 127})
		})
	}
}

func TestMaskPaintingGroupAdjustmentMasksAndLuminance(t *testing.T) {
	for _, kind := range []string{"group", "adjustment"} {
		for _, tool := range []string{"brush", "gradient"} {
			t.Run(kind+"/"+tool, func(t *testing.T) {
				svc, tab := newTestService(t, 16, 12)
				sess := activeSessionOf(t, svc)
				l := newPixelLayer(kind, 16, 12)
				l.ImageFile = nil
				key := l.ID + ".mask.png"
				l.MaskFile = &key
				if kind == "group" {
					group := true
					l.IsGroup = &group
					sess.doc.Layers[0].ParentID = idPtr(l.ID)
				} else {
					l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
				}
				sess.doc.Layers = append(sess.doc.Layers, l)
				sess.bitmaps[key] = newLayerMaskBitmap(1, 1, []uint8{255})
				if tool == "brush" {
					token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, l.ID, map[string]any{"r": 1, "erase": true}), "", 4.5, 5.5)
					if err != nil {
						t.Fatal(err)
					}
					maskPreview(t, svc, token)
					if _, err := svc.EndPaintStroke(token, false); err != nil {
						t.Fatal(err)
					}
				} else {
					settings := payloadJSON(t, map[string]any{"layerID": l.ID, "targetMask": true, "style": "foregroundToBackground", "foreground": map[string]float64{"r": 1}, "background": map[string]float64{"r": 1}})
					if _, err := svc.DrawGradient(tab, 0.5, 5.5, 15.5, 5.5, settings, ""); err != nil {
						t.Fatal(err)
					}
				}
				assertOpaqueMask(t, sess.bitmaps[key])
				if got := pixelAt(t, sess.bitmaps[key], 4, 5); got != [4]uint8{54, 54, 54, 255} {
					t.Fatalf("red luminance mask=%v", got)
				}
				want := [4]uint8{54, 54, 54, 54}
				if kind == "adjustment" {
					want = [4]uint8{201, 201, 201, 255}
				}
				assertDrawingPixel(t, svc, tab, 4, 5, want)
			})
		}
	}
}

func TestMaskPaintingConflictUsesOriginalAssetPointer(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, id, nil), "", 4.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.PaintStrokePoint(token, 5.5, 5.5); err != nil {
		t.Fatalf("private expansion caused conflict: %v", err)
	}
	replacement := sess.bitmaps[key].Clone()
	sess.bitmaps[key] = replacement
	if _, err := svc.EndPaintStroke(token, false); err == nil || !strings.Contains(err.Error(), "PAINT_CONFLICT") {
		t.Fatalf("want original pointer conflict, got %v", err)
	}
	if sess.bitmaps[key] != replacement || sess.hist.UndoCount() != 0 || sess.stroke != nil {
		t.Fatal("conflicted stroke committed or stayed busy")
	}
}

func TestHealingStrokeExplicitLayerIDSurvivesActiveLayerSwitch(t *testing.T) {
	svc, tab, sess, id, maskKey := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	key := *l.ImageFile
	base := sess.bitmaps[key]
	for i := 0; i < len(base.Pix); i += 4 {
		base.Pix[i], base.Pix[i+1], base.Pix[i+2], base.Pix[i+3] = 100, 100, 100, 255
	}
	for y := 4; y < 7; y++ {
		for x := 6; x < 9; x++ {
			i := (y*base.W + x) * 4
			base.Pix[i], base.Pix[i+1], base.Pix[i+2] = 255, 255, 255
		}
	}
	otherKey := *sess.doc.Layers[1].ImageFile
	other, mask := sess.bitmaps[otherKey], sess.bitmaps[maskKey]
	settings := payloadJSON(t, map[string]any{"layerID": id, "mode": 1, "diameter": 4, "hardness": 1, "opacity": 1, "seed": 7})
	// The active layer changed before the queued Begin reaches the bridge.
	token, err := svc.BeginHealingStroke(tab, settings, "", 7.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	maskPreview(t, svc, token)
	if _, err := svc.EndHealingStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[key] == base || pixelAt(t, sess.bitmaps[key], 7, 5)[0] >= 255 {
		t.Fatal("healing did not edit the explicitly pinned non-active layer")
	}
	if sess.bitmaps[otherKey] != other || sess.bitmaps[maskKey] != mask {
		t.Fatal("healing edited the active layer or its target's mask")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[key] != base {
		t.Fatal("healing undo did not restore pinned image asset")
	}
	missing := payloadJSON(t, map[string]any{"layerID": "missing", "diameter": 4})
	if _, err := svc.BeginHealingStroke(tab, missing, "", 7.5, 5.5); err == nil {
		t.Fatal("missing explicit healing target fell back to active layer")
	}
}

func TestMaskPaintingUniformImageExpandsMaskToDisplayedGrid(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	sess.bitmaps[*l.ImageFile] = newLayerMaskBitmap(1, 1, []uint8{255})
	token, err := svc.BeginPaintStroke(tab, maskBrushJSON(t, id, nil), "", 4.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if got := sess.bitmaps[key]; got.W != 16 || got.H != 12 {
		t.Fatalf("compact image left mask in uniform %dx%d grid", got.W, got.H)
	}
	assertDrawingPixel(t, svc, tab, 4, 5, [4]uint8{})
	assertDrawingPixel(t, svc, tab, 12, 5, [4]uint8{255, 255, 255, 255})
}
