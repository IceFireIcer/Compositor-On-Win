package render

// 内容感知填充的蒙版极性（真机验收时用来定位"填错了半边"的判据）：
// mask != 0 的像素是**要填的目标**，未标记且不透明的像素是**取样源**。
// 这条约定与 tests/golden/c/ContentFill.c 的 content_fill 一致。
import "testing"

func TestContentFillTargetsMarkedPixels(t *testing.T) {
	const n = 64
	b := NewBitmap(n, n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := (y*n + x) * 4
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 10, 200, 10, 255 // 绿底
		}
	}
	for y := 24; y < 40; y++ {
		for x := 24; x < 40; x++ {
			i := (y*n + x) * 4
			b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 230, 20, 20 // 中央红斑 = 选区
		}
	}
	mask := make([]uint8, n*n)
	for y := 24; y < 40; y++ {
		for x := 24; x < 40; x++ {
			mask[y*n+x] = 255
		}
	}
	ContentFill(b, mask)
	at := func(x, y int) (uint8, uint8, uint8) { i := (y*n + x) * 4; return b.Pix[i], b.Pix[i+1], b.Pix[i+2] }
	cr, cg, cb := at(32, 32)
	or_, og, ob := at(2, 2)
	if cr != 10 || cg != 200 || cb != 10 {
		t.Errorf("被标记的中央没有被填（仍是 %d,%d,%d）—— 内核填反了半边", cr, cg, cb)
	}
	if or_ != 10 || og != 200 || ob != 10 {
		t.Errorf("未标记的角落被改了（%d,%d,%d）—— 内核填到了蒙版外面", or_, og, ob)
	}
}
