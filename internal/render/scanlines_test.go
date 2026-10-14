package render

// Scanlines tests — the Go mirror of CompositorTests/ScanlinesTests.swift.
// The pixel acceptance (docs/testing.md §2: byte-for-byte against the C
// reference PNGs, ε declared in the case JSON) lives in the golden harness,
// tests/golden/golden_test.go's goPort, together with every other kernel.

import "testing"

// scanlinesFlat builds an opaque flat-gray raster (premultiplied: gray on
// gray is its own premultiplied value).
func scanlinesFlat(w, h int, gray uint8) *Bitmap {
	b := NewBitmap(w, h)
	for i := 0; i < w*h; i++ {
		b.Pix[i*4] = gray
		b.Pix[i*4+1] = gray
		b.Pix[i*4+2] = gray
		b.Pix[i*4+3] = 255
	}
	return b
}

// scanlinesColumn reads one column's red channel, top row first.
func scanlinesColumn(b *Bitmap, x int) []int {
	out := make([]int, b.H)
	for y := 0; y < b.H; y++ {
		out[y] = int(b.Pix[(y*b.W+x)*4])
	}
	return out
}

func scanlinesSum(b *Bitmap) int {
	total := 0
	for i := 0; i < len(b.Pix); i += 4 {
		total += int(b.Pix[i])
	}
	return total
}

// scanlinesColumnOf renders a flat gray with the given settings and returns
// one column (ScanlinesTests.scanlines).
func scanlinesColumnOf(gray uint8, adjust func(*ScanlinesSettings)) []int {
	b := scanlinesFlat(16, 32, gray)
	s := DefaultScanlinesSettings()
	adjust(&s)
	ApplyScanlinesFilter(b, s)
	return scanlinesColumn(b, 5)
}

// Scanlines is a CRT: lines of light on a dark screen, every Line Spacing
// pixels, thicker and brighter where the picture is light.
// (ScanlinesTests.scanlinesAreLinesOfLightThatBloomWithBrightness)
func TestScanlinesAreLinesOfLightThatBloomWithBrightness(t *testing.T) {
	white := scanlinesColumnOf(255, func(s *ScanlinesSettings) { s.LineSpacing = 8 })
	gray := scanlinesColumnOf(89, func(s *ScanlinesSettings) { s.LineSpacing = 8 }) // 0.35 × 255
	black := scanlinesColumnOf(0, func(s *ScanlinesSettings) { s.LineSpacing = 8 })
	for band := 0; band < 4; band++ {
		rows := white[band*8 : band*8+8]
		if rows[3] != 255 || rows[4] != 255 {
			t.Errorf("一条白线应在中段点亮：%v", rows)
		}
		dark := false
		for _, v := range rows {
			if v < 40 {
				dark = true
			}
		}
		if !dark {
			t.Errorf("线之间应留暗屏：%v", rows)
		}
	}
	sum := func(v []int) int {
		total := 0
		for _, x := range v {
			total += x
		}
		return total
	}
	if sum(gray)*2 >= sum(white) {
		t.Errorf("灰线的总亮度应低于白线的一半：灰 %d，白 %d", sum(gray), sum(white))
	}
	for y, v := range black {
		if v != 0 {
			t.Fatalf("纯黑画面在黑位 0 时不应有线：第 %d 行 = %d", y, v)
		}
	}
}

// On solid white, glow lights the gaps without filling them up to the lines;
// on solid black, Black Level lights them.
// (ScanlinesTests.linesShowOnSolidWhiteAndBlack)
func TestScanlinesLinesShowOnSolidWhiteAndBlack(t *testing.T) {
	white := scanlinesColumnOf(255, func(s *ScanlinesSettings) { s.LineSpacing = 8; s.Glow = 100 })
	lo, hi := white[0], white[0]
	for _, v := range white {
		lo, hi = min(lo, v), max(hi, v)
	}
	if hi-lo <= 40 {
		t.Errorf("全辉光下白屏仍应有线与缝：%v", white)
	}
	black := scanlinesColumnOf(0, func(s *ScanlinesSettings) { s.LineSpacing = 8; s.BlackLevel = 40 })
	first := black[:8]
	lo, hi = first[0], first[0]
	for _, v := range first {
		lo, hi = min(lo, v), max(hi, v)
	}
	if !(hi > 60 && lo < 10) {
		t.Errorf("黑位 40 下纯黑屏应有线与缝：%v", first)
	}
}

// Glow lights the dark screen between the lines.
// (ScanlinesTests.glowLightsBetweenTheLines)
func TestScanlinesGlowLightsBetweenTheLines(t *testing.T) {
	plain := scanlinesColumnOf(255, func(s *ScanlinesSettings) { s.LineSpacing = 8 })
	glowing := scanlinesColumnOf(255, func(s *ScanlinesSettings) { s.LineSpacing = 8; s.Glow = 100 })
	if glowing[0] <= plain[0]+40 {
		t.Errorf("线之间的缝应被辉光点亮：无辉光 %d，有辉光 %d", plain[0], glowing[0])
	}
}

// Dots break the lines into beads where the picture is darker than the Dots
// level: along a gray line's middle, dark gaps come every line spacing, while
// a white line above the level stays solid.
// (ScanlinesTests.dotsBeadTheLinesBelowTheirLevel)
func TestScanlinesDotsBeadTheLinesBelowTheirLevel(t *testing.T) {
	middle := func(gray uint8, adjust func(*ScanlinesSettings)) []int {
		b := scanlinesFlat(64, 16, gray)
		s := DefaultScanlinesSettings()
		adjust(&s)
		ApplyScanlinesFilter(b, s)
		out := make([]int, 64)
		for x := 0; x < 64; x++ {
			out[x] = int(b.Pix[(4*b.W+x)*4])
		}
		return out
	}
	white := middle(255, func(s *ScanlinesSettings) { s.LineSpacing = 8; s.Dots = 70 })
	for x, v := range white {
		if v != 255 {
			t.Fatalf("白线应保持实线：第 %d 列 = %d", x, v)
		}
	}
	gray := middle(102, func(s *ScanlinesSettings) { s.LineSpacing = 8; s.Dots = 70 }) // 0.4 × 255
	if !(gray[3] > 100 && gray[4] > 100 && gray[0] < 40 && gray[8] < 40) {
		t.Errorf("每 8 像素应断成一颗珠：%v", gray)
	}
}

// Wobble pushes lines sideways by different amounts: a vertical edge no
// longer lines up from line to line.
// (ScanlinesTests.wobbleMovesLinesSideways)
func TestScanlinesWobbleMovesLinesSideways(t *testing.T) {
	edges := func(wobble float64) map[int]bool {
		b := NewBitmap(64, 64)
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				i := (y*64 + x) * 4
				if x >= 32 {
					b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 255, 255, 255
				}
				b.Pix[i+3] = 255
			}
		}
		s := DefaultScanlinesSettings()
		s.LineSpacing = 8
		s.Wobble = wobble
		ApplyScanlinesFilter(b, s)
		set := map[int]bool{}
		for line := 0; line < 8; line++ {
			first := -1
			for x := 0; x < 64; x++ {
				if b.Pix[((line*8+4)*64+x)*4] > 128 {
					first = x
					break
				}
			}
			set[first] = true
		}
		return set
	}
	still := edges(0)
	if len(still) != 1 || !still[32] {
		t.Errorf("不摇摆时各行的竖直边缘应重合在 32：%v", still)
	}
	if len(edges(12)) < 3 {
		t.Errorf("摇摆 12 像素时各行边缘应分散：%v", edges(12))
	}
}

// Displace lifts a line where the picture is bright: over a white half, the
// line's middle sits higher than over the black half, where it stays put.
// (ScanlinesTests.displaceLiftsLinesWhereThePictureIsBright)
func TestScanlinesDisplaceLiftsLinesWhereThePictureIsBright(t *testing.T) {
	b := NewBitmap(64, 64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			i := (y*64 + x) * 4
			if x >= 32 {
				b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 255, 255, 255
			}
			b.Pix[i+3] = 255
		}
	}
	s := DefaultScanlinesSettings()
	s.LineSpacing = 16
	s.Displace = 6
	s.Colors = DitherColorsOriginal
	ApplyScanlinesFilter(b, s)
	brightest := func(column int) int {
		best, row := 0, 0
		for y := 16; y < 32; y++ {
			if v := int(b.Pix[(y*b.W+column)*4]); v > best {
				best, row = v, y
			}
		}
		return row
	}
	if got := brightest(48); got >= 24 {
		t.Errorf("白处的线应抬到它自己中点（24）之上，实际 %d", got)
	}
}

// Displace: a line lifted over a bright band hides the line behind it, which
// still shows where nothing is in front.
// (ScanlinesTests.aLiftedLineHidesTheLinesBehindIt)
func TestScanlinesALiftedLineHidesTheLinesBehindIt(t *testing.T) {
	b := NewBitmap(64, 80)
	for y := 0; y < 80; y++ {
		for x := 0; x < 64; x++ {
			i := (y*64 + x) * 4
			v := uint8(64) // 0.25 × 255
			if x >= 16 && x < 48 && y >= 32 && y < 48 {
				v = 255
			}
			b.Pix[i], b.Pix[i+1], b.Pix[i+2] = v, v, v
			b.Pix[i+3] = 255
		}
	}
	s := DefaultScanlinesSettings()
	s.LineSpacing = 16
	s.Displace = 40
	s.Smoothness = 0
	ApplyScanlinesFilter(b, s)
	if v := b.Pix[(14*b.W+4)*4]; v <= 20 {
		t.Errorf("前面没有东西挡住时第二条线应露出：%d", v)
	}
	if v := b.Pix[(14*b.W+32)*4]; v >= 8 {
		t.Errorf("抬起的线应挡住它后面的线：%d", v)
	}
	if v := b.Pix[(2*b.W+32)*4]; v <= 200 {
		t.Errorf("抬起的线本身应被画出：%d", v)
	}
}

// Threshold leaves the screen dark where the picture is darker than it.
// (ScanlinesTests.thresholdDarkensWhatsBelowIt)
func TestScanlinesThresholdDarkensWhatsBelowIt(t *testing.T) {
	lit := func(threshold float64) bool {
		b := scanlinesFlat(32, 32, 77) // 0.3 × 255
		s := DefaultScanlinesSettings()
		s.LineSpacing = 8
		s.Threshold = threshold
		ApplyScanlinesFilter(b, s)
		for y := 0; y < 32; y++ {
			if b.Pix[(y*b.W+16)*4] > 10 {
				return true
			}
		}
		return false
	}
	if !lit(0) {
		t.Error("阈值 0 时暗灰应画线")
	}
	if lit(60) {
		t.Error("阈值 60 时 0.3 的画面应全暗")
	}
}

// 参数单调性：厚度、间距、阈值与黑位都应朝它们字面的方向改变画面。
func TestScanlinesParametersMoveThePictureOneWay(t *testing.T) {
	render := func(adjust func(*ScanlinesSettings)) *Bitmap {
		b := scanlinesFlat(32, 32, 153) // 0.6 × 255
		s := DefaultScanlinesSettings()
		s.LineSpacing = 8
		adjust(&s)
		ApplyScanlinesFilter(b, s)
		return b
	}
	lit := func(b *Bitmap) int {
		n := 0
		for i := 0; i < len(b.Pix); i += 4 {
			if b.Pix[i] > 0 {
				n++
			}
		}
		return n
	}
	thin := render(func(s *ScanlinesSettings) { s.Thickness = 10 })
	thick := render(func(s *ScanlinesSettings) { s.Thickness = 100 })
	if lit(thick) <= lit(thin) {
		t.Errorf("厚度越大线越宽：10 → %d，100 → %d", lit(thin), lit(thick))
	}
	dense := render(func(s *ScanlinesSettings) { s.Density = 50 })
	if scanlinesSum(dense) >= scanlinesSum(render(func(*ScanlinesSettings) {})) {
		t.Error("正密度应压暗画面，线更细更暗")
	}
	punchy := render(func(s *ScanlinesSettings) { s.Contrast = 50 })
	if scanlinesSum(punchy) <= scanlinesSum(render(func(*ScanlinesSettings) {})) {
		t.Error("正对比应以中灰为轴把 0.6 推亮")
	}
	litLow := render(func(s *ScanlinesSettings) { s.Threshold = 0 })
	litHigh := render(func(s *ScanlinesSettings) { s.Threshold = 70 })
	if lit(litHigh) >= lit(litLow) {
		t.Errorf("阈值越高亮的像素越少：0 → %d，70 → %d", lit(litLow), lit(litHigh))
	}
	// 间距：行距决定亮带的疏密——4 像素的行距在同样高度里排出的亮带更多。
	bands := func(spacing float64) int {
		b := render(func(s *ScanlinesSettings) { s.LineSpacing = spacing })
		count, lit := 0, false
		for y := 0; y < b.H; y++ {
			on := b.Pix[(y*b.W+b.W/2)*4] > 128
			if on && !lit {
				count++
			}
			lit = on
		}
		return count
	}
	if bands(4) <= bands(16) {
		t.Errorf("行距越小亮带越多：4 → %d，16 → %d", bands(4), bands(16))
	}
	// 黑位：画面全黑时，黑位越高线越亮。
	blackLevel := func(level float64) int {
		b := scanlinesFlat(32, 32, 0)
		s := DefaultScanlinesSettings()
		s.LineSpacing = 8
		s.BlackLevel = level
		ApplyScanlinesFilter(b, s)
		return scanlinesSum(b)
	}
	if blackLevel(80) <= blackLevel(20) {
		t.Errorf("黑位越高纯黑屏上的线越亮：20 → %d，80 → %d", blackLevel(20), blackLevel(80))
	}
}

// 颜色模式：双色调用 dark/light 画线，原始色保留画面自己的颜色。
func TestScanlinesColorModes(t *testing.T) {
	two := scanlinesFlat(32, 32, 255)
	s := DefaultScanlinesSettings()
	s.LineSpacing = 8
	s.Thickness = 50
	s.Colors = DitherColorsTwo
	s.Dark = [3]float64{0, 0, 1}
	s.Light = [3]float64{1, 0, 0}
	ApplyScanlinesFilter(two, s)
	lit := (4*two.W + 10) * 4
	if two.Pix[lit] != 255 || two.Pix[lit+1] != 0 || two.Pix[lit+2] != 0 {
		t.Errorf("双色调的线应取 light：%d,%d,%d", two.Pix[lit], two.Pix[lit+1], two.Pix[lit+2])
	}
	gap := (0*two.W + 10) * 4
	if two.Pix[gap+2] != 255 {
		t.Errorf("双色调的屏应取 dark：%d,%d,%d", two.Pix[gap], two.Pix[gap+1], two.Pix[gap+2])
	}

	red := NewBitmap(32, 32)
	for i := 0; i < 32*32; i++ {
		red.Pix[i*4], red.Pix[i*4+1], red.Pix[i*4+2], red.Pix[i*4+3] = 255, 0, 0, 255
	}
	o := DefaultScanlinesSettings()
	o.LineSpacing = 8
	o.Thickness = 100
	o.Colors = DitherColorsOriginal
	ApplyScanlinesFilter(red, o)
	at := (4*red.W + 10) * 4
	if red.Pix[at] != 255 || red.Pix[at+1] != 0 || red.Pix[at+2] != 0 {
		t.Errorf("原始色模式的线应保留画面的红：%d,%d,%d", red.Pix[at], red.Pix[at+1], red.Pix[at+2])
	}
}

// 色彩分离：红往一边、蓝往另一边，线的边缘因此错开。
func TestScanlinesColorSplitMovesRedAndBlueApart(t *testing.T) {
	b := NewBitmap(64, 64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			i := (y*64 + x) * 4
			if x >= 32 {
				b.Pix[i], b.Pix[i+1], b.Pix[i+2] = 255, 255, 255
			}
			b.Pix[i+3] = 255
		}
	}
	s := DefaultScanlinesSettings()
	s.LineSpacing = 8
	s.Thickness = 100
	s.Split = 3
	ApplyScanlinesFilter(b, s)
	row := 4 * b.W * 4 // 第一条线的中段
	first := func(channel int) int {
		for x := 0; x < 64; x++ {
			if b.Pix[row+x*4+channel] > 128 {
				return x
			}
		}
		return -1
	}
	if got := first(0); got != 35 {
		t.Errorf("红通道的边缘应右移 3 像素到 35，实际 %d", got)
	}
	if got := first(2); got != 29 {
		t.Errorf("蓝通道的边缘应左移 3 像素到 29，实际 %d", got)
	}
}

// 透明边界：alpha 为 0 的像素原样保留，部分透明的像素保留自己的 alpha。
func TestScanlinesLeaveTransparentPixelsAlone(t *testing.T) {
	b := NewBitmap(32, 32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			if x < 16 {
				continue // 全透明，保持全零
			}
			b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = 128, 128, 128, 128
		}
	}
	s := DefaultScanlinesSettings()
	s.LineSpacing = 8
	s.BlackLevel = 100
	ApplyScanlinesFilter(b, s)
	for y := 0; y < 32; y++ {
		for x := 0; x < 16; x++ {
			i := (y*32 + x) * 4
			if b.Pix[i] != 0 || b.Pix[i+1] != 0 || b.Pix[i+2] != 0 || b.Pix[i+3] != 0 {
				t.Fatalf("全透明像素 (%d,%d) 不应被改动：%v", x, y, b.Pix[i:i+4])
			}
		}
		for x := 16; x < 32; x++ {
			if a := b.Pix[(y*32+x)*4+3]; a != 128 {
				t.Fatalf("半透明像素 (%d,%d) 的 alpha 应保持 128：%d", x, y, a)
			}
		}
	}
}

// 纯黑与纯白边界：黑位 0 时纯黑屏全暗，纯白屏的线打满、缝留黑；间距被夹到至少 2。
func TestScanlinesBlackWhiteAndSpacingBounds(t *testing.T) {
	white := scanlinesFlat(32, 32, 255)
	s := DefaultScanlinesSettings()
	s.LineSpacing = 8
	ApplyScanlinesFilter(white, s)
	lo, hi := 255, 0
	for i := 0; i < len(white.Pix); i += 4 {
		lo, hi = min(lo, int(white.Pix[i])), max(hi, int(white.Pix[i]))
	}
	if hi != 255 || lo != 0 {
		t.Errorf("纯白屏应有打满的线与全黑的缝：min %d，max %d", lo, hi)
	}
	// 间距 0/1 都夹到 2：不会除零，输出与显式间距 2 完全一致。
	clamped := scanlinesFlat(16, 16, 255)
	if !ScanlinesApply(clamped, &ScanlinesParams{Spacing: 1, Thickness: 0.7, Light: [3]uint8{255, 255, 255}}) {
		t.Fatal("间距 1 应被夹到 2 并正常返回")
	}
	twoRows := scanlinesFlat(16, 16, 255)
	ScanlinesApply(twoRows, &ScanlinesParams{Spacing: 2, Thickness: 0.7, Light: [3]uint8{255, 255, 255}})
	for i := range twoRows.Pix {
		if clamped.Pix[i] != twoRows.Pix[i] {
			t.Fatalf("间距 1 应被夹到 2：字节 %d 为 %d，间距 2 是 %d", i, clamped.Pix[i], twoRows.Pix[i])
		}
	}
	if empty := NewBitmap(0, 0); !ScanlinesApply(empty, &ScanlinesParams{Spacing: 4}) {
		t.Error("空位图应直接返回成功")
	}
}

// 默认值与钳位对齐 Scanlines.swift。
func TestScanlinesDefaultSettingsAndClamping(t *testing.T) {
	d := DefaultScanlinesSettings()
	if d.LineSpacing != 4 || d.Thickness != 70 || d.Glow != 0 || d.Dots != 0 || d.Wobble != 0 ||
		d.Displace != 0 || d.Smoothness != 50 || d.Threshold != 0 || d.Split != 0 ||
		d.Density != 0 || d.Contrast != 0 || d.BlackLevel != 0 || d.Colors != DitherColorsBlackWhite {
		t.Errorf("默认值应与原版一致：%+v", d)
	}
	if d.Dark != [3]float64{0, 0, 0} || d.Light != [3]float64{1, 1, 1} {
		t.Errorf("默认颜色应为纯黑/纯白：%v %v", d.Dark, d.Light)
	}
	n := ScanlinesSettings{
		LineSpacing: 100, Thickness: 200, Glow: -5, Dots: 200, Wobble: 100, Displace: -400,
		Smoothness: 200, Threshold: -1, Split: 40, Density: -400, Contrast: 400,
		BlackLevel: 400, Colors: DitherColorsTwo, Dark: [3]float64{-1, 0.5, 2}, Light: [3]float64{2, 0.5, -1},
	}.Normalized()
	if n.LineSpacing != 32 || n.Thickness != 100 || n.Glow != 0 || n.Dots != 100 || n.Wobble != 64 ||
		n.Displace != -100 || n.Smoothness != 100 || n.Threshold != 0 || n.Split != 16 ||
		n.Density != -100 || n.Contrast != 100 || n.BlackLevel != 100 {
		t.Errorf("钳位应落在原版区间：%+v", n)
	}
	if n.Dark != [3]float64{0, 0.5, 1} || n.Light != [3]float64{1, 0.5, 0} {
		t.Errorf("颜色应夹到 0–1：%v %v", n.Dark, n.Light)
	}
	// 行距四舍五入到整数像素；非有限值回落到默认。
	round := ScanlinesSettings{LineSpacing: 7.6, Split: 2.5}.Normalized()
	if round.LineSpacing != 8 || round.Split != 3 {
		t.Errorf("行距与色彩分离应四舍五入：%v %v", round.LineSpacing, round.Split)
	}
	nan := ScanlinesSettings{LineSpacing: nan(), Thickness: nan(), Smoothness: nan()}.Normalized()
	if nan.LineSpacing != 4 || nan.Thickness != 70 || nan.Smoothness != 50 {
		t.Errorf("非有限值应回落到默认：%+v", nan)
	}
}

func nan() float64 {
	var zero float64
	return zero / zero
}
