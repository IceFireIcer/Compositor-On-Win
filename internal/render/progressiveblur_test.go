package render

// 票 73：渐进模糊的内核断言照抄上游 ProgressiveBlurTests —— 沿线上从 From 的模糊量
// 渐变到 To 的，径向则从起点向外。上游用 CIMaskedVariableBlur + 渐变遮罩；这里用
// 一列固定半径的高斯按遮罩插值近似（同一个语义，没有 Core Image）。

import "testing"

const progressiveSize = 200

// progressiveStripes is the upstream fixture: black and white vertical stripes
// two pixels wide, which swing 0–255 sharp and gray out when blurred.
func progressiveStripes() *Bitmap {
	b := NewBitmap(progressiveSize, progressiveSize)
	for y := 0; y < progressiveSize; y++ {
		for x := 0; x < progressiveSize; x++ {
			i := (y*progressiveSize + x) * 4
			v := uint8(255)
			if (x/2)%2 == 1 {
				v = 0
			}
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = v, v, v, 255
		}
	}
	return b
}

// progressiveContrast is how far the stripes still swing at (x, y): 0 is flat
// gray, 255 is as sharp as they started.
func progressiveContrast(b *Bitmap, x, y int) int {
	min, max := 255, 0
	for i := x; i < x+8; i++ {
		v := int(b.Pix[(y*b.W+i)*4])
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return max - min
}

func progressiveBlurred(t *testing.T, radial, inverted bool, from, to float64, start, end Point) *Bitmap {
	t.Helper()
	b := progressiveStripes()
	ApplyProgressiveBlur(b, ProgressiveBlurSettings{
		Start: start, End: end, From: from, To: to, Radial: radial, Inverted: inverted,
	})
	return b
}

func TestProgressiveBlurLinearIsSharpAtTheStartAndBlurredAtTheEnd(t *testing.T) {
	b := progressiveBlurred(t, false, false, 0, 6,
		Point{X: 100, Y: 20}, Point{X: 100, Y: 180})
	if got := progressiveContrast(b, 96, 10); got <= 230 {
		t.Fatalf("起点之前应保持清晰，对比度 = %d", got)
	}
	halfway := progressiveContrast(b, 96, 100)
	early := progressiveContrast(b, 96, 30)
	if halfway >= early {
		t.Fatalf("半程应比早段更糊：halfway=%d early=%d", halfway, early)
	}
	if got := progressiveContrast(b, 96, 190); got >= 40 {
		t.Fatalf("终点之后应糊掉，对比度 = %d", got)
	}
}

func TestProgressiveBlurFromAndToSetEachEnd(t *testing.T) {
	b := progressiveBlurred(t, false, false, 1.5, 6,
		Point{X: 100, Y: 20}, Point{X: 100, Y: 180})
	start := progressiveContrast(b, 96, 10)
	end := progressiveContrast(b, 96, 190)
	if start >= 230 {
		t.Fatalf("From 不为 0 时起点也该软化，对比度 = %d", start)
	}
	if start <= end {
		t.Fatalf("终点应比起点更糊：start=%d end=%d", start, end)
	}
}

func TestProgressiveBlurRadialIsSharpInTheMiddleAndBlurredOutside(t *testing.T) {
	center := Point{X: 100, Y: 100}
	edge := Point{X: 160, Y: 100}
	b := progressiveBlurred(t, true, false, 0, 6, center, edge)
	if got := progressiveContrast(b, 96, 100); got <= 230 {
		t.Fatalf("圆心应保持清晰，对比度 = %d", got)
	}
	if got := progressiveContrast(b, 4, 4); got >= 40 {
		t.Fatalf("圆外应糊掉，对比度 = %d", got)
	}

	inverted := progressiveBlurred(t, true, true, 0, 6, center, edge)
	if got := progressiveContrast(inverted, 96, 100); got >= 40 {
		t.Fatalf("反相后圆心应糊掉，对比度 = %d", got)
	}
	if got := progressiveContrast(inverted, 4, 4); got <= 230 {
		t.Fatalf("反相后圆外应清晰，对比度 = %d", got)
	}
}

func TestProgressiveBlurWithNoAmountChangesNothing(t *testing.T) {
	b := progressiveStripes()
	before := append([]uint8(nil), b.Pix...)
	ApplyProgressiveBlur(b, ProgressiveBlurSettings{
		Start: Point{X: 100, Y: 20}, End: Point{X: 100, Y: 180}, From: 0, To: 0,
	})
	for i := range before {
		if before[i] != b.Pix[i] {
			t.Fatal("From 与 To 都是 0 时不该动像素")
		}
	}
}
