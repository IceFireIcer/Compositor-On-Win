package bridge

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

func renderedPixelAt(t *testing.T, svc *Service, tabID string, x, y int) [4]uint8 {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/render/"+tabID+".png", nil)
	svc.RenderHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("render request returned %d: %s", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("decode rendered PNG: %v", err)
	}
	r, g, b, a := img.At(x, y).RGBA()
	return [4]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func TestSetLayerMaskCreateHideAllRendersAndUndoRedo(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	env := mustDocumentSnapshot(t, svc)
	layerID := *env.Doc.ActiveLayerID
	if got := renderedPixelAt(t, svc, tabID, 0, 0); got != [4]uint8{255, 255, 255, 255} {
		t.Fatalf("unmasked background pixel = %v, want opaque white", got)
	}

	out, err := svc.SetLayerMask(tabID, layerID, "createHideAll", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := parseEnvelope(t, out).Rev; got != 1 {
		t.Fatalf("create mask rev = %d, want 1", got)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0); got[3] != 0 {
		t.Fatalf("hide-all mask rendered alpha = %d, want 0", got[3])
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0); got != [4]uint8{255, 255, 255, 255} {
		t.Fatalf("undo rendered pixel = %v, want opaque white", got)
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0); got[3] != 0 {
		t.Fatalf("redo rendered alpha = %d, want 0", got[3])
	}
}

func TestSetLayerMaskCreatesRevealAll(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	out, err := svc.SetLayerMask(tabID, layerID, "createRevealAll", "")
	if err != nil {
		t.Fatal(err)
	}
	layer := parseEnvelope(t, out).Doc.Layers[0]
	if layer.MaskFile == nil || *layer.MaskFile != layerID+".mask.png" || layer.MaskEnabled == nil || !*layer.MaskEnabled || layer.MaskLinked == nil || !*layer.MaskLinked || layer.MaskPlacement != nil {
		t.Fatalf("created mask metadata = %+v", layer)
	}
	if got := renderedPixelAt(t, svc, tabID, 3, 3); got != [4]uint8{255, 255, 255, 255} {
		t.Fatalf("reveal-all pixel = %v, want opaque white", got)
	}
}

func TestSetLayerMaskCreatesSelectionInTransformedLayerGrid(t *testing.T) {
	for _, tc := range []struct {
		op   string
		want [4]uint8
	}{
		{"createRevealSelection", [4]uint8{255, 128, 0, 0}},
		{"createHideSelection", [4]uint8{0, 127, 255, 255}},
	} {
		t.Run(tc.op, func(t *testing.T) {
			svc, tabID := newTestService(t, 4, 4)
			layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
			// Scaling and flipping map the document's x=3 selection to local x=0.
			transform := domain.Transform{Origin: [2]float64{0, 0}, Size: [2]float64{4, 4}, FlipX: true, Sampling: domain.SamplingNearest}
			if _, err := svc.ApplyLayerTransform(layerID, payloadJSON(t, transform)); err != nil {
				t.Fatal(err)
			}
			selection := payloadJSON(t, map[string]any{"x": 2, "y": 0, "w": 2, "h": 1, "mask": []uint8{128, 255}})
			if _, err := svc.SetLayerMask(tabID, layerID, tc.op, selection); err != nil {
				t.Fatal(err)
			}
			for x, want := range tc.want {
				// Local order is mirrored on screen.
				if got := renderedPixelAt(t, svc, tabID, 3-x, 0)[3]; got != want {
					t.Fatalf("rendered mask at local x=%d = %d, want %d", x, got, want)
				}
			}
			outside := uint8(0)
			if tc.op == "createHideSelection" {
				outside = 255
			}
			if got := renderedPixelAt(t, svc, tabID, 0, 1)[3]; got != outside {
				t.Fatalf("outside selection coverage = %d, want %d", got, outside)
			}
		})
	}
}

func TestSetLayerMaskEnableDisableRendersAndUndoRedo(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	if _, err := svc.SetLayerMask(tabID, layerID, "createHideAll", ""); err != nil {
		t.Fatal(err)
	}

	disabled, err := svc.SetLayerMask(tabID, layerID, "disable", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0)[3]; got != 255 {
		t.Fatalf("disabled hide-all mask alpha = %d, want 255", got)
	}
	if layer := parseEnvelope(t, disabled).Doc.Layers[0]; layer.MaskEnabled == nil || *layer.MaskEnabled {
		t.Fatalf("disabled mask metadata = %+v", layer)
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0)[3]; got != 0 {
		t.Fatalf("undo disable alpha = %d, want 0", got)
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0)[3]; got != 255 {
		t.Fatalf("redo disable alpha = %d, want 255", got)
	}

	enabled, err := svc.SetLayerMask(tabID, layerID, "enable", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0)[3]; got != 0 {
		t.Fatalf("enabled hide-all mask alpha = %d, want 0", got)
	}
	if layer := parseEnvelope(t, enabled).Doc.Layers[0]; layer.MaskEnabled == nil || !*layer.MaskEnabled {
		t.Fatalf("enabled mask metadata = %+v", layer)
	}
}

func TestSetLayerMaskLinkUnlinkPreservesDocumentPlacement(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	initial := mustDocumentSnapshot(t, svc)
	layerID := *initial.Doc.ActiveLayerID
	selection := payloadJSON(t, map[string]any{
		"x": 0, "y": 0, "w": 4, "h": 4,
		"mask": []uint8{
			255, 255, 0, 0,
			255, 255, 0, 0,
			255, 255, 0, 0,
			255, 255, 0, 0,
		},
	})
	if _, err := svc.SetLayerMask(tabID, layerID, "createRevealSelection", selection); err != nil {
		t.Fatal(err)
	}
	unlinked, err := svc.SetLayerMask(tabID, layerID, "unlink", "")
	if err != nil {
		t.Fatal(err)
	}
	unlinkedLayer := parseEnvelope(t, unlinked).Doc.Layers[0]
	if unlinkedLayer.MaskLinked == nil || *unlinkedLayer.MaskLinked || unlinkedLayer.MaskPlacement == nil || *unlinkedLayer.MaskPlacement != initial.Doc.Layers[0].Transform {
		t.Fatalf("unlinked mask metadata = %+v", unlinkedLayer)
	}

	resized := domain.Transform{
		Origin:   [2]float64{0, 0},
		Size:     [2]float64{2, 4},
		Sampling: domain.SamplingNearest,
	}
	if _, err := svc.ApplyLayerTransform(layerID, payloadJSON(t, resized)); err != nil {
		t.Fatal(err)
	}
	// The unlinked mask stays on its original 4×4 document grid, so both
	// visible columns still sample its white left half.
	if got := renderedPixelAt(t, svc, tabID, 1, 0)[3]; got != 255 {
		t.Fatalf("unlinked mask after layer resize alpha = %d, want 255", got)
	}

	linked, err := svc.SetLayerMask(tabID, layerID, "link", "")
	if err != nil {
		t.Fatal(err)
	}
	linkedLayer := parseEnvelope(t, linked).Doc.Layers[0]
	if linkedLayer.MaskLinked == nil || !*linkedLayer.MaskLinked || linkedLayer.MaskPlacement == nil {
		t.Fatalf("linked mask metadata lost placement = %+v", linkedLayer)
	}
	// Once linked, the same 4×4 mask is resampled over the layer's 2×4
	// transform, making the second document column sample the black half.
	if got := renderedPixelAt(t, svc, tabID, 1, 0)[3]; got != 0 {
		t.Fatalf("linked mask after layer resize alpha = %d, want 0", got)
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 1, 0)[3]; got != 255 {
		t.Fatalf("undo link alpha = %d, want 255", got)
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 1, 0)[3]; got != 0 {
		t.Fatalf("redo link alpha = %d, want 0", got)
	}
}

func TestSetLayerMaskInvertIsCopyOnWriteAndUndoRedo(t *testing.T) {
	svc, tabID := newTestService(t, 4, 1)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	selection := payloadJSON(t, map[string]any{"x": 0, "y": 0, "w": 4, "h": 1, "mask": []uint8{0, 64, 128, 255}})
	if _, err := svc.SetLayerMask(tabID, layerID, "createRevealSelection", selection); err != nil {
		t.Fatal(err)
	}
	key := layerID + ".mask.png"
	before := activeSessionOf(t, svc).bitmaps[key]
	beforePixels := append([]uint8(nil), before.Pix...)

	if _, err := svc.SetLayerMask(tabID, layerID, "invert", ""); err != nil {
		t.Fatal(err)
	}
	after := activeSessionOf(t, svc).bitmaps[key]
	if after == before || !bytes.Equal(before.Pix, beforePixels) {
		t.Fatal("invert mutated or reused the original mask bitmap")
	}
	want := []uint8{255, 255, 255, 255, 191, 191, 191, 255, 127, 127, 127, 255, 0, 0, 0, 255}
	if !bytes.Equal(after.Pix, want) {
		t.Fatalf("inverted mask pixels = %v, want %v", after.Pix, want)
	}
	for x, wantAlpha := range []uint8{255, 191, 127, 0} {
		if got := renderedPixelAt(t, svc, tabID, x, 0)[3]; got != wantAlpha {
			t.Fatalf("inverted render x=%d alpha = %d, want %d", x, got, wantAlpha)
		}
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).bitmaps[key]; got != before || !bytes.Equal(got.Pix, beforePixels) {
		t.Fatal("undo did not restore the original mask bitmap")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).bitmaps[key]; got != after {
		t.Fatal("redo did not restore the inverted mask bitmap")
	}
}

func TestSetLayerMaskDeleteRetainsBitmapForUndo(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	if _, err := svc.SetLayerMask(tabID, layerID, "createHideAll", ""); err != nil {
		t.Fatal(err)
	}
	key := layerID + ".mask.png"
	mask := activeSessionOf(t, svc).bitmaps[key]
	if _, err := svc.SetLayerMask(tabID, layerID, "delete", ""); err != nil {
		t.Fatal(err)
	}
	deleted := mustDocumentSnapshot(t, svc)
	if deleted.Doc.Layers[0].MaskFile != nil || renderedPixelAt(t, svc, tabID, 0, 0)[3] != 255 {
		t.Fatalf("delete did not remove mask from render/manifest: %+v", deleted.Doc.Layers[0])
	}
	if got := activeSessionOf(t, svc).bitmaps[key]; got != mask {
		t.Fatal("delete discarded the orphan bitmap needed by undo")
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored := mustDocumentSnapshot(t, svc)
	if restored.Doc.Layers[0].MaskFile == nil || renderedPixelAt(t, svc, tabID, 0, 0)[3] != 0 {
		t.Fatalf("undo delete did not restore masked render: %+v", restored.Doc.Layers[0])
	}
	if got := activeSessionOf(t, svc).bitmaps[key]; got != mask {
		t.Fatal("undo delete did not reuse the retained bitmap")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := mustDocumentSnapshot(t, svc).Doc.Layers[0].MaskFile; got != nil {
		t.Fatalf("redo delete restored mask metadata: %v", got)
	}
}

func TestSetLayerMaskSupportsGroupAndAdjustmentLayers(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	initial := mustDocumentSnapshot(t, svc)
	baseID := initial.Doc.Layers[0].ID
	added := mustLayerOp(t, svc, "addLayer", "")
	addedID := *added.Doc.ActiveLayerID
	grouped := mustLayerOp(t, svc, "groupLayers", payloadJSON(t, map[string]any{"ids": []string{baseID, addedID}, "id": addedID}))
	var groupID string
	for i := range grouped.Doc.Layers {
		if grouped.Doc.Layers[i].IsGroupLayer() {
			groupID = grouped.Doc.Layers[i].ID
			break
		}
	}
	if groupID == "" {
		t.Fatal("groupLayers did not create a group target")
	}
	groupMask, err := svc.SetLayerMask(tabID, groupID, "createHideAll", "")
	if err != nil {
		t.Fatal(err)
	}
	groupLayer := snapshotLayer(t, parseEnvelope(t, groupMask).Doc, groupID)
	if groupLayer.MaskFile == nil || groupLayer.ImageFile != nil {
		t.Fatalf("group mask metadata = %+v", groupLayer)
	}
	if got := activeSessionOf(t, svc).bitmaps[*groupLayer.MaskFile]; got == nil || got.W != 1 || got.H != 1 {
		t.Fatalf("group mask grid = %+v", got)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0)[3]; got != 0 {
		t.Fatalf("group hide-all render alpha = %d, want 0", got)
	}

	adjustmentSvc, adjustmentTab := newTestService(t, 4, 4)
	adjustment := mustLayerOp(t, adjustmentSvc, "addAdjustment", `{"kind":"Gaussian Blur"}`)
	adjustmentID := *adjustment.Doc.ActiveLayerID
	selection := payloadJSON(t, map[string]any{"x": 0, "y": 0, "w": 4, "h": 4, "mask": []uint8{
		255, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
	}})
	adjustmentMask, err := adjustmentSvc.SetLayerMask(adjustmentTab, adjustmentID, "createRevealSelection", selection)
	if err != nil {
		t.Fatal(err)
	}
	adjustmentLayer := snapshotLayer(t, parseEnvelope(t, adjustmentMask).Doc, adjustmentID)
	if adjustmentLayer.MaskFile == nil || adjustmentLayer.ImageFile != nil || adjustmentLayer.Adjustment == nil {
		t.Fatalf("adjustment mask metadata = %+v", adjustmentLayer)
	}
	if got := activeSessionOf(t, adjustmentSvc).bitmaps[*adjustmentLayer.MaskFile]; got == nil || got.W != 4 || got.H != 4 {
		t.Fatalf("adjustment mask grid = %+v", got)
	}
}

func TestSetLayerMaskNoOpsPreserveRevisionDirtyAndRedo(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	if _, err := svc.SetLayerMask(tabID, layerID, "createRevealAll", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLayerMask(tabID, layerID, "disable", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	before := mustDocumentSnapshot(t, svc)
	sess := activeSessionOf(t, svc)
	undoCount, redoName := sess.hist.UndoCount(), sess.hist.RedoName()
	dirty := svc.ws.Snapshot().Tabs[0].Dirty

	for _, operation := range []string{"enable", "link"} {
		got, err := svc.SetLayerMask(tabID, layerID, operation, "")
		if err != nil {
			t.Fatalf("no-op %s: %v", operation, err)
		}
		after := parseEnvelope(t, got)
		if after.Rev != before.Rev || !reflect.DeepEqual(after.Doc, before.Doc) {
			t.Fatalf("no-op %s changed snapshot: before=%+v after=%+v", operation, before, after)
		}
		if activeSessionOf(t, svc).hist.UndoCount() != undoCount || activeSessionOf(t, svc).hist.RedoName() != redoName || svc.ws.Snapshot().Tabs[0].Dirty != dirty {
			t.Fatalf("no-op %s changed history/dirty state", operation)
		}
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, tabID, 0, 0)[3]; got != 255 {
		t.Fatalf("redo after no-op alpha = %d, want 255", got)
	}
}

func TestSetLayerMaskRejectsMalformedSelectionBusyAndStaleRequests(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	before := mustDocumentSnapshot(t, svc)
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{"missing selection", "", "需要选区"},
		{"malformed JSON", "{oops", "解析选区"},
		{"wrong mask size", `{"x":0,"y":0,"w":2,"h":1,"mask":[255]}`, "尺寸不符"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.SetLayerMask(tabID, layerID, "createRevealSelection", tc.json); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
			if got := mustDocumentSnapshot(t, svc); got.Rev != before.Rev || !reflect.DeepEqual(got.Doc, before.Doc) {
				t.Fatalf("rejected request changed document: before=%+v after=%+v", before, got)
			}
		})
	}
	if _, err := svc.SetLayerMask(tabID, layerID, "unknown", ""); err == nil {
		t.Fatal("unknown mask operation should fail")
	}
	if _, err := svc.SetLayerMask(tabID, layerID, "disable", ""); err == nil {
		t.Fatal("metadata operation without a mask should fail")
	}
	if _, err := svc.SetLayerMask("missing-tab", layerID, "createRevealAll", ""); err != errNoDocument {
		t.Fatalf("stale tab error = %v, want %v", err, errNoDocument)
	}

	busySvc, busyTab := newTestService(t, 4, 4)
	busyID := *mustDocumentSnapshot(t, busySvc).Doc.ActiveLayerID
	if _, err := busySvc.BeginStroke(1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := busySvc.SetLayerMask(busyTab, busyID, "createRevealAll", ""); err != errDocumentBusy {
		t.Fatalf("busy tab error = %v, want %v", err, errDocumentBusy)
	}
	if _, err := busySvc.EndStroke(); err != nil {
		t.Fatal(err)
	}
}

func TestSetLayerMaskPinsTheRequestedBackgroundTab(t *testing.T) {
	svc, firstTab := newTestService(t, 4, 4)
	second, err := svc.ws.newDocumentWithWhiteBackground(4, 4, 72)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.SelectTab(firstTab); err != nil {
		t.Fatal(err)
	}
	first := mustDocumentSnapshot(t, svc)
	firstLayerID := *first.Doc.ActiveLayerID
	if _, err := svc.ws.SelectTab(second.ActiveID); err != nil {
		t.Fatal(err)
	}

	out, err := svc.SetLayerMask(firstTab, firstLayerID, "createHideAll", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := parseEnvelope(t, out).Doc.DocumentID; got != first.Doc.DocumentID {
		t.Fatalf("background-tab response document = %s, want %s", got, first.Doc.DocumentID)
	}
	if got := renderedPixelAt(t, svc, firstTab, 0, 0)[3]; got != 0 {
		t.Fatalf("background-tab mask alpha = %d, want 0", got)
	}
	active := mustDocumentSnapshot(t, svc)
	if active.Doc.DocumentID != second.ActiveID || active.Doc.Layers[0].MaskFile != nil {
		t.Fatalf("background-tab edit changed active tab: %+v", active.Doc)
	}
	if got := renderedPixelAt(t, svc, second.ActiveID, 0, 0)[3]; got != 255 {
		t.Fatalf("active tab alpha after background edit = %d, want 255", got)
	}
}
