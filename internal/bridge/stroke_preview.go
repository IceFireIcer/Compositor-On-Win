package bridge

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"compositor-win/internal/render"
)

// strokePreviewPathRe is intentionally separate from the document render
// route. The token is opaque and the preview is never part of document rev or
// the layer asset cache.
var strokePreviewPathRe = regexp.MustCompile(`^/pixel/stroke/([A-Za-z0-9-]+)\.png$`)

// serveStrokePreview serves GET /pixel/stroke/{token}.png?sequence=N. It is
// a mountable handler helper: renderhandler.go should call it before the
// ordinary /render and /pixel asset routes. sequence is an ordering/cache
// hint only; the response always reflects the token's current acknowledged
// state. The endpoint returns 404 after EndPaintStroke, EndHealingStroke, or
// token retirement.
func (s *Service) serveStrokePreview(w http.ResponseWriter, r *http.Request) bool {
	m := strokePreviewPathRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "笔刷预览仅支持 GET", http.StatusMethodNotAllowed)
		return true
	}
	if raw := r.URL.Query().Get("sequence"); raw != "" {
		sequence, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "sequence 无效", http.StatusBadRequest)
			return true
		}
		w.Header().Set("X-Stroke-Sequence", strconv.FormatUint(sequence, 10))
	}
	if s.ws == nil {
		http.NotFound(w, r)
		return true
	}

	// Paint and healing previews are composed while holding the workspace
	// writer lock, which keeps the temporary raster and its pinned document in
	// one revision. Retouch has its own read-only preview seam and acquires the
	// same lock internally, so it is dispatched after this lock is released.
	s.ws.mu.Lock()
	if p, err := paintSessionLocked(s.ws, m[1]); err == nil {
		bmp, previewErr := paintStrokePreviewLocked(p)
		s.ws.mu.Unlock()
		return writeStrokePreview(w, r, bmp, previewErr)
	}
	if p, err := healingSessionLocked(s.ws, m[1]); err == nil {
		bmp, previewErr := healingStrokePreviewLocked(p)
		s.ws.mu.Unlock()
		return writeStrokePreview(w, r, bmp, previewErr)
	}
	if _, err := retouchSessionLocked(s.ws, m[1]); err == nil {
		s.ws.mu.Unlock()
		bmp, previewErr := s.retouchStrokePreview(m[1])
		return writeStrokePreview(w, r, bmp, previewErr)
	}
	if _, err := gradientSessionLocked(s.ws, m[1]); err == nil {
		bmp, previewErr := s.gradientStrokePreviewLocked(s.ws, m[1])
		s.ws.mu.Unlock()
		return writeStrokePreview(w, r, bmp, previewErr)
	}
	s.ws.mu.Unlock()
	http.NotFound(w, r)
	return true
}

func paintStrokePreviewLocked(p *paintSession) (*render.Bitmap, error) {
	if err := validatePaintTarget(p); err != nil {
		return nil, err
	}
	asset := p.stroke.View().Bitmap()
	blendThroughSelection(asset, p.grid, p.selection, p.transform)
	if p.targetMask {
		opaqueGrayMask(asset)
		// Only the mask asset is overridden. Procedural shapes remain the
		// compositor's source for the layer image, exactly as after commit.
		return render.Render(p.owner.doc, func(name string) (*render.Bitmap, error) {
			if name == p.key {
				return asset, nil
			}
			return p.owner.pixelSource()(name)
		})
	}
	return renderWithStrokeAsset(p.owner, p.key, asset)
}

func healingStrokePreviewLocked(p *healingSession) (*render.Bitmap, error) {
	if err := validateHealingTarget(p); err != nil {
		return nil, err
	}
	asset := p.base.Clone()
	asset.DropDeep()
	// SpotHeal reads the coverage and writes only this clone. The live
	// HealStroke remains appendable, so an HTTP preview cannot finish a stroke.
	render.SpotHeal(asset, p.stroke.Coverage(), float32(p.opacity), p.mode, p.seed)
	blendThroughSelection(asset, p.base, p.selection, p.transform)
	return renderWithStrokeAsset(p.owner, p.key, asset)
}

func renderWithStrokeAsset(sess *session, key string, asset *render.Bitmap) (*render.Bitmap, error) {
	source := func(name string) (*render.Bitmap, error) {
		if name == key {
			return asset, nil
		}
		return sess.pixelSource()(name)
	}
	// Render the temporary pixel edit instead of regenerating procedural
	// shapes. This detached document never changes the editable source layer.
	doc := cloneDocForEdit(sess.doc)
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.ImageFile != nil && *l.ImageFile == key {
			l.Shape, l.Text = nil, nil
		}
	}
	return render.Render(doc, source)
}

func writeStrokePreview(w http.ResponseWriter, r *http.Request, bmp *render.Bitmap, err error) bool {
	if err != nil {
		if r.Context().Err() != nil {
			return true
		}
		// A conflict retires the session in the normal point/end path. The
		// preview request itself remains read-only, so report it as a conflict
		// and let the caller stop fetching before the next pointer event.
		if strings.Contains(err.Error(), "_CONFLICT:") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return true
	}
	if bmp == nil || bmp.W < 1 || bmp.H < 1 {
		http.Error(w, "笔刷预览位图无效", http.StatusInternalServerError)
		return true
	}
	data, err := render.EncodePNG(bmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
	return true
}
