package bridge

import (
	"net/http"
	"regexp"
)

// renderPathRe matches GET /render/{docID}.png — docID is the tab ID or
// the domain documentID. The ?v=rev query is a cache buster for the
// WebView; the server itself always composes the document's current state.
var renderPathRe = regexp.MustCompile(`^/render/([^/]+)\.png$`)

// RenderHandler serves the canvas rendering endpoint. Mount it on the
// Wails asset server (assetserver.Options.Handler): requests that miss the
// embedded frontend fall through to it.
func (s *Service) RenderHandler() http.Handler {
	return http.HandlerFunc(s.serveRender)
}

func (s *Service) serveRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "渲染端点仅支持 GET", http.StatusMethodNotAllowed)
		return
	}
	m := renderPathRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	if s.ws == nil {
		http.NotFound(w, r)
		return
	}
	data, ok, err := s.ws.RenderPNG(m[1])
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// no-cache: every fetch revalidates with the Go process, so a replaced
	// session (same tab ID, rev reset) can never paint a stale canvas. The
	// Go-side per-rev cache keeps identical revs cheap.
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
