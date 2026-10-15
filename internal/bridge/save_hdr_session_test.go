package bridge

// 评审发现的回归：alignAssetNames 改的是**活动**位图库，而它自己的注释写着
// 「The rename is on the snapshot only」。导入的 HDR 图层在会话里叫 <UUID>.png，
// 第一次保存把位图库的键改成 .exr，而 sess.doc 仍说 .png —— 图层从画布消失，
// 下一次编辑报「内存位图库缺少资产」。

import (
	"context"
	"path/filepath"
	"testing"

	"compositor-win/internal/render"
)

func hdrSessionForSave(t *testing.T) (*Service, *session, string) {
	t.Helper()
	svc, _ := newTestService(t, 8, 8)
	sess := activeSessionOf(t, svc)
	key := *sess.doc.Layers[0].ImageFile
	deep := render.NewDeep(8, 8, true)
	for i := 0; i < len(deep.Pix); i += 4 {
		deep.Pix[i] = render.FloatToHalf(2.5) // 高于白
		deep.Pix[i+1] = render.FloatToHalf(0.25)
		deep.Pix[i+2] = render.FloatToHalf(0.1)
		deep.Pix[i+3] = render.FloatToHalf(1)
	}
	bmp := deep.ToBitmap()
	bmp.Deep = deep
	sess.bitmaps[key] = bmp
	return svc, sess, key
}

func TestSavingAnHDRLayerLeavesTheLiveSessionAlone(t *testing.T) {
	svc, sess, key := hdrSessionForSave(t)
	dir := t.TempDir()
	svc.saveDialog = func(context.Context) (string, error) { return filepath.Join(dir, "hdr.comp"), nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}

	// 活动会话仍须叫得出自己那份位图。
	live := *sess.doc.Layers[0].ImageFile
	if sess.bitmaps[live] == nil {
		t.Fatalf("保存后活动会话缺少 %s：位图库被改了名（原来叫 %s）", live, key)
	}
	// 下一次编辑还得找得到它。
	if _, err := svc.FillSelection("", "foreground", `{"r":1,"g":0,"b":0,"a":1}`); err != nil {
		t.Fatalf("保存后编辑: %v", err)
	}
}

// 保存两次也要一致：第一次改名之后，第二次不能因为名字对不上而失败。
func TestSavingAnHDRLayerTwiceIsStable(t *testing.T) {
	svc, sess, _ := hdrSessionForSave(t)
	dir := t.TempDir()
	svc.saveDialog = func(context.Context) (string, error) { return filepath.Join(dir, "hdr.comp"), nil }
	for i := 1; i <= 2; i++ {
		if _, err := svc.SaveProjectDialog(); err != nil {
			t.Fatalf("第 %d 次保存: %v", i, err)
		}
	}
	if sess.bitmaps[*sess.doc.Layers[0].ImageFile] == nil {
		t.Fatal("两次保存后活动会话仍须拿得到位图")
	}
}
