package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// renderSnapshot is the small control-plane document used by the WebGPU
// compositor. Pixel payloads stay on the /pixel asset endpoints; this reply
// carries only the authoritative document model and immutable asset identity.
type renderSnapshot struct {
	Rev           int                     `json:"rev"`
	FilterRev     int                     `json:"filterRev"`
	FilterPreview bool                    `json:"filterPreview"`
	Doc           *domain.Document        `json:"doc"`
	Assets        []renderAssetDescriptor `json:"assets"`
}

type renderAssetDescriptor struct {
	LayerID string `json:"layerId"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Format  string `json:"format"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Hash    string `json:"hash"`
}

var renderSnapshotPathRe = regexp.MustCompile(`^/render/([^/]+)\.json$`)

// serveRenderSnapshot serves /render/{tab-or-document-id}.json. The
// document and asset identities are captured under one workspace lock so a
// caller cannot pair a new manifest with old pixels after an edit.
func (s *Service) serveRenderSnapshot(w http.ResponseWriter, r *http.Request) bool {
	m := renderSnapshotPathRe.FindStringSubmatch(r.URL.Path)
	if m == nil || len(m) < 2 {
		return false
	}
	if !safeRenderSegment(m[1]) {
		http.NotFound(w, r)
		return true
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "渲染快照仅支持 GET", http.StatusMethodNotAllowed)
		return true
	}
	if s.ws == nil {
		http.NotFound(w, r)
		return true
	}
	if r.Context().Err() != nil {
		return true
	}

	s.ws.mu.Lock()
	sess := s.ws.sessionByIDLocked(m[1])
	if sess == nil {
		s.ws.mu.Unlock()
		http.NotFound(w, r)
		return true
	}
	filterRev := 0
	filterPreview := false
	if sess.filter != nil {
		filterRev = sess.filter.previewRev
		filterPreview = sess.filter.preview != nil
	}
	if !matchesRenderRevision(r, sess.rev) || !matchesQueryRevision(r, "filterRev", filterRev) {
		s.ws.mu.Unlock()
		http.Error(w, "文档渲染修订已变化，请重新获取快照", http.StatusConflict)
		return true
	}

	snapshot := renderSnapshot{
		Rev:           sess.rev,
		FilterRev:     filterRev,
		FilterPreview: filterPreview,
		// The lock protects the live model only until this point. Marshal a
		// detached copy so an edit racing the JSON encoder cannot produce a
		// mixed-revision document snapshot.
		Doc:    cloneDocForEdit(sess.doc),
		Assets: make([]renderAssetDescriptor, 0, len(sess.doc.Layers)*2),
	}
	for i := range sess.doc.Layers {
		layer := &sess.doc.Layers[i]
		if layer.ImageFile != nil {
			if bmp := sess.bitmaps[*layer.ImageFile]; bmp != nil {
				if !validRenderBitmap(bmp) {
					s.ws.mu.Unlock()
					http.Error(w, "图层像素数据无效", http.StatusInternalServerError)
					return true
				}
				snapshot.Assets = append(snapshot.Assets, makeRenderAssetDescriptor(
					m[1], layer.ID, "image", bmp, sess.rev,
				))
			}
		}
		if layer.MaskFile != nil {
			if bmp := sess.bitmaps[*layer.MaskFile]; bmp != nil {
				if !validRenderBitmap(bmp) {
					s.ws.mu.Unlock()
					http.Error(w, "图层像素数据无效", http.StatusInternalServerError)
					return true
				}
				snapshot.Assets = append(snapshot.Assets, makeRenderAssetDescriptor(
					m[1], layer.ID, "mask", bmp, sess.rev,
				))
			}
		}
	}
	payload, err := json.Marshal(snapshot)
	s.ws.mu.Unlock()
	if err != nil {
		http.Error(w, fmt.Sprintf("无法编码渲染快照: %v", err), http.StatusInternalServerError)
		return true
	}
	if r.Context().Err() != nil {
		return true
	}

	sum := sha256.Sum256(payload)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	_, _ = w.Write(payload)
	return true
}

func makeRenderAssetDescriptor(docID, layerID, kind string, bmp *render.Bitmap, rev int) renderAssetDescriptor {
	data := rasterBytes(bmp, kind)
	hash := rasterRevision(bmp.W, bmp.H, kind, data)
	format := "rgba8-premultiplied-srgb"
	ext := "rgba"
	if kind == "mask" {
		format = "gray8"
		ext = "gray"
	}
	return renderAssetDescriptor{
		LayerID: layerID,
		Kind:    kind,
		URL: fmt.Sprintf(
			"/pixel/%s/%s/%s.%s?v=%d&asset=%s",
			url.PathEscape(docID), url.PathEscape(layerID), kind, ext, rev, hash,
		),
		Format: format,
		Width:  bmp.W,
		Height: bmp.H,
		Hash:   hash,
	}
}

func matchesQueryRevision(r *http.Request, key string, current int) bool {
	value := r.URL.Query().Get(key)
	if value == "" {
		return true
	}
	expected, err := strconv.Atoi(value)
	return err == nil && expected == current
}
