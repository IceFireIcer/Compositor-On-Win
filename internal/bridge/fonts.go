package bridge

import (
	"encoding/json"
	"fmt"

	"compositor-win/internal/layerrender"
)

// Font enumeration endpoint (ticket 45): the text tool's font picker lists
// the machine's installed faces. The response is raw JSON without the
// document envelope — the list is machine-wide, so it needs no open
// document:
//
//	{"fonts":[{"family":"Segoe UI","postScriptName":"SegoeUI","subfamily":"Regular"}]}
func (s *Service) ListFonts() (string, error) {
	fonts := layerrender.ListInstalledFonts()
	if fonts == nil {
		fonts = []layerrender.FontInfo{}
	}
	data, err := json.Marshal(struct {
		Fonts []layerrender.FontInfo `json:"fonts"`
	}{Fonts: fonts})
	if err != nil {
		return "", fmt.Errorf("无法序列化字体列表: %w", err)
	}
	return string(data), nil
}
