package bridge

// 剪贴板测试（票 48）：语义对着 reference/Swift/CompositorTests/SelectionClipboardTests.swift
// 写 —— 拷贝的像素原位粘回、剪切留洞并可撤销、整层复制、外部剪贴板图像居中、
// 别人拷贝后本应用的记忆失效。系统剪贴板换成内存假实现（export_test.go 的
// fakeClipboard），测试永不碰用户的剪贴板。

import (
	"encoding/json"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// clipFixture 把活动文档的底层画成"左半红、右半蓝"的全画布像素
// （SelectionClipboardTests.makeSession 的同一张图）。
func clipFixture(t *testing.T, svc *Service, width, height int) *session {
	t.Helper()
	sess := activeSessionOf(t, svc)
	l := &sess.doc.Layers[0]
	bmp := render.NewBitmap(sess.doc.Width, sess.doc.Height)
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			if x < width {
				bmp.Pix[i] = 255 // red
			} else {
				bmp.Pix[i+2] = 255 // blue
			}
			bmp.Pix[i+3] = 255
		}
	}
	sess.bitmaps[*l.ImageFile] = bmp
	return sess
}

// selPayload builds the frontend's selection payload: a fully-on mask.
func selPayload(x, y, w, h int) string {
	mask := make([]uint8, w*h)
	for i := range mask {
		mask[i] = 255
	}
	b, err := json.Marshal(filterSelection{X: x, Y: y, W: w, H: h, Mask: mask})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// layerBitmap reads one layer's pixels for assertions.
func layerBitmap(t *testing.T, sess *session, id string) *render.Bitmap {
	t.Helper()
	idx := findLayerIndex(sess.doc, id)
	if idx < 0 {
		t.Fatalf("找不到图层 %s", id)
	}
	l := &sess.doc.Layers[idx]
	if l.ImageFile == nil {
		t.Fatalf("图层 %s 没有像素", id)
	}
	bmp := sess.bitmaps[*l.ImageFile]
	if bmp == nil {
		t.Fatalf("内存位图库缺少 %s", *l.ImageFile)
	}
	return bmp
}

func pixelAt(t *testing.T, bmp *render.Bitmap, x, y int) [4]uint8 {
	t.Helper()
	if x < 0 || y < 0 || x >= bmp.W || y >= bmp.H {
		t.Fatalf("像素 (%d,%d) 在 %d×%d 之外", x, y, bmp.W, bmp.H)
	}
	i := (y*bmp.W + x) * 4
	return [4]uint8{bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3]}
}

func TestCopyAndPastePutsPixelsOnANewLayerInPlace(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	fake := useFakeClipboard(t)
	sess := clipFixture(t, svc, 50, 40)
	source := *sess.doc.ActiveLayerID

	reply, err := svc.CopySelection(selPayload(40, 10, 20, 20))
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON[copyReply](t, reply)
	if got.Kind != "pixels" || got.Width != 20 || got.Height != 20 || !got.Clipboard || got.Layer {
		t.Fatalf("拷贝回复 %+v", got)
	}
	if fake.puts != 1 || fake.last == nil || fake.last.W != 20 {
		t.Fatalf("系统剪贴板应收到 20 宽的位图: puts=%d last=%+v", fake.puts, fake.last)
	}

	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("粘贴后应有 2 层，得到 %d", len(env.Doc.Layers))
	}
	pasted := &env.Doc.Layers[len(env.Doc.Layers)-1]
	if pasted.ID == source {
		t.Fatal("粘贴应产生新层")
	}
	if pasted.Transform.Origin != [2]float64{40, 10} || pasted.Transform.Size != [2]float64{20, 20} {
		t.Fatalf("粘贴层几何 = %v/%v，想要 40,10 / 20×20", pasted.Transform.Origin, pasted.Transform.Size)
	}
	sess = activeSessionOf(t, svc)
	bmp := layerBitmap(t, sess, pasted.ID)
	if got := pixelAt(t, bmp, 5, 5); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("粘贴层左上（源的红区）= %v", got)
	}
	if got := pixelAt(t, bmp, 15, 5); got != [4]uint8{0, 0, 255, 255} {
		t.Fatalf("粘贴层右上（源的蓝区）= %v", got)
	}
	// 源层没被动过。
	src := layerBitmap(t, sess, source)
	if got := pixelAt(t, src, 45, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("源层像素应保持红色: %v", got)
	}
	// 粘贴是一步可撤销的 "粘贴"。
	snap, err := svc.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if env2 := parseEnvelope(t, snap); len(env2.Doc.Layers) != 1 {
		t.Fatalf("撤销后应回到 1 层，得到 %d", len(env2.Doc.Layers))
	}
}

func TestCutLeavesAHoleAndPasteRestoresThePixels(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	sess := clipFixture(t, svc, 50, 40)
	source := *sess.doc.ActiveLayerID

	out, err := svc.CutSelection(selPayload(10, 10, 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	parseEnvelope(t, out)
	sess = activeSessionOf(t, svc)
	if got := pixelAt(t, layerBitmap(t, sess, source), 15, 15); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("剪切后选区内的像素应为透明: %v", got)
	}
	if got := pixelAt(t, layerBitmap(t, sess, source), 25, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("剪切不应动到选区外: %v", got)
	}
	// 撤销把洞补回来（像素日志）。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	sess = activeSessionOf(t, svc)
	if got := pixelAt(t, layerBitmap(t, sess, source), 15, 15); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("撤销剪切后应恢复像素: %v", got)
	}
	// 再切一次并粘贴：像素回到原处。
	if _, err := svc.CutSelection(selPayload(10, 10, 10, 10)); err != nil {
		t.Fatal(err)
	}
	pastedRaw, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, pastedRaw)
	top := &env.Doc.Layers[len(env.Doc.Layers)-1]
	if top.Transform.Origin != [2]float64{10, 10} {
		t.Fatalf("粘贴回原位: origin=%v", top.Transform.Origin)
	}
	sess = activeSessionOf(t, svc)
	if got := pixelAt(t, layerBitmap(t, sess, top.ID), 5, 5); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("粘贴回来的像素 = %v", got)
	}
}

func TestCutWithoutSelectionFails(t *testing.T) {
	svc, _ := newTestService(t, 20, 20)
	useFakeClipboard(t)
	clipFixture(t, svc, 10, 20)
	if _, err := svc.CutSelection(""); err == nil || !strings.Contains(err.Error(), "没有选区") {
		t.Fatalf("没有选区的剪切应报错，得到 %v", err)
	}
}

func TestCopyWithoutSelectionTakesTheWholeLayerAndRemembersIt(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	sess := clipFixture(t, svc, 50, 40)
	source := *sess.doc.ActiveLayerID

	reply, err := svc.CopySelection("")
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON[copyReply](t, reply)
	if got.Kind != "pixels" || got.Width != 100 || got.Height != 40 || !got.Layer {
		t.Fatalf("整层拷贝回复 %+v", got)
	}
	// 粘贴优先整层（原版 pasteCopiedLayer 的同标签分支）。
	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("整层粘贴应复制一层，得到 %d 层", len(env.Doc.Layers))
	}
	copied := &env.Doc.Layers[len(env.Doc.Layers)-1]
	if copied.ID == source || copied.Transform.Origin != [2]float64{0, 0} {
		t.Fatalf("副本 %+v", copied)
	}
	if !strings.Contains(copied.Name, "copy") {
		t.Fatalf("副本命名应带 copy: %q", copied.Name)
	}
}

// 回归：整层拷贝记住的是**根**，同标签粘贴只能复制一次子树。先前存整棵子树的 id
// 列表时，duplicateLayersInSession 会把每个后代当独立根，子层被复制两遍。
func TestPasteOfACopiedFolderDuplicatesTheSubtreeOnce(t *testing.T) {
	svc, _ := newTestService(t, 60, 40)
	useFakeClipboard(t)
	sess := activeSessionOf(t, svc)
	// 底 + 编组(f) + 组内两层：活动层是编组。
	folder := newGroupLayer("组", 60, 40)
	sess.doc.Layers = append(sess.doc.Layers, folder)
	childA := newPixelLayer("甲", 60, 40)
	childB := newPixelLayer("乙", 60, 40)
	for _, l := range []domain.Layer{childA, childB} {
		l := l
		id := folder.ID
		l.ParentID = &id
		sess.bitmaps[*l.ImageFile] = render.NewBitmap(60, 40)
		sess.doc.Layers = append(sess.doc.Layers, l)
	}
	sess.doc.ActiveLayerID = &folder.ID
	before := len(sess.doc.Layers)

	if reply, err := svc.CopySelection(""); err != nil {
		t.Fatal(err)
	} else if got := parseJSON[copyReply](t, reply); !got.Layer {
		t.Fatalf("整层拷贝应记住图层: %+v", got)
	}
	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	// 复制的是"编组 + 两个孩子"这一棵子树，一次。
	if got, want := len(env.Doc.Layers), before+3; got != want {
		t.Fatalf("粘贴后层数 = %d，想要 %d（原 %d + 组与两个孩子各一份）", got, want, before)
	}
	names := make([]string, 0, len(env.Doc.Layers))
	for i := range env.Doc.Layers {
		names = append(names, env.Doc.Layers[i].Name)
	}
	count := func(name string) int {
		n := 0
		for _, s := range names {
			if s == name {
				n++
			}
		}
		return n
	}
	// insertCopy 只给根加 " copy" 后缀，子层保留原名。
	if count("甲") != 2 || count("乙") != 2 || count("组") != 1 || count("组 copy") != 1 {
		t.Fatalf("子树应各复制一次: %v", names)
	}
	// 副本里的两个孩子挂在副本编组下（副本编组名带 " copy"）。
	var copiedFolder *domain.Layer
	for i := range env.Doc.Layers {
		if env.Doc.Layers[i].Name == "组 copy" {
			copiedFolder = &env.Doc.Layers[i]
		}
	}
	if copiedFolder == nil {
		t.Fatalf("没有找到副本编组: %v", names)
	}
	children := 0
	for i := range env.Doc.Layers {
		if p := env.Doc.Layers[i].ParentID; p != nil && *p == copiedFolder.ID {
			children++
		}
	}
	if children != 2 {
		t.Fatalf("副本编组应带两个孩子，得到 %d: %v", children, names)
	}
}

func TestCopyWithSelectionDoesNotRememberTheLayer(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	clipFixture(t, svc, 50, 40)
	reply, err := svc.CopySelection(selPayload(0, 0, 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	if got := parseJSON[copyReply](t, reply); got.Layer {
		t.Fatalf("有选区时不应记住整层: %+v", got)
	}
	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("粘贴应加一层: %d", len(env.Doc.Layers))
	}
	if top := env.Doc.Layers[1]; top.Transform.Size != [2]float64{10, 10} {
		t.Fatalf("粘贴的是像素（10×10），得到 %v", top.Transform.Size)
	}
}

// 跨标签的 ⌘V：在 A 标签拷的整层，切到 B 标签粘贴 —— 走 copyLayersInto，
// 副本居中落在 B，B 一步可撤销，A 一点不动（ProjectWorkspace.pasteCopiedLayer
// 的跨项目分支，ProjectWorkspace.swift:199-206）。
func TestPasteOfALayerCopiedInAnotherTab(t *testing.T) {
	src := newFixture(t, 100, 80)
	srcID := src.pixel("红块")
	src.fill(t, srcID, 10, 10, 20, 20, 255, 0, 0)
	src.active(srcID)
	dst := newFixture(t, 64, 48)
	dst.active(dst.pixel("底"))
	p := newTransferPair(t, src, dst)
	useFakeClipboard(t)

	if _, err := p.ws.SelectTab(p.srcTab); err != nil {
		t.Fatal(err)
	}
	if _, err := p.svc.CopySelection(""); err != nil {
		t.Fatalf("源标签拷贝失败: %v", err)
	}
	if _, err := p.ws.SelectTab(p.dstTab); err != nil {
		t.Fatal(err)
	}
	out, err := p.svc.Paste()
	if err != nil {
		t.Fatalf("跨标签粘贴失败: %v", err)
	}
	env := parseEnvelope(t, out)
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("目标应有 2 层（底 + 副本），得到 %d", len(env.Doc.Layers))
	}
	copied := env.Doc.Layers[len(env.Doc.Layers)-1]
	if copied.Name != "红块" {
		t.Fatalf("副本名称 = %q，想要原名（跨项目复制不加 copy）", copied.Name)
	}
	// 100×80 的图层居中到 64×48 → 左上 (-18,-16)。
	if copied.Transform.Origin != [2]float64{-18, -16} {
		t.Fatalf("副本原点 = %v，想要居中后的 (-18,-16)", copied.Transform.Origin)
	}
	if env.Doc.ActiveLayerID == nil || *env.Doc.ActiveLayerID != copied.ID {
		t.Fatalf("目标活动层 = %v，想要副本", env.Doc.ActiveLayerID)
	}
	if p.dst.hist.UndoCount() != 1 {
		t.Fatalf("跨标签粘贴应是目标上的一步历史，UndoCount = %d", p.dst.hist.UndoCount())
	}
	if _, err := p.svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if len(p.dst.doc.Layers) != 1 {
		t.Fatalf("撤销后目标应回到 1 层，得到 %d", len(p.dst.doc.Layers))
	}
	if len(p.src.doc.Layers) != 1 {
		t.Fatalf("源标签不应被改动，得到 %d 层", len(p.src.doc.Layers))
	}
}

func TestPasteExternalImageIsCentered(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	fake := useFakeClipboard(t)
	// 别人拷的图：没有本应用的像素记忆，只有系统剪贴板里的位图。
	backed := render.NewBitmap(10, 6)
	for i := range backed.Pix {
		backed.Pix[i] = 255
	}
	fake.backed = backed
	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("外部图像应作为新层: %d", len(env.Doc.Layers))
	}
	top := env.Doc.Layers[1]
	if top.Transform.Origin != [2]float64{45, 17} || top.Transform.Size != [2]float64{10, 6} {
		t.Fatalf("外部图像应居中: origin=%v size=%v", top.Transform.Origin, top.Transform.Size)
	}
}

func TestPasteWithNothingOnTheClipboardFails(t *testing.T) {
	svc, _ := newTestService(t, 20, 20)
	fake := useFakeClipboard(t)
	fake.backed = nil // 空剪贴板
	if _, err := svc.Paste(); err == nil || !strings.Contains(err.Error(), "没有可粘贴") {
		t.Fatalf("空剪贴板粘贴应报错，得到 %v", err)
	}
}

func TestForeignCopyInvalidatesTheRememberedPixels(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	fake := useFakeClipboard(t)
	clipFixture(t, svc, 50, 40)

	if _, err := svc.CopySelection(selPayload(40, 10, 20, 20)); err != nil {
		t.Fatal(err)
	}
	// 另一个应用随后写了剪贴板：序号前进，本应用的记忆失效 → 走外部图像路径。
	fake.seq++
	backed := render.NewBitmap(4, 4)
	for i := 3; i < len(backed.Pix); i += 4 {
		backed.Pix[i] = 255
	}
	fake.backed = backed
	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	top := parseEnvelope(t, out).Doc.Layers[1]
	if top.Transform.Size != [2]float64{4, 4} {
		t.Fatalf("外部拷贝应覆盖本应用的记忆，得到 %v", top.Transform.Size)
	}
}

func TestLayerViaCopyMakesANewLayerInPlace(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	clipFixture(t, svc, 50, 40)
	out, err := svc.LayerViaCopy(selPayload(60, 0, 10, 40))
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("通过拷贝的图层应加一层: %d", len(env.Doc.Layers))
	}
	top := env.Doc.Layers[1]
	if top.Transform.Origin != [2]float64{60, 0} || top.Transform.Size != [2]float64{10, 40} {
		t.Fatalf("新层几何 = %v/%v", top.Transform.Origin, top.Transform.Size)
	}
	if _, err := svc.LayerViaCopy(""); err == nil {
		t.Fatal("没有选区的 LayerViaCopy 应报错")
	}
}

// 编组（和调整层）没有自己的像素：有选区时拷不出像素，报错；没有选区时按
// 原版 canCopyLayer 的语义**整层复制**（编组也照样能跨项目搬）。
func TestCopyOfAFolderNeedsNoPixelsWithoutASelection(t *testing.T) {
	svc, _ := newTestService(t, 20, 20)
	useFakeClipboard(t)
	clipFixture(t, svc, 10, 20)
	sess := activeSessionOf(t, svc)
	group := newGroupLayer("组", 20, 20)
	sess.doc.Layers = append(sess.doc.Layers, group)
	sess.doc.ActiveLayerID = &group.ID

	if _, err := svc.CopySelection(selPayload(0, 0, 5, 5)); err == nil ||
		!strings.Contains(err.Error(), "像素") {
		t.Fatalf("有选区时编组拷贝应报错（没有像素），得到 %v", err)
	}
	reply, err := svc.CopySelection("")
	if err != nil {
		t.Fatalf("无选区时编组应能整层复制: %v", err)
	}
	got := parseJSON[copyReply](t, reply)
	if !got.Layer || got.Kind != "layer" || got.Width != 0 {
		t.Fatalf("编组的整层复制回复 = %+v，想要 kind=layer、无像素", got)
	}
	// 粘贴回去：编组被复制一份。
	out, err := svc.Paste()
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	folders := 0
	for i := range env.Doc.Layers {
		if env.Doc.Layers[i].IsGroupLayer() {
			folders++
		}
	}
	if folders != 2 {
		t.Fatalf("粘贴后应有 2 个编组，得到 %d", folders)
	}
}

// 选区不是矩形全开时（套索/羽化边缘）：拷贝只带选区内的像素，软边按覆盖度
// 乘预乘通道。SelectionClipboardTests.lassoShapedSelectionCopiesAndPastes 的语义。
func TestCopyWithASoftLassoSelectionKeepsCoverage(t *testing.T) {
	svc, _ := newTestService(t, 20, 20)
	useFakeClipboard(t)
	sess := clipFixture(t, svc, 10, 20) // 左半红、右半蓝
	// 4×4 的选区：左两列全选（255），右两列半透明（128）—— 相当于一个软边套索。
	mask := make([]uint8, 16)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if x < 2 {
				mask[y*4+x] = 255
			} else {
				mask[y*4+x] = 128
			}
		}
	}
	payload, err := json.Marshal(filterSelection{X: 2, Y: 2, W: 4, H: 4, Mask: mask})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := svc.CopySelection(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON[copyReply](t, reply)
	if got.Width != 4 || got.Height != 4 {
		t.Fatalf("拷贝区域 = %d×%d，想要 4×4（选区包围盒）", got.Width, got.Height)
	}
	sess = activeSessionOf(t, svc)
	clip := sess.clipPixels
	if clip == nil || clip.bmp == nil {
		t.Fatal("没有记住拷贝的像素")
	}
	if clip.origin != [2]float64{2, 2} {
		t.Fatalf("拷贝原点 = %v，想要 (2,2)", clip.origin)
	}
	// 全选列：原样；半选列：预乘通道乘 128/255。
	if got := pixelAt(t, clip.bmp, 0, 0); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("全选列像素 = %v，想要不透明红", got)
	}
	if got := pixelAt(t, clip.bmp, 2, 0); got != [4]uint8{128, 0, 0, 128} {
		t.Fatalf("半选列像素 = %v，想要 alpha 128 的预乘红", got)
	}
	// 选区外（掩码 0）完全不拷贝：换个只覆盖一半画布的掩码验证。
	half := make([]uint8, 16)
	for y := 0; y < 4; y++ {
		for x := 0; x < 2; x++ {
			half[y*4+x] = 255
		}
	}
	payload2, err := json.Marshal(filterSelection{X: 0, Y: 0, W: 4, H: 4, Mask: half})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CopySelection(string(payload2)); err != nil {
		t.Fatal(err)
	}
	sess = activeSessionOf(t, svc)
	if got := pixelAt(t, sess.clipPixels.bmp, 3, 1); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("掩码为 0 的像素应完全透明: %v", got)
	}
}

func TestCopySelectionClippedToCanvas(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	clipFixture(t, svc, 50, 40)
	fake := useFakeClipboard(t)
	// 选区比画布大：拷贝区域裁到画布。
	reply, err := svc.CopySelection(selPayload(90, 30, 50, 50))
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON[copyReply](t, reply)
	if got.Width != 10 || got.Height != 10 {
		t.Fatalf("越界选区应裁到画布: %+v", got)
	}
	// 完全在画布外的选区：报错且不动剪贴板。
	fake.puts = 0
	if _, err := svc.CopySelection(selPayload(200, 200, 10, 10)); err == nil {
		t.Fatal("画布外选区应报错")
	}
	if fake.puts != 0 {
		t.Fatalf("失败的拷贝不应写剪贴板: puts=%d", fake.puts)
	}
}

func TestCutSelectionIsOneUndoStep(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	clipFixture(t, svc, 50, 40)
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.CutSelection(selPayload(10, 10, 10, 10)); err != nil {
		t.Fatal(err)
	}
	// 只有像素变了：文档模型本身（层数/几何）不变。
	after := mustDocumentSnapshot(t, svc)
	if len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatalf("剪切不应改层数: %d → %d", len(before.Doc.Layers), len(after.Doc.Layers))
	}
	if after.Rev == before.Rev {
		t.Fatal("剪切应推进 rev（像素撤销需要）")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	sess := activeSessionOf(t, svc)
	if got := pixelAt(t, layerBitmap(t, sess, *sess.doc.ActiveLayerID), 15, 15); got != [4]uint8{0, 0, 0, 0} {
		t.Fatalf("重做剪切后应又是透明: %v", got)
	}
}

func TestCutOutsideTheLayerIsNotAnUndoStep(t *testing.T) {
	svc, _ := newTestService(t, 100, 40)
	useFakeClipboard(t)
	sess := clipFixture(t, svc, 50, 40)
	// 把层挪到画布右下角之外，选区落在层外。
	sess.doc.Layers[0].Transform.Origin = [2]float64{60, 20}
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.CutSelection(selPayload(0, 0, 5, 5)); err != nil {
		t.Fatal(err)
	}
	if after := mustDocumentSnapshot(t, svc); after.Rev != before.Rev {
		t.Fatalf("选区没盖住图层的剪切不应产生历史: rev %d → %d", before.Rev, after.Rev)
	}
}
