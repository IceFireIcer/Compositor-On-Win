package bridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// selectionEditReply is the stable control-plane envelope used by selection
// commands. The mask is a gray8 byte slice, which encoding/json represents as
// base64 without adding a second schema for binary data.
type selectionEditReply struct {
	TabID     string           `json:"tabId"`
	Selection *filterSelection `json:"selection"`
}

type sampleColorRangeReply struct {
	TabID string                 `json:"tabId"`
	Color *sampleColorRangeValue `json:"color"`
}

type sampleColorRangeValue struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

// colorRangeSettings mirrors ColorRangeSelection.swift's editable state. The
// bridge accepts byte-valued RGB triplets because color-range sampling crosses
// the control plane as straight sRGB bytes.
type colorRangeSettings struct {
	Include   [][]int `json:"include"`
	Exclude   [][]int `json:"exclude"`
	Fuzziness *int    `json:"fuzziness"`
	Invert    bool    `json:"invert"`
}

// LoadLayerSelection loads either a layer's alpha coverage (pixels) or the
// selected black regions of its mask (mask) into document space. It is a read
// operation: the document, history, revision, and active layer never change.
// The requested tab is resolved while holding the workspace lock so a tab
// switch cannot redirect the layer lookup.
func (s *Service) LoadLayerSelection(tabID, layerID, kind string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if strings.TrimSpace(layerID) == "" {
		return "", fmt.Errorf("图层 ID 不能为空")
	}
	if kind != "pixels" && kind != "mask" {
		return "", fmt.Errorf("不支持的选区来源 %q", kind)
	}

	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.sessionByIDLocked(tabID)
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	if err := validSelectionCanvas(sess.doc.Width, sess.doc.Height); err != nil {
		return "", err
	}
	layer, err := layerByID(sess.doc, layerID)
	if err != nil {
		return "", err
	}
	if layer.IsGroupLayer() {
		return "", fmt.Errorf("编组没有可载入的选区")
	}
	if !validLayerTransform(layer.Transform) {
		return "", fmt.Errorf("图层 %s 的变换无效", layer.ID)
	}

	assetName := layer.ImageFile
	if kind == "mask" {
		assetName = layer.MaskFile
	}
	if assetName == nil || strings.TrimSpace(*assetName) == "" {
		if kind == "mask" {
			return "", fmt.Errorf("图层 %s 没有蒙版", layer.ID)
		}
		return "", fmt.Errorf("图层 %s 没有像素", layer.ID)
	}
	source := sess.bitmaps[*assetName]
	if !validSelectionBitmap(source) {
		return "", fmt.Errorf("图层 %s 的选区来源位图无效", layer.ID)
	}

	placed := render.Place(source, layer.Transform, sess.doc.Width, sess.doc.Height)
	mask := make([]uint8, sess.doc.Width*sess.doc.Height)
	for i := range mask {
		if kind == "pixels" {
			mask[i] = placed.Pix[i*4+3]
		} else {
			// Layer masks are gray8 assets whose red channel is coverage;
			// Swift selects the black region, hence the inverse coverage.
			mask[i] = 255 - placed.Pix[i*4]
		}
	}
	return marshalSelectionEdit(tabID, sess.doc.Width, sess.doc.Height, mask)
}

// SelectColorRange computes a document-space mask from the visible composite.
// The matching rules stay in render.ColorRangeMask so premultiplied input is
// unpremultiplied and compared in straight sRGB exactly once, as in Swift.
// This command is deliberately read-only and does not install a selection in
// the document.
func (s *Service) SelectColorRange(tabID, settingsJSON string) (string, error) {
	settings, err := parseColorRangeSettings(settingsJSON)
	if err != nil {
		return "", err
	}
	bmp, err := s.pickingRaster(tabID, true)
	if err != nil {
		return "", err
	}
	if err := validSelectionCanvas(bmp.W, bmp.H); err != nil {
		return "", err
	}

	include, err := flattenColorTable(settings.Include, "include")
	if err != nil {
		return "", err
	}
	exclude, err := flattenColorTable(settings.Exclude, "exclude")
	if err != nil {
		return "", err
	}
	fuzziness := 40
	if settings.Fuzziness != nil {
		fuzziness = *settings.Fuzziness
	}
	mask := make([]uint8, bmp.W*bmp.H)
	render.ColorRangeMask(bmp.Pix, bmp.W, bmp.H, bmp.W*4, include, exclude, fuzziness, settings.Invert, mask)
	return marshalSelectionEdit(tabID, bmp.W, bmp.H, mask)
}

// SampleColorRange returns the straight-sRGB color around a document point,
// matching ColorRangeSelection.swift's alpha-weighted 3x3 sample. The
// visible composite is premultiplied RGBA; transparent neighbors, including
// pixels outside the canvas, contribute neither color nor alpha.
func (s *Service) SampleColorRange(tabID string, x, y int) (string, error) {
	bmp, err := s.pickingRaster(tabID, true)
	if err != nil {
		return "", err
	}
	if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
		return "", fmt.Errorf("取样点不在画布内")
	}

	var sums [4]int
	for sy := y - 1; sy <= y+1; sy++ {
		if sy < 0 || sy >= bmp.H {
			continue
		}
		for sx := x - 1; sx <= x+1; sx++ {
			if sx < 0 || sx >= bmp.W {
				continue
			}
			p := bmp.Pix[(sy*bmp.W+sx)*4:][:4]
			for channel := 0; channel < 4; channel++ {
				sums[channel] += int(p[channel])
			}
		}
	}

	var color *sampleColorRangeValue
	if sums[3] > 0 {
		straight := func(sum int) uint8 {
			value := (sum*255 + sums[3]/2) / sums[3]
			if value > 255 {
				value = 255
			}
			return uint8(value)
		}
		color = &sampleColorRangeValue{R: straight(sums[0]), G: straight(sums[1]), B: straight(sums[2])}
	}

	data, err := json.Marshal(sampleColorRangeReply{TabID: tabID, Color: color})
	if err != nil {
		return "", fmt.Errorf("无法编码取样颜色: %w", err)
	}
	return string(data), nil
}

func parseColorRangeSettings(raw string) (colorRangeSettings, error) {
	settings := colorRangeSettings{}
	if strings.TrimSpace(raw) == "" {
		return settings, nil
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return colorRangeSettings{}, fmt.Errorf("无法解析色彩范围设置: %w", err)
	}
	if settings.Fuzziness != nil && (*settings.Fuzziness < 0 || *settings.Fuzziness > 255) {
		return colorRangeSettings{}, fmt.Errorf("色彩范围模糊度须在 0–255")
	}
	return settings, nil
}

func flattenColorTable(colors [][]int, name string) ([]byte, error) {
	if len(colors) > domain.MaxSurfacePixels/3 {
		return nil, fmt.Errorf("色彩范围 %s 颜色数量过多", name)
	}
	flat := make([]byte, 0, len(colors)*3)
	for i, color := range colors {
		if len(color) != 3 {
			return nil, fmt.Errorf("色彩范围 %s[%d] 必须是 RGB 三元组", name, i)
		}
		for channel, value := range color {
			if value < 0 || value > 255 {
				return nil, fmt.Errorf("色彩范围 %s[%d][%d] 超出 0–255", name, i, channel)
			}
			flat = append(flat, byte(value))
		}
	}
	return flat, nil
}

func marshalSelectionEdit(tabID string, width, height int, mask []uint8) (string, error) {
	if len(mask) != width*height {
		return "", fmt.Errorf("选区蒙版尺寸不符")
	}
	payload := selectionEditReply{
		TabID: tabID,
		Selection: &filterSelection{
			X:    0,
			Y:    0,
			W:    width,
			H:    height,
			Mask: mask,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("无法编码选区: %w", err)
	}
	return string(data), nil
}

func validSelectionCanvas(width, height int) error {
	if width <= 0 || height <= 0 || width > domain.MaxSide || height > domain.MaxSide {
		return fmt.Errorf("选区画布尺寸无效")
	}
	if width > domain.MaxSurfacePixels/height {
		return fmt.Errorf("选区画布像素超出限制")
	}
	return nil
}

func validSelectionBitmap(bmp *render.Bitmap) bool {
	if !validRenderBitmap(bmp) {
		return false
	}
	if bmp.W > domain.MaxSide || bmp.H > domain.MaxSide || bmp.W > domain.MaxSurfacePixels/bmp.H {
		return false
	}
	return true
}
