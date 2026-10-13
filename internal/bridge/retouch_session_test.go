package bridge

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"compositor-win/internal/render"
)

// Observe retouch pixels at the public pixel-plane boundary used by Canvas.
// Fixtures may install source pixels through the approved document seam.
func retouchPixels(t *testing.T, svc *Service, tabID, layerID string) *render.Bitmap {
	t.Helper()
	rec := httptest.NewRecorder()
	url := fmt.Sprintf("/pixel/%s/%s/image.rgba", tabID, layerID)
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, rec.Code, rec.Body.String())
	}
	w, err := strconv.Atoi(rec.Header().Get("X-Pixel-Width"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := strconv.Atoi(rec.Header().Get("X-Pixel-Height"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != w*h*4 {
		t.Fatalf("pixel body has %d bytes for %dx%d", rec.Body.Len(), w, h)
	}
	bmp := render.NewBitmap(w, h)
	copy(bmp.Pix, rec.Body.Bytes())
	return bmp
}

func TestRetouchCloneCommitsInitialDabOnceUndoRedo(t *testing.T) {
	svc, tabID := newTestService(t, 16, 8)
	_, _, source := moveFixture(t, svc)
	// A premultiplied half-transparent source must remain premultiplied.
	i := (3*source.W + 2) * 4
	copy(source.Pix[i:i+4], []byte{128, 32, 0, 128})
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	before := retouchPixels(t, svc, tabID, layerID)
	settings := `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":3.5}`
	token, err := svc.BeginRetouchStroke(tabID, settings, "", 10.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	if !uuidRe.MatchString(token) {
		t.Fatalf("Begin returned %q instead of a raw token", token)
	}
	if !bytes.Equal(retouchPixels(t, svc, tabID, layerID).Pix, before.Pix) {
		t.Fatal("Begin changed committed pixels")
	}
	out, err := svc.EndRetouchStroke(token, false)
	if err != nil {
		t.Fatal(err)
	}
	if env := parseEnvelope(t, out); env.Rev != 1 || env.Doc.DocumentID != tabID {
		t.Fatalf("unexpected End envelope: %+v", env)
	}
	after := retouchPixels(t, svc, tabID, layerID)
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "Clone Stamp" {
		t.Fatalf("clone undo name = %q, want %q", got, "Clone Stamp")
	}
	if got := pixelAt(t, after, 10, 3); got != [4]uint8{128, 32, 0, 128} {
		t.Fatalf("initial clone dab = %v, want [128 32 0 128]", got)
	}
	if got := pixelAt(t, after, 2, 3); got != [4]uint8{128, 32, 0, 128} {
		t.Fatalf("clone changed its source: %v", got)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retouchPixels(t, svc, tabID, layerID).Pix, before.Pix) {
		t.Fatal("Undo did not restore source pixels")
	}
	if _, err := svc.Undo(); err == nil {
		t.Fatal("one dab created more than one undo entry")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retouchPixels(t, svc, tabID, layerID).Pix, after.Pix) {
		t.Fatal("Redo did not restore clone output")
	}
	if _, err := svc.EndRetouchStroke(token, false); err == nil {
		t.Fatal("ended token was accepted")
	}
}

func TestRetouchCloneAllowsZeroUnusedBlurRadius(t *testing.T) {
	svc, tabID := newTestService(t, 16, 8)
	_, _, bmp := moveFixture(t, svc)
	source := (3*bmp.W + 2) * 4
	bmp.Pix[source], bmp.Pix[source+1], bmp.Pix[source+2], bmp.Pix[source+3] = 255, 0, 0, 255
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID

	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"blurRadius":0,"sourceX":2.5,"sourceY":3.5}`, "", 10.5, 3.5)
	if err != nil {
		t.Fatalf("clone with unused blurRadius=0: %v", err)
	}
	if _, err := svc.EndRetouchStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, retouchPixels(t, svc, tabID, layerID), 10, 3); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("clone output with unused blurRadius=0 = %v, want opaque red", got)
	}
}

func TestRetouchRespectsExplicitOwnerAfterActiveLayerChanges(t *testing.T) {
	svc, tabID := newTestService(t, 16, 8)
	sess, _, bmp := moveFixture(t, svc)
	ownerID := *sess.doc.ActiveLayerID
	source := (3*bmp.W + 2) * 4
	copy(bmp.Pix[source:source+4], []byte{255, 0, 0, 255})
	mustLayerOp(t, svc, "addLayer", "{}")
	otherID := *sess.doc.ActiveLayerID
	otherBefore := retouchPixels(t, svc, tabID, otherID).Clone()
	settings := fmt.Sprintf(`{"layerID":%q,"targetMask":false,"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":3.5}`, ownerID)
	token, err := svc.BeginRetouchStroke(tabID, settings, "", 10.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndRetouchStroke(token, false); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, retouchPixels(t, svc, tabID, ownerID), 10, 3); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("queued clone missed its pinned owner: %v", got)
	}
	if !bytes.Equal(retouchPixels(t, svc, tabID, otherID).Pix, otherBefore.Pix) {
		t.Fatal("queued clone modified the newly active layer")
	}
	settings = fmt.Sprintf(`{"layerID":%q,"targetMask":true,"kind":"blur"}`, ownerID)
	if _, err := svc.BeginRetouchStroke(tabID, settings, "", 5, 3); err == nil {
		t.Fatal("unsupported mask retouch silently edited pixels")
	}
}

func TestRetouchWarpModesProduceOutputAndOneUndoStep(t *testing.T) {
	for _, kind := range []string{"blur", "smudge", "liquify"} {
		t.Run(kind, func(t *testing.T) {
			svc, tabID := newTestService(t, 32, 16)
			sess, key, _ := moveFixture(t, svc)
			for y := 0; y < 16; y++ {
				for x := 0; x < 32; x++ {
					i := (y*32 + x) * 4
					v := uint8((x*17 + y*11) & 255)
					sess.bitmaps[key].Pix[i], sess.bitmaps[key].Pix[i+1], sess.bitmaps[key].Pix[i+2], sess.bitmaps[key].Pix[i+3] = v, uint8(255-v), v/2, 255
				}
			}
			layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
			before := retouchPixels(t, svc, tabID, layerID)
			settings := fmt.Sprintf(`{"kind":%q,"diameter":8,"hardness":0.7,"opacity":1,"strength":1,"blurRadius":3}`, kind)
			token, err := svc.BeginRetouchStroke(tabID, settings, "", 12.5, 8.5)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "blur" {
				if err := svc.RetouchStrokePoint(token, 20.5, 8.5); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.EndRetouchStroke(token, false); err != nil {
				t.Fatal(err)
			}
			after := retouchPixels(t, svc, tabID, layerID)
			if bytes.Equal(after.Pix, before.Pix) {
				t.Fatalf("%s did not change pixels", kind)
			}
			if got := sess.hist.UndoCount(); got != 1 {
				t.Fatalf("%s undo count = %d, want 1", kind, got)
			}
			wantName := map[string]string{"blur": "Blur", "smudge": "Smudge", "liquify": "Liquify"}[kind]
			if got := sess.hist.UndoName(); got != wantName {
				t.Fatalf("%s undo name = %q, want %q", kind, got, wantName)
			}
			if _, err := svc.Undo(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(retouchPixels(t, svc, tabID, layerID).Pix, before.Pix) {
				t.Fatalf("%s Undo did not restore pixels", kind)
			}
		})
	}
}

func TestRetouchCancelAndNoopPreserveHistoryAndRedo(t *testing.T) {
	svc, tabID := newTestService(t, 20, 12)
	sess, key, _ := moveFixture(t, svc)
	for i := range sess.bitmaps[key].Pix {
		sess.bitmaps[key].Pix[i] = 255
	}
	sourcePixel := (2*20 + 2) * 4
	sess.bitmaps[key].Pix[sourcePixel], sess.bitmaps[key].Pix[sourcePixel+1], sess.bitmaps[key].Pix[sourcePixel+2], sess.bitmaps[key].Pix[sourcePixel+3] = 0, 0, 255, 255
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	settings := `{"kind":"blur","diameter":6,"hardness":1,"opacity":1,"strength":1,"blurRadius":3}`
	start := retouchPixels(t, svc, tabID, layerID)
	token, err := svc.BeginRetouchStroke(tabID, settings, "", 8.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndRetouchStroke(token, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retouchPixels(t, svc, tabID, layerID).Pix, start.Pix) || sess.hist.UndoCount() != 0 {
		t.Fatal("cancel changed pixels or history")
	}

	// A zero-strength warp is a public no-op. It must not consume the redo
	// entry left by the preceding undo.
	cloneSettings := `{"kind":"clone","diameter":4,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":2.5}`
	clone, err := svc.BeginRetouchStroke(tabID, cloneSettings, "", 10.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndRetouchStroke(clone, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !sess.hist.CanRedo() {
		t.Fatal("redo entry after undo is missing")
	}
	noopSettings := `{"kind":"liquify","diameter":8,"hardness":1,"opacity":1,"strength":0}`
	noop, err := svc.BeginRetouchStroke(tabID, noopSettings, "", 8.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndRetouchStroke(noop, false); err != nil {
		t.Fatal(err)
	}
	if !sess.hist.CanRedo() || sess.hist.UndoCount() != 0 {
		t.Fatalf("no-op changed history: undo=%d redo=%v", sess.hist.UndoCount(), sess.hist.CanRedo())
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
}

func TestRetouchSelectionClipsOutputAndUndo(t *testing.T) {
	svc, tabID := newTestService(t, 16, 8)
	_, _, bmp := moveFixture(t, svc)
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i+3] = 255
	}
	source := (3*bmp.W + 2) * 4
	bmp.Pix[source], bmp.Pix[source+1], bmp.Pix[source+2] = 255, 0, 0
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	before := retouchPixels(t, svc, tabID, layerID)
	settings := `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":3.5}`
	token, err := svc.BeginRetouchStroke(tabID, settings, selPayload(10, 3, 1, 1), 10.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndRetouchStroke(token, false); err != nil {
		t.Fatal(err)
	}
	after := retouchPixels(t, svc, tabID, layerID)
	if got := pixelAt(t, after, 10, 3); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("selected clone pixel = %v, want opaque red", got)
	}
	for i := range after.Pix {
		if (i / 4) == (3*after.W + 10) {
			continue
		}
		if after.Pix[i] != before.Pix[i] {
			t.Fatalf("selection changed pixel byte %d outside selected destination", i)
		}
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 1 {
		t.Fatalf("selection clone undo count = %d, want 1", got)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retouchPixels(t, svc, tabID, layerID).Pix, before.Pix) {
		t.Fatal("selection clone Undo did not restore all pixels")
	}
}

func TestRetouchStrokeRemainsPinnedToOriginatingTab(t *testing.T) {
	svc, tabA := newTestService(t, 16, 8)
	sessA, keyA, _ := moveFixture(t, svc)
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			i := (y*16 + x) * 4
			v := uint8((x*31 + y*17) & 255)
			sessA.bitmaps[keyA].Pix[i], sessA.bitmaps[keyA].Pix[i+1], sessA.bitmaps[keyA].Pix[i+2], sessA.bitmaps[keyA].Pix[i+3] = v, uint8(255-v), v/2, 255
		}
	}
	layerA := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	beforeA := retouchPixels(t, svc, tabA, layerA)
	token, err := svc.BeginRetouchStroke(tabA, `{"kind":"blur","diameter":6,"hardness":1,"opacity":1,"strength":1,"blurRadius":3}`, "", 7.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	snapB, err := svc.ws.NewDocument(16, 8, 72)
	if err != nil {
		t.Fatal(err)
	}
	layerB := *activeSessionOf(t, svc).doc.ActiveLayerID
	beforeB := retouchPixels(t, svc, snapB.ActiveID, layerB)
	out, err := svc.EndRetouchStroke(token, false)
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, out).Doc.DocumentID != tabA {
		t.Fatal("End returned the active tab instead of the originating tab")
	}
	if bytes.Equal(retouchPixels(t, svc, tabA, layerA).Pix, beforeA.Pix) || sessA.hist.UndoCount() != 1 {
		t.Fatal("originating tab did not receive the retouch history entry")
	}
	if !bytes.Equal(retouchPixels(t, svc, snapB.ActiveID, layerB).Pix, beforeB.Pix) {
		t.Fatal("ending tab A retouch changed tab B")
	}
}

func TestRetouchClonePreviewIsReadOnlyAndKeepsTokenLive(t *testing.T) {
	svc, tabID := newTestService(t, 16, 8)
	sess, _, bmp := moveFixture(t, svc)
	source := (3*bmp.W + 2) * 4
	bmp.Pix[source], bmp.Pix[source+1], bmp.Pix[source+2], bmp.Pix[source+3] = 255, 0, 0, 255
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	before := retouchPixels(t, svc, tabID, layerID)
	revBefore := sess.rev
	historyBefore := sess.hist.Revision()
	undoBefore := sess.hist.UndoCount()
	_, dirtyBefore, ok := svc.ws.TabState(tabID)
	if !ok {
		t.Fatal("retouch tab disappeared before preview")
	}

	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":3.5}`, "", 10.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.retouchStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, preview, 10, 3); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("clone preview initial dab = %v, want opaque red", got)
	}
	if bytes.Equal(preview.Pix, before.Pix) {
		t.Fatal("clone preview did not contain the live dab")
	}
	if got := retouchPixels(t, svc, tabID, layerID); !bytes.Equal(got.Pix, before.Pix) {
		t.Fatal("clone preview changed committed layer pixels")
	}
	if sess.rev != revBefore || sess.hist.Revision() != historyBefore || sess.hist.UndoCount() != undoBefore {
		t.Fatal("clone preview changed revision or history")
	}
	_, dirtyAfter, ok := svc.ws.TabState(tabID)
	if !ok {
		t.Fatal("retouch tab disappeared after preview")
	}
	if dirtyAfter != dirtyBefore {
		t.Fatal("clone preview changed the dirty state")
	}

	// A preview must not retire the token: the queued next point is accepted.
	if err := svc.RetouchStrokePoint(token, 11.5, 3.5); err != nil {
		t.Fatalf("point after clone preview: %v", err)
	}
	if _, err := svc.EndRetouchStroke(token, true); err != nil {
		t.Fatal(err)
	}
	if got := retouchPixels(t, svc, tabID, layerID); !bytes.Equal(got.Pix, before.Pix) {
		t.Fatal("cancel after clone preview changed committed pixels")
	}
	if sess.rev != revBefore || sess.hist.Revision() != historyBefore || sess.hist.UndoCount() != undoBefore {
		t.Fatal("cancel after clone preview changed revision or history")
	}
}

func TestRetouchClonePreviewDoesNotFinishSmoothingTail(t *testing.T) {
	svc, tabID := newTestService(t, 20, 8)
	_, _, bmp := moveFixture(t, svc)
	for x := 0; x < 10; x++ {
		source := (3*bmp.W + x) * 4
		bmp.Pix[source], bmp.Pix[source+1], bmp.Pix[source+2], bmp.Pix[source+3] = 255, 0, 0, 255
	}
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	before := retouchPixels(t, svc, tabID, layerID)
	settings := `{"kind":"clone","diameter":4,"hardness":1,"opacity":1,"smoothing":4,"sourceX":2.5,"sourceY":3.5}`
	token, err := svc.BeginRetouchStroke(tabID, settings, "", 10.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	// This point is inside the smoothing string. It updates the raw pointer,
	// but must not lay a dab until the string is pulled taut.
	if err := svc.RetouchStrokePoint(token, 13.5, 3.5); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.retouchStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, preview, 13, 3); got != pixelAt(t, before, 13, 3) {
		t.Fatalf("clone preview finalized smoothing tail at (13,3): got %v, want %v", got, pixelAt(t, before, 13, 3))
	}
	if _, err := svc.EndRetouchStroke(token, true); err != nil {
		t.Fatal(err)
	}
}

func TestRetouchWarpPreviewIsReadOnlyAndKeepsTokenLive(t *testing.T) {
	svc, tabID := newTestService(t, 32, 16)
	sess, key, _ := moveFixture(t, svc)
	bmp := sess.bitmaps[key]
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			v := uint8((x*17 + y*11) & 255)
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = v, uint8(255-v), v/2, 255
		}
	}
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	before := retouchPixels(t, svc, tabID, layerID)
	revBefore := sess.rev
	historyBefore := sess.hist.Revision()
	undoBefore := sess.hist.UndoCount()
	_, dirtyBefore, _ := svc.ws.TabState(tabID)

	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"blur","diameter":8,"hardness":0.7,"opacity":1,"strength":1,"blurRadius":3}`, "", 12.5, 8.5)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RetouchStrokePoint(token, 20.5, 8.5); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.retouchStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(preview.Pix, before.Pix) {
		t.Fatal("warp preview did not contain the live blur")
	}
	if got := retouchPixels(t, svc, tabID, layerID); !bytes.Equal(got.Pix, before.Pix) {
		t.Fatal("warp preview changed committed layer pixels")
	}
	if sess.rev != revBefore || sess.hist.Revision() != historyBefore || sess.hist.UndoCount() != undoBefore {
		t.Fatal("warp preview changed revision or history")
	}
	_, dirtyAfter, ok := svc.ws.TabState(tabID)
	if !ok || dirtyAfter != dirtyBefore {
		t.Fatal("warp preview changed the dirty state")
	}

	if err := svc.RetouchStrokePoint(token, 21.5, 8.5); err != nil {
		t.Fatalf("point after warp preview: %v", err)
	}
	if _, err := svc.EndRetouchStroke(token, true); err != nil {
		t.Fatal(err)
	}
	if got := retouchPixels(t, svc, tabID, layerID); !bytes.Equal(got.Pix, before.Pix) {
		t.Fatal("cancel after warp preview changed committed pixels")
	}
}

func TestRetouchPreviewCompositesPinnedDocumentWithoutMutatingOtherLayers(t *testing.T) {
	svc, tabID := newTestService(t, 16, 8)
	mustLayerOp(t, svc, "addLayer", "{}")
	sess := activeSessionOf(t, svc)
	bottom := &sess.doc.Layers[0]
	target := &sess.doc.Layers[1]
	background := sess.bitmaps[*bottom.ImageFile]
	for i := 0; i < len(background.Pix); i += 4 {
		background.Pix[i], background.Pix[i+1], background.Pix[i+2], background.Pix[i+3] = 0, 180, 0, 255
	}
	base := sess.bitmaps[*target.ImageFile]
	source := (3*base.W + 2) * 4
	base.Pix[source], base.Pix[source+1], base.Pix[source+2], base.Pix[source+3] = 255, 0, 0, 255
	sess.renderPNG = nil
	beforeTarget := base.Clone()
	beforeRevision := sess.hist.Revision()
	beforeUndo := sess.hist.UndoCount()

	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":3.5}`, "", 10.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.retouchStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, preview, 0, 0); got != [4]uint8{0, 180, 0, 255} {
		t.Fatalf("preview changed the non-target layer: %v", got)
	}
	if got := pixelAt(t, preview, 10, 3); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("composited clone preview = %v, want opaque red", got)
	}
	if !bytes.Equal(base.Pix, beforeTarget.Pix) {
		t.Fatal("composited preview mutated the pinned target bitmap")
	}
	if sess.hist.Revision() != beforeRevision || sess.hist.UndoCount() != beforeUndo {
		t.Fatal("composited preview changed history")
	}
	if _, err := svc.EndRetouchStroke(token, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(base.Pix, beforeTarget.Pix) {
		t.Fatal("cancel after composited preview changed the target bitmap")
	}
}

func TestRetouchLiveShapePreviewMatchesCommitAndUndoRestoresShape(t *testing.T) {
	svc, tabID := newTestService(t, 32, 24)
	out, err := svc.DrawShape(tabID, "Ellipse", 4, 4, 28, 20, `{"foreground":{"r":1,"g":0,"b":0}}`)
	if err != nil {
		t.Fatal(err)
	}
	before := parseEnvelope(t, out)
	shapeID := *before.Doc.ActiveLayerID
	assertDrawingPixel(t, svc, tabID, 5, 5, [4]uint8{255, 255, 255, 255})

	// Clone the red center into a transparent corner of the live ellipse.
	// The full preview must show these pending pixels without rasterizing the
	// editable document until the gesture commits.
	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":16.5,"sourceY":12.5}`, "", 5.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png?sequence=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("live shape preview = %d: %s", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	preview := render.BitmapFromImage(img)
	if got := pixelAt(t, preview, 5, 5); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("pending clone over ellipse corner = %v, want opaque red", got)
	}
	snapshot := mustDocumentSnapshot(t, svc)
	if snapshot.Rev != before.Rev || snapshot.Doc.Layers[1].ID != shapeID || snapshot.Doc.Layers[1].Shape == nil {
		t.Fatal("preview changed revision or rasterized the editable shape")
	}
	assertDrawingPixel(t, svc, tabID, 5, 5, [4]uint8{255, 255, 255, 255})

	out, err = svc.EndRetouchStroke(token, false)
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, out).Doc.Layers[1].Shape != nil {
		t.Fatal("commit retained live shape metadata over the cloned pixels")
	}
	assertDrawingPixel(t, svc, tabID, 5, 5, [4]uint8{255, 0, 0, 255})
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if mustDocumentSnapshot(t, svc).Doc.Layers[1].Shape == nil {
		t.Fatal("Undo did not restore the editable ellipse")
	}
	assertDrawingPixel(t, svc, tabID, 5, 5, [4]uint8{255, 255, 255, 255})
}
