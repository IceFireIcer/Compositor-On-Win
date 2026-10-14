package bridge

// 票 67：图层特效与可编辑内容随尺寸编辑保留。
//
// 上游 94e90bd（LayerEffectsSurviveTests.swift）的 Go 对应。原版的病灶是
// "从快照重建图层"时漏掉 effects / live text / live shape；Go 侧除了
// applyImageSize 之外都是就地改 *domain.Layer，本来就不会丢。这些测试把
// 每一条路径都钉住：
//
//	CanvasSize / Crop / Trim   平移图层，Effects / Text / Shape 原样保留
//	ImageSize（仅改分辨率）    提前 return，三样都保留
//	ImageSize（真的重采样）    按 sqrt(sx*sy) 缩放 Effects，文字与形状栅格化
//	Hue/Saturation、Levels、Invert、选区变换  活动图层的 Effects 保留

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"compositor-win/internal/domain"
)

// surviveEffects 是 LayerEffectsSurviveTests.swift:9 的 fixture：
// stroke 4、shadow distance 20 / blur 10、outer glow 8。
func surviveEffects() *domain.Effects {
	return &domain.Effects{
		Stroke:    &domain.StrokeEffect{Size: 4, Red: 1, Opacity: 1},
		Shadow:    &domain.ShadowEffect{Angle: 90, Distance: 20, Blur: 10, Opacity: 0.5},
		OuterGlow: &domain.GlowEffect{Size: 8, Red: 1, Green: 1, Blue: 1, Opacity: 0.75},
	}
}

// layerExtras 是原版测试里的 Extras：图层像素之外还带着什么。
func layerExtras(l *domain.Layer) (effects *domain.Effects, text *domain.TextStyle, shape *domain.ShapeStyle) {
	if l == nil {
		return nil, nil, nil
	}
	return l.Effects, l.Text, l.Shape
}

func extrasOf(sess *session, id string) (*domain.Effects, *domain.TextStyle, *domain.ShapeStyle) {
	return layerExtras(layerAt(sess.doc, id))
}

// effectsEqual compares the bundles field by field. Go's struct == on a
// pointer field compares the addresses, so *a == *b would always be false for
// two separately built bundles.
func effectsEqual(a, b *domain.Effects) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if !strokeEqual(a.Stroke, b.Stroke) || !shadowEqual(a.Shadow, b.Shadow) ||
		!shadowEqual(a.InnerShadow, b.InnerShadow) || !overlayEqual(a.ColorOverlay, b.ColorOverlay) ||
		!glowEqual(a.OuterGlow, b.OuterGlow) || !glowEqual(a.InnerGlow, b.InnerGlow) {
		return false
	}
	return true
}

func strokeEqual(a, b *domain.StrokeEffect) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func shadowEqual(a, b *domain.ShadowEffect) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func overlayEqual(a, b *domain.OverlayEffect) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func glowEqual(a, b *domain.GlowEffect) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// textEqual / shapeEqual compare through the JSON the manifest would carry:
// TextStyle holds run slices, so it is not comparable with ==.
func textEqual(a, b *domain.TextStyle) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && string(aJSON) == string(bJSON)
}

func shapeEqual(a, b *domain.ShapeStyle) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// extrasEqual 比较三件套是否逐字段相同。
func extrasEqual(a, b *domain.Effects, aText, bText *domain.TextStyle, aShape, bShape *domain.ShapeStyle) bool {
	return effectsEqual(a, b) && textEqual(aText, bText) && shapeEqual(aShape, bShape)
}

// surviveFixture 建一份 200×120 的透明文档，带一个可编辑文字层和一个可编辑
// 形状层，两层都挂着 surviveEffects()。返回 service、session 与
// [文字层, 形状层] 的 id。
func surviveFixture(t *testing.T) (*Service, *session, []string) {
	t.Helper()
	svc, _ := newTestService(t, 200, 120)
	sess := activeSessionOf(t, svc)
	sess.doc.Layers[0].ImageFile = nil // 空文档：修剪能真的裁掉四周空白

	if _, err := svc.TextCommit(textCommitPayload("", "Text", "", "")); err != nil {
		t.Fatalf("TextCommit failed: %v", err)
	}
	if _, err := svc.DrawShape(sess.doc.DocumentID, "Rectangle", 120, 30, 170, 80, ""); err != nil {
		t.Fatalf("DrawShape failed: %v", err)
	}
	textID, shapeID := "", ""
	for i := range sess.doc.Layers {
		l := &sess.doc.Layers[i]
		switch {
		case l.Text != nil:
			textID = l.ID
			l.Effects = surviveEffects()
		case l.Shape != nil:
			shapeID = l.ID
			l.Effects = surviveEffects()
		}
	}
	if textID == "" || shapeID == "" || textID == shapeID {
		t.Fatalf("fixture 缺少可编辑文字/形状层: text=%q shape=%q", textID, shapeID)
	}
	return svc, sess, []string{textID, shapeID}
}

// extrasSnapshot 取一组图层当前的三件套。
func extrasSnapshot(ids []string, sess *session) ([]*domain.Effects, []*domain.TextStyle, []*domain.ShapeStyle) {
	effects := make([]*domain.Effects, len(ids))
	texts := make([]*domain.TextStyle, len(ids))
	shapes := make([]*domain.ShapeStyle, len(ids))
	for i, id := range ids {
		effects[i], texts[i], shapes[i] = extrasOf(sess, id)
	}
	return effects, texts, shapes
}

// assertExtrasKept 断言这些图层还带着原来的 Effects / Text / Shape。
func assertExtrasKept(t *testing.T, sess *session, ids []string, wantEffects []*domain.Effects, wantText []*domain.TextStyle, wantShape []*domain.ShapeStyle) {
	t.Helper()
	for i, id := range ids {
		effects, text, shape := extrasOf(sess, id)
		if !extrasEqual(effects, wantEffects[i], text, wantText[i], shape, wantShape[i]) {
			t.Fatalf("图层 %s 的三件套变了: effects=%+v text=%+v shape=%+v", id, effects, text, shape)
		}
	}
}

// pixelLayerWithEffects 是一张 48×48 的文档，活动图层画了一个实心方块并挂着
// surviveEffects()。
func pixelLayerWithEffects(t *testing.T) (*Service, *session, string) {
	t.Helper()
	svc, _ := newTestService(t, 48, 48)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 3, 3, 21, 21, 180)
	id := sess.doc.Layers[0].ID
	sess.doc.Layers[0].Effects = surviveEffects()
	return svc, sess, id
}

// ---------------------------------------------------------------------------
// 保存重开：包里的 effects 与内存里的一致

// TestEffectsSurviveSaveAndReopen 覆盖票 67 第三条的后半句：重采样把特效缩放、
// 把文字与形状栅格化之后，保存下来的 .comp 与重开后的文档必须还是那一份——
// 否则「保存重开」会悄悄把 Image Size 的结果退回去。
func TestEffectsSurviveSaveAndReopen(t *testing.T) {
	svc, sess, id := pixelLayerWithEffects(t)
	if _, err := svc.ImageSize(`{"width":24,"height":24,"resolution":72,"sampling":"High quality"}`); err != nil {
		t.Fatal(err)
	}
	scaled, _, _ := extrasOf(sess, id)
	if scaled == nil || scaled.Stroke == nil || scaled.Stroke.Size != 2 ||
		scaled.Shadow == nil || scaled.Shadow.Distance != 10 || scaled.OuterGlow == nil || scaled.OuterGlow.Size != 4 {
		t.Fatalf("重采样应把特效缩到一半: %+v", scaled)
	}

	dir := t.TempDir()
	savePath := filepath.Join(dir, "effects.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	svc.openDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatal(err)
	}

	ws2 := NewWorkspace()
	svc2 := NewService(ws2)
	svc2.openDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc2.OpenProjectDialog(); err != nil {
		t.Fatal(err)
	}
	reopened, _, _ := extrasOf(activeSessionOf(t, svc2), id)
	if !effectsEqual(reopened, scaled) {
		t.Fatalf("保存重开后特效变了: %+v → %+v", scaled, reopened)
	}
}

// ---------------------------------------------------------------------------
// CanvasSize / Crop / Trim：平移图层，三件套不动

func TestCanvasSizeKeepsEffectsTextAndShapes(t *testing.T) {
	svc, sess, ids := surviveFixture(t)
	wantEffects, wantText, wantShape := extrasSnapshot(ids, sess)
	if wantText[0] == nil || wantShape[1] == nil {
		t.Fatal("fixture 应带可编辑文字与形状")
	}
	env := parseJSON[envelope](t, callCanvasSize(t, svc, `{"width":260,"height":160,"anchor":4}`))
	if env.Doc.Width != 260 || env.Doc.Height != 160 {
		t.Fatalf("画布 %d×%d，想要 260×160", env.Doc.Width, env.Doc.Height)
	}
	assertExtrasKept(t, sess, ids, wantEffects, wantText, wantShape)
}

func TestCropKeepsEffectsTextAndShapes(t *testing.T) {
	svc, sess, ids := surviveFixture(t)
	wantEffects, wantText, wantShape := extrasSnapshot(ids, sess)
	env := parseJSON[envelope](t, callCrop(t, svc, `{"x":10,"y":10,"width":180,"height":100}`))
	if env.Doc.Width != 180 || env.Doc.Height != 100 {
		t.Fatalf("裁剪后 %d×%d，想要 180×100", env.Doc.Width, env.Doc.Height)
	}
	assertExtrasKept(t, sess, ids, wantEffects, wantText, wantShape)
}

func TestTrimKeepsEffectsTextAndShapes(t *testing.T) {
	svc, sess, ids := surviveFixture(t)
	wantEffects, wantText, wantShape := extrasSnapshot(ids, sess)
	env := parseJSON[envelope](t, callTrim(t, svc, `{"basedOn":"transparent","top":true,"bottom":true,"left":true,"right":true}`))
	if env.Doc.Width >= 200 {
		t.Fatalf("修剪应缩小画布，得到 %d×%d", env.Doc.Width, env.Doc.Height)
	}
	assertExtrasKept(t, sess, ids, wantEffects, wantText, wantShape)
}

// ---------------------------------------------------------------------------
// ImageSize：只改分辨率 vs 真的重采样

func TestResolutionOnlyImageSizeKeepsEffectsTextAndShapes(t *testing.T) {
	svc, sess, ids := surviveFixture(t)
	wantEffects, wantText, wantShape := extrasSnapshot(ids, sess)
	env := parseJSON[envelope](t, callImageSize(t, svc, `{"width":200,"height":120,"resolution":300,"sampling":"Smooth"}`))
	if env.Doc.Resolution == nil || *env.Doc.Resolution != 300 {
		t.Fatalf("分辨率 = %v，想要 300", env.Doc.Resolution)
	}
	assertExtrasKept(t, sess, ids, wantEffects, wantText, wantShape)
}

// 重采样把像素缩放，特效按 Photoshop 的 Scale Styles 跟着缩放；文字与形状
// 变成普通像素，可编辑元数据被丢弃（ImageResizer.swift:98-101）。
func TestImageSizeKeepsEffectsScaledWithTheImage(t *testing.T) {
	svc, sess, id := pixelLayerWithEffects(t)
	layer := layerAt(sess.doc, id)
	layer.Text = &domain.TextStyle{Content: "栅格化"}
	layer.Shape = &domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: 1}
	env := parseJSON[envelope](t, callImageSize(t, svc, `{"width":24,"height":24,"resolution":72,"sampling":"High quality"}`))
	if env.Doc.Width != 24 || env.Doc.Height != 24 {
		t.Fatalf("图像尺寸 %d×%d，想要 24×24", env.Doc.Width, env.Doc.Height)
	}
	// 48→24 是 0.5 倍：stroke 4→2、outerGlow 8→4、shadow distance 20→10、
	// blur 10→5，angle 不变。
	kept := layerAt(activeSessionOf(t, svc).doc, id).Effects
	if kept == nil || kept.Stroke == nil || kept.Stroke.Size != 2 {
		t.Fatalf("stroke 应缩放到 2：%+v", kept)
	}
	if kept.OuterGlow == nil || kept.OuterGlow.Size != 4 {
		t.Fatalf("outer glow 应缩放到 4：%+v", kept.OuterGlow)
	}
	if kept.Shadow == nil || kept.Shadow.Distance != 10 || kept.Shadow.Blur != 5 || kept.Shadow.Angle != 90 {
		t.Fatalf("shadow 应为 distance 10 / blur 5 / angle 90：%+v", kept.Shadow)
	}
	l := layerAt(activeSessionOf(t, svc).doc, id)
	if l.Text != nil || l.Shape != nil {
		t.Fatalf("重采样应栅格化文字与形状：text=%+v shape=%+v", l.Text, l.Shape)
	}
	// 撤销把缩放前的 Effects 与可编辑元数据一起带回来。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := layerAt(activeSessionOf(t, svc).doc, id)
	if !effectsEqual(back.Effects, surviveEffects()) || back.Text == nil || back.Shape == nil {
		t.Fatalf("撤销后三件套未复原：effects=%+v text=%+v shape=%+v", back.Effects, back.Text, back.Shape)
	}
}

// 非等比缩放按 sqrt(sx*sy) 缩放（ImageResizer.swift:30 的两轴均值）。
func TestImageSizeStretchedScalesEffectsByMeanFactor(t *testing.T) {
	svc, _, id := pixelLayerWithEffects(t)
	// 48×48 → 96×24：sx = 2, sy = 0.5, factor = 1。
	parseJSON[envelope](t, callImageSize(t, svc, `{"width":96,"height":24,"resolution":72,"sampling":"High quality"}`))
	kept := layerAt(activeSessionOf(t, svc).doc, id).Effects
	if kept == nil || kept.Stroke == nil || kept.Stroke.Size != 4 {
		t.Fatalf("sx*sy = 1 时 stroke 应保持 4：%+v", kept)
	}
	if kept.Shadow == nil || kept.Shadow.Distance != 20 || kept.Shadow.Blur != 10 {
		t.Fatalf("sx*sy = 1 时 shadow 应保持 20/10：%+v", kept.Shadow)
	}
}

// 没有位图的图层（编组/调整层）在重采样时同样缩放到位的特效，与
// ImageResizer 的无条件写入一致。
func TestImageSizeScalesEffectsOnAssetlessLayers(t *testing.T) {
	svc, _ := newTestService(t, 48, 48)
	sess := activeSessionOf(t, svc)
	group := newGroupLayer("Folder", 48, 48)
	group.Effects = surviveEffects()
	sess.doc.Layers = append(sess.doc.Layers, group)
	callImageSize(t, svc, `{"width":24,"height":24,"resolution":72,"sampling":"Smooth"}`)
	kept := layerAt(activeSessionOf(t, svc).doc, group.ID).Effects
	if kept == nil || kept.Stroke == nil || kept.Stroke.Size != 2 || kept.OuterGlow == nil || kept.OuterGlow.Size != 4 {
		t.Fatalf("无位图图层的特效也应缩放：%+v", kept)
	}
}

// ---------------------------------------------------------------------------
// 像素调整与选区变换：活动图层的 Effects 保留

func TestHueSaturationKeepsEffects(t *testing.T) {
	svc, sess, id := pixelLayerWithEffects(t)
	if _, err := svc.BeginFilterEdit("adjust:Hue/Saturation", "", "{}", 7, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitFilter(`{"adjustment":{"kind":"Hue/Saturation","hue":90}}`); err != nil {
		t.Fatal(err)
	}
	if effects := layerAt(sess.doc, id).Effects; !effectsEqual(effects, surviveEffects()) {
		t.Fatalf("Hue/Saturation 后 effects = %+v", effects)
	}
}

func TestLevelsKeepsEffects(t *testing.T) {
	svc, sess, id := pixelLayerWithEffects(t)
	settings := domain.LevelsSettings{}
	for c := range settings.Ranges {
		settings.Ranges[c] = domain.IdentityLevelRange()
	}
	settings.Ranges[0] = domain.LevelRange{Black: 64, Gamma: 1, White: 128, OutputWhite: 255}
	payload := payloadJSON(t, map[string]any{"adjustment": map[string]any{
		"kind": "Levels", "levels": settings,
	}})
	if _, err := svc.ApplyFilter("adjust:Levels", "", payload, 7, ""); err != nil {
		t.Fatal(err)
	}
	if effects := layerAt(sess.doc, id).Effects; !effectsEqual(effects, surviveEffects()) {
		t.Fatalf("Levels 后 effects = %+v", effects)
	}
}

func TestInvertKeepsEffects(t *testing.T) {
	svc, sess, id := pixelLayerWithEffects(t)
	if _, err := svc.ApplyFilter(FilterInvert, "", "{}", 0, ""); err != nil {
		t.Fatal(err)
	}
	if effects := layerAt(sess.doc, id).Effects; !effectsEqual(effects, surviveEffects()) {
		t.Fatalf("Invert 后 effects = %+v", effects)
	}
}

// 选区变换（Go 侧是 MoveSelectedPixels，浮动选区合并的对应实现）：像素与选区
// 移动，图层记录不被重建，Effects 保留。
func TestTransformingASelectionKeepsEffects(t *testing.T) {
	svc, sess, id := pixelLayerWithEffects(t)
	sel := &filterSelection{X: 4, Y: 4, W: 20, H: 20, Mask: make([]uint8, 20*20)}
	for i := range sel.Mask {
		sel.Mask[i] = 255
	}
	if _, err := svc.MoveSelectedPixels(payloadJSON(t, sel), 8, 0); err != nil {
		t.Fatal(err)
	}
	if got := sess.hist.UndoName(); got != "移动像素" {
		t.Fatalf("历史项名 = %q，想要「移动像素」", got)
	}
	if effects := layerAt(sess.doc, id).Effects; !effectsEqual(effects, surviveEffects()) {
		t.Fatalf("选区变换后 effects = %+v", effects)
	}
}
