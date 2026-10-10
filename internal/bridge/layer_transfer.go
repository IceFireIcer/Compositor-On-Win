package bridge

import (
	"encoding/json"
	"fmt"
	"math"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// 跨标签/跨项目的图层复制（票 48 验收 2「整层复制粘贴跨项目可用」与验收 4
// 「图层跨标签拖拽」的 Go 侧）。
//
// 真值：
//
//	reference/Swift/Compositor/Document/ProjectWorkspace.swift:193-268
//	    pasteCopiedLayer / copyLayer / copyLayers —— 把图层（编组连它持有的
//	    一切）复制进另一个项目：新 UUID、一步撤销、副本成为目标的活动层。
//	reference/Swift/Compositor/Document/SelectionClipboard.swift:187-233
//	    duplicateLayers / insertCopy —— 子树、新 id、parentID 与 maskSourceID
//	    重映射、10000 层上限。同会话复制继续走 layer_commands.go 的
//	    duplicateLayer op（⌘J / 粘贴），不经过本文件。
//
// 复用 layer_commands.go 的 helper：descendantIDsOf / cloneLayerRecord /
// copyLayerAssets / cloneAsset / findLayerIndex / idPtr / cloneIDPtr，插入位置
// 沿用 import_images.go:384-427 commitImportedLayers 的语义。
//
// 与 Swift 的三处有意差异（都在对应函数处再写明）：
//  1. 像素：Swift 的 ImageLayer.asset 是值类型，跨项目复制直接搬运；Go 的像素
//     在 session.bitmaps 里按资产名存放，所以副本拿到 <新UUID>.png /
//     <新UUID>.mask.png 并深拷贝像素（copyLayerAssets 的事），两个项目此后
//     各持独立位图。
//  2. 位置：Swift copyLayers 把副本整体平移到目标画布中心（有落点时落在落点，
//     ProjectWorkspace.swift:246-255）；这里同样按目标画布居中
//     （centerCopiesOnCanvas），但没有落点参数 —— 拖到标签上的落点语义
//     （拖到画布某处）留给前端流程。
//  3. 层外剪贴：maskSourceID 指向子树外时丢弃为 nil（Swift 同一处 flatMap 的
//     语义，ProjectWorkspace.swift:258）。原版在丢弃前还会用
//     LiveMaskBaker.bake（LiveLayerMask.swift:74）把「当前被剪贴的外观」烘进
//     图层像素；本移植未实现该烘焙，一律丢弃链接 —— 视觉上副本不再被层外
//     图层剪贴。

// transferEditName 是跨标签复制落在目标标签上的历史事务名（undo 菜单文案）。
// 原版的事务名是 "Copy Layers from Project"（ProjectWorkspace.swift:261）。
const transferEditName = "跨标签粘贴图层"

// copyLayersInto 把源会话里的这些图层连同子树复制进目标会话，作为目标上的
// 一步历史（事务名 editName）。dst 可以在后台标签里，不必是前台会话。
//
// 校验先于修改：任一 id 未知、src/dst 没有文档、超过 domain.MaxLayers 或者
// src == dst（同会话请走 duplicateLayer op）都会在任何写入之前返回错误，
// 两个会话的文档、修订、历史与位图库都保持原样。
func (w *Workspace) copyLayersInto(ids []string, src, dst *session, editName string) error {
	if src == nil || src.doc == nil {
		return fmt.Errorf("源文档不存在，无法复制图层")
	}
	if dst == nil || dst.doc == nil {
		return fmt.Errorf("目标文档不存在，无法复制图层")
	}
	if src == dst {
		return fmt.Errorf("源与目标是同一个文档，请改用 duplicateLayer 复制图层")
	}
	if len(ids) == 0 {
		return fmt.Errorf("没有可复制的图层")
	}

	// 复制横跨两个会话，必须作为一个原子步骤落地，所以整段握在工作区锁里。
	// 这也意味着不能调用 EditActive（它自己加 w.mu，会死锁），改用下面的
	// editSessionLocked —— 事务语义与 EditActive 完全一致。
	w.mu.Lock()
	defer w.mu.Unlock()

	// 每个 id 连它的后代一起入选；任一 id 未知就整体拒绝。
	included := make(map[string]bool, len(ids))
	for _, id := range ids {
		if findLayerIndex(src.doc, id) < 0 {
			return fmt.Errorf("图层不存在: %s", id)
		}
		included[id] = true
		for descendant := range descendantIDsOf(src.doc, id) {
			included[descendant] = true
		}
	}
	// 按源文档的扁平序取子树（Swift copyLayers 的 layers.filter），子树内部的
	// 相对层序与剪贴关系因此原样保留。
	originals := make([]domain.Layer, 0, len(included))
	for i := range src.doc.Layers {
		if included[src.doc.Layers[i].ID] {
			originals = append(originals, src.doc.Layers[i])
		}
	}
	if len(dst.doc.Layers)+len(originals) > domain.MaxLayers {
		return fmt.Errorf("图层数超过 %d 上限，无法复制图层", domain.MaxLayers)
	}

	// 克隆 + 重映射 + 位图深拷贝。位图先落在 staged 暂存表里，整段成功后才
	// 发布进 dst.bitmaps —— 中途失败时目标位图库一点不动。
	mapping := make(map[string]string, len(originals))
	for _, original := range originals {
		mapping[original.ID] = newUUID()
	}
	staged := make(map[string]*render.Bitmap, len(originals))
	copies := make([]domain.Layer, 0, len(originals))
	for _, original := range originals {
		copied := cloneLayerRecord(original)
		copied.ID = mapping[original.ID]
		copied.ParentID = remapToCopy(original.ParentID, mapping)
		copied.MaskSourceID = remapToCopy(original.MaskSourceID, mapping)
		if err := copyLayerAssets(src, &copied, staged); err != nil {
			return err
		}
		copies = append(copies, copied)
	}

	// 副本整体平移到目标画布中心（Swift copyLayers 在没有拖放落点时把 ids[0]
	// 的中心放到画布中心，ProjectWorkspace.swift:246-255）：跨项目画布尺寸
	// 不同时，原位照搬会把内容留在画布外。平移量对子树里每个图层一致，层与层
	// 的相对位置不变。
	centerCopiesOnCanvas(src.doc, dst.doc, originals, copies)

	// 插入位置照 commitImportedLayers（import_images.go:399-424）：目的地活动
	// 层是编组就放进该组，否则跟随活动层的父级；被切开的根（父级留在源文档，
	// remapToCopy 已把它清成 nil）挂到同一父级下。按源扁平序追加在列表末尾，
	// 最后插入的在上。
	parentID := destinationParent(dst.doc)
	for i := range copies {
		if copies[i].ParentID == nil {
			copies[i].ParentID = cloneIDPtr(parentID)
		}
	}

	candidate := cloneDocForEdit(dst.doc)
	candidate.Layers = append(candidate.Layers, copies...)
	candidate.ActiveLayerID = idPtr(mapping[ids[0]])
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("复制后的目标文档不合法: %w", err)
	}

	return w.editSessionLocked(editName, dst, func(sess *session) error {
		sess.doc.Layers = candidate.Layers
		sess.doc.ActiveLayerID = candidate.ActiveLayerID
		for name, bmp := range staged {
			sess.bitmaps[name] = bmp
		}
		return nil
	})
}

// centerCopiesOnCanvas shifts every copy by the same offset so the subtree's
// bounding box (measured from the originals' transforms) lands centered on the
// destination canvas. One offset for all of them keeps their relative layout —
// the original shifts the whole run the same way.
func centerCopiesOnCanvas(src, dst *domain.Document, originals, copies []domain.Layer) {
	if len(originals) == 0 || len(originals) != len(copies) {
		return
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i := range originals {
		t := originals[i].Transform
		if t.Size[0] <= 0 || t.Size[1] <= 0 {
			continue
		}
		minX = math.Min(minX, t.Origin[0])
		minY = math.Min(minY, t.Origin[1])
		maxX = math.Max(maxX, t.Origin[0]+t.Size[0])
		maxY = math.Max(maxY, t.Origin[1]+t.Size[1])
	}
	if math.IsInf(minX, 1) {
		return // nothing with a size to center
	}
	wantX := (float64(dst.Width) - (maxX - minX)) / 2
	wantY := (float64(dst.Height) - (maxY - minY)) / 2
	dx := math.Round(wantX - minX)
	dy := math.Round(wantY - minY)
	for i := range copies {
		copies[i].Transform.Origin[0] += dx
		copies[i].Transform.Origin[1] += dy
	}
}

// remapToCopy 把子树内的引用换成副本 id。指向子树外的引用按 Swift copyLayers
// （ProjectWorkspace.swift:257-258 的 flatMap { mapping[$0] }）落为 nil：那些
// 图层留在源文档里，在目标文档中不可能成立 —— 悬空的 parentID / maskSourceID
// 会被 domain.Validate 拒绝；被切开的视觉根随后由 copyLayersInto 挂到目的地
// 父级下，只有层外剪贴链接是真的丢弃（见文件头差异 3）。
func remapToCopy(ref *string, mapping map[string]string) *string {
	if ref == nil {
		return nil
	}
	if mapped, ok := mapping[*ref]; ok {
		return idPtr(mapped)
	}
	return nil
}

// destinationParent 给出新层在目的地里的父级：活动层是编组就进组，否则跟随
// 活动层的父级（nil = 根级）—— 与 commitImportedLayers 同一套语义
// （import_images.go:400-411）。
func destinationParent(doc *domain.Document) *string {
	if doc.ActiveLayerID == nil {
		return nil
	}
	idx := findLayerIndex(doc, *doc.ActiveLayerID)
	if idx < 0 {
		return nil
	}
	active := &doc.Layers[idx]
	if active.IsGroupLayer() {
		return idPtr(active.ID)
	}
	return cloneIDPtr(active.ParentID)
}

// sessionOwningLayerLocked 找出图层所属的会话：跨标签复制只知道图层 id，而
// 图层 id 是 UUID，在打开的标签之间唯一。按 w.tabs 的顺序遍历，结果稳定。
func (w *Workspace) sessionOwningLayerLocked(layerID string) *session {
	for i := range w.tabs {
		sess := w.sessions[w.tabs[i].ID]
		if sess == nil || sess.doc == nil {
			continue
		}
		if findLayerIndex(sess.doc, layerID) >= 0 {
			return sess
		}
	}
	return nil
}

// tabIDForSessionLocked 给出会话对应的标签 id（脏标记按标签记录）。会话不在
// 任何标签里时返回 ""，setTabDirtyLocked 会当无事发生。
func (w *Workspace) tabIDForSessionLocked(sess *session) string {
	for i := range w.tabs {
		if w.sessions[w.tabs[i].ID] == sess {
			return w.tabs[i].ID
		}
	}
	return ""
}

// editSessionLocked 是 Workspace.EditActive（workspace.go:223-246）的按会话
// 版本：EditActive 只认前台会话，而跨标签复制要改的可能是后台标签。事务语义
// 逐行一致 —— fn 必须自己先校验；出错就把文档整份回滚；文档没变就不记历史、
// 不推 rev；有变化才推 rev 并把该标签标脏。调用方必须已经持有 w.mu。
func (w *Workspace) editSessionLocked(name string, sess *session, fn func(*session) error) error {
	before := cloneDocForEdit(sess.doc)
	revBefore := sess.hist.Revision()
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	err := fn(sess)
	if err != nil {
		sess.doc = before
	}
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	if err != nil {
		return err
	}
	if sess.hist.Revision() != revBefore {
		sess.rev++
		w.setTabDirtyLocked(w.tabIDForSessionLocked(sess), true)
	}
	return nil
}

// TransferLayer 把 layerID（连同编组持有的子树）从它所在的标签复制进
// destDocID 指向的标签，作为目标上的一步历史，然后把活动标签切到目标 ——
// 用户立刻看到结果。返回工作区 Snapshot 的 JSON，与 Workspace.SelectTab 同形
// （{"tabs":[...],"activeId":"..."}）。
//
// destDocID 与 sessionByIDLocked 同语义：先是 w.sessions 的键（Document.ID，
// 前端从 workspace Snapshot 的 tab.id 传入），再退化到 domain 文档的
// documentID。layerID 按归属定位会话（前端约定它来自当前活动标签）。
//
// 错误：图层不存在、目标标签不存在、目标就是图层所在标签（同标签复制请直接
// 走 duplicateLayer op）、超过 domain.MaxLayers。
func (s *Service) TransferLayer(layerID string, destDocID string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	// 先取好指针再调用：copyLayersInto 与 SelectTab 都会自己加 w.mu，握着锁
	// 调它们会死锁。
	s.ws.mu.Lock()
	src := s.ws.sessionOwningLayerLocked(layerID)
	dst := s.ws.sessionByIDLocked(destDocID)
	dstTab := ""
	if dst != nil {
		dstTab = s.ws.tabIDForSessionLocked(dst)
	}
	s.ws.mu.Unlock()

	if src == nil {
		return "", fmt.Errorf("图层不存在: %s", layerID)
	}
	if dst == nil || dstTab == "" {
		return "", fmt.Errorf("目标标签不存在: %s", destDocID)
	}
	if src == dst {
		return "", fmt.Errorf("目标就是图层所在标签，请改用 duplicateLayer 复制图层")
	}
	if err := s.ws.copyLayersInto([]string{layerID}, src, dst, transferEditName); err != nil {
		return "", err
	}
	snap, err := s.ws.SelectTab(dstTab)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return "", fmt.Errorf("无法编码工作区快照: %w", err)
	}
	return string(b), nil
}
