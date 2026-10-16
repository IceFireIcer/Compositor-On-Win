package bridge

import (
	"net/http"
	"strconv"

	"compositor-win/internal/render"
)

// Transform drafts belong to the view, while document and pixels stay in Go.
// Render an isolated placement on the existing pixel plane; never publish it
// into the committed document, history, or revision-keyed render cache.
func (s *Service) serveTransformPreview(w http.ResponseWriter, r *http.Request, docID string) {
	query := r.URL.Query()
	target, err := parseLayerTransform(query.Get("transform"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rev, err := strconv.ParseUint(query.Get("rev"), 10, 64)
	if err != nil || query.Get("layerID") == "" {
		http.Error(w, "变换预览需要图层与 revision", http.StatusBadRequest)
		return
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.sessionByIDLocked(docID)
	if sess == nil {
		http.NotFound(w, r)
		return
	}
	if sess.rev < 0 || uint64(sess.rev) != rev || s.ws.active != sess.doc.DocumentID {
		http.Error(w, "变换预览已过期", http.StatusConflict)
		return
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil {
		http.Error(w, errDocumentBusy.Error(), http.StatusConflict)
		return
	}
	draft := cloneDocForEdit(sess.doc)
	layer, err := layerByID(draft, query.Get("layerID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	layer.Transform = target
	bmp, err := render.Render(draft, sess.pixelSource())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := render.EncodePNG(bmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
