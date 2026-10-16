package render

import "testing"

// ApplyInvert 是 PixelInvert.swift 的 vImage 那一趟：预乘域里每个颜色变成 **alpha − color**
// （4×4 矩阵 −256/256、除数 256，按 pixel × matrix 施加），alpha 本身不动。
//
// 这条测试的关键在**部分透明**的像素上：全不透明时 A − C 与 255 − C 完全同值，分辨不出实现对不对
// （桥接侧那条既有断言 `filter_test.go:908` 用的就是全不透明像素，只覆盖了退化情形）。
func TestApplyInvertKeepsAlphaAndInvertsAgainstIt(t *testing.T) {
	b := NewBitmap(3, 1)
	// 像素 0：部分透明（A=128、R=100）→ R' = 128 − 100 = 28。
	// 若写成 255 − 100 = 155，说明把「反相」当成了不透明域的操作。
	b.Pix[0], b.Pix[1], b.Pix[2], b.Pix[3] = 100, 20, 0, 128
	// 像素 1：全不透明（A=255、R=100）→ 155，即通常说的 255 − v。
	b.Pix[4], b.Pix[5], b.Pix[6], b.Pix[7] = 100, 100, 100, 255
	// 像素 2：全透明（A=0）→ 颜色只能是 0。
	b.Pix[8], b.Pix[9], b.Pix[10], b.Pix[11] = 0, 0, 0, 0

	ApplyInvert(b)

	want := []uint8{28, 108, 128, 128, 155, 155, 155, 255, 0, 0, 0, 0}
	for i := range want {
		if b.Pix[i] != want[i] {
			t.Fatalf("像素字节 %d = %d，想要 %d（整块 %v）", i, b.Pix[i], want[i], b.Pix)
		}
	}
}

// 预乘数据合法时结果必然在范围内；这条钉住那道低位钳制（vImage 输出也会钳）。
// C > A 属于非法预乘，钳到 0 而不是回绕成一个大数。
func TestApplyInvertClampsAnOutOfRangeChannel(t *testing.T) {
	b := NewBitmap(1, 1)
	b.Pix[0], b.Pix[1], b.Pix[2], b.Pix[3] = 200, 50, 10, 32
	ApplyInvert(b)
	if b.Pix[0] != 0 {
		t.Fatalf("C > A 时应当钳到 0，得到 %d", b.Pix[0])
	}
	if b.Pix[1] != 0 || b.Pix[2] != 22 || b.Pix[3] != 32 {
		t.Fatalf("预乘反相结果 = %v，想要 [0 0 22 32]", b.Pix)
	}
}
