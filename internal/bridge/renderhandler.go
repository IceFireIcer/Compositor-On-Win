package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
)

// renderPathRe matches GET /render/{docID}.png — docID is the tab ID or
// the domain documentID. The ?v=rev query is a cache buster for the
// WebView; the server itself always composes the document's current state.
var renderPathRe = regexp.MustCompile(`^/render/([^/]+)\.png$`)

// Pixel plane (architecture §3.3): staged bitmaps are fetched over HTTP,
// never pushed through the JSON bridge as base64.
var (
	stagePathRe  = regexp.MustCompile(`^/pixel/stage/([A-Za-z0-9-]+)(?:\.(?:jpg|png))?$`)
	uploadPathRe = regexp.MustCompile(`^/pixel/upload/([A-Za-z0-9-]+)$`)
	uploadLimit  = int64(512 << 20)
)

// RenderHandler serves the canvas rendering endpoint. Mount it on the
// Wails asset server (assetserver.Options.Handler): requests that miss the
// embedded frontend fall through to it.
func (s *Service) RenderHandler() http.Handler {
	return http.HandlerFunc(s.serveRender)
}

func (s *Service) serveRender(w http.ResponseWriter, r *http.Request) {
	if m := uploadPathRe.FindStringSubmatch(r.URL.Path); m != nil && r.Method == http.MethodPut {
		s.servePixelUpload(w, r, m[1])
		return
	}
	if m := stagePathRe.FindStringSubmatch(r.URL.Path); m != nil && r.Method == http.MethodGet {
		s.serveStagedPixel(w, m[1])
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		http.Error(w, "像素端点仅支持 GET/PUT", http.StatusMethodNotAllowed)
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

// stagePixel keeps an encoded bitmap for HTTP pickup and returns its URL
// path. Tokens live until the owning dialog releases them.
func (s *Service) stagePixel(data []byte, mime string) string {
	token := newPixelToken()
	s.pixelMu.Lock()
	if s.pixelStage == nil {
		s.pixelStage = map[string]stagedPixel{}
	}
	s.pixelStage[token] = stagedPixel{data: data, mime: mime}
	s.pixelMu.Unlock()
	ext := "png"
	if mime == "image/jpeg" {
		ext = "jpg"
	}
	return "/pixel/stage/" + token + "." + ext
}

// releaseStaged drops staged bitmaps by token.
func (s *Service) releaseStaged(tokens []string) {
	s.pixelMu.Lock()
	for _, token := range tokens {
		delete(s.pixelStage, token)
	}
	s.pixelMu.Unlock()
}

// releaseStagedURLs drops staged bitmaps by their URLs.
func (s *Service) releaseStagedURLs(urls []string) {
	tokens := make([]string, 0, len(urls))
	for _, url := range urls {
		if m := stagePathRe.FindStringSubmatch(url); m != nil {
			tokens = append(tokens, m[1])
		}
	}
	s.releaseStaged(tokens)
}

func (s *Service) serveStagedPixel(w http.ResponseWriter, token string) {
	s.pixelMu.Lock()
	staged, ok := s.pixelStage[token]
	s.pixelMu.Unlock()
	if !ok {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", staged.mime)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(staged.data)
}

// servePixelUpload receives one rasterized SVG (PUT body = PNG bytes) and
// slots it into the pending import batch by its upload token.
func (s *Service) servePixelUpload(w http.ResponseWriter, r *http.Request, token string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, uploadLimit))
	if err != nil {
		http.Error(w, "读取上传失败", http.StatusBadRequest)
		return
	}
	if err := s.storeUploadedRaster(token, body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newPixelToken() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b[:])
}
