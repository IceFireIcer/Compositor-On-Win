package bridge

// 票 85：HDR 的 EXR 导出。

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/hdrio"
	"compositor-win/internal/render"
)

// hdrLayerForExport 把活动图层换成一份带超白值的 HDR 深层栅格，并把变换摆成恒等 ——
// 也就是闸门允许的那一种情形。
func hdrLayerForExport(t *testing.T, svc *Service) {
	t.Helper()
	sess := activeSessionOf(t, svc)
	l := &sess.doc.Layers[0]
	l.Origin = [2]float64{0, 0}
	l.Size = [2]float64{float64(sess.doc.Width), float64(sess.doc.Height)}
	l.Rotation, l.FlipX, l.FlipY = 0, false, false
	deep := render.NewDeep(sess.doc.Width, sess.doc.Height, true)
	for x := 0; x < deep.W; x++ {
		i := x * 4
		// 左半 0.5（白内），右半 4.0（超白）。
		v := float32(0.5)
		if x >= deep.W/2 {
			v = 4
		}
		deep.Pix[i] = render.FloatToHalf(v)
		deep.Pix[i+1] = render.FloatToHalf(v)
		deep.Pix[i+2] = render.FloatToHalf(v)
		deep.Pix[i+3] = render.FloatToHalf(1)
	}
	sess.bitmaps[*l.ImageFile].Deep = deep
}

// 超白值必须原样出去：解码回来比一半还亮，才说明没有被截断到白。
func TestExportAsEXRKeepsLightBrighterThanWhite(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	hdrLayerForExport(t, svc)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "hdr"))
	reply, err := svc.ExportAs(`{"format":"exr","defaultName":"hdr.exr"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	back, err := hdrio.DecodeEXR(data)
	if err != nil {
		t.Fatalf("导出的 EXR 应可重新打开核验: %v", err)
	}
	if !back.HDR {
		t.Fatal("导出的 EXR 应当仍是 HDR")
	}
	peak := back.Peak()
	if peak <= 1.001 {
		t.Fatalf("超白高光被截断了：峰值 %v", peak)
	}
	if math.Abs(float64(peak)-4) > 0.01 {
		t.Fatalf("峰值 = %v，想要约 4", peak)
	}
}

// 没有 HDR 栅格的文档仍然能导出 EXR：退回 8 位拼合结果的 half 展开（上游 hdrRaster ?? raster）。
func TestExportAsEXRFallsBackToTheFlattenedRaster(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "plain"))
	reply, err := svc.ExportAs(`{"format":"exr","defaultName":"plain.exr"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	back, err := hdrio.DecodeEXR(data)
	if err != nil {
		t.Fatalf("导出的 EXR 应可重新打开核验: %v", err)
	}
	if back.W != 4 || back.H != 1 {
		t.Fatalf("尺寸 = %d×%d", back.W, back.H)
	}
}

// HDR 的 EXR 不接受改尺寸：重采样只有 8 位实现，会把超白值压掉。
func TestExportAsEXRRefusesResizingAnHDRRaster(t *testing.T) {
	svc, _ := newTestService(t, 4, 1)
	hdrLayerForExport(t, svc)
	if _, err := svc.ExportAs(`{"format":"exr","width":2,"height":1}`); err == nil || !strings.Contains(err.Error(), "尺寸") {
		t.Fatalf("HDR 的 EXR 改尺寸应被拒绝，得到 %v", err)
	}
}
