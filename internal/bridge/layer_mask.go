package bridge

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// SetLayerMask edits a raster layer mask in the explicitly named tab and returns
// that tab's document envelope. White reveals, black hides, and gray gives soft
// coverage. Pixel assets are replaced immutably so history can retain them.
func (s *Service) SetLayerMask(tabID, layerID, operation, selectionJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	if !validLayerMaskOperation(operation) {
		return "", fmt.Errorf("蒙版操作无效: %q", operation)
	}
	var selection *filterSelection
	var err error
	if operation == "createRevealSelection" || operation == "createHideSelection" {
		selection, err = parseFilterSelection(selectionJSON)
		if err != nil {
			return "", err
		}
		if selection == nil {
			return "", fmt.Errorf("%s 需要选区", operation)
		}
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
	layer, err := layerByID(sess.doc, layerID)
	if err != nil {
		return "", err
	}
	if isLayerMaskCreation(operation) {
		if layer.MaskFile != nil {
			return "", fmt.Errorf("图层 %s 已有蒙版", layerID)
		}
		return createLayerMaskLocked(w, tabID, sess, layer, operation, selection)
	}
	return mutateLayerMaskLocked(w, tabID, sess, layer, operation)
}

func validLayerMaskOperation(operation string) bool {
	switch operation {
	case "createRevealAll", "createHideAll", "createRevealSelection", "createHideSelection",
		"enable", "disable", "link", "unlink", "invert", "delete":
		return true
	default:
		return false
	}
}

func isLayerMaskCreation(operation string) bool {
	switch operation {
	case "createRevealAll", "createHideAll", "createRevealSelection", "createHideSelection":
		return true
	default:
		return false
	}
}

func createLayerMaskLocked(w *Workspace, tabID string, sess *session, layer *domain.Layer, operation string, selection *filterSelection) (string, error) {
	w_, h, err := layerMaskGridSize(sess, layer)
	if err != nil {
		return "", err
	}
	var coverage []uint8
	switch operation {
	case "createRevealAll":
		w_, h = 1, 1
		coverage = []uint8{255}
	case "createHideAll":
		w_, h = 1, 1
		coverage = []uint8{0}
	case "createRevealSelection", "createHideSelection":
		coverage = selectionLayerMask(selection, layer.Transform, w_, h)
		if operation == "createHideSelection" {
			for i := range coverage {
				coverage[i] = 255 - coverage[i]
			}
		}
	default:
		return "", fmt.Errorf("蒙版操作无效: %q", operation)
	}
	mask := newLayerMaskBitmap(w_, h, coverage)
	key := strings.ToUpper(layer.ID) + ".mask.png"
	old := sess.bitmaps[key]
	beforeRev := sess.hist.Revision()
	sess.hist.Begin(layerMaskHistoryName(operation), *sess.doc, sess.doc.ActiveLayerID)
	enabled, linked := true, true
	layer.MaskFile = &key
	layer.MaskEnabled = &enabled
	layer.MaskLinked = &linked
	layer.MaskPlacement = nil
	sess.bitmaps[key] = mask
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	if sess.hist.Revision() != beforeRev {
		sess.journalRasters(sess.hist.Revision(),
			map[string]*render.Bitmap{key: old}, map[string]*render.Bitmap{key: mask})
		sess.rev++
		w.setTabDirtyLocked(tabID, true)
	}
	return sess.envelope()
}

func layerMaskGridSize(sess *session, layer *domain.Layer) (int, int, error) {
	if layer.ImageFile != nil {
		bmp := sess.bitmaps[*layer.ImageFile]
		if bmp == nil {
			return 0, 0, fmt.Errorf("内存位图库缺少资产 %s", *layer.ImageFile)
		}
		if !validLayerMaskDimensions(bmp.W, bmp.H) {
			return 0, 0, fmt.Errorf("图层 %s 的蒙版尺寸超出限制", layer.ID)
		}
		return bmp.W, bmp.H, nil
	}
	w := int(math.Round(layer.Transform.Size[0]))
	h := int(math.Round(layer.Transform.Size[1]))
	if !validLayerMaskDimensions(w, h) {
		return 0, 0, fmt.Errorf("图层 %s 的蒙版尺寸超出限制", layer.ID)
	}
	return w, h, nil
}

func validLayerMaskDimensions(w, h int) bool {
	return w > 0 && h > 0 && w <= domain.MaxSide && h <= domain.MaxSide && w <= domain.MaxSurfacePixels/h
}

func newLayerMaskBitmap(w, h int, coverage []uint8) *render.Bitmap {
	bmp := render.NewBitmap(w, h)
	for i, value := range coverage {
		offset := i * 4
		bmp.Pix[offset], bmp.Pix[offset+1], bmp.Pix[offset+2], bmp.Pix[offset+3] = value, value, value, 255
	}
	return bmp
}

// mutateLayerMaskLocked applies operations to an existing mask. The caller
// holds Workspace.mu and has already checked the tab, layer, and busy state.
// Metadata edits use a normal history transaction; invert uses the raster
// journal because its payload lives in the bitmap library rather than in the
// manifest snapshot.
func mutateLayerMaskLocked(w *Workspace, tabID string, sess *session, layer *domain.Layer, operation string) (string, error) {
	if layer.MaskFile == nil || *layer.MaskFile == "" {
		return "", fmt.Errorf("图层 %s 没有蒙版", layer.ID)
	}
	key := *layer.MaskFile
	mask := sess.bitmaps[key]
	if mask == nil || !validLayerMaskBitmap(mask) {
		return "", fmt.Errorf("内存位图库缺少蒙版 %s", key)
	}

	commitMetadata := func(change func()) (string, error) {
		beforeRev := sess.hist.Revision()
		sess.hist.Begin(layerMaskHistoryName(operation), *sess.doc, sess.doc.ActiveLayerID)
		change()
		sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
		if sess.hist.Revision() != beforeRev {
			sess.rev++
			w.setTabDirtyLocked(tabID, true)
		}
		return sess.envelope()
	}

	switch operation {
	case "enable":
		if layer.MaskEnabled == nil || *layer.MaskEnabled {
			return sess.envelope()
		}
		enabled := true
		return commitMetadata(func() { layer.MaskEnabled = &enabled })

	case "disable":
		if layer.MaskEnabled != nil && !*layer.MaskEnabled {
			return sess.envelope()
		}
		enabled := false
		return commitMetadata(func() { layer.MaskEnabled = &enabled })

	case "link":
		if layer.MaskLinked == nil || *layer.MaskLinked {
			return sess.envelope()
		}
		linked := true
		return commitMetadata(func() { layer.MaskLinked = &linked })

	case "unlink":
		if layer.MaskLinked != nil && !*layer.MaskLinked {
			return sess.envelope()
		}
		linked := false
		return commitMetadata(func() {
			layer.MaskLinked = &linked
			// A 1×1 mask is spatially uniform, so it has no meaningful
			// document-space placement. Keep the manifest compact as Swift
			// LayerMask.placement(movingLayer:to:) does for solid masks.
			if mask.W == 1 && mask.H == 1 {
				layer.MaskPlacement = nil
				return
			}
			placement := layer.Transform
			layer.MaskPlacement = &placement
		})

	case "invert":
		inverted := mask.Clone()
		for i := 0; i < len(inverted.Pix); i += 4 {
			value := 255 - mask.Pix[i]
			inverted.Pix[i], inverted.Pix[i+1], inverted.Pix[i+2], inverted.Pix[i+3] = value, value, value, 255
		}
		if bytes.Equal(mask.Pix, inverted.Pix) {
			return sess.envelope()
		}
		beforeRev := sess.hist.Revision()
		sess.hist.Begin(layerMaskHistoryName(operation), *sess.doc, sess.doc.ActiveLayerID)
		sess.bitmaps[key] = inverted
		sess.hist.EndForced(*sess.doc, sess.doc.ActiveLayerID)
		if sess.hist.Revision() == beforeRev {
			return sess.envelope()
		}
		sess.journalRasters(sess.hist.Revision(),
			map[string]*render.Bitmap{key: mask}, map[string]*render.Bitmap{key: inverted})
		sess.rev++
		w.setTabDirtyLocked(tabID, true)
		return sess.envelope()

	case "delete":
		return commitMetadata(func() {
			// Keep sess.bitmaps[key] alive. The history snapshot restores the
			// manifest on undo, and retaining this orphan lets that restore
			// reuse the exact immutable raster without re-decoding it.
			layer.MaskFile = nil
			layer.MaskEnabled = nil
			layer.MaskLinked = nil
			layer.MaskPlacement = nil
		})

	default:
		return "", fmt.Errorf("蒙版操作无效: %q", operation)
	}
}

func validLayerMaskBitmap(mask *render.Bitmap) bool {
	return mask != nil && validLayerMaskDimensions(mask.W, mask.H) && len(mask.Pix) == mask.W*mask.H*4
}

func layerMaskHistoryName(operation string) string {
	switch operation {
	case "createRevealAll":
		return "添加全显示蒙版"
	case "createHideAll":
		return "添加全隐藏蒙版"
	case "createRevealSelection":
		return "按选区显示蒙版"
	case "createHideSelection":
		return "按选区隐藏蒙版"
	case "enable":
		return "启用图层蒙版"
	case "disable":
		return "禁用图层蒙版"
	case "link":
		return "链接图层蒙版"
	case "unlink":
		return "取消链接图层蒙版"
	case "invert":
		return "反相图层蒙版"
	case "delete":
		return "删除图层蒙版"
	default:
		return "编辑图层蒙版"
	}
}
