package render

// V05：导出与图像尺寸的缩小采样改成 Lanczos（单趟、浮点、核宽随倍率），
// 替代原先的盒平均。

import (
	"math"
	"testing"
)

func solidRow(w int, v uint8) *Bitmap {
	b := NewBitmap(w, 1)
	for x := 0; x < w; x++ {
		i := x * 4
		b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
	}
	return b
}

// 权重归一化：常量图缩放后仍是同一个常量（边缘也不许塌）。
func TestLanczosKeepsAConstantImageConstant(t *testing.T) {
	for _, size := range [][2]int{{3, 1}, {7, 1}, {16, 1}, {40, 1}} {
		src := solidRow(12, 200)
		out := LanczosResample(src, size[0], size[1])
		if out.W != size[0] || out.H != size[1] {
			t.Fatalf("尺寸 = %dx%d，想要 %dx%d", out.W, out.H, size[0], size[1])
		}
		for x := 0; x < out.W; x++ {
			if v := int(out.Pix[x*4]); v < 199 || v > 201 {
				t.Fatalf("缩到 %d 后第 %d 个像素 = %d，想要 ~200", size[0], x, v)
			}
		}
	}
}

// 1:1 是副本，不共享底层数组（与 ResampleBitmap 的约定一致）。
func TestLanczosIdentityCopies(t *testing.T) {
	src := solidRow(6, 90)
	same := LanczosResample(src, 6, 1)
	if same.W != 6 || &same.Pix[0] == &src.Pix[0] {
		t.Fatal("1:1 应返回尺寸相同的副本，不共享底层数组")
	}
	for i := range src.Pix {
		if same.Pix[i] != src.Pix[i] {
			t.Fatalf("1:1 副本像素应一致：下标 %d", i)
		}
	}
}

// 陡边放大后，Lanczos 的过渡带应比双线性窄（这正是「锐利缩小采样」要的性质）。
func TestLanczosKeepsAStepEdgeSharperThanBilinear(t *testing.T) {
	src := NewBitmap(4, 1)
	for x := 0; x < 4; x++ {
		i := x * 4
		v := uint8(0)
		if x >= 2 {
			v = 255
		}
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = v, v, v, 255
	}
	bilinear := ResampleBitmap(src, 16, 1)
	lanczos := LanczosResample(src, 16, 1)
	// 过渡带里最陡的那一步：越陡说明边越锐。
	steepest := func(b *Bitmap) int {
		best := 0
		for x := 1; x < b.W; x++ {
			d := int(b.Pix[x*4]) - int(b.Pix[(x-1)*4])
			if d < 0 {
				d = -d
			}
			if d > best {
				best = d
			}
		}
		return best
	}
	if steepest(lanczos) <= steepest(bilinear) {
		t.Fatalf("Lanczos 的最陡一步 %d 应大于双线性的 %d", steepest(lanczos), steepest(bilinear))
	}
}

// 缩小要抗锯齿：1px 黑白条纹缩一半后不该留下原频率的痕迹，也不该整体偏亮/偏暗。
func TestLanczosDownscaleAntialiasesWithoutDrifting(t *testing.T) {
	src := NewBitmap(16, 1)
	for x := 0; x < 16; x++ {
		i := x * 4
		v := uint8(0)
		if x%2 == 0 {
			v = 255
		}
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = v, v, v, 255
	}
	out := LanczosResample(src, 8, 1)
	for x := 0; x < out.W; x++ {
		if v := int(out.Pix[x*4]); v < 100 || v > 155 {
			t.Fatalf("第 %d 个像素 = %d，想要接近中灰（抗锯齿后不该留原频率）", x, v)
		}
	}
}

// 缩小是**平均**不是求和：4×4 里的单个亮点缩到 2×2 后，总量应落在 255/4 附近
// （每个输出像素是它那块的平均），而不是把原能量原样带过来。
func TestLanczosDownscaleAveragesRatherThanSums(t *testing.T) {
	src := NewBitmap(4, 4)
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	src.Pix[(1*4+1)*4] = 255
	out := LanczosResample(src, 2, 2)
	total := 0
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			total += int(out.Pix[(y*2+x)*4])
		}
	}
	// 亮点的 255 被摊进它所在的那一块（4 个源像素），所以总量约 64。
	// 允许核的负瓣带来的偏差，但绝不该接近 255（那是求和不是平均）。
	if math.Abs(float64(total)-64) > 40 {
		t.Fatalf("亮点缩小后的总量 = %d，想要接近 255/4 ≈ 64", total)
	}
}
