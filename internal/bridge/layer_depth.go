package bridge

// 图层位深转换（票 69 / ADR-0007）。
//
// 真值：reference/Swift/Compositor/Document/DeepLayers.swift 的
// EditorSession.convertLayerDepth。位深**不是** manifest 字段：一个图层是不是
// 16 位由它资产 PNG 的位深决定（原版同一约定）。所以这里改的是像素——
// render.Bitmap.Deep 的有无——而像素改动必须进 raster journal 才能撤销，
// 哪怕清单一个字节都没变。
//
// 方向由「有没有深层像素」决定，与原版一致：有深层就是转 8 位（抖降后丢掉深层），
// 没有就是转 16 位（把现有 8 位像素按 v*257 展宽，不发明任何精度）。

import (
	"encoding/json"
	"fmt"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// depthLabel 是图层面板显示的位深标签，与原版 NativeLayerList 的 depthTag
// 一致：HDR 层是 "HDR"，16 位层是 "16-Bit"，普通 8 位层没有标签。
func depthLabel(bmp *render.Bitmap) string {
	if bmp == nil || bmp.Deep == nil {
		return ""
	}
	if bmp.Deep.HDR {
		return "HDR"
	}
	return "16-Bit"
}

// convertibleLayer 返回活动图层，前提是它能换位深：一层像素，不是编组、调整
// 图层或可编辑文字（原版 EditorSession.canConvertLayerDepth）。返回的位图为 nil
// 表示这是一个还没有像素的空白图层——转 16 位会先给它画布大小的透明像素。
//
// 「只选了一个图层」那一半不在 Go 侧：选区是前端状态（AGENTS.md 不变量 5），
// 由前端在调用前把关。
func convertibleLayer(sess *session) (*domain.Layer, *render.Bitmap, error) {
	if sess == nil || sess.doc == nil || sess.doc.ActiveLayerID == nil {
		return nil, nil, fmt.Errorf("没有活动图层")
	}
	for i := range sess.doc.Layers {
		l := &sess.doc.Layers[i]
		if l.ID != *sess.doc.ActiveLayerID {
			continue
		}
		if l.IsGroupLayer() || l.Adjustment != nil || l.Text != nil {
			return nil, nil, fmt.Errorf("图层 %s 不是像素图层，无法转换位深", l.ID)
		}
		if l.ImageFile == nil {
			return l, nil, nil
		}
		bmp := sess.bitmaps[*l.ImageFile]
		if bmp == nil {
			return nil, nil, fmt.Errorf("缺少图层位图 %s", *l.ImageFile)
		}
		return l, bmp, nil
	}
	return nil, nil, fmt.Errorf("没有活动图层")
}

// ConvertHDRToSixteenBit 是 HDR 图层的「转换为 16 位」（票 71）。它把高于白色的
// 光裁到白，图层保持 16 位，比半精度浮点更细。这和 ConvertLayerDepth 对 HDR
// 图层的处理不同：那条是抖降到 8 位并丢掉深层像素。撤销装回原来的 HDR 栅格，
// 所以高光没有被永久截掉。
func (s *Service) ConvertHDRToSixteenBit(tabID string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.editDrawingTabFor(tabID, func(*session) string {
		return "转换为 16 位"
	}, func(sess *session) (*rasterChange, error) {
		layer, bmp, err := convertibleLayer(sess)
		if err != nil {
			return nil, err
		}
		if bmp == nil || bmp.Deep == nil || !bmp.Deep.HDR {
			return nil, fmt.Errorf("活动图层不是 HDR 图层")
		}
		converted := bmp.Clone()
		converted.Deep = render.HalfToSixteen(bmp.Deep)
		derived := converted.Deep.ToBitmap()
		converted.Pix = derived.Pix
		key := *layer.ImageFile
		sess.bitmaps[key] = converted
		return &rasterChange{
			before: map[string]*render.Bitmap{key: bmp},
			after:  map[string]*render.Bitmap{key: converted},
		}, nil
	})
}

// ConvertLayerDepth 在 8 位与 16 位之间转换活动图层，作为一次撤销。
func (s *Service) ConvertLayerDepth(tabID string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.editDrawingTabFor(tabID, func(sess *session) string {
		if _, bmp, err := convertibleLayer(sess); err == nil && bmp != nil && bmp.Deep != nil {
			return "转换为 8 位"
		}
		return "转换为 16 位"
	}, func(sess *session) (*rasterChange, error) {
		layer, bmp, err := convertibleLayer(sess)
		if err != nil {
			return nil, err
		}
		return convertLayerDepth(sess, layer, bmp)
	})
}

// convertLayerDepth 是转换本身。它把结果装进 sess.bitmaps（editDrawingTabFor 的
// 约定：mutator 自己换资产），并返回 before/after 交给 raster journal——撤销因此
// 能原样装回转换前的深层像素（或把它拿走）。
func convertLayerDepth(sess *session, layer *domain.Layer, bmp *render.Bitmap) (*rasterChange, error) {
	if layer.ImageFile == nil {
		// 空白图层还没有像素：给它画布大小的透明 16 位像素去画（原版同一分支）。
		// before 里放一张同样大小的空位图，撤销时装回去的是「没有任何像素」，
		// 与清单里被还原成 nil 的 ImageFile 一致。
		w, h := sess.doc.Width, sess.doc.Height
		if w <= 0 || h <= 0 {
			return nil, fmt.Errorf("画布尺寸无效: %d×%d", w, h)
		}
		name := layerAssetName(layer.ID)
		layer.ImageFile = &name
		layer.Transform = domain.Transform{
			Origin:   [2]float64{0, 0},
			Size:     [2]float64{float64(w), float64(h)},
			Sampling: domain.SamplingHighQuality,
		}
		deep := render.DeepFromBitmap(render.NewBitmap(w, h))
		converted := deep.ToBitmap()
		converted.Deep = deep
		sess.bitmaps[name] = converted
		return &rasterChange{
			before: map[string]*render.Bitmap{name: render.NewBitmap(w, h)},
			after:  map[string]*render.Bitmap{name: converted},
		}, nil
	}
	key := *layer.ImageFile
	if bmp.Deep != nil {
		// 转 8 位：抖降到 8 位并丢掉深层像素，图层从此就是 8 位。
		converted := bmp.Deep.ToBitmap()
		sess.bitmaps[key] = converted
		return &rasterChange{
			before: map[string]*render.Bitmap{key: bmp},
			after:  map[string]*render.Bitmap{key: converted},
		}, nil
	}
	// 转 16 位：8 位像素原样保留（它就是派生副本），深层像素是它的精确展宽。
	converted := bmp.Clone()
	converted.Deep = render.DeepFromBitmap(bmp)
	sess.bitmaps[key] = converted
	return &rasterChange{
		before: map[string]*render.Bitmap{key: bmp},
		after:  map[string]*render.Bitmap{key: converted},
	}, nil
}

// layerDepthReply 是位深查询的回包：每个有像素的图层的标签，以及活动图层能否
// 转换。前端图层面板画标签、右键菜单启用「转换为 16 位 / 8 位」都读它。
type layerDepthReply struct {
	Labels      map[string]string `json:"labels"`
	ActiveLabel string            `json:"activeLabel"`
	CanConvert  bool              `json:"canConvert"`
}

// LayerDepthInfo 报告位深标签与可转换性（票 69）。
func (s *Service) LayerDepthInfo() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	s.ws.mu.Lock()
	defer s.ws.mu.Unlock()
	sess := s.ws.activeSessionLocked()
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	reply := layerDepthReply{Labels: map[string]string{}}
	for i := range sess.doc.Layers {
		l := &sess.doc.Layers[i]
		if l.ImageFile == nil {
			continue
		}
		if label := depthLabel(sess.bitmaps[*l.ImageFile]); label != "" {
			reply.Labels[l.ID] = label
		}
	}
	if layer, bmp, err := convertibleLayer(sess); err == nil {
		reply.CanConvert = true
		reply.ActiveLabel = depthLabel(bmp)
		_ = layer
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
