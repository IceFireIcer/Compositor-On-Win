package bridge

// 图层变换提交（票 17 的 Enter / "Apply current canvas operation"）。
//
// 真值：reference/Swift/Compositor/Document/EditorSession.swift:451-492
// commitTransform —— 变换会话（拖拽/数值输入/方向键步进）由前端的
// lib/state/transformSession.ts 拥有，Go 只负责把草稿**烘焙进图层**：
// 写 layer.transform，像素与分辨率一律不动（票 17 验收项"变换后图层保留原
// 分辨率（非破坏）"）。
//
// 不在本 RPC 范围：⌘ 拖角的自由扭曲（commitTransform 的 corners 分支）——
// 那需要 DistortWarp 的形变烘焙，属渲染票；这里只接受 Transform。

import (
	"encoding/json"
	"fmt"
	"strings"

	"compositor-win/internal/domain"
)

// ApplyLayerTransform commits one transform draft onto a layer as one
// "变换图层" undo step. layerID "" means the active layer. transformJSON is
// domain.Transform (origin/size/rotation/flipX/flipY/sampling), validated with
// the same bounds as LayerTransform.isValid (validLayerTransform).
//
// Non-destructive by construction: only the placement changes — ImageFile,
// MaskFile and the bitmap library are untouched, so a scaled-up layer keeps
// its original resolution.
func (s *Service) ApplyLayerTransform(layerID string, transformJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	trimmed := strings.TrimSpace(transformJSON)
	if trimmed == "" {
		return "", fmt.Errorf("图层变换不能为空")
	}
	var t domain.Transform
	if err := json.Unmarshal([]byte(trimmed), &t); err != nil {
		return "", fmt.Errorf("无法解析图层变换: %w", err)
	}
	if !validLayerTransform(t) {
		return "", fmt.Errorf("图层变换无效: 尺寸须在 1–%d px、原点须在 ±%d px 内，采样须为 %s/%s/%s",
			maxTransformSide, maxTransformOrigin,
			domain.SamplingNearest, domain.SamplingSmooth, domain.SamplingHighQuality)
	}
	return s.ws.EditActive("变换图层", func(sess *session) error {
		id := layerID
		if id == "" {
			if sess.doc.ActiveLayerID == nil {
				return fmt.Errorf("没有活动图层")
			}
			id = *sess.doc.ActiveLayerID
		}
		layer, err := layerByID(sess.doc, id)
		if err != nil {
			return err
		}
		layer.Transform = t
		return nil
	})
}
