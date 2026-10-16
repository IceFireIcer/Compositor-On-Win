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

func TestExportAsSixteenBitRefusesWhatItCannotKeep(t *testing.T) {
	// 多图层：拼合要在 16 位下做，我们没有那条路 —— 必须明确拒绝，而不是静默给 8 位。
	svc, _ := newTestService(t, 4, 1)
	deepLayerForExport(t, svc)
	sess := activeSessionOf(t, svc)
	sess.doc.Layers = append(sess.doc.Layers, sess.doc.Layers[0])
	if _, err := svc.ExportAs(`{"format":"png","sixteenBit":true}`); err == nil || !strings.Contains(err.Error(), "单图层") {
		t.Fatalf("多图层应被拒绝并说明原因，得到 %v", err)
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
