package bridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The compositor must obtain document structure and source-asset identities
// from the same revision. A PNG URL alone cannot describe transforms, masks,
// or blend metadata without copying the document model into the frontend.
func TestRenderSnapshotDescribesAuthoritativeLayerAssets(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	layer := e.Doc.Layers[0]
	url := fmt.Sprintf("/render/%s.json?v=%d&filterRev=0", e.Doc.DocumentID, e.Rev)
	rec := httptest.NewRecorder()
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("render snapshot unavailable: GET %s = %d, %s", url, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("render snapshot Content-Type = %q, want application/json", got)
	}
	var got struct {
		Rev       int `json:"rev"`
		FilterRev int `json:"filterRev"`
		Doc       struct {
			DocumentID string `json:"documentID"`
			Layers     []struct {
				ID        string `json:"id"`
				Transform struct {
					Origin [2]float64 `json:"origin"`
					Size   [2]float64 `json:"size"`
				} `json:"transform"`
			} `json:"layers"`
		} `json:"doc"`
		Assets []struct {
			LayerID string `json:"layerId"`
			Kind    string `json:"kind"`
			URL     string `json:"url"`
			Format  string `json:"format"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
			Hash    string `json:"hash"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid render snapshot: %v", err)
	}
	if got.Rev != e.Rev || got.FilterRev != 0 || got.Doc.DocumentID != e.Doc.DocumentID {
		t.Fatalf("snapshot identity = rev %d/filter %d/doc %q, want %d/0/%q", got.Rev, got.FilterRev, got.Doc.DocumentID, e.Rev, e.Doc.DocumentID)
	}
	if len(got.Doc.Layers) != 1 || got.Doc.Layers[0].ID != layer.ID ||
		got.Doc.Layers[0].Transform.Size != [2]float64{2, 1} {
		t.Fatalf("snapshot lost authoritative layer metadata: %+v", got.Doc.Layers)
	}
	if len(got.Assets) != 1 {
		t.Fatalf("snapshot assets = %d, want one image asset", len(got.Assets))
	}
	asset := got.Assets[0]
	if asset.LayerID != layer.ID || asset.Kind != "image" || asset.Format != "rgba8-premultiplied-srgb" ||
		asset.Width != 2 || asset.Height != 1 || asset.Hash == "" ||
		!strings.Contains(asset.URL, "/pixel/"+e.Doc.DocumentID+"/"+layer.ID+"/image.rgba") {
		t.Fatalf("snapshot asset = %+v", asset)
	}
}

func TestRenderSnapshotRejectsStaleRevision(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	e := mustDocumentSnapshot(t, svc)
	if _, err := svc.BeginStroke(0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndStroke(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	url := fmt.Sprintf("/render/%s.json?v=%d&filterRev=0", e.Doc.DocumentID, e.Rev)
	svc.RenderHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale snapshot status = %d, want 409", rec.Code)
	}
}
