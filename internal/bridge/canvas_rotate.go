package bridge

// 画布旋转 90 度（票 56 / 对等矩阵 U03）。
//
// 真值：reference/Swift/Compositor/Document/CanvasRotation.swift 的 rotateCanvas
// ——整幅画布转四分之一圈：宽高互换，每个图层（含编组）与独立蒙版 placement 一起
// 做四分之一转，参考线换轴，整件事算一次撤销。像素一律不重采样：只改 placement，
// 位图不动（与 canvas_flip.go 的翻转同构）。
//
// 选区不在 domain.Document 里：Windows 侧选区是前端 store（selection.ts），所以
// Go 只管图层 / 蒙版 / 参考线 / 宽高，选区旋转由 frontend/src/lib/state/
// canvasRotate.ts 在同一个 RPC 回复之后做。

import (
	"fmt"
	"math"

	"compositor-win/internal/domain"
)

// RotateCanvas 把显式命名的文档顺时针或逆时针转 90 度，返回标准文档信封。
//
// direction 只接受 "clockwise" 或 "counterclockwise"，其它值在动文档之前就报错。
// tab id 是必需的：原生/菜单调用可能比发起它的标签选择活得更久，editTransformTab
// 会在打开历史之前拒绝已切换、已关闭、已重载或忙碌的会话。
func (s *Service) RotateCanvas(tabID string, direction string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	clockwise, historyName, err := canvasRotateDirection(direction)
	if err != nil {
		return "", err
	}
	return s.ws.editTransformTab(tabID, nil, historyName, func(sess *session) error {
		rotateCanvasDocument(sess.doc, clockwise)
		// 宽高互换必须同步到标签描述符：工作区快照是前端画布盒子、状态栏尺寸与
		// 裁剪覆盖层起始框的依据（见 tabsize_test.go 的回归）。editTransformTab
		// 不像 EditActive 那样自动同步，所以在这里补上——本回调在工作区锁内执行。
		s.ws.syncTabSizeLocked(tabID, sess.doc)
		return nil
	})
}

func canvasRotateDirection(direction string) (clockwise bool, historyName string, err error) {
	switch direction {
	case "clockwise":
		return true, "旋转画布 90°（顺时针）", nil
	case "counterclockwise":
		return false, "旋转画布 90°（逆时针）", nil
	default:
		return false, "", fmt.Errorf("画布旋转方向无效: %q（应为 clockwise 或 counterclockwise）", direction)
	}
}

// rotateCanvasDocument 移植 CanvasRotation.swift 的 rotateCanvas 主体。domain 模型
// 把编组存成扁平列表里的普通行，所以遍历所有行天然带上编组及其子层，不需要另外
// 递归。宽高在最后互换：之前的 placement 与参考线数学都用转之前的画布尺寸。
func rotateCanvasDocument(doc *domain.Document, clockwise bool) {
	if doc == nil {
		return
	}
	canvasW, canvasH := float64(doc.Width), float64(doc.Height)
	for i := range doc.Layers {
		layer := &doc.Layers[i]
		layer.Transform = quarterTurnedTransform(layer.Transform, clockwise, canvasW, canvasH)
		if layer.MaskPlacement != nil {
			placement := quarterTurnedTransform(*layer.MaskPlacement, clockwise, canvasW, canvasH)
			layer.MaskPlacement = &placement
		}
	}
	if doc.GuidesList != nil {
		for i := range *doc.GuidesList {
			(*doc.GuidesList)[i] = quarterTurnedGuide((*doc.GuidesList)[i], clockwise, canvasW, canvasH)
		}
	}
	doc.Width, doc.Height = doc.Height, doc.Width
}

// quarterTurnedTransform 对齐 LayerTransform.quarterTurned(clockwise:canvas:)：
// 中心移到这圈转带它去的地方，角度跟着转，尺寸不变——因此整像素上的图层仍落在
// 整像素上，什么都不重采样。canvasW/canvasH 是转之前的画布尺寸。
func quarterTurnedTransform(t domain.Transform, clockwise bool, canvasW, canvasH float64) domain.Transform {
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	var mx, my float64
	if clockwise {
		mx, my = canvasH-cy, cx
	} else {
		mx, my = cy, canvasW-cx
	}
	t.Origin = [2]float64{mx - t.Size[0]/2, my - t.Size[1]/2}
	delta := -90.0
	if clockwise {
		delta = 90
	}
	t.Rotation = math.Mod(t.Rotation+delta, 360)
	return t
}

// quarterTurnedGuide 对齐 CanvasGuide.quarterTurned(clockwise:canvas:)：竖线变横线、
// 横线变竖线，位置按同一圈转过去（与原版 switch (axis, clockwise) 逐分支一致）。
func quarterTurnedGuide(g domain.Guide, clockwise bool, canvasW, canvasH float64) domain.Guide {
	switch {
	case g.Axis == domain.GuideAxisVertical && clockwise:
		g.Axis = domain.GuideAxisHorizontal
	case g.Axis == domain.GuideAxisVertical:
		g.Axis = domain.GuideAxisHorizontal
		g.Position = canvasW - g.Position
	case clockwise: // 横线顺时针
		g.Axis = domain.GuideAxisVertical
		g.Position = canvasH - g.Position
	default: // 横线逆时针
		g.Axis = domain.GuideAxisVertical
	}
	return g
}
