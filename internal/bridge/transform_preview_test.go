package bridge

import (
	"bytes"
	"crypto/sha256"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestTransformPreviewHTTPVariantsPreserveCommittedState(t *testing.T) {
	svc, tab := newTestService(t, 20, 16)
	sess := activeSessionOf(t, svc)
	layer := &sess.doc.Layers[0]
	layerID := layer.ID
	layer.Transform.Origin = [2]float64{3, 4}
	layer.Transform.Size = [2]float64{12, 8}
	original := layer.Transform
	asset := render.NewBitmap(12, 8)
	for y := 0; y < asset.H; y++ {
		for x := 0; x < asset.W; x++ {
			i := (y*asset.W + x) * 4
			// Opaque, asymmetric pixels expose rotation and size changes.
			asset.Pix[i], asset.Pix[i+1], asset.Pix[i+2], asset.Pix[i+3] =
				uint8(20+x*17), uint8(30+y*23), uint8(40+x*5+y*7), 255
		}
	}
	sess.bitmaps[*layer.ImageFile] = asset
	beforePixels := bytes.Clone(asset.Pix)

	// Keep both undo and redo entries live so a preview cannot silently
	// replace an empty history with another empty history.
	mustLayerOp(t, svc, "rename", payloadJSON(t, map[string]string{"id": layerID, "name": "First name"}))
	mustLayerOp(t, svc, "rename", payloadJSON(t, map[string]string{"id": layerID, "name": "Second name"}))
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	beforeEnvelope, err := svc.DocumentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	beforeRev := sess.rev
	beforeHistoryRev, beforeUndoCount := sess.hist.Revision(), sess.hist.UndoCount()
	beforeUndoName, beforeRedoName := sess.hist.UndoName(), sess.hist.RedoName()
	beforeModified := sess.hist.IsModified()
	if !sess.hist.CanUndo() || !sess.hist.CanRedo() {
		t.Fatal("fixture must retain both undo and redo entries")
	}

	server := httptest.NewServer(svc.RenderHandler())
	t.Cleanup(server.Close)
	client := server.Client()
	fetchPNG := func(t *testing.T, query url.Values, cacheControl string) []byte {
		t.Helper()
		response, err := client.Get(server.URL + "/render/" + tab + ".png?" + query.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("HTTP status %d: %s", response.StatusCode, data)
		}
		if response.Header.Get("Content-Type") != "image/png" || response.Header.Get("Cache-Control") != cacheControl {
			t.Fatalf("unexpected PNG headers: %v", response.Header)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("HTTP response is not a valid PNG: %v", err)
		}
		if decoded.Bounds().Dx() != 20 || decoded.Bounds().Dy() != 16 {
			t.Fatalf("preview canvas bounds changed: %v", decoded.Bounds())
		}
		return data
	}
	beforePNG := fetchPNG(t, nil, "no-cache") // Warm the committed render cache.
	svc.ws.mu.Lock()
	beforeCache := bytes.Clone(sess.renderPNG)
	beforeCacheRev, beforeCacheFilterRev := sess.renderRev, sess.renderFilterRev
	svc.ws.mu.Unlock()

	sized, rotated, different := original, original, original
	sized.Size = [2]float64{8, 4}
	rotated.Rotation = 33
	different.Origin = [2]float64{11, 1}
	different.Size = [2]float64{6, 12}
	different.Rotation = -25
	different.FlipX = true
	seen := map[[32]byte]string{}
	for _, tc := range []struct {
		name      string
		transform domain.Transform
		unchanged bool
	}{
		{"origin and transform unchanged", original, true},
		{"size changed with origin unchanged", sized, false},
		{"rotation changed with origin unchanged", rotated, false},
		{"obviously different transform", different, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{"layerID": {layerID}, "transform": {payloadJSON(t, tc.transform)}, "rev": {strconv.Itoa(beforeRev)}}
			preview := fetchPNG(t, query, "no-store")
			hash := sha256.Sum256(preview)
			t.Logf("HTTP 200: PNG bytes=%d SHA256=%x transform=%s", len(preview), hash, payloadJSON(t, tc.transform))
			if bytes.Equal(beforePNG, preview) != tc.unchanged {
				t.Fatalf("preview equality with committed PNG = %v, want %v", bytes.Equal(beforePNG, preview), tc.unchanged)
			}
			if previous, exists := seen[hash]; exists {
				t.Fatalf("distinct transform reused %q PNG hash %x", previous, hash)
			}
			seen[hash] = tc.name
			afterEnvelope, err := svc.DocumentSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if afterEnvelope != beforeEnvelope {
				t.Fatal("preview changed committed document or numeric revision")
			}
			svc.ws.mu.Lock()
			defer svc.ws.mu.Unlock()
			if sess.hist.Revision() != beforeHistoryRev || sess.hist.UndoCount() != beforeUndoCount ||
				sess.hist.UndoName() != beforeUndoName || sess.hist.RedoName() != beforeRedoName ||
				sess.hist.IsModified() != beforeModified || !sess.hist.CanUndo() || !sess.hist.CanRedo() {
				t.Fatal("preview changed undo/redo history")
			}
			if sess.renderRev != beforeCacheRev || sess.renderFilterRev != beforeCacheFilterRev || !bytes.Equal(sess.renderPNG, beforeCache) {
				t.Fatal("preview changed the committed render cache")
			}
			if !bytes.Equal(asset.Pix, beforePixels) {
				t.Fatal("preview changed the committed pixel asset")
			}
		})
	}
	if afterPNG := fetchPNG(t, nil, "no-cache"); !bytes.Equal(afterPNG, beforePNG) {
		t.Fatal("committed HTTP render changed after transform previews")
	}
	redo, err := svc.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, redo).Doc.Layers[0].Name != "Second name" {
		t.Fatal("preview damaged the existing redo entry")
	}
	undo, err := svc.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, undo).Doc.Layers[0].Name != "First name" {
		t.Fatal("preview damaged the existing undo entry")
	}
}

func TestTransformPreviewMatchesCommitWithoutEditingDocument(t *testing.T) {
	svc, tab := newTestService(t, 8, 6)
	sess := activeSessionOf(t, svc)
	layer := &sess.doc.Layers[0]
	before, _, err := svc.ws.RenderPNG(tab)
	if err != nil {
		t.Fatal(err)
	}
	target := layer.Transform
	target.Origin = [2]float64{2, 1}
	query := url.Values{"layerID": {layer.ID}, "transform": {payloadJSON(t, target)}, "rev": {"0"}}
	rec := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/render/"+tab+".png?"+query.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status %d: %s", rec.Code, rec.Body.String())
	}
	if bytes.Equal(before, rec.Body.Bytes()) {
		t.Fatal("transform preview still displays the committed placement")
	}
	if sess.rev != 0 || sess.hist.UndoCount() != 0 || layer.Transform.Origin != ([2]float64{0, 0}) {
		t.Fatal("preview changed placement, revision, or undo history")
	}
	unchanged, _, _ := svc.ws.RenderPNG(tab)
	if !bytes.Equal(before, unchanged) {
		t.Fatal("preview poisoned the committed render cache")
	}
	if _, err := svc.ApplyLayerTransformToTab(tab, 0, layer.ID, payloadJSON(t, target)); err != nil {
		t.Fatal(err)
	}
	committed, _, err := svc.ws.RenderPNG(tab)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Body.Bytes(), committed) {
		t.Fatal("preview pixels differ from committed transform")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("draft previews must not be cached")
	}
}

func TestTransformPreviewRejectsInvalidOrStaleContext(t *testing.T) {
	svc, tab := newTestService(t, 8, 6)
	layer := &activeSessionOf(t, svc).doc.Layers[0]
	for _, tc := range []struct {
		name, layer, transform, rev string
		want                        int
	}{
		{"stale", layer.ID, payloadJSON(t, layer.Transform), "1", http.StatusConflict},
		{"missing revision", layer.ID, payloadJSON(t, layer.Transform), "", http.StatusBadRequest},
		{"missing layer", "absent", payloadJSON(t, layer.Transform), "0", http.StatusNotFound},
		{"bad transform", layer.ID, "{}", "0", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{"layerID": {tc.layer}, "transform": {tc.transform}, "rev": {tc.rev}}
			rec := httptest.NewRecorder()
			svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/render/"+tab+".png?"+query.Encode(), nil))
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
