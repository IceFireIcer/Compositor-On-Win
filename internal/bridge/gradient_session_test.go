package bridge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func gradientSessionSettings(layerID string) string {
	return payloadJSONForGradient(map[string]any{
		"layerID": layerID, "shape": "Linear", "style": "foregroundToBackground", "opacity": 1,
		"foreground": map[string]float64{"r": 0, "g": 0, "b": 0},
		"background": map[string]float64{"r": 1, "g": 1, "b": 1},
	})
}

func payloadJSONForGradient(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func gradientFilterRevision(t *testing.T, raw string) int {
	t.Helper()
	var envelope struct {
		FilterRev int `json:"filterRev"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.FilterRev
}

func TestGradientEditPreviewsFromOriginalAndCommitsOneUndo(t *testing.T) {
	svc, tab := newTestService(t, 16, 12)
	sess := activeSessionOf(t, svc)
	l := &sess.doc.Layers[0]
	key := *l.ImageFile
	original := sess.bitmaps[key]
	before := append([]byte(nil), original.Pix...)
	black := payloadJSONForGradient(map[string]any{
		"layerID": l.ID, "shape": "Linear", "style": "foregroundToBackground", "opacity": 1,
		"foreground": map[string]float64{"r": 0}, "background": map[string]float64{"r": 0},
	})
	token, err := svc.BeginGradientEdit(tab, black, "", 0.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if sess.stroke == nil || sess.bitmaps[key] != original || sess.hist.UndoCount() != 0 {
		t.Fatal("beginning gradient changed the document")
	}
	if out, err := svc.UpdateGradientEdit(token, 0.5, 5.5, 15.5, 5.5, black); err != nil {
		t.Fatal(err)
	} else if got := gradientFilterRevision(t, out); got != 1 {
		t.Fatalf("first gradient filterRev=%d", got)
	}
	first, err := svc.gradientStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, first, 4, 5); got != [4]uint8{0, 0, 0, 255} {
		t.Fatalf("first preview pixel=%v", got)
	}
	blue := payloadJSONForGradient(map[string]any{
		"layerID": l.ID, "shape": "Linear", "style": "foregroundToBackground", "opacity": 1,
		"foreground": map[string]float64{"b": 1}, "background": map[string]float64{"b": 1},
	})
	if out, err := svc.UpdateGradientEdit(token, 0.5, 5.5, 15.5, 5.5, blue); err != nil {
		t.Fatal(err)
	} else if got := gradientFilterRevision(t, out); got != 2 {
		t.Fatalf("second gradient filterRev=%d", got)
	}
	second, err := svc.gradientStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, second, 4, 5); got != [4]uint8{0, 0, 255, 255} {
		t.Fatalf("updated preview accumulated old gradient: %v", got)
	}
	if bytes.Equal(first.Pix, second.Pix) || !bytes.Equal(original.Pix, before) {
		t.Fatal("preview mutated the original raster")
	}
	out, err := svc.CommitGradientEdit(token, false)
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, out).Rev != 1 || sess.hist.UndoCount() != 1 || sess.bitmaps[key] == original {
		t.Fatal("gradient commit did not create one raster undo step")
	}
	assertDrawingPixel(t, svc, tab, 4, 5, [4]uint8{0, 0, 255, 255})
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[key] != original || !bytes.Equal(original.Pix, before) {
		t.Fatal("gradient undo did not restore original pointer and pixels")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
}

func TestGradientEditCancelNoopAndTargetPinning(t *testing.T) {
	svc, tab := newTestService(t, 16, 12)
	sess := activeSessionOf(t, svc)
	first := &sess.doc.Layers[0]
	other := newPixelLayer("Other", 16, 12)
	sess.bitmaps[*other.ImageFile] = render.NewBitmap(16, 12)
	sess.doc.Layers = append(sess.doc.Layers, other)
	sess.doc.ActiveLayerID = idPtr(other.ID)
	original := sess.bitmaps[*first.ImageFile]
	token, err := svc.BeginGradientEdit(tab, gradientSessionSettings(first.ID), "", 2.5, 2.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateGradientEdit(token, 2.5, 2.5, 12.5, 2.5, gradientSessionSettings(first.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitGradientEdit(token, true); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[*first.ImageFile] != original || sess.hist.UndoCount() != 0 || sess.rev != 0 {
		t.Fatal("cancel mutated the pinned target")
	}

	token, err = svc.BeginGradientEdit(tab, gradientSessionSettings(first.ID), "", 2.5, 2.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitGradientEdit(token, false); err != nil {
		t.Fatal(err)
	}
	if sess.bitmaps[*first.ImageFile] != original || sess.hist.UndoCount() != 0 || sess.rev != 0 {
		t.Fatal("zero-length gradient was not a no-op")
	}

	token, err = svc.BeginGradientEdit(tab, gradientSessionSettings(first.ID), "", 2.5, 2.5)
	if err != nil {
		t.Fatal(err)
	}
	sess.bitmaps[*first.ImageFile] = original.Clone()
	if _, err := svc.CommitGradientEdit(token, false); err == nil || !strings.Contains(err.Error(), "GRADIENT_CONFLICT") {
		t.Fatal("changed original pointer was not rejected")
	}
}

func TestGradientEditMaskPreviewPreservesShapeAndUsesLuminance(t *testing.T) {
	svc, tab, sess, id, key := maskPaintingFixture(t)
	l := mustLayerByID(t, sess.doc, id)
	l.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
	previewSettings := payloadJSONForGradient(map[string]any{
		"layerID": id, "targetMask": true, "shape": "Linear", "style": "foregroundToBackground", "opacity": 1,
		"foreground": map[string]float64{"r": 1, "g": 1, "b": 1}, "background": map[string]float64{"r": 1, "g": 1, "b": 1},
	})
	token, err := svc.BeginGradientEdit(tab, previewSettings, "", 4.5, 5.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateGradientEdit(token, 4.5, 5.5, 12.5, 5.5, previewSettings); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.gradientStrokePreview(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, preview, 12, 5); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("mask preview lost shape metadata: %v", got)
	}
	commitSettings := payloadJSONForGradient(map[string]any{
		"layerID": id, "targetMask": true, "shape": "Linear", "style": "foregroundToBackground", "opacity": 1,
		"foreground": map[string]float64{"r": 1}, "background": map[string]float64{"r": 1},
	})
	if _, err := svc.UpdateGradientEdit(token, 4.5, 5.5, 12.5, 5.5, commitSettings); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitGradientEdit(token, false); err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, sess.bitmaps[key], 4, 5); got != [4]uint8{54, 54, 54, 255} {
		t.Fatalf("mask gradient did not use luminance: %v", got)
	}
	if l.Shape == nil {
		t.Fatal("mask gradient cleared shape metadata")
	}
}
