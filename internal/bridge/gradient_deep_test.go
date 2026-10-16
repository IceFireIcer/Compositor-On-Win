package bridge

// 票 82 第 3 条：16 位图层上画渐变要把改过的像素折进深层栅格，而不是丢掉整层精度
// （丢掉就等于压回 8 位）；撤销要恢复原来的深层与像素。
// 填充那一半已由 deep_stale_test.go 的 TestFillReconcilesTheDeepRaster 覆盖。

import "testing"

func TestGradientOnA16BitLayerKeepsAndRestoresTheDeepRaster(t *testing.T) {
	svc, tab := newTestService(t, 40, 20)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 40, 20, 255)
	key := attachDeep(t, sess)
	originalDeep := sess.bitmaps[key].Deep.Clone()

	sel := solidSelPayload(10, 0, 20, 20, 255)
	settings := `{"shape":"Linear","style":"foregroundToBackground","foreground":{"r":0,"g":0,"b":0,"a":1},"background":{"r":1,"g":1,"b":1,"a":1},"opacity":1}`
	if _, err := svc.DrawGradient(tab, 10.5, 10, 30.5, 10, settings, sel); err != nil {
		t.Fatalf("画渐变: %v", err)
	}

	after := sess.bitmaps[key]
	if after.Deep == nil {
		t.Fatal("渐变后应把改过的像素折进深层栅格，而不是丢掉整层精度")
	}
	// 渐变中点（x=20）落在 128 灰上：深层要跟着变，不能还是渐变前的白。
	i := (10*40 + 20) * 4
	want := int(after.Pix[i]) * 257
	if diff := int(after.Deep.Pix[i]) - want; diff > 300 || diff < -300 {
		t.Fatalf("深层 %d 与 8 位副本 %d×257=%d 不一致", after.Deep.Pix[i], after.Pix[i], want)
	}
	if after.Deep.Pix[i] == originalDeep.Pix[i] {
		t.Fatal("深层应反映渐变，而不是渐变前的像素")
	}

	// 撤销：原深度与原像素都要回来。
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("撤销: %v", err)
	}
	back := sess.bitmaps[key]
	if back.Deep == nil {
		t.Fatal("撤销后深层不该消失")
	}
	for j := range originalDeep.Pix {
		if back.Deep.Pix[j] != originalDeep.Pix[j] {
			t.Fatalf("撤销后深层应逐字节回到原样：下标 %d %d ≠ %d", j, back.Deep.Pix[j], originalDeep.Pix[j])
		}
	}
	if back.Pix[i] != 255 {
		t.Fatalf("撤销后 8 位副本应回到原样，得到 %d", back.Pix[i])
	}
}
