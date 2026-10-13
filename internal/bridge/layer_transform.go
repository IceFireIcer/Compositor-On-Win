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
	t, err := parseLayerTransform(transformJSON)
	if err != nil {
		return "", err
	}
	return s.ws.EditActive("变换图层", func(sess *session) error {
		return applyLayerTransform(sess, layerID, t)
	})
}

// ApplyLayerTransformToTab commits a draft only while its tab is still active
// and its document envelope's numeric rev still equals expectedRev. The
// revision check covers intervening edits, undo/redo and disk reloads. Resolving
// the session, checking its edit state and committing history share one lock,
// so a delayed Wails call cannot land on a replacement session or another tab.
// The legacy active-tab method remains for existing callers.
func (s *Service) ApplyLayerTransformToTab(tabID string, expectedRev uint64, layerID string, transformJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	t, err := parseLayerTransform(transformJSON)
	if err != nil {
		return "", err
	}
	return s.ws.editTransformTab(tabID, &expectedRev, "变换图层", func(sess *session) error {
		return applyLayerTransform(sess, layerID, t)
	})
}

func parseLayerTransform(transformJSON string) (domain.Transform, error) {
	trimmed := strings.TrimSpace(transformJSON)
	if trimmed == "" {
		return domain.Transform{}, fmt.Errorf("图层变换不能为空")
	}
	var t domain.Transform
	if err := json.Unmarshal([]byte(trimmed), &t); err != nil {
		return domain.Transform{}, fmt.Errorf("无法解析图层变换: %w", err)
	}
	if !validLayerTransform(t) {
		return domain.Transform{}, fmt.Errorf("图层变换无效: 尺寸须在 1–%d px、原点须在 ±%d px 内，采样须为 %s/%s/%s",
			maxTransformSide, maxTransformOrigin,
			domain.SamplingNearest, domain.SamplingSmooth, domain.SamplingHighQuality)
	}
	return t, nil
}

func applyLayerTransform(sess *session, layerID string, t domain.Transform) error {
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
}

// editTransformTab is the shared document/history boundary for tab-pinned
// placement commands. expectedRev=nil is a direct canvas command with no
// frontend draft. In either case a tab switch or in-flight pixel/filter edit
// rejects the command before it can open an undo transaction.
func (w *Workspace) editTransformTab(tabID string, expectedRev *uint64, name string, fn func(*session) error) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessions[tabID]
	if sess == nil || sess.doc == nil || !w.hasLocked(tabID) {
		return "", errNoDocument
	}
	if w.active != tabID || sess.doc.DocumentID != tabID {
		return "", fmt.Errorf("TRANSFORM_STALE: 当前标签或文档已经变化")
	}
	if expectedRev != nil && (sess.rev < 0 || uint64(sess.rev) != *expectedRev) {
		return "", fmt.Errorf("TRANSFORM_STALE: 文档 revision 已变化 (expected %d, current %d)", *expectedRev, sess.rev)
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
		return "", errDocumentBusy
	}

	before := cloneDocForEdit(sess.doc)
	historyRevision := sess.hist.Revision()
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	err := fn(sess)
	if err != nil {
		sess.doc = before
	}
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if sess.hist.Revision() != historyRevision {
		sess.rev++
		w.setTabDirtyLocked(tabID, true)
	}
	return marshalEnvelope(sess.rev, sess.doc)
}
