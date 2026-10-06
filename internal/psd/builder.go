package psd

// The builder (PSDDocumentBuilder.swift): a parsed Document converted into
// the domain model — layers with assets, masks on the layer grid, clipping
// chains, adjustment layers, and the conversion report.

import (
	"fmt"

	crand "crypto/rand"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// Import is the built document (PSDImport): ready for the workspace.
type Import struct {
	Width       int
	Height      int
	Resolution  int
	Document    *domain.Document
	Assets      map[string]*render.Bitmap
	Conversions []Conversion
}

// Build converts the parsed document (PSDDocumentBuilder.makeImport).
// Text, vector, smart-object and effect layers import as the raster pixels
// Photoshop stores for every layer, with a conversion note — the live
// re-rendering ports are not part of ticket 38.
func Build(doc Document) (*Import, error) {
	out := &Import{
		Width:       doc.Width,
		Height:      doc.Height,
		Resolution:  int(doc.Resolution),
		Assets:      map[string]*render.Bitmap{},
		Conversions: []Conversion{},
	}
	if doc.Resolution >= 1 {
		out.Resolution = int(doc.Resolution + 0.5)
	}
	canvasW, canvasH := doc.Width, doc.Height
	result := &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: domainUUID(),
		Width:      canvasW,
		Height:     canvasH,
	}
	res := out.Resolution
	result.Resolution = &res
	result.Layers = []domain.Layer{}

	for _, record := range doc.Layers {
		if record.CroppedToCanvas {
			out.Conversions = append(out.Conversions, Conversion{
				LayerName: record.Name,
				Message:   "裁剪到画布以放入内存：画布外的像素未被导入。",
			})
		}
		var notes []string
		switch record.Kind {
		case KindText:
			notes = append(notes, "文字图层以像素导入，实时文字未随文件转换。")
		case KindSmartObject:
			notes = append(notes, "智能对象已栅格化，链接内容不可编辑。")
		case KindEffects:
			notes = append(notes, "图层特效被丢弃，外观可能不同。")
		case KindVector:
			notes = append(notes, "矢量形状已栅格化为像素。")
		case KindOther:
			notes = append(notes, "该 Photoshop 图层类型不受支持，已按像素导入。")
		}
		blendMode := BlendModeFromPSD(record.BlendKey)
		if record.IsGroup {
			if record.BlendKey != "pass" && record.BlendKey != "norm" {
				notes = append(notes, fmt.Sprintf("文件夹混合模式“%s”不受支持，将按穿透处理。", record.BlendKey))
			}
		} else if blendMode == "" && record.BlendKey != "pass" {
			notes = append(notes, fmt.Sprintf("混合模式“%s”不受支持，将按正常处理。", trimBlendKey(record.BlendKey)))
		}
		if record.Kind == KindAdjustment {
			if record.Adjustment == nil {
				notes = append(notes, "该调整类型不受支持，已跳过。")
			} else {
				notes = append(notes, "调整参数可能与 Photoshop 不完全一致。")
			}
		}
		for _, note := range notes {
			out.Conversions = append(out.Conversions, Conversion{LayerName: record.Name, Message: note})
		}
		if record.Kind == KindAdjustment && record.Adjustment == nil {
			continue
		}
		opacity := min(1, max(0, record.Opacity))
		isGroup := record.IsGroup
		isGroupFlag := &isGroup
		blend := domain.BlendNormal
		if blendMode != "" {
			if m, ok := domain.ParseBlendMode(blendMode); ok {
				blend = m
			}
		}
		visible := record.IsVisible
		var layer domain.Layer
		layer.Name = record.Name
		layer.IsVisible = visible
		layer.Opacity = &opacity
		layer.BlendMode = &blend
		layer.IsGroup = isGroupFlag
		layer.ID = record.ID
		if record.ParentID != "" {
			parent := record.ParentID
			layer.ParentID = &parent
		}
		if isGroup || record.Adjustment != nil {
			layer.Transform = domain.Transform{
				Origin:   [2]float64{0, 0},
				Size:     [2]float64{float64(canvasW), float64(canvasH)},
				Sampling: domain.SamplingNearest,
			}
		}
		if adj, ok := record.Adjustment.(*ParsedAdjustment); ok && adj != nil {
			a := adj.Adjustment
			layer.Adjustment = &a
		}
		if !isGroup && record.Adjustment == nil && record.Image != nil {
			assetName := record.ID + ".png"
			out.Assets[assetName] = record.Image
			layer.ImageFile = &assetName
			origin := [2]float64{float64(record.Left), float64(record.Top)}
			w, h := float64(record.Width), float64(record.Height)
			if w <= 0 || h <= 0 {
				w, h = float64(record.Image.W), float64(record.Image.H)
			}
			layer.Transform = domain.Transform{
				Origin: origin, Size: [2]float64{w, h}, Sampling: domain.SamplingNearest,
			}
		}
		if record.Mask != nil && len(record.Mask) > 0 {
			if gridMask := maskOnLayerGrid(record, layer, canvasW, canvasH); gridMask != nil {
				maskName := record.ID + ".mask.png"
				out.Assets[maskName] = gridMask
				layer.MaskFile = &maskName
				enabled := record.MaskEnabled
				layer.MaskEnabled = &enabled
				linked := record.MaskLinked
				layer.MaskLinked = &linked
			} else {
				out.Conversions = append(out.Conversions, Conversion{
					LayerName: record.Name,
					Message:   "图层蒙版无法转换为 8 位灰度，已被跳过。",
				})
			}
		}
		result.Layers = append(result.Layers, layer)
	}

	// Clipping: a clipped layer masks onto the nearest unclipped, non-group,
	// non-adjustment layer below it in the same parent (baseForParent).
	idToIndex := map[string]int{}
	for i := range result.Layers {
		idToIndex[result.Layers[i].ID] = i
	}
	baseForParent := map[string]string{}
	for _, record := range doc.Layers {
		index, ok := idToIndex[record.ID]
		if !ok {
			continue
		}
		parent := record.ParentID
		if record.Clipping {
			source, hasSource := baseForParent[parent]
			if hasSource {
				l := &result.Layers[idToIndex[source]]
				if !l.IsGroupLayer() && l.Adjustment == nil {
					result.Layers[index].MaskSourceID = &source
					continue
				}
			}
			out.Conversions = append(out.Conversions, Conversion{
				LayerName: record.Name,
				Message:   "该剪贴蒙版的基底不受支持，剪贴被跳过。",
			})
		} else {
			l := &result.Layers[index]
			if !l.IsGroupLayer() && l.Adjustment == nil {
				baseForParent[parent] = record.ID
			} else {
				delete(baseForParent, parent)
			}
		}
	}
	out.Document = result
	return out, nil
}

// maskOnLayerGrid ports PSDDocumentBuilder.maskOnLayerGrid: the stored mask
// patch drawn where it sits on the layer's pixel grid, Photoshop's default
// value everywhere else. Adjustment layers and folders cover the canvas.
func maskOnLayerGrid(record Record, layer domain.Layer, canvasW, canvasH int) *render.Bitmap {
	gridW, gridH := canvasW, canvasH
	if layer.ImageFile != nil {
		// The caller fills assets before this runs in makeImport; here the
		// record's own image dimensions are the grid.
		gridW, gridH = record.Width, record.Height
		if gridW < 1 || gridH < 1 {
			gridW, gridH = canvasW, canvasH
		}
	}
	if record.Adjustment != nil || record.IsGroup {
		gridW, gridH = canvasW, canvasH
	}
	placedW, placedH := float64(record.Width), float64(record.Height)
	if record.Adjustment != nil || record.IsGroup || record.Image == nil {
		placedW, placedH = float64(canvasW), float64(canvasH)
	}
	originX, originY := float64(record.Left), float64(record.Top)
	if record.Adjustment != nil || record.IsGroup || record.Image == nil {
		originX, originY = 0, 0
	}
	if gridW < 1 || gridH < 1 || placedW <= 0 || placedH <= 0 ||
		record.MaskWidth <= 0 || record.MaskHeight <= 0 {
		// Degenerate: the patch as-is is the best we can offer.
		return grayToMask(record.Mask, record.MaskWidth, record.MaskHeight)
	}
	scaleX := float64(gridW) / placedW
	scaleY := float64(gridH) / placedH
	x0 := int((float64(record.MaskLeft) - originX) * scaleX)
	y0 := int((float64(record.MaskTop) - originY) * scaleY)
	rw := int(float64(record.MaskWidth) * scaleX)
	rh := int(float64(record.MaskHeight) * scaleY)
	// Already the layer's grid: nothing to place.
	if x0 == 0 && y0 == 0 && rw == gridW && rh == gridH &&
		record.MaskWidth == gridW && record.MaskHeight == gridH {
		return grayToMask(record.Mask, gridW, gridH)
	}
	out := render.NewBitmap(gridW, gridH)
	for i := 0; i < gridW*gridH; i++ {
		v := record.MaskDefault
		out.Pix[i*4] = v
		out.Pix[i*4+1] = v
		out.Pix[i*4+2] = v
		out.Pix[i*4+3] = 255
	}
	for y := 0; y < rh; y++ {
		my := y0 + y
		if my < 0 || my >= gridH {
			continue
		}
		for x := 0; x < rw; x++ {
			mx := x0 + x
			if mx < 0 || mx >= gridW {
				continue
			}
			sx := x * record.MaskWidth / max(1, rw)
			sy := y * record.MaskHeight / max(1, rh)
			v := record.Mask[sy*record.MaskWidth+sx]
			i := (my*gridW + mx) * 4
			out.Pix[i] = v
			out.Pix[i+1] = v
			out.Pix[i+2] = v
			out.Pix[i+3] = 255
		}
	}
	return out
}

func grayToMask(gray []uint8, w, h int) *render.Bitmap {
	if w < 1 || h < 1 || len(gray) < w*h {
		return nil
	}
	out := render.NewBitmap(w, h)
	for i := 0; i < w*h; i++ {
		out.Pix[i*4] = gray[i]
		out.Pix[i*4+1] = gray[i]
		out.Pix[i*4+2] = gray[i]
		out.Pix[i*4+3] = 255
	}
	return out
}

func trimBlendKey(key string) string {
	start, end := 0, len(key)
	for start < end && key[start] == ' ' {
		start++
	}
	for end > start && key[end-1] == ' ' {
		end--
	}
	return key[start:end]
}

func domainUUID() string {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hexDigits = "0123456789ABCDEF"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexDigits[v>>4], hexDigits[v&0xF])
	}
	return string(out)
}
