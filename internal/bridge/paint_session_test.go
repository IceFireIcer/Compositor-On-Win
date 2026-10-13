package bridge

import (
	"bytes"
	"encoding/json"
	"testing"

	"compositor-win/internal/domain"
)

const paintSettingsJSON = `{"diameter":6,"hardness":1,"opacity":0.5,"smoothing":0,"zoom":1,"erase":false,"r":1,"g":0,"b":0}`

func TestPaintSessionCommitsOneUndoableStroke(t *testing.T) {
	svc, tabID := newTestService(t, 32, 24)
	sess, key, before := moveFixture(t, svc)
	token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 8.5, 8.5)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token[0] == '{' {
		t.Fatalf("want raw token, got %q", token)
	}
	if err := svc.PaintStrokePoint(token, 20.5, 8.5); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) {
		t.Fatal("stroke changed committed pixels before End")
	}
	out, err := svc.EndPaintStroke(token, false)
	if err != nil {
		t.Fatal(err)
	}
	if env := parseEnvelope(t, out); env.Doc.DocumentID != tabID || env.Rev != 1 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	painted := append([]byte(nil), sess.bitmaps[key].Pix...)
	for _, x := range []int{8, 14, 20} {
		if got := pixelAt(t, sess.bitmaps[key], x, 8); got != [4]uint8{128, 0, 0, 128} {
			t.Fatalf("pixel %d = %v", x, got)
		}
	}
	if sess.hist.UndoCount() != 1 {
		t.Fatalf("undo steps = %d", sess.hist.UndoCount())
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) {
		t.Fatal("undo did not restore original pixels")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, painted) {
		t.Fatal("redo did not restore stroke")
	}
	if err := svc.PaintStrokePoint(token, 1, 1); err == nil {
		t.Fatal("ended token accepted")
	}
}

func TestPaintSessionSelectionClipsAndBlendsGrayCoverage(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask []uint8
		want [4]uint8
	}{
		{name: "outside", mask: []uint8{0}, want: [4]uint8{0, 0, 0, 0}},
		{name: "soft", mask: []uint8{128}, want: [4]uint8{64, 0, 0, 64}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, tabID := newTestService(t, 16, 16)
			sess, key, _ := moveFixture(t, svc)
			selection, err := json.Marshal(filterSelection{X: 7, Y: 7, W: 1, H: 1, Mask: tc.mask})
			if err != nil {
				t.Fatal(err)
			}
			token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, string(selection), 7.5, 7.5)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.EndPaintStroke(token, false); err != nil {
				t.Fatal(err)
			}
			if got := pixelAt(t, sess.bitmaps[key], 7, 7); got != tc.want {
				t.Fatalf("selected pixel = %v, want %v", got, tc.want)
			}
			wantHistory := 0
			if tc.name == "soft" {
				wantHistory = 1
			}
			if sess.hist.UndoCount() != wantHistory {
				t.Fatal("selection with no effective coverage must not create history")
			}
		})
	}

	svc, tabID := newTestService(t, 16, 16)
	sess, key, _ := moveFixture(t, svc)
	selection := `{"x":7,"y":7,"w":1,"h":1,"mask":[128]}`
	token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, selection, 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, sess.bitmaps[key], 7, 7); got != [4]uint8{64, 0, 0, 64} {
		t.Fatalf("half-selected pixel = %v, want [64 0 0 64]", got)
	}
	if sess.hist.UndoCount() != 1 {
		t.Fatalf("soft selection undo count = %d, want 1", sess.hist.UndoCount())
	}
}

func TestPaintSessionCancelAndOffCanvasNoopPreserveState(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	sess, key, before := moveFixture(t, svc)
	initialRev, initialDirty := sess.hist.Revision(), svc.ws.Snapshot().Tabs[0].Dirty
	token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) || sess.hist.Revision() != initialRev || sess.hist.UndoCount() != 0 || svc.ws.Snapshot().Tabs[0].Dirty != initialDirty {
		t.Fatal("cancel changed pixels, revision, history, or dirty state")
	}
	token, err = svc.BeginPaintStroke(tabID, paintSettingsJSON, "", -100, -100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) || sess.hist.Revision() != initialRev || sess.hist.UndoCount() != 0 || svc.ws.Snapshot().Tabs[0].Dirty != initialDirty {
		t.Fatal("off-canvas no-op changed pixels, revision, history, or dirty state")
	}
}

func TestPaintSessionRejectsConcurrentStrokeOnSameTab(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 8.5, 8.5); err == nil {
		t.Fatal("second paint token on one tab must be rejected")
	}
	if _, err := svc.EndPaintStroke(token, true); err != nil {
		t.Fatal(err)
	}
}

func TestPaintSessionRemainsPinnedToOriginatingTab(t *testing.T) {
	svc, tabA := newTestService(t, 16, 16)
	sessA, keyA, _ := moveFixture(t, svc)
	token, err := svc.BeginPaintStroke(tabA, paintSettingsJSON, "", 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.ws.NewDocument(16, 16, 72)
	if err != nil {
		t.Fatal(err)
	}
	sessB := svc.ws.sessions[snap.ActiveID]
	keyB := *sessB.doc.Layers[0].ImageFile
	beforeB := sessB.bitmaps[keyB].Clone()
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sessB.bitmaps[keyB].Pix, beforeB.Pix) {
		t.Fatal("ending tab A stroke changed tab B")
	}
	if got := pixelAt(t, sessA.bitmaps[keyA], 7, 7); got == [4]uint8{0, 0, 0, 0} {
		t.Fatal("originating tab A was not painted")
	}
}

func TestPaintSessionMapsDocumentPointThroughLayerTransform(t *testing.T) {
	svc, tabID := newTestService(t, 32, 24)
	sess, key, _ := moveFixture(t, svc)
	l := mustLayerByID(t, sess.doc, *sess.doc.ActiveLayerID)
	l.Transform = domain.Transform{Origin: [2]float64{5, 4}, Size: [2]float64{16, 12}, Sampling: domain.SamplingNearest}
	settings := `{"diameter":2,"hardness":1,"opacity":1,"zoom":1,"r":1}`
	token, err := svc.BeginPaintStroke(tabID, settings, "", 9, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, sess.bitmaps[key], 8, 6); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("mapped layer pixel (8,6) = %v, want opaque red", got)
	}
}

func TestPaintSessionShiftLineUsesExactDocumentEndpointsThroughTransform(t *testing.T) {
	svc, tabID := newTestService(t, 32, 24)
	sess, key, _ := moveFixture(t, svc)
	l := mustLayerByID(t, sess.doc, *sess.doc.ActiveLayerID)
	l.Transform = domain.Transform{Origin: [2]float64{5, 4}, Size: [2]float64{16, 12}, Sampling: domain.SamplingNearest}
	// The smoothing value is deliberately high. lineStart must select Stroke.Line
	// and therefore keep both transformed endpoints instead of trailing the
	// pointer behind the smoothing string.
	settings := `{"diameter":2,"hardness":1,"opacity":1,"smoothing":100,"zoom":1,"r":1,"lineStart":[7,7]}`
	token, err := svc.BeginPaintStroke(tabID, settings, "", 13, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		t.Fatal(err)
	}
	for _, point := range [][2]int{{4, 6}, {10, 9}, {16, 12}} {
		if got := pixelAt(t, sess.bitmaps[key], point[0], point[1]); got != [4]uint8{255, 0, 0, 255} {
			t.Fatalf("line endpoint/interior %v = %v, want opaque red", point, got)
		}
	}
	if sess.hist.UndoCount() != 1 {
		t.Fatalf("shift line undo count = %d, want 1", sess.hist.UndoCount())
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, sess.bitmaps[key], 4, 6); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("shift line undo endpoint = %v, want transparent", got)
	}
}

func TestPaintSessionRejectsInvalidShiftLineStart(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	if _, err := svc.BeginPaintStroke(tabID, `{"diameter":4,"lineStart":[NaN,1]}`, "", 4, 4); err == nil {
		t.Fatal("non-finite lineStart must be rejected")
	}
}

func TestPaintSessionRejectsUnsupportedLayer(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	l := mustLayerByID(t, sess.doc, *sess.doc.ActiveLayerID)
	l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	if _, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 7.5, 7.5); err == nil {
		t.Fatal("adjustment layer must not accept raster painting")
	}
}

func TestHealingSessionCommitsUndoableInitialDab(t *testing.T) {
	svc, tabID := newTestService(t, 32, 32)
	sess, key, _ := moveFixture(t, svc)
	bmp := sess.bitmaps[key]
	for i := range bmp.Pix {
		bmp.Pix[i] = 100
	}
	for y := 13; y < 19; y++ {
		for x := 13; x < 19; x++ {
			i := (y*bmp.W + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 255, 255, 255, 255
		}
	}
	before := append([]byte(nil), bmp.Pix...)
	token, err := svc.BeginHealingStroke(tabID, `{"mode":1,"diameter":6,"hardness":1,"smoothing":0,"zoom":1,"opacity":1,"seed":7}`, "", 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || token[0] == '{' {
		t.Fatalf("want raw healing token, got %q", token)
	}
	if bytes.Equal(sess.bitmaps[key].Pix, before) == false {
		t.Fatal("healing changed committed pixels before End")
	}
	out, err := svc.EndHealingStroke(token, false)
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, out).Rev != 1 || sess.hist.UndoCount() != 1 {
		t.Fatalf("healing commit did not create one revision: %s", out)
	}
	if got := pixelAt(t, sess.bitmaps[key], 16, 16); got[0] >= 255 {
		t.Fatalf("initial healing dab did not affect center: %v", got)
	}
	committed := append([]byte(nil), sess.bitmaps[key].Pix...)
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before) {
		t.Fatal("healing undo did not restore source")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, committed) {
		t.Fatal("healing redo did not restore commit")
	}
	if err := svc.HealingStrokePoint(token, 1, 1); err == nil {
		t.Fatal("ended healing token accepted")
	}
}

func TestHealingSessionCancelAndEmptySelectionAreNoop(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	sess, key, before := moveFixture(t, svc)
	rev := sess.hist.Revision()
	token, err := svc.BeginHealingStroke(tabID, `{"mode":1,"diameter":6,"hardness":1,"opacity":1}`, "", 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndHealingStroke(token, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) || sess.hist.Revision() != rev || sess.hist.UndoCount() != 0 {
		t.Fatal("healing cancel changed pixels or history")
	}
	token, err = svc.BeginHealingStroke(tabID, `{"mode":1,"diameter":6,"hardness":1,"opacity":1}`, `{"x":0,"y":0,"w":16,"h":16,"mask":[0]}`, 7.5, 7.5)
	if err == nil {
		t.Fatal("invalid selection dimensions should be rejected")
	}
	_ = token
	mask := make([]uint8, 16*16)
	selection := payloadJSON(t, filterSelection{X: 0, Y: 0, W: 16, H: 16, Mask: mask})
	token, err = svc.BeginHealingStroke(tabID, `{"mode":1,"diameter":6,"hardness":1,"opacity":1}`, selection, 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndHealingStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) || sess.hist.Revision() != rev || sess.hist.UndoCount() != 0 {
		t.Fatal("empty selection healing changed pixels or history")
	}
}
