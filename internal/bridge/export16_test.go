package bridge

// 票 84：Export As 的 16 位每通道。

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/tiff"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// deepLayerForExport 给活动图层挂上一份只差低位的深层栅格，并把变换摆成恒等 —— 也就是
// 闸门允许的那一种情形。四个像素的值相差 3，8 位往返会把它们压成同一个数。
func deepLayerForExport(t *testing.T, svc *Service) *render.DeepRaster {
	t.Helper()
	sess := activeSessionOf(t, svc)
	l := &sess.doc.Layers[0]
	l.Origin = [2]float64{0, 0}
	l.Size = [2]float64{float64(sess.doc.Width), float64(sess.doc.Height)}
	l.Rotation, l.FlipX, l.FlipY = 0, false, false
	deep := render.NewDeep(sess.doc.Width, sess.doc.Height, false)
	for x := 0; x < deep.W; x++ {
		i := x * 4
		v := uint16(0x4000 + x*3)
		deep.Pix[i], deep.Pix[i+1], deep.Pix[i+2] = v, v, v
		deep.Pix[i+3] = 65535
	}
	bmp := sess.bitmaps[*l.ImageFile]
	bmp.Deep = deep
	return deep
}

func TestExportAsSixteenBitPNGKeepsSixteenBits(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	deepLayerForExport(t, svc)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "wide"))
	reply, err := svc.ExportAs(`{"format":"png","sixteenBit":true,"defaultName":"wide.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	path := parseJSON[map[string]string](t, reply)["path"]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("导出的 PNG 应可重新打开核验: %v", err)
	}
	if img.Bounds().Dx() != 4 {
		t.Fatalf("尺寸 = %v", img.Bounds())
	}
	var got [4]uint32
	for x := 0; x < 4; x++ {
		r, _, _, _ := img.At(x, 0).RGBA()
		got[x] = r
	}
	same := true
	for x := 1; x < 4; x++ {
		if got[x] != got[0] {
			same = false
		}
	}
	if same {
		t.Fatalf("四个只差低位的像素导出后相同（%d）—— 说明先压成了 8 位", got[0])
	}
}

func TestExportAsSixteenBitTIFFKeepsSixteenBits(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	deepLayerForExport(t, svc)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "wide"))
	reply, err := svc.ExportAs(`{"format":"tiff","sixteenBit":true,"defaultName":"wide.tif"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	img, err := tiff.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("导出的 TIFF 应可重新打开核验: %v", err)
	}
	var got [4]uint32
	for x := 0; x < 4; x++ {
		r, _, _, _ := img.At(x, 0).RGBA()
		got[x] = r
	}
	if got[0] == got[3] {
		t.Fatalf("两端像素相同（%d）—— 说明先压成了 8 位", got[0])
	}
}

// 票 85：HDR 栅格不能走 16 位 PNG/TIFF —— 超白值会被裁到 1.0 且**无声无息**。
//
// 这条是变异审计查出来的：把 sixteenBitExportRaster 里那段 HDR 拒绝去掉，全部测试照样通过。
// 同时钉住另一侧 —— 同一份文档走 EXR 必须成功，否则「拒绝」就变成了「谁也导不出」。
func TestExportAsSixteenBitRefusesAnHDRRasterButEXRAcceptsIt(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	deep := deepLayerForExport(t, svc)
	deep.HDR = true
	// half 表示的超白：0x4C00 是 4.0，远在白点之上。
	for x := 0; x < deep.W; x++ {
		i := x * 4
		deep.Pix[i], deep.Pix[i+1], deep.Pix[i+2] = 0x4C00, 0x4C00, 0x4C00
		deep.Pix[i+3] = 0x3C00
	}

	if _, err := svc.ExportAs(`{"format":"png","sixteenBit":true}`); err == nil || !strings.Contains(err.Error(), "HDR") {
		t.Fatalf("HDR 栅格走 16 位应当被拒绝并说明去 EXR，得到 %v", err)
	}

	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "wide"))
	reply, err := svc.ExportAs(`{"format":"exr","defaultName":"wide.exr"}`)
	if err != nil {
		t.Fatalf("同一份 HDR 栅格走 EXR 应当成功，得到 %v", err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("EXR 是空的")
	}
}

// 多图层现在**成功**（票 84 的 U31 剩余）：render.CompositeDeep 用 16 位样本跑 8 位那条普通
// 公式。上面那层不透明，所以合出来就是它的值 —— 而那两个值只差低位，8 位往返会把它们压成同一个数。
func TestExportAsSixteenBitCompositesMultipleLayers(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	deepLayerForExport(t, svc)
	sess := activeSessionOf(t, svc)
	second := sess.doc.Layers[0]
	second.ID, second.Name = "top", "top"
	image := "top.png"
	second.ImageFile = &image
	bitmap := render.NewBitmap(sess.doc.Width, sess.doc.Height)
	deep := render.NewDeep(sess.doc.Width, sess.doc.Height, false)
	for x := 0; x < deep.W; x++ {
		i := x * 4
		v := uint16(0x8000 + x*3)
		deep.Pix[i], deep.Pix[i+1], deep.Pix[i+2] = v, v, v
		deep.Pix[i+3] = 65535
	}
	bitmap.Deep = deep
	sess.bitmaps[image] = bitmap
	sess.doc.Layers = append(sess.doc.Layers, second)

	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "stack"))
	reply, err := svc.ExportAs(`{"format":"png","sixteenBit":true,"defaultName":"stack.png"}`)
	if err != nil {
		t.Fatalf("多图层 16 位导出应当成功，得到 %v", err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("导出的 PNG 应可重新打开核验: %v", err)
	}
	var got [4]uint32
	for x := 0; x < 4; x++ {
		r, _, _, _ := img.At(x, 0).RGBA()
		got[x] = r
	}
	if got[0] == got[3] {
		t.Fatalf("两端像素相同（%d）—— 多图层这条路先把结果压成了 8 位", got[0])
	}
	if want := uint32(0x8080); got[0] < want-0x80 || got[0] > want+0x80 {
		t.Errorf("合出来 %d，想要接近上面那层的 %d", got[0], want)
	}
}

// 合成描述不了的情形仍然**明确拒绝**，并指出是哪一层、哪一条属性。
func TestExportAsSixteenBitRefusesWhatTheCompositeCannotDescribe(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*domain.Layer)
		want   string
	}{
		{"蒙版", func(l *domain.Layer) {
			f, on := "m.png", true
			l.MaskFile, l.MaskEnabled = &f, &on
		}, "蒙版"},
		{"非正常混合", func(l *domain.Layer) {
			m := domain.BlendMultiply
			l.BlendMode = &m
		}, "混合模式"},
		{"组", func(l *domain.Layer) {
			isGroup := true
			l.IsGroup = &isGroup
		}, "组"},
	}
	for _, c := range cases {
		svc, _ := newTestService(t, 4, 1)
		deepLayerForExport(t, svc)
		sess := activeSessionOf(t, svc)
		sess.doc.Layers = append(sess.doc.Layers, sess.doc.Layers[0])
		c.mutate(&sess.doc.Layers[1])
		if _, err := svc.ExportAs(`{"format":"png","sixteenBit":true}`); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s 应被拒绝并说明原因，得到 %v", c.name, err)
		}
	}
}

// exportSixteenBitSamples 导出一张 16 位 PNG，返回第一行各像素的 16 位红通道样本。
func exportSixteenBitSamples(t *testing.T, adj *domain.Adjustment) [4]uint32 {
	t.Helper()
	svc, _ := newTestService(t, 4, 1)
	deepLayerForExport(t, svc)
	if adj != nil {
		sess := activeSessionOf(t, svc)
		opacity := 1.0
		sess.doc.Layers = append(sess.doc.Layers, domain.Layer{
			ID: "adj", Name: "Levels", IsVisible: true, Opacity: &opacity, Adjustment: adj,
		})
	}
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "adj"))
	reply, err := svc.ExportAs(`{"format":"png","sixteenBit":true,"defaultName":"adj.png"}`)
	if err != nil {
		t.Fatalf("16 位导出: %v", err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("导出的 PNG 应可重新打开核验: %v", err)
	}
	var out [4]uint32
	for x := 0; x < 4; x++ {
		r, _, _, _ := img.At(x, 0).RGBA()
		out[x] = r
	}
	return out
}

// 票 71：调整图层现在在**深层**上作用（`CompositeDeep` → `deepAdjust`），所以 16 位导出不再因为
// 「有调整层」被拒 —— 而且色阶要真的改到像素，不是被跳过。
func TestExportAsSixteenBitAppliesAnAdjustmentLayer(t *testing.T) {
	base := exportSixteenBitSamples(t, nil)

	dark := domain.LevelsSettings{Ranges: [4]domain.LevelRange{
		{Black: 51, Gamma: 1, White: 204, OutputBlack: 0, OutputWhite: 255},
		{Black: 51, Gamma: 1, White: 204, OutputBlack: 0, OutputWhite: 255},
		{Black: 51, Gamma: 1, White: 204, OutputBlack: 0, OutputWhite: 255},
		domain.IdentityLevelRange(),
	}}
	adjusted := exportSixteenBitSamples(t, &domain.Adjustment{Kind: domain.AdjustmentLevels, Levels: dark})

	for x := 0; x < 4; x++ {
		if adjusted[x] >= base[x] {
			t.Fatalf("像素 %d：色阶调整层应当压暗，得到 %d → %d", x, base[x], adjusted[x])
		}
	}
}

func TestExportAsSixteenBitRefusesOtherFormatsAndResizes(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	deepLayerForExport(t, svc)
	if _, err := svc.ExportAs(`{"format":"jpeg","sixteenBit":true}`); err == nil || !strings.Contains(err.Error(), "PNG") {
		t.Fatalf("JPEG 的 16 位应被拒绝，得到 %v", err)
	}
	if _, err := svc.ExportAs(`{"format":"png","sixteenBit":true,"width":2,"height":1}`); err == nil || !strings.Contains(err.Error(), "尺寸") {
		t.Fatalf("16 位改尺寸应被拒绝，得到 %v", err)
	}
}

func TestExportAsSixteenBitNeedsADeepRaster(t *testing.T) {
	// 普通 8 位文档：没有深层栅格，不能声称导出 16 位。
	svc, _ := newTestService(t, 4, 1)
	if _, err := svc.ExportAs(`{"format":"png","sixteenBit":true}`); err == nil || !strings.Contains(err.Error(), "16 位栅格") {
		t.Fatalf("没有深层栅格时应被拒绝，得到 %v", err)
	}
}
