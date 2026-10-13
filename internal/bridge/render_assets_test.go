package bridge

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"compositor-win/internal/render"
)

// The pixel plane is a public document/bridge boundary: a GPU canvas must
// be able to obtain the actual source pixels without flattening the document
// or losing premultiplied byte values through PNG/browser conversion.
func TestRenderLayerPixelsAvailableOverHTTP(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	layerID := e.Doc.Layers[0].ID
	url := fmt.Sprintf("/pixel/%s/%s/image.rgba?v=%d", e.Doc.DocumentID, layerID, e.Rev)
	w := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GPU source pixels unavailable: GET %s = %d, %s", url, w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), []byte{255, 255, 255, 255, 255, 255, 255, 255}) {
		t.Fatalf("source pixels = %v, want two opaque white premultiplied RGBA8 pixels", w.Body.Bytes())
	}
	if w.Header().Get("X-Pixel-Format") != "rgba8-premultiplied-srgb" ||
		w.Header().Get("X-Pixel-Width") != "2" || w.Header().Get("X-Pixel-Height") != "1" {
		t.Fatalf("missing pixel format/dimensions: %v", w.Header())
	}
}

func TestRenderLayerPixelsRejectsStaleRevision(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	if _, err := svc.BeginStroke(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("/pixel/%s/%s/image.rgba?v=%d", e.Doc.DocumentID, e.Doc.Layers[0].ID, e.Rev)
	rec := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale pixel status = %d, want 409", rec.Code)
	}
}

func TestRenderLayerPixelsRejectsWrongAssetHash(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	url := fmt.Sprintf("/pixel/%s/%s/image.rgba?v=%d&asset=not-the-snapshot-hash", e.Doc.DocumentID, e.Doc.Layers[0].ID, e.Rev)
	rec := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("wrong pixel hash status = %d, want 409", rec.Code)
	}
}

func TestRenderLayerPixelsSupportsETagRevalidationAndPNG(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	baseURL := fmt.Sprintf("/pixel/%s/%s/image", e.Doc.DocumentID, e.Doc.Layers[0].ID)

	first := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(first, httptest.NewRequest(http.MethodGet, baseURL+".rgba", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first pixel status = %d, want 200", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first pixel response did not include an ETag")
	}
	revalidatedRequest := httptest.NewRequest(http.MethodGet, baseURL+".rgba", nil)
	revalidatedRequest.Header.Set("If-None-Match", etag)
	revalidated := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(revalidated, revalidatedRequest)
	if revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
		t.Fatalf("revalidated pixel = status %d, body %d bytes; want 304 and empty body", revalidated.Code, revalidated.Body.Len())
	}

	pngResponse := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(pngResponse, httptest.NewRequest(http.MethodGet, baseURL+".png", nil))
	if pngResponse.Code != http.StatusOK || pngResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("PNG response = status %d/content-type %q", pngResponse.Code, pngResponse.Header().Get("Content-Type"))
	}
	img, err := png.Decode(bytes.NewReader(pngResponse.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode pixel PNG: %v", err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 255 || b>>8 != 255 || a>>8 != 255 {
		t.Fatalf("decoded pixel = %d,%d,%d,%d; want opaque white", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestRenderLayerMaskPixelsUseGrayTransport(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	sess := activeSessionOf(t, svc)
	maskName := "mask.png"
	sess.doc.Layers[0].MaskFile = &maskName
	sess.bitmaps[maskName] = &render.Bitmap{
		W:   2,
		H:   1,
		Pix: []byte{17, 2, 3, 255, 201, 4, 5, 255},
	}
	baseURL := fmt.Sprintf("/pixel/%s/%s/mask", e.Doc.DocumentID, e.Doc.Layers[0].ID)

	gray := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(gray, httptest.NewRequest(http.MethodGet, baseURL+".gray?v=0", nil))
	if gray.Code != http.StatusOK || !bytes.Equal(gray.Body.Bytes(), []byte{17, 201}) {
		t.Fatalf("mask.gray = status %d/body %v, want 200/[17 201]", gray.Code, gray.Body.Bytes())
	}
	if gray.Header().Get("X-Pixel-Format") != "gray8" {
		t.Fatalf("mask.gray format = %q, want gray8", gray.Header().Get("X-Pixel-Format"))
	}

	pngResponse := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(pngResponse, httptest.NewRequest(http.MethodGet, baseURL+".png?v=0", nil))
	if pngResponse.Code != http.StatusOK {
		t.Fatalf("mask.png status = %d, want 200", pngResponse.Code)
	}
	img, err := png.Decode(bytes.NewReader(pngResponse.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode mask PNG: %v", err)
	}
	r, g, b, a := img.At(1, 0).RGBA()
	if r>>8 != 201 || g>>8 != 201 || b>>8 != 201 || a>>8 != 255 {
		t.Fatalf("decoded mask pixel = %d,%d,%d,%d; want 201 gray opaque", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestRenderLayerPixelsRejectMalformedBitmap(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	sess := activeSessionOf(t, svc)
	imageName := *sess.doc.Layers[0].ImageFile
	sess.bitmaps[imageName] = &render.Bitmap{W: 2, H: 1, Pix: []byte{255}}

	assetURL := fmt.Sprintf("/pixel/%s/%s/image.rgba?v=%d", e.Doc.DocumentID, sess.doc.Layers[0].ID, e.Rev)
	asset := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, assetURL, nil))
	if asset.Code != http.StatusInternalServerError {
		t.Fatalf("malformed image asset status = %d, want 500", asset.Code)
	}

	snapshotURL := fmt.Sprintf("/render/%s.json?v=%d&filterRev=0", e.Doc.DocumentID, e.Rev)
	snapshot := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(snapshot, httptest.NewRequest(http.MethodGet, snapshotURL, nil))
	if snapshot.Code != http.StatusInternalServerError {
		t.Fatalf("malformed image snapshot status = %d, want 500", snapshot.Code)
	}
}

func TestRenderLayerMaskPixelsRejectMalformedBitmap(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	sess := activeSessionOf(t, svc)
	maskName := "mask.png"
	sess.doc.Layers[0].MaskFile = &maskName
	sess.bitmaps[maskName] = &render.Bitmap{W: 2, H: 1, Pix: []byte{255}}

	assetURL := fmt.Sprintf("/pixel/%s/%s/mask.gray?v=%d", e.Doc.DocumentID, sess.doc.Layers[0].ID, e.Rev)
	asset := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, assetURL, nil))
	if asset.Code != http.StatusInternalServerError {
		t.Fatalf("malformed mask asset status = %d, want 500", asset.Code)
	}
}

func TestRenderAssetsReturnNotFoundForUnknownIDs(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	paths := []string{
		"/render/not-a-document.json",
		"/pixel/not-a-document/not-a-layer/image.rgba",
	}
	for _, path := range paths {
		rec := httptest.NewRecorder()
		svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, rec.Code)
		}
	}
}

func TestRenderAssetsRejectEncodedPathSegments(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	paths := []string{
		"/render/%2e%2e.json",
		"/pixel/%2e%2e/not-a-layer/image.rgba",
		"/pixel/not-a-document/%2e%2e/image.rgba",
	}
	for _, path := range paths {
		rec := httptest.NewRecorder()
		svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, rec.Code)
		}
	}
}
