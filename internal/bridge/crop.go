package bridge

// 裁剪（票 18 尾项）：把"按给定矩形裁剪并平移内容"补成 Go 侧的文档命令。
//
// 桥里原有 CanvasSize / ImageSize / Trim 三个几何端点，语义都不是"按给定矩形
// 裁剪"：Trim 自己算内容包围盒，CanvasSize 只按九宫格锚点平移，都不接受调用方
// 给的矩形。前端 crop.ts 的 commitCrop 早已产出纯历史数据
// {width,height,offsetX,offsetY}，却没有任何 RPC 消费它，裁剪工具因此整条链路
// 不可达（工具轨里 C 键选中后画布上什么都不做）。
//
// 平移语义与 CanvasSize 的平移分支一致（doc_geometry.go）：新画布 = 矩形尺寸，
// 每个图层的变换原点（以及未链接蒙版的位移）平移 -offset，参考线按同一偏移
// 平移。唯一的差别是**越界参考线**：CanvasSize 原样保留画布外的参考线，裁剪则
// 按票 18 的约定丢弃落到新画布外的（与前端 offsetGuides 同规则）——裁剪后画布
// 外的参考线再也拖不回来。
//
// 参考线只在这里平移一次：前端提交后应用 Crop 返回的快照就够了
// （document.applyDocumentSnapshot → view.syncGuidesFromDocument 会重建前端
// 镜像），前端若再 offsetGuides + SetGuides 写回会二次偏移。
//
// 矩形虽然由前端 normalize/round/clamp 过，桥不能信前端：正尺寸、DocumentLimits
// 的边长与单表面像素、原点范围、与当前画布的正面积交集都在这里独立校验。非法
// 一律返回中文错误；Workspace.EditActive 会把工作副本回滚，因此失败的 Crop
// 既不留历史也不改动文档，且任何输入都不会 panic。

import (
	"encoding/json"
	"fmt"

	"compositor-win/internal/domain"
)

// cropRectRequest 是 Crop 的载荷：文档像素的裁剪矩形，左上角 + 尺寸。
// 前端 crop.ts 的 CropCommit 用的就是这套字段名。
type cropRectRequest struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// cropMaxPosition 与 CropGeometry.valid / 前端 CROP_MAX_POSITION 同值：
// 原版把裁剪框原点的绝对值上限钉在 1,000,000 px。没有它，一个巨大的负原点
// 会悄悄造出一份原点远离画布的文档。
const cropMaxPosition = 1_000_000

// Crop 按给定矩形裁剪活动文档。rectJSON 形如
// {"x":<int>,"y":<int>,"width":<int>,"height":<int>}；成功返回标准文档信封
// （{"rev":N,"doc":{…}}），整个裁剪是一个名为"裁剪"的历史项。
func (s *Service) Crop(rectJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	var req cropRectRequest
	if err := json.Unmarshal([]byte(rectJSON), &req); err != nil {
		return "", fmt.Errorf("无法解析裁剪矩形: %w", err)
	}
	return s.ws.EditActive("裁剪", func(sess *session) error {
		return applyCrop(sess, req)
	})
}

// applyCrop validates the rect and applies it in place. It is the mutator of
// the "裁剪" transaction: on error the caller rolls the document back, so it
// must finish its checks before the first write.
func applyCrop(sess *session, req cropRectRequest) error {
	doc := sess.doc
	if req.Width < 1 || req.Height < 1 {
		return fmt.Errorf("裁剪矩形尺寸必须为正（得到 %d × %d）", req.Width, req.Height)
	}
	if req.Width > domain.MaxSide || req.Height > domain.MaxSide {
		return fmt.Errorf("裁剪尺寸 %d × %d 超出 %d 像素边长限制", req.Width, req.Height, domain.MaxSide)
	}
	if req.X < -cropMaxPosition || req.X > cropMaxPosition || req.Y < -cropMaxPosition || req.Y > cropMaxPosition {
		return fmt.Errorf("裁剪原点 (%d, %d) 超出 ±%d px", req.X, req.Y, cropMaxPosition)
	}
	// 允许裁剪框越出画布（原版语义），只要有正面积交集：越界部分本来就是空白。
	if !cropIntersectsCanvas(req, doc.Width, doc.Height) {
		return fmt.Errorf("裁剪矩形与当前画布没有正面积交集")
	}
	// 整幅裁剪是空操作：不写历史、不涨 rev，和 CanvasSize 的"什么都没变"一致。
	if req.X == 0 && req.Y == 0 && req.Width == doc.Width && req.Height == doc.Height {
		return nil
	}
	if req.Width*req.Height > domain.MaxSurfacePixels {
		return fmt.Errorf("裁剪尺寸 %d × %d 超出单表面 %.0f 百万像素上限",
			req.Width, req.Height, float64(domain.MaxSurfacePixels)/1e6)
	}

	offsetX, offsetY := float64(req.X), float64(req.Y)
	// 所有图层（含编组、调整层）连同未链接蒙版的位移一起平移：新原点落在旧
	// 坐标 (offsetX, offsetY)，内容保持不动就等价于每个原点减去它。
	for i := range doc.Layers {
		layer := &doc.Layers[i]
		layer.Transform.Origin[0] -= offsetX
		layer.Transform.Origin[1] -= offsetY
		if !transformFinite(layer.Transform) {
			return fmt.Errorf("图层变换超出范围")
		}
		if layer.MaskPlacement != nil {
			layer.MaskPlacement.Origin[0] -= offsetX
			layer.MaskPlacement.Origin[1] -= offsetY
		}
	}
	// 参考线：竖线的位置是 x、横线是 y，按同一偏移平移，落在新画布外的丢掉。
	// 只有本来就有参考线时才写回（nil 指针 = manifest 里没有这个字段，不能被
	// 裁剪凭空造出一个 "guides":[]）。
	if guides := doc.Guides(); guides != nil {
		kept := make([]domain.Guide, 0, len(guides))
		for _, guide := range guides {
			shifted := guide
			length := req.Width
			if guide.Axis == domain.GuideAxisHorizontal {
				shifted.Position -= offsetY
				length = req.Height
			} else {
				shifted.Position -= offsetX
			}
			// 正好压在新边缘上的参考线保留（与前端 offsetGuides 的闭区间一致）。
			if shifted.Position >= 0 && shifted.Position <= float64(length) {
				kept = append(kept, shifted)
			}
		}
		doc.GuidesList = &kept
	}
	doc.Width, doc.Height = req.Width, req.Height
	return nil
}

// cropIntersectsCanvas reports whether the rect and the canvas share positive
// area — half-open intervals, so merely touching an edge is not an overlap.
func cropIntersectsCanvas(req cropRectRequest, canvasWidth, canvasHeight int) bool {
	left, right := max(req.X, 0), min(req.X+req.Width, canvasWidth)
	top, bottom := max(req.Y, 0), min(req.Y+req.Height, canvasHeight)
	return left < right && top < bottom
}
