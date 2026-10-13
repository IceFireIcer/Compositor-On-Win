package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/png"
	"net/http"
	"regexp"
	"strconv"

	"compositor-win/internal/render"
)

// Source rasters belong to an open document, never to a caller-supplied
// filename. RGBA bytes avoid the loss from unpremultiplying/re-premultiplying
// a PNG on the way to a GPU texture. Masks carry one grayscale byte per pixel.
var layerPixelPathRe = regexp.MustCompile(`^/pixel/([^/]+)/([^/]+)/(image|mask)\.(rgba|gray|png)$`)

func (s *Service) serveRenderAssets(w http.ResponseWriter, r *http.Request) bool {
	m := layerPixelPathRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	if !safeRenderSegment(m[1]) || !safeRenderSegment(m[2]) {
		http.NotFound(w, r)
		return true
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "像素读取仅支持 GET", http.StatusMethodNotAllowed)
		return true
	}
	if s.ws == nil || (m[3] == "image" && m[4] == "gray") || (m[3] == "mask" && m[4] == "rgba") {
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
	if !matchesRenderRevision(r, sess.rev) {
		s.ws.mu.Unlock()
		http.Error(w, "文档修订已变化，请重新获取渲染快照", http.StatusConflict)
		return true
	}
	layer, err := layerByID(sess.doc, m[2])
	if err != nil {
		s.ws.mu.Unlock()
		http.NotFound(w, r)
		return true
	}
	name := layer.ImageFile
	if m[3] == "mask" {
		name = layer.MaskFile
	}
	var bmp *render.Bitmap
	if name != nil {
		bmp = sess.bitmaps[*name]
	}
	if bmp == nil {
		s.ws.mu.Unlock()
		http.NotFound(w, r)
		return true
	}
	if !validRenderBitmap(bmp) {
		s.ws.mu.Unlock()
		http.Error(w, "图层像素数据无效", http.StatusInternalServerError)
		return true
	}
	data := rasterBytes(bmp, m[3])
	revision := rasterRevision(bmp.W, bmp.H, m[3], data)
	width, height := bmp.W, bmp.H
	s.ws.mu.Unlock()
	if r.Context().Err() != nil {
		return true
	}
	if expected := r.URL.Query().Get("asset"); expected != "" && expected != revision {
		http.Error(w, "图层像素已变化，请重新获取渲染快照", http.StatusConflict)
		return true
	}
	etag := `"` + revision + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Pixel-Width", strconv.Itoa(width))
	w.Header().Set("X-Pixel-Height", strconv.Itoa(height))
	w.Header().Set("X-Pixel-Revision", revision)
	format := "rgba8-premultiplied-srgb"
	if m[3] == "mask" {
		format = "gray8"
	}
	w.Header().Set("X-Pixel-Format", format)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	if m[4] == "png" {
		var encoded []byte
		if m[3] == "mask" {
			var buf bytes.Buffer
			err = png.Encode(&buf, &image.Gray{Pix: data, Stride: width, Rect: image.Rect(0, 0, width, height)})
			encoded = buf.Bytes()
		} else {
			errBmp := &render.Bitmap{W: width, H: height, Pix: data}
			encoded, err = render.EncodePNG(errBmp)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return true
		}
		data = encoded
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
	return true
}

func matchesRenderRevision(r *http.Request, rev int) bool {
	if v := r.URL.Query().Get("v"); v != "" {
		expected, err := strconv.Atoi(v)
		return err == nil && expected == rev
	}
	return true
}

func rasterBytes(bmp *render.Bitmap, kind string) []byte {
	if kind == "mask" {
		gray := make([]byte, bmp.W*bmp.H)
		for i := range gray {
			gray[i] = bmp.Pix[i*4] // CPU maskCoverage uses red, never alpha.
		}
		return gray
	}
	return bytes.Clone(bmp.Pix)
}

func validRenderBitmap(bmp *render.Bitmap) bool {
	if bmp == nil || bmp.W <= 0 || bmp.H <= 0 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if bmp.W > maxInt/bmp.H {
		return false
	}
	pixels := bmp.W * bmp.H
	if pixels > maxInt/4 {
		return false
	}
	return len(bmp.Pix) == pixels*4
}

// Geometry belongs in the content identity: the same bytes at a different
// stride must never reuse a texture with the old dimensions.
func rasterRevision(width, height int, kind string, pixels []byte) string {
	h := sha256.New()
	var dims [8]byte
	binary.LittleEndian.PutUint32(dims[:4], uint32(width))
	binary.LittleEndian.PutUint32(dims[4:], uint32(height))
	_, _ = h.Write(dims[:])
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write(pixels)
	return hex.EncodeToString(h.Sum(nil))
}
