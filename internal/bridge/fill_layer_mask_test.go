package bridge

import (
	"bytes"
	"path/filepath"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// Exercise the public document endpoint, including actual mask pixels and the
// raster undo journal. Swift SelectionEdits.swift:53-72 is the fill/clear truth:
// an empty selection does nothing, and mask Clear fills with the background.
func TestFillLayerMaskSoftCoverageAndUndoRedo(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
	l.Text = &domain.TextStyle{Content: "Keep editable"}
	metadata := payloadJSON(t, l)
	original := sess.bitmaps[key]
	image := sess.bitmaps[*l.ImageFile]
	imageBytes := append([]byte(nil), image.Pix...)
	sess.hist.MarkSaved()
	sess.path = filepath.Join(t.TempDir(), "saved.comp")
	svc.ws.setTabDirtyLocked(tab, false)
	sel := selMaskPayload(2, 3, 4, 1, func(x, _ int) uint8 { return []uint8{255, 128, 1, 0}[x] })
	out, err := svc.FillLayerMask(tab, id, sel, "foreground", "")
	if err != nil {
		t.Fatal(err)
	}
	if env := parseEnvelope(t, out); env.Rev != 1 || env.Doc.DocumentID != tab {
		t.Fatalf("mask fill envelope = %+v", env)
	}
	after := sess.bitmaps[key]
	if after.W != 16 || after.H != 12 {
		t.Fatalf("partial fill must expand uniform mask: %dx%d", after.W, after.H)
	}
	assertOpaqueMask(t, after)
	for x, want := range []uint8{0, 127, 254, 255} {
		if got := pixelAt(t, after, x+2, 3); got != [4]uint8{want, want, want, 255} {
			t.Fatalf("coverage pixel %d = %v, want gray %d", x, got, want)
		}
	}
	if got := pixelAt(t, after, 0, 0); got != [4]uint8{255, 255, 255, 255} {
		t.Fatalf("outside selection changed: %v", got)
	}
	if payloadJSON(t, l) != metadata || sess.bitmaps[*l.ImageFile] != image || !bytes.Equal(image.Pix, imageBytes) {
		t.Fatal("mask fill changed layer metadata or image raster")
	}
	if sess.hist.UndoCount() != 1 || sess.hist.UndoName() != "填充蒙版" || !svc.ws.Snapshot().Tabs[0].Dirty {
		t.Fatal("fill must be one named, dirty undo transaction")
	}
	assertDrawingPixel(t, svc, tab, 2, 3, [4]uint8{})
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[key].W != 1 || !bytes.Equal(sess.bitmaps[key].Pix, original.Pix) || svc.ws.Snapshot().Tabs[0].Dirty {
		t.Fatal("undo did not restore compact mask and saved state")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, after.Pix) || sess.bitmaps[key].W != after.W {
		t.Fatal("redo did not restore mask pixels and dimensions")
	}
}

func TestFillLayerMaskClearUsesOpaqueBackground(t *testing.T) {
	for _, tc := range []struct {
		name, color string
		want        uint8
	}{
		{"default white", "", 255},
		{"custom background ignores alpha", `{"r":0.25,"g":0.25,"b":0.25,"a":0}`, 64},
		{"swapped background black", `{"r":0,"g":0,"b":0}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			sess.bitmaps[key] = newLayerMaskBitmap(1, 1, []uint8{100})
			if _, err := svc.FillLayerMask(tab, id, solidSelPayload(3, 2, 1, 1, 255), "clear", tc.color); err != nil {
				t.Fatal(err)
			}
			mask := sess.bitmaps[key]
			assertOpaqueMask(t, mask)
			if got := pixelAt(t, mask, 3, 2); got != [4]uint8{tc.want, tc.want, tc.want, 255} {
				t.Fatalf("mask Clear = %v, want opaque background %d", got, tc.want)
			}
			if got := pixelAt(t, mask, 4, 2); got != [4]uint8{100, 100, 100, 255} {
				t.Fatalf("Clear escaped selection: %v", got)
			}
		})
	}
}

func TestFillLayerMaskFeatherReachesPixels(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	mask := make([]uint8, 16*12)
	for y := 4; y < 8; y++ {
		for x := 4; x < 12; x++ {
			mask[y*16+x] = 255
		}
	}
	selection := payloadJSON(t, filterSelection{W: 16, H: 12, Mask: mask, Feather: 2})
	if _, err := svc.FillLayerMask(tab, id, selection, FillModeForeground, ""); err != nil {
		t.Fatal(err)
	}
	after := sess.bitmaps[key]
	assertOpaqueMask(t, after)
	// Feather must soften both sides of the hard edge, not just survive JSON.
	inside, outside := pixelAt(t, after, 4, 5)[0], pixelAt(t, after, 3, 5)[0]
	if inside == 0 || inside >= outside || outside == 255 {
		t.Fatalf("feather did not reach mask pixels: inside=%d outside=%d", inside, outside)
	}
	if pixelAt(t, after, 0, 0)[0] != 255 || sess.hist.UndoCount() != 1 {
		t.Fatal("feather escaped its support or recorded multiple edits")
	}
}

func TestFillLayerMaskWholeMaskAndColorLuminance(t *testing.T) {
	for _, tc := range []struct {
		mode, color string
		want        uint8
	}{
		{"foreground", "", 0},
		{"background", `{"r":0,"g":1,"b":0,"a":0}`, 182},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			if _, err := svc.FillLayerMask(tab, id, "", tc.mode, tc.color); err != nil {
				t.Fatal(err)
			}
			mask := sess.bitmaps[key]
			assertOpaqueMask(t, mask)
			for i := 0; i < len(mask.Pix); i += 4 {
				if mask.Pix[i] != tc.want {
					t.Fatalf("whole-mask fill pixel %d = %d, want %d", i/4, mask.Pix[i], tc.want)
				}
			}
		})
	}
}

func TestFillLayerMaskIndependentPlacement(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	l.Transform = domain.Transform{Origin: [2]float64{0, 0}, Size: [2]float64{2, 2}, Sampling: domain.SamplingNearest}
	linked := false
	l.MaskLinked = &linked
	l.MaskPlacement = &domain.Transform{Origin: [2]float64{6, 3}, Size: [2]float64{4, 4}, Rotation: 90, FlipX: true, Sampling: domain.SamplingNearest}
	metadata := payloadJSON(t, l)
	sess.bitmaps[key] = newLayerMaskBitmap(2, 2, []uint8{10, 20, 30, 40})
	// The first mask pixel's center lands at (9,6) after scale, flip, rotation.
	if _, err := svc.FillLayerMask(tab, id, solidSelPayload(9, 6, 1, 1, 128), "background", ""); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint8{133, 20, 30, 40} {
		if got := sess.bitmaps[key].Pix[i*4]; got != want {
			t.Fatalf("placed mask pixel %d = %d, want %d", i, got, want)
		}
	}
	if payloadJSON(t, l) != metadata {
		t.Fatal("mask fill changed independent placement")
	}
}

func TestFillLayerMaskUniformIndependentPlacement(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	linked := false
	l.MaskLinked = &linked
	l.MaskPlacement = &domain.Transform{Origin: [2]float64{7, 3}, Size: [2]float64{3, 2}, Sampling: domain.SamplingNearest}
	if _, err := svc.FillLayerMask(tab, id, solidSelPayload(8, 4, 1, 1, 255), "foreground", ""); err != nil {
		t.Fatal(err)
	}
	mask := sess.bitmaps[key]
	if mask.W != 3 || mask.H != 2 || pixelAt(t, mask, 1, 1)[0] != 0 || pixelAt(t, mask, 0, 0)[0] != 255 {
		t.Fatalf("uniform placed mask was not edited in its own grid: %+v", mask)
	}
}

func TestFillLayerMaskGroupAndAdjustmentTargets(t *testing.T) {
	for _, group := range []bool{true, false} {
		t.Run(map[bool]string{true: "group", false: "adjustment"}[group], func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			l := mustLayerByID(t, sess.doc, id)
			l.ImageFile = nil
			l.IsGroup = &group
			if !group {
				l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentLevels}
			}
			l.Transform = domain.Transform{Origin: [2]float64{2, 2}, Size: [2]float64{3, 2}, Sampling: domain.SamplingNearest}
			metadata := payloadJSON(t, l)
			if _, err := svc.FillLayerMask(tab, id, solidSelPayload(3, 2, 1, 1, 255), "foreground", ""); err != nil {
				t.Fatal(err)
			}
			mask := sess.bitmaps[key]
			if mask.W != 3 || mask.H != 2 || pixelAt(t, mask, 1, 0)[0] != 0 || pixelAt(t, mask, 0, 0)[0] != 255 {
				t.Fatal("non-image mask target was not edited in its own grid")
			}
			if payloadJSON(t, l) != metadata {
				t.Fatal("mask fill changed procedural metadata")
			}
		})
	}
}

func TestFillLayerMaskPinnedTabAndLayer(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	// Even the originating tab's active layer may change before this RPC runs.
	otherLayer := *mustLayerByID(t, sess.doc, id)
	otherLayer.ID = "other-layer"
	otherKey := "other-mask.png"
	otherLayer.MaskFile = &otherKey
	sess.doc.Layers = append(sess.doc.Layers, otherLayer)
	sess.doc.ActiveLayerID = &otherLayer.ID
	sess.bitmaps[otherKey] = newLayerMaskBitmap(1, 1, []uint8{80})
	otherMask := sess.bitmaps[otherKey]
	sess.hist.MarkSaved()
	svc.ws.setTabDirtyLocked(tab, false)
	other, err := svc.ws.newDocumentWithWhiteBackground(16, 12, 72)
	if err != nil {
		t.Fatal(err)
	}
	otherSess := svc.ws.sessions[other.ActiveID]
	otherSess.hist.MarkSaved()
	svc.ws.setTabDirtyLocked(other.ActiveID, false)
	otherDoc := payloadJSON(t, otherSess.doc)
	out, err := svc.FillLayerMask(tab, id, solidSelPayload(1, 1, 1, 1, 255), "foreground", "")
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, out).Doc.DocumentID != tab || svc.ws.Snapshot().ActiveID != other.ActiveID {
		t.Fatal("mask fill did not stay on the explicit tab")
	}
	if pixelAt(t, sess.bitmaps[key], 1, 1)[0] != 0 || payloadJSON(t, otherSess.doc) != otherDoc || otherSess.hist.UndoCount() != 0 || otherSess.rev != 0 {
		t.Fatal("mask fill touched the active tab instead of the captured owner")
	}
	if sess.bitmaps[otherKey] != otherMask || *sess.doc.ActiveLayerID != otherLayer.ID {
		t.Fatal("mask fill touched the newly active layer or changed layer selection")
	}
	snap := svc.ws.Snapshot()
	if !snap.Tabs[0].Dirty || snap.Tabs[1].Dirty {
		t.Fatal("mask fill dirtied the wrong tab")
	}
}

func TestFillLayerMaskNoOpsPreserveRedoAndCompactAsset(t *testing.T) {
	for _, tc := range []struct{ name, selection, mode, color string }{
		{"empty", solidSelPayload(0, 0, 16, 12, 0), "foreground", ""},
		{"misses layer", solidSelPayload(20, 20, 1, 1, 255), "foreground", ""},
		{"identical", "", "background", ""},
		{"clear without selection", "", "clear", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			if _, err := svc.FillLayerMask(tab, id, solidSelPayload(1, 1, 1, 1, 255), "foreground", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Undo(); err != nil {
				t.Fatal(err)
			}
			original, rev, journal := sess.bitmaps[key], sess.rev, len(sess.rasterJournal)
			snapshot := payloadJSON(t, svc.ws.Snapshot())
			out, err := svc.FillLayerMask(tab, id, tc.selection, tc.mode, tc.color)
			if err != nil {
				t.Fatal(err)
			}
			if parseEnvelope(t, out).Rev != rev || sess.bitmaps[key] != original || sess.hist.UndoCount() != 0 || !sess.hist.CanRedo() || len(sess.rasterJournal) != journal || payloadJSON(t, svc.ws.Snapshot()) != snapshot {
				t.Fatal("no-op changed pixels, revision, dirty state, history or redo")
			}
			if _, err := svc.Redo(); err != nil || pixelAt(t, sess.bitmaps[key], 1, 1)[0] != 0 {
				t.Fatalf("redo was lost after no-op: %v", err)
			}
		})
	}
}

func TestFillLayerMaskClipsToCanvas(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	l.Transform = domain.Transform{Origin: [2]float64{-1, 0}, Size: [2]float64{3, 1}, Sampling: domain.SamplingNearest}
	sess.bitmaps[key] = newLayerMaskBitmap(3, 1, []uint8{255, 255, 255})
	if _, err := svc.FillLayerMask(tab, id, solidSelPayload(-1, 0, 3, 1, 255), "foreground", ""); err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint8{255, 0, 0} {
		if got := sess.bitmaps[key].Pix[i*4]; got != want {
			t.Fatalf("canvas-clipped pixel %d = %d, want %d", i, got, want)
		}
	}
}

func TestFillLayerMaskRejectsInvalidAndBusyRequests(t *testing.T) {
	// I08 之后 "saving" 不再属于这一组：写在后台进行时不挡编辑，见
	// TestFillLayerMaskIsNotBlockedByASaveInFlight。
	for _, name := range []string{"tab", "layer", "empty layer", "mode", "selection", "color", "missing mask", "missing asset", "invalid asset", "disabled", "transform", "stroke", "filter", "heal"} {
		t.Run(name, func(t *testing.T) {
			svc, tab, sess, id, key := maskPaintingFixture(t)
			l := mustLayerByID(t, sess.doc, id)
			sel, mode, color := "", "foreground", ""
			switch name {
			case "tab":
				tab = "missing"
			case "layer":
				id = "missing"
			case "empty layer":
				id = ""
			case "mode":
				mode = "wrong"
			case "selection":
				sel = "{oops"
			case "color":
				color = "{oops"
			case "missing mask":
				l.MaskFile = nil
			case "missing asset":
				delete(sess.bitmaps, key)
			case "invalid asset":
				sess.bitmaps[key] = &render.Bitmap{W: 2, H: 2, Pix: []uint8{0}}
			case "disabled":
				enabled := false
				l.MaskEnabled = &enabled
			case "transform":
				l.Transform.Size[0] = 0
			case "stroke":
				sess.stroke = &render.Stroke{}
			case "filter":
				sess.filter = &filterSession{}
			case "heal":
				sess.heal = &healBrush{}
			}
			before := payloadJSON(t, sess.doc)
			original := sess.bitmaps[key]
			if _, err := svc.FillLayerMask(tab, id, sel, mode, color); err == nil {
				t.Fatal("invalid or busy request was accepted")
			}
			if sess.rev != 0 || sess.hist.UndoCount() != 0 || sess.bitmaps[key] != original || payloadJSON(t, sess.doc) != before {
				t.Fatal("rejected request changed document, asset or history")
			}
		})
	}
	if _, err := (&Service{}).FillLayerMask("tab", "layer", "", "foreground", ""); err != errNoDocument {
		t.Fatalf("service without workspace: %v", err)
	}
}

// I08：写在后台进行时不该把编辑挡在门外。上游在写之前就放开 isProjectBusy
// （ProjectController.swift:33-35），本仓的编辑入口不该再查 sess.saving。
func TestFillLayerMaskIsNotBlockedByASaveInFlight(t *testing.T) {
	svc, tab, sess, id, _ := maskPaintingFixture(t)
	sess.saving = true
	defer func() { sess.saving = false }()
	if _, err := svc.FillLayerMask(tab, id, "", "foreground", ""); err != nil {
		t.Fatalf("保存写入期间 FillLayerMask 被挡住了: %v", err)
	}
}
