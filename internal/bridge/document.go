package bridge

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// documentEnvelope is the JSON shape every document endpoint returns:
// {"rev":N,"doc":{…domain.Document…}}. doc is null when no tab is open.
type documentEnvelope struct {
	Rev int              `json:"rev"`
	Doc *domain.Document `json:"doc"`
}

// saveResult is SaveProjectDialog's reply: {"path":"…","rev":N}.
type saveResult struct {
	Path string `json:"path"`
	Rev  int    `json:"rev"`
}

func marshalEnvelope(rev int, doc *domain.Document) (string, error) {
	b, err := json.Marshal(documentEnvelope{Rev: rev, Doc: doc})
	if err != nil {
		return "", fmt.Errorf("无法编码文档快照: %w", err)
	}
	return string(b), nil
}

// DocumentSnapshot returns the active tab's document envelope. With no tab
// open it is {"rev":0,"doc":null}.
func (s *Service) DocumentSnapshot() (string, error) {
	if s.ws == nil {
		return marshalEnvelope(0, nil)
	}
	return s.ws.ActiveDocumentJSON()
}

// LayerOp applies one layer command to the active document and returns the
// fresh envelope. ops: setActive / setVisible / rename / setOpacity /
// setBlendMode / deleteLayer / addLayer / moveLayer. Every op runs inside a
// history transaction (中文命令名); a document change bumps rev.
func (s *Service) LayerOp(op string, payload string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	name, fn, err := planLayerOp(op, payload)
	if err != nil {
		return "", err
	}
	return s.ws.EditActive(name, fn)
}

// Undo rolls the active document back one entry (frontend menu plumbing;
// not part of the M2 binding contract).
func (s *Service) Undo() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.UndoActive()
}

// Redo replays the newest undone entry.
func (s *Service) Redo() (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	return s.ws.RedoActive()
}

// layerOpPayload is the union of every op's fields; each op picks what it
// needs and rejects the request when a required field is missing.
type layerOpPayload struct {
	LayerID string   `json:"id"`
	Visible *bool    `json:"visible"`
	Name    *string  `json:"name"`
	Opacity *float64 `json:"opacity"`
	Mode    string   `json:"mode"`
	To      *int     `json:"to"`
}

// planLayerOp parses and fully validates a LayerOp request, returning the
// history transaction name (undo menu name) and the mutator. Validation
// happens before any mutation, so failures leave the document untouched.
func planLayerOp(op string, payload string) (string, func(*session) error, error) {
	var p layerOpPayload
	if trimmed := strings.TrimSpace(payload); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &p); err != nil {
			return "", nil, fmt.Errorf("无法解析 payload: %w", err)
		}
	}
	switch op {
	case "setActive":
		if p.LayerID == "" {
			return "", nil, fmt.Errorf("setActive 缺少 id")
		}
		return "选择图层", func(sess *session) error {
			if findLayerIndex(sess.doc, p.LayerID) < 0 {
				return fmt.Errorf("图层不存在: %s", p.LayerID)
			}
			id := p.LayerID
			sess.doc.ActiveLayerID = &id
			return nil
		}, nil

	case "setVisible":
		if p.LayerID == "" {
			return "", nil, fmt.Errorf("setVisible 缺少 id")
		}
		if p.Visible == nil {
			return "", nil, fmt.Errorf("setVisible 缺少 visible")
		}
		visible := *p.Visible
		return "切换可见性", func(sess *session) error {
			l, err := layerByID(sess.doc, p.LayerID)
			if err != nil {
				return err
			}
			l.IsVisible = visible
			return nil
		}, nil

	case "rename":
		if p.LayerID == "" {
			return "", nil, fmt.Errorf("rename 缺少 id")
		}
		if p.Name == nil {
			return "", nil, fmt.Errorf("rename 缺少 name")
		}
		name := *p.Name
		if strings.TrimSpace(name) == "" {
			return "", nil, fmt.Errorf("图层名称不能为空白")
		}
		if len(name) > maxLayerNameBytes {
			return "", nil, fmt.Errorf("图层名称超过 %d 字节", maxLayerNameBytes)
		}
		return "重命名图层", func(sess *session) error {
			l, err := layerByID(sess.doc, p.LayerID)
			if err != nil {
				return err
			}
			l.Name = name
			return nil
		}, nil

	case "setOpacity":
		if p.LayerID == "" {
			return "", nil, fmt.Errorf("setOpacity 缺少 id")
		}
		if p.Opacity == nil {
			return "", nil, fmt.Errorf("setOpacity 缺少 opacity")
		}
		opacity := *p.Opacity
		if math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
			return "", nil, fmt.Errorf("不透明度 %v 超出 0–1", opacity)
		}
		return "调整不透明度", func(sess *session) error {
			l, err := layerByID(sess.doc, p.LayerID)
			if err != nil {
				return err
			}
			l.Opacity = &opacity
			return nil
		}, nil

	case "setBlendMode":
		if p.LayerID == "" {
			return "", nil, fmt.Errorf("setBlendMode 缺少 id")
		}
		mode, ok := domain.ParseBlendMode(p.Mode)
		if !ok {
			return "", nil, fmt.Errorf("无效的混合模式 %q", p.Mode)
		}
		return "切换混合模式", func(sess *session) error {
			l, err := layerByID(sess.doc, p.LayerID)
			if err != nil {
				return err
			}
			l.BlendMode = &mode
			return nil
		}, nil

	case "deleteLayer":
		if p.LayerID == "" {
			return "", nil, fmt.Errorf("deleteLayer 缺少 id")
		}
		return "删除图层", func(sess *session) error {
			return deleteLayerSubtree(sess.doc, p.LayerID)
		}, nil

	case "addLayer":
		return "新建图层", func(sess *session) error {
			sess.layerSeq++
			l := newPixelLayer(fmt.Sprintf("图层 %d", sess.layerSeq), sess.doc.Width, sess.doc.Height)
			sess.bitmaps[*l.ImageFile] = render.NewBitmap(sess.doc.Width, sess.doc.Height)
			sess.doc.Layers = append(sess.doc.Layers, l) // bottom-to-top: append is on top
			id := l.ID
			sess.doc.ActiveLayerID = &id
			return nil
		}, nil

	case "moveLayer":
		if p.LayerID == "" || p.To == nil {
			return "", nil, fmt.Errorf("moveLayer 缺少 id/to")
		}
		return "移动图层", func(sess *session) error {
			n := len(sess.doc.Layers)
			from := -1
			for i := range sess.doc.Layers {
				if sess.doc.Layers[i].ID == p.LayerID {
					from = i
					break
				}
			}
			if from < 0 {
				return fmt.Errorf("moveLayer 找不到图层 %s", p.LayerID)
			}
			to := *p.To
			if from < 0 || from >= n || to < 0 || to >= n {
				return fmt.Errorf("moveLayer 索引越界: from=%d to=%d 共 %d 层", from, to, n)
			}
			if from == to {
				return nil // no-op: history records nothing, rev stays put
			}
			layers := sess.doc.Layers
			moving := layers[from]
			without := make([]domain.Layer, 0, n-1)
			without = append(without, layers[:from]...)
			without = append(without, layers[from+1:]...)
			out := make([]domain.Layer, 0, n)
			out = append(out, without[:to]...)
			out = append(out, moving)
			out = append(out, without[to:]...)
			sess.doc.Layers = out
			return nil
		}, nil

	default:
		return "", nil, fmt.Errorf("未知操作 %q", op)
	}
}

// deleteLayerSubtree removes a layer and, when it is a folder, everything
// inside it (orphans would fail package validation). Removing the active
// layer (or its ancestor) activates the neighbor that took its flat index,
// or the top layer when the deletion was above the selection.
func deleteLayerSubtree(doc *domain.Document, id string) error {
	idx := findLayerIndex(doc, id)
	if idx < 0 {
		return fmt.Errorf("图层不存在: %s", id)
	}
	removed := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for i := range doc.Layers {
			l := &doc.Layers[i]
			if !removed[l.ID] && l.ParentID != nil && removed[*l.ParentID] {
				removed[l.ID] = true
				changed = true
			}
		}
	}
	kept := make([]domain.Layer, 0, len(doc.Layers))
	for i := range doc.Layers {
		if !removed[doc.Layers[i].ID] {
			kept = append(kept, doc.Layers[i])
		}
	}
	doc.Layers = kept
	if doc.ActiveLayerID != nil && removed[*doc.ActiveLayerID] {
		doc.ActiveLayerID = nil
		if len(kept) > 0 {
			next := idx
			if next > len(kept)-1 {
				next = len(kept) - 1
			}
			if next < 0 {
				next = 0
			}
			active := kept[next].ID
			doc.ActiveLayerID = &active
		}
	}
	return nil
}

// layerByID fetches a mutable layer, with the missing-ID error shared by
// all single-layer ops.
func layerByID(doc *domain.Document, id string) (*domain.Layer, error) {
	idx := findLayerIndex(doc, id)
	if idx < 0 {
		return nil, fmt.Errorf("图层不存在: %s", id)
	}
	return &doc.Layers[idx], nil
}

// maxLayerNameBytes mirrors project.maxNameBytes (the package validator
// rejects longer names at save time, so reject them at edit time too).
const maxLayerNameBytes = 16_384
