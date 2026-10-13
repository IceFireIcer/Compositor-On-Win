package bridge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestDestructiveFiltersRasterizeProceduralMetadataAndUndo(t *testing.T) {
	for _, path := range []string{"dialog", "one-shot"} {
		t.Run(path, func(t *testing.T) {
			svc, _ := newTestService(t, 16, 16)
			sess := activeSessionOf(t, svc)
			paintRect(sess, 0, 0, 16, 16, 100)
			l := &sess.doc.Layers[0]
			l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
			l.Text = &domain.TextStyle{Content: "editable"}
			original := firstLayerBitmap(t, sess).Clone()
			if path == "dialog" {
				if _, err := svc.BeginFilterEdit(FilterCameraRaw, "", `{}`, 7, ""); err != nil {
					t.Fatal(err)
				}
				// Raster sessions must also drop any adjustment metadata at commit.
				l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
				if _, err := svc.CommitFilter(`{"cameraRaw":{"exposure":1}}`); err != nil {
					t.Fatal(err)
				}
			} else {
				l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
				if _, err := svc.ApplyFilter("adjust:Invert", "", `{}`, 7, ""); err != nil {
					t.Fatal(err)
				}
			}
			filtered := firstLayerBitmap(t, sess).Clone()
			if bytes.Equal(original.Pix, filtered.Pix) {
				t.Fatal("fixture did not change pixels")
			}
			assertRaster := func() {
				t.Helper()
				l := &sess.doc.Layers[0]
				if l.Shape != nil || l.Text != nil || l.Adjustment != nil {
					t.Fatal("destructive filter left procedural metadata hiding the raster")
				}
				visible, err := render.Render(sess.doc, sess.pixelSource())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(visible.Pix, filtered.Pix) {
					t.Fatal("composite does not show filtered raster")
				}
			}
			assertRaster()
			if _, err := svc.Undo(); err != nil {
				t.Fatal(err)
			}
			l = &sess.doc.Layers[0]
			if l.Shape == nil || l.Text == nil || l.Adjustment == nil || !bytes.Equal(firstLayerBitmap(t, sess).Pix, original.Pix) {
				t.Fatal("undo failed to restore procedural metadata and pixels")
			}
			if _, err := svc.Redo(); err != nil {
				t.Fatal(err)
			}
			assertRaster()
		})
	}
}

func TestFilterCancelAndNoOpPreserveProceduralMetadata(t *testing.T) {
	for _, terminal := range []string{"cancel", "noop", "one-shot-noop"} {
		t.Run(terminal, func(t *testing.T) {
			svc, _ := newTestService(t, 16, 16)
			sess := activeSessionOf(t, svc)
			paintRect(sess, 0, 0, 16, 16, 100)
			l := &sess.doc.Layers[0]
			l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
			l.Text = &domain.TextStyle{Content: "editable"}
			before, _ := json.Marshal(sess.doc)
			history := sess.hist.Revision()
			var err error
			if terminal == "one-shot-noop" {
				_, err = svc.ApplyFilter("adjust:Exposure", "", `{"adjustment":{"kind":"Exposure","exposureSettings":{"gamma":1}}}`, 8, "")
			} else {
				if _, err := svc.BeginFilterEdit("adjust:Exposure", "", `{}`, 8, ""); err != nil {
					t.Fatal(err)
				}
				if terminal == "cancel" {
					_, err = svc.CancelFilterEdit()
				} else {
					_, err = svc.CommitFilter(`{"adjustment":{"kind":"Exposure","exposureSettings":{"gamma":1}}}`)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(sess.doc)
			if !bytes.Equal(before, after) || history != sess.hist.Revision() {
				t.Fatal("cancel/no-op changed metadata or history")
			}
		})
	}
}

func TestFilterPreviewRasterClonePreservesLiveMetadata(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 100)
	l := &sess.doc.Layers[0]
	l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
	l.Text = &domain.TextStyle{Content: "editable"}
	if _, err := svc.BeginFilterEdit(FilterCameraRaw, "", `{}`, 7, ""); err != nil {
		t.Fatal(err)
	}
	l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentInvert}
	before, _ := json.Marshal(sess.doc)
	sess.filter.preview = firstLayerBitmap(t, sess).Clone()
	for i := 0; i < len(sess.filter.preview.Pix); i += 4 {
		sess.filter.preview.Pix[i] = 155
	}
	doc, source := sess.filterRenderSource()
	visible, err := render.Render(doc, source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(visible.Pix, sess.filter.preview.Pix) {
		t.Fatal("procedural metadata hides the preview raster")
	}
	after, _ := json.Marshal(sess.doc)
	if !bytes.Equal(before, after) {
		t.Fatal("preview mutated live metadata")
	}
	if _, err := svc.CancelFilterEdit(); err != nil {
		t.Fatal(err)
	}
	after, _ = json.Marshal(sess.doc)
	if !bytes.Equal(before, after) {
		t.Fatal("cancel failed to preserve metadata")
	}
}

func TestLevelsSamplerUsesDocumentMappingAndFrozenSource(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 32, 32, 100)
	sess.doc.Layers[0].Transform.Origin = [2]float64{8, 8}
	bmp := firstLayerBitmap(t, sess)
	i := (1*bmp.W + 1) * 4
	bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2] = 200, 180, 160
	if _, err := svc.BeginFilterEdit("adjust:Levels", "", `{}`, 7, ""); err != nil {
		t.Fatal(err)
	}
	// A later pixel edit must not change the open dialog's original sample.
	bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2] = 10, 10, 10
	settings := domain.LevelsSettings{}
	for c := range settings.Ranges {
		settings.Ranges[c] = domain.LevelRange{Gamma: 1, White: 255, OutputWhite: 255}
	}
	payload, _ := json.Marshal(settings)
	raw, err := svc.ApplyLevelsSample(string(payload), 9, 9, 0)
	if err != nil {
		t.Fatal(err)
	}
	var levels domain.LevelsSettings
	if err := json.Unmarshal([]byte(raw), &levels); err != nil {
		t.Fatal(err)
	}
	if levels.Ranges[1].Black != 200 || levels.Ranges[2].Black != 180 || levels.Ranges[3].Black != 160 {
		t.Fatalf("sampled wrong coordinates/source: %+v", levels.Ranges)
	}
}

func ownedFilterSettings(tabID string, seed uint32, radius int) string {
	b, _ := json.Marshal(map[string]any{
		"radius":        radius,
		"filterSession": map[string]any{"tabId": tabID, "seed": seed},
	})
	return string(b)
}

func TestFilterSessionRequestsRemainOwnedAcrossTabSwitch(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	a := activeSessionOf(t, svc)
	idA := a.doc.DocumentID
	paintRect(a, 8, 8, 24, 24, 200)
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", ownedFilterSettings(idA, 41, 1), 41, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.newDocumentWithWhiteBackground(32, 32, 72); err != nil {
		t.Fatal(err)
	}
	b := activeSessionOf(t, svc)
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{"radius":1}`, 42, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFilterPreview(ownedFilterSettings(idA, 41, 4)); err != nil {
		t.Fatal(err)
	}
	if a.filter.params.Radius != 4 || b.filter.params.Radius != 1 {
		t.Fatalf("preview targeted wrong tab: A=%v B=%v", a.filter.params.Radius, b.filter.params.Radius)
	}
	if _, err := svc.CommitFilter(ownedFilterSettings(idA, 41, 4)); err != nil {
		t.Fatal(err)
	}
	if a.filter != nil || b.filter == nil {
		t.Fatal("confirm closed the wrong tab's session")
	}
	if !svc.ws.tabs[0].Dirty {
		t.Fatal("confirm did not dirty the owning tab")
	}
}

func TestFilterSessionRejectsLateRequestsAfterReopen(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	id := activeSessionOf(t, svc).doc.DocumentID
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{"radius":1}`, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelFilterEdit(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{"radius":2}`, 2, ""); err != nil {
		t.Fatal(err)
	}
	for _, call := range []func(string) (string, error){svc.UpdateFilterPreview, svc.CommitFilter} {
		if _, err := call(ownedFilterSettings(id, 1, 8)); err == nil {
			t.Fatal("stale session request was accepted")
		}
	}
	if got := activeSessionOf(t, svc).filter; got == nil || got.params.Radius != 2 {
		t.Fatal("late request changed the new session")
	}
}

func TestFilterCancelTargetsOwnerAndRejectsReplacedSession(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	a := activeSessionOf(t, svc)
	idA := a.doc.DocumentID
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{}`, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.newDocumentWithWhiteBackground(16, 16, 72); err != nil {
		t.Fatal(err)
	}
	b := activeSessionOf(t, svc)
	if _, err := svc.BeginFilterEdit(FilterGaussian, "", `{}`, 2, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelFilterSession(idA, 9); err == nil {
		t.Fatal("accepted stale cancellation")
	}
	if a.filter == nil || b.filter == nil {
		t.Fatal("stale cancel changed an open session")
	}
	raw, err := svc.CancelFilterSession(idA, 1)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Doc domain.Document `json:"doc"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Doc.DocumentID != idA || a.filter != nil || b.filter == nil {
		t.Fatal("cancel targeted or returned the active tab instead of its owner")
	}
}

func TestNoOpFilterConfirmationAdvancesDisplayRevision(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 128)
	beforeRev, beforeHistory := sess.rev, sess.hist.Revision()
	if _, err := svc.BeginFilterEdit("adjust:Exposure", "", `{"adjustment":{"kind":"Exposure","exposureSettings":{"exposure":1,"gamma":1}}}`, 1, ""); err != nil {
		t.Fatal(err)
	}
	// A changed preview may be visible even when OK has reset to identity.
	sess.filter.previewRev = 1
	if _, err := svc.CommitFilter(`{"adjustment":{"kind":"Exposure","exposureSettings":{"exposure":0,"gamma":1}}}`); err != nil {
		t.Fatal(err)
	}
	if sess.rev <= beforeRev {
		t.Fatal("no-op OK leaves the old preview revision current in the frontend")
	}
	if sess.hist.Revision() != beforeHistory {
		t.Fatal("no-op OK added history")
	}
}

func TestCameraRawSamplersRejectMetadataOnlySessions(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	l := &sess.doc.Layers[0]
	l.ImageFile = nil
	l.Adjustment = &domain.Adjustment{Kind: domain.AdjustmentExposure}
	if _, err := svc.BeginFilterEdit("adjust:Exposure", l.ID, `{"adjustment":null}`, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CameraRawAutoWhiteBalance(); err == nil || !strings.Contains(err.Error(), "Camera Raw") {
		t.Fatalf("want Camera Raw session error, got %v", err)
	}
}
