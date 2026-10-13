package bridge

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"compositor-win/internal/render"
)

func TestStrokePreviewServesLivePaintWithoutCommitting(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	sess, key, before := moveFixture(t, svc)
	token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 7.5, 7.5)
	if err != nil {
		t.Fatal(err)
	}
	rev := sess.rev
	req := httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png?sequence=4", nil)
	rec := httptest.NewRecorder()
	if !svc.serveStrokePreview(rec, req) || rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Stroke-Sequence") != "4" {
		t.Fatalf("preview headers = %#v", rec.Header())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	preview := pixelAt(t, render.BitmapFromImage(img), 7, 7)
	if preview == [4]uint8{0, 0, 0, 0} {
		t.Fatal("preview did not include the initial dab")
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) || sess.rev != rev || sess.hist.UndoCount() != 0 {
		t.Fatal("preview mutated committed pixels, revision, or history")
	}
	if err := svc.PaintStrokePoint(token, 10.5, 7.5); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png?sequence=5", nil)) || rec.Code != http.StatusOK {
		t.Fatalf("second preview status = %d", rec.Code)
	}
	if _, err := svc.EndPaintStroke(token, true); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png", nil)) || rec.Code != http.StatusNotFound {
		t.Fatalf("ended preview status = %d, want 404", rec.Code)
	}
}

func TestStrokePreviewRejectsInvalidSequenceAndMethod(t *testing.T) {
	svc, tabID := newTestService(t, 8, 8)
	token, err := svc.BeginPaintStroke(tabID, paintSettingsJSON, "", 3.5, 3.5)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.EndPaintStroke(token, true)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "bad sequence", method: http.MethodGet, path: "/pixel/stroke/" + token + ".png?sequence=nope", want: http.StatusBadRequest},
		{name: "post", method: http.MethodPost, path: "/pixel/stroke/" + token + ".png", want: http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			svc.serveStrokePreview(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestStrokePreviewServesGradientAndRetiresAfterCommitOrCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "cancel"}[cancel], func(t *testing.T) {
			svc, tab := newTestService(t, 16, 12)
			sess := activeSessionOf(t, svc)
			layer := &sess.doc.Layers[0]
			key := *layer.ImageFile
			before := sess.bitmaps[key].Clone()
			rev := sess.rev
			settings := payloadJSONForGradient(map[string]any{
				"layerID": layer.ID, "shape": "Linear", "style": "foregroundToBackground", "opacity": 1,
				"foreground": map[string]float64{"b": 1}, "background": map[string]float64{"b": 1},
			})
			token, err := svc.BeginGradientEdit(tab, settings, "", 0.5, 5.5)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.UpdateGradientEdit(token, 0.5, 5.5, 15.5, 5.5, settings); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png?sequence=1", nil)) || rec.Code != http.StatusOK {
				t.Fatalf("gradient preview status = %d, body = %s", rec.Code, rec.Body.String())
			}
			img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if got := pixelAt(t, render.BitmapFromImage(img), 4, 5); got != [4]uint8{0, 0, 255, 255} {
				t.Fatalf("gradient preview pixel = %v, want opaque blue", got)
			}
			if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) || sess.rev != rev || sess.hist.UndoCount() != 0 {
				t.Fatal("gradient preview mutated committed pixels, revision, or history")
			}
			if _, err := svc.CommitGradientEdit(token, cancel); err != nil {
				t.Fatal(err)
			}
			rec = httptest.NewRecorder()
			if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png", nil)) || rec.Code != http.StatusNotFound {
				t.Fatalf("ended gradient preview status = %d, want 404", rec.Code)
			}
		})
	}
}

func TestStrokePreviewKeepsHealingTokenLiveAndRetiresAfterEnd(t *testing.T) {
	svc, tabID := newTestService(t, 24, 24)
	sess, key, before := moveFixture(t, svc)
	for i := range sess.bitmaps[key].Pix {
		if i%4 == 3 {
			sess.bitmaps[key].Pix[i] = 255
		} else {
			sess.bitmaps[key].Pix[i] = 96
		}
	}
	for y := 9; y < 15; y++ {
		for x := 9; x < 15; x++ {
			i := (y*sess.bitmaps[key].W + x) * 4
			sess.bitmaps[key].Pix[i], sess.bitmaps[key].Pix[i+1], sess.bitmaps[key].Pix[i+2] = 255, 255, 255
		}
	}
	before = sess.bitmaps[key].Clone()
	token, err := svc.BeginHealingStroke(tabID, `{"mode":1,"diameter":6,"hardness":1,"opacity":1}`, "", 12, 12)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png?sequence=2", nil)) || rec.Code != http.StatusOK {
		t.Fatalf("healing preview status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("healing preview is not PNG: %v", err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, before.Pix) {
		t.Fatal("healing preview mutated committed pixels")
	}
	if err := svc.HealingStrokePoint(token, 16, 12); err != nil {
		t.Fatalf("healing token was finalized by preview: %v", err)
	}
	if _, err := svc.EndHealingStroke(token, true); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png", nil)) || rec.Code != http.StatusNotFound {
		t.Fatalf("ended healing preview status = %d, want 404", rec.Code)
	}
}

func TestStrokePreviewServesRetouchAndFallsThroughOtherRoutes(t *testing.T) {
	svc, tabID := newTestService(t, 20, 12)
	sess, key, _ := moveFixture(t, svc)
	i := (2*sess.bitmaps[key].W + 2) * 4
	sess.bitmaps[key].Pix[i], sess.bitmaps[key].Pix[i+1], sess.bitmaps[key].Pix[i+2], sess.bitmaps[key].Pix[i+3] = 255, 0, 0, 255
	token, err := svc.BeginRetouchStroke(tabID, `{"kind":"clone","diameter":2,"hardness":1,"opacity":1,"sourceX":2.5,"sourceY":2.5}`, "", 10.5, 2.5)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if !svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/pixel/stroke/"+token+".png", nil)) || rec.Code != http.StatusOK {
		t.Fatalf("retouch preview status = %d, body = %s", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := pixelAt(t, render.BitmapFromImage(img), 10, 2); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("retouch preview target = %v, want opaque red", got)
	}
	if err := svc.RetouchStrokePoint(token, 12.5, 2.5); err != nil {
		t.Fatalf("retouch token was finalized by preview: %v", err)
	}
	if _, err := svc.EndRetouchStroke(token, true); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	if svc.serveStrokePreview(rec, httptest.NewRequest(http.MethodGet, "/render/other.png", nil)) {
		t.Fatal("preview handler claimed an unrelated route")
	}
}
