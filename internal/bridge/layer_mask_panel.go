package bridge

import (
	"fmt"
	"reflect"

	"compositor-win/internal/domain"
)

// PlaceLayer is the panel's atomic, tab-bound drop operation. Before/after
// refer to top-first panel order; inside appends at the top of an existing
// folder. A folder keeps its descendants and never creates a wrapper folder.
func (s *Service) PlaceLayer(tabID, layerID, targetID, placement string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	w := s.ws
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasLocked(tabID) {
		return "", errNoDocument
	}
	sess := w.sessions[tabID]
	if sess == nil || sess.doc == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil || sess.filter != nil || sess.heal != nil || sess.saving {
		return "", errDocumentBusy
	}
	candidate := cloneDocForEdit(sess.doc)
	source, target := layerAt(candidate, layerID), layerAt(candidate, targetID)
	if source == nil || target == nil || layerID == targetID {
		return "", fmt.Errorf("移动图层目标无效")
	}
	if descendantIDsOf(candidate, layerID)[targetID] {
		return "", fmt.Errorf("不能移动到自己的子树")
	}
	parent := parentIDOf(target)
	if placement == "inside" {
		if !target.IsGroupLayer() {
			return "", fmt.Errorf("目标不是文件夹")
		}
		parent = targetID
	} else if placement != "before" && placement != "after" {
		return "", fmt.Errorf("移动位置无效")
	}

	// Resolve sibling order before flattening so moving groups carries the
	// whole subtree, including descendants stored far from their parent.
	children := layerChildren(candidate)
	siblings := make([]string, 0)
	for _, idx := range children[parent] {
		if candidate.Layers[idx].ID != layerID {
			siblings = append(siblings, candidate.Layers[idx].ID)
		}
	}
	at := len(siblings)
	if placement != "inside" {
		for i, id := range siblings {
			if id == targetID {
				at = i
				break
			}
		}
		if placement == "before" {
			at++
		}
	}
	siblings = append(siblings, "")
	copy(siblings[at+1:], siblings[at:])
	siblings[at] = layerID
	source.ParentID = nil
	if parent != "" {
		source.ParentID = idPtr(parent)
	}
	byID := make(map[string]domain.Layer, len(candidate.Layers))
	order := make(map[string][]string)
	for _, layer := range candidate.Layers {
		byID[layer.ID] = layer
		if layer.ID != layerID {
			key := parentIDOf(&layer)
			order[key] = append(order[key], layer.ID)
		}
	}
	order[parent] = siblings
	var out []domain.Layer
	var visit func(string)
	visit = func(key string) {
		for _, id := range order[key] {
			layer := byID[id]
			out = append(out, layer)
			if layer.IsGroupLayer() {
				visit(id)
			}
		}
	}
	visit("")
	if len(out) != len(candidate.Layers) {
		return "", fmt.Errorf("移动后层次不完整")
	}
	candidate.Layers = out
	adoptClipping(candidate, layerID)
	releaseDetachedClipping(candidate)
	if err := candidate.Validate(); err != nil {
		return "", err
	}
	if err := validateClipGraph(candidate); err != nil {
		return "", err
	}
	if reflect.DeepEqual(sess.doc.Layers, candidate.Layers) {
		return sess.envelope()
	}
	before := sess.hist.Revision()
	sess.hist.Begin("移动图层", *sess.doc, sess.doc.ActiveLayerID)
	sess.doc.Layers = candidate.Layers
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	if sess.hist.Revision() != before {
		sess.rev++
		w.setTabDirtyLocked(tabID, true)
	}
	return sess.envelope()
}
