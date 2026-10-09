package layerrender

// Ticket 45 tests: per-run text styles (manifest v10/v11 colorRuns/fontRuns).
// Runs address UTF-16 offsets into the content and carry only the letters that
// differ from the base style (TypeTool.color(at:)/fontName(at:)); layout and
// rasterization must honor each rune's effective face and color, and stay
// total for malformed runs.

import (
	"math"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"

	"golang.org/x/image/font/sfnt"
)

// TestTextImageColorRunsPaintPerLetter pins the per-letter color override: the
// base black ink keeps r=g=b=0 and the run's red ink keeps g=b=0 in the
// premultiplied output, so every inked pixel classifies exactly, and the run's
// letter sits to the right of the base letter.
func TestTextImageColorRunsPaintPerLetter(t *testing.T) {
	style := domain.TextStyle{
		Content:   "AB",
		FontName:  "Segoe UI",
		FontSize:  96,
		Alignment: domain.TextAlignmentLeft,
		ColorRuns: []domain.TextColorRun{{Location: 1, Length: 1, Red: 1}},
	}
	bmp, ok := TextImage(style)
	if !ok || bmp == nil {
		t.Fatal("带颜色 run 的文本应能光栅化")
	}
	blackLeft, redRight := 0, 0
	maxBlackX, minRedX := -1, bmp.W
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			r, g, b, a := bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3]
			if a == 0 {
				continue
			}
			switch {
			case r == 0 && g == 0 && b == 0:
				if x < bmp.W/2 {
					blackLeft++
				}
				if x > maxBlackX {
					maxBlackX = x
				}
			case r > 0 && g == 0 && b == 0:
				if x >= bmp.W/2 {
					redRight++
				}
				if x < minRedX {
					minRedX = x
				}
			default:
				t.Fatalf("墨迹既不是底色黑也不是 run 红 (%d,%d): %v", x, y, bmp.Pix[i:i+4])
			}
		}
	}
	if blackLeft == 0 {
		t.Error("左半区应有底色黑墨迹")
	}
	if redRight == 0 {
		t.Error("右半区应有 run 的红墨迹")
	}
	if maxBlackX >= minRedX {
		t.Errorf("A 的黑墨迹应在 B 的红墨迹左侧: maxBlackX=%d minRedX=%d", maxBlackX, minRedX)
	}
}

// TestTextImageRunsUseUTF16Offsets pins the offset unit: the emoji is one rune
// but two UTF-16 units, so a run at location 3 covers the trailing B. A
// rune-indexed implementation would leave B black (location 3 out of range).
func TestTextImageRunsUseUTF16Offsets(t *testing.T) {
	style := domain.TextStyle{
		Content:   "A\U0001F600B",
		FontName:  "Segoe UI",
		FontSize:  64,
		ColorRuns: []domain.TextColorRun{{Location: 3, Length: 1, Red: 1}},
	}
	bmp, ok := TextImage(style)
	if !ok || bmp == nil {
		t.Fatal("含代理对的文本应能光栅化")
	}
	redCount := 0
	maxBlackX, minRedX := -1, bmp.W
	for y := 0; y < bmp.H; y++ {
		for x := 0; x < bmp.W; x++ {
			i := (y*bmp.W + x) * 4
			r, g, b, a := bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3]
			if a == 0 {
				continue
			}
			if r == 0 && g == 0 && b == 0 {
				if x > maxBlackX {
					maxBlackX = x
				}
				continue
			}
			if r > 0 && g == 0 && b == 0 {
				redCount++
				if x < minRedX {
					minRedX = x
				}
				continue
			}
			t.Fatalf("墨迹既不是底色黑也不是 run 红 (%d,%d): %v", x, y, bmp.Pix[i:i+4])
		}
	}
	if redCount == 0 {
		t.Error("UTF-16 偏移 3 处的 B 应被 run 涂红")
	}
	if maxBlackX >= minRedX {
		t.Errorf("红 B 应在 A（及 emoji 字形）右侧: maxBlackX=%d minRedX=%d", maxBlackX, minRedX)
	}
}

// TestTextImageFontRunsUseRunAdvance pins per-character advances: "MM" with a
// font run on the first M measures to the run face's M plus the base face's M,
// which differs from two base M's (a run in Consolas is not Segoe UI wide).
func TestTextImageFontRunsUseRunAdvance(t *testing.T) {
	baseName, runName, baseFont, runFont, ok := pickDistinctFacePair()
	if !ok {
		t.Skip("本机没有两套可用的不同面孔")
	}
	const size = 100
	var buf sfnt.Buffer
	runAdv := glyphAdvance(runFont.Sfnt, &buf, 'M', size)
	baseAdv := glyphAdvance(baseFont.Sfnt, &buf, 'M', size)
	if runAdv == baseAdv {
		t.Skipf("%s 与 %s 的 M 宽度相同，无法分辨逐字符度量", baseName, runName)
	}
	style := domain.TextStyle{
		Content:  "MM",
		FontName: baseName,
		FontSize: size,
		FontRuns: []domain.TextFontRun{{Location: 0, Length: 1, FontName: runName}},
	}
	want := int(math.Max(16, math.Ceil(runAdv+baseAdv+2*TextPadding+size*0.1)))
	bmp, ok := TextImage(style)
	if !ok || bmp == nil {
		t.Fatal("混排文本应能光栅化")
	}
	if bmp.W != want {
		t.Errorf("混排宽度应为 run(%.2f)+base(%.2f) 推出的 %d，得到 %d", runAdv, baseAdv, want, bmp.W)
	}
	single := style
	single.FontRuns = nil
	singleBmp, ok := TextImage(single)
	if !ok || singleBmp == nil {
		t.Fatal("单一字体文本应能光栅化")
	}
	if singleBmp.W == bmp.W {
		t.Errorf("换用 run 面孔应改变量出的宽度（都是 %d）", bmp.W)
	}
}

// TestTextImageWrapsByRunFaceAdvances pins run-aware wrapping: a box wide
// enough for the single-face text but too narrow for the mixed text must be
// the style that wraps, whichever face is the wider one.
func TestTextImageWrapsByRunFaceAdvances(t *testing.T) {
	baseName, runName, baseFont, runFont, ok := pickDistinctFacePair()
	if !ok {
		t.Skip("本机没有两套可用的不同面孔")
	}
	const size = 100
	var buf sfnt.Buffer
	wBase := glyphAdvance(baseFont.Sfnt, &buf, 'M', size)
	wRun := glyphAdvance(runFont.Sfnt, &buf, 'M', size)
	wSpace := glyphAdvance(baseFont.Sfnt, &buf, ' ', size)
	singleWidth := 2*wBase + wSpace
	mixedWidth := wRun + wSpace + wBase
	if singleWidth == mixedWidth {
		t.Skipf("%s 与 %s 的混排宽度相同", baseName, runName)
	}
	wide, narrow := mixedWidth, singleWidth
	if wide < narrow {
		wide, narrow = narrow, wide
	}
	maxWidth := (wide + narrow) / 2
	if maxWidth < 1 || maxWidth+2*TextPadding > 30000 {
		t.Skipf("面孔度量不适合构造折行窗口: %.2f/%.2f", wide, narrow)
	}
	box := &[2]float64{maxWidth + 2*TextPadding, 400}
	mixed := domain.TextStyle{Content: "M M", FontName: baseName, FontSize: size, BoxSize: box,
		FontRuns: []domain.TextFontRun{{Location: 0, Length: 1, FontName: runName}}}
	single := mixed
	single.FontRuns = nil
	mixedBmp, ok := TextImage(mixed)
	if !ok || mixedBmp == nil {
		t.Fatal("混排文本应能在框内光栅化")
	}
	singleBmp, ok := TextImage(single)
	if !ok || singleBmp == nil {
		t.Fatal("单一字体文本应能在框内光栅化")
	}
	// Exactly one style exceeds the box by the run-aware measurement, and the
	// style that exceeds it is the one that has to wrap.
	mixedRows, singleRows := runTestInkRows(mixedBmp), runTestInkRows(singleBmp)
	if mixedWidth > maxWidth {
		if mixedRows <= singleRows {
			t.Errorf("混排宽度 %.2f 超过框宽 %.2f，应折成更多行: %d vs %d",
				mixedWidth, maxWidth, mixedRows, singleRows)
		}
	} else if singleRows <= mixedRows {
		t.Errorf("单一字体宽度 %.2f 超过框宽 %.2f，应折成更多行: %d vs %d",
			singleWidth, maxWidth, singleRows, mixedRows)
	}
}

// TestTextImageMalformedRunsAreTotal keeps rendering total: runs beyond the
// content, with negative locations, zero lengths, overlaps or unresolvable
// faces are clamped or skipped, never fatal, and the output keeps the
// premultiplied invariant (no channel above alpha).
func TestTextImageMalformedRunsAreTotal(t *testing.T) {
	style := domain.TextStyle{
		Content:  "abc",
		FontName: "Segoe UI",
		FontSize: 32,
		Red:      0.2, Green: 0.2, Blue: 0.2,
		ColorRuns: []domain.TextColorRun{
			{Location: 9, Length: 4, Red: 1},                          // beyond the content
			{Location: -2, Length: 2, Red: 1},                         // negative location
			{Location: 1, Length: 0, Blue: 1},                         // zero length
			{Location: 2, Length: 8, Green: 1},                        // overruns the end
			{Location: 0, Length: 3, Red: 0.2, Green: 0.2, Blue: 0.2}, // base color, covers all
		},
		FontRuns: []domain.TextFontRun{
			{Location: 4, Length: 3, FontName: "NoSuchFace-XYZ"}, // beyond the content
			{Location: -1, Length: 2, FontName: "Consolas"},      // negative location
			{Location: 0, Length: 0, FontName: "Arial"},          // zero length
			{Location: 1, Length: 99, FontName: ""},              // empty name, overruns
			{Location: 0, Length: 2, FontName: "NoSuchFace-ABC"}, // overlaps the first one
		},
	}
	bmp, ok := TextImage(style)
	if !ok || bmp == nil {
		t.Fatal("畸形 run 不应让渲染失败")
	}
	inked := 0
	for i := 0; i < bmp.W*bmp.H; i++ {
		a := bmp.Pix[i*4+3]
		if a > 0 {
			inked++
		}
		for c := 0; c < 3; c++ {
			if bmp.Pix[i*4+c] > a {
				t.Fatalf("预乘不变式被破坏 (%d): %v", i, bmp.Pix[i*4:i*4+4])
			}
		}
	}
	if inked == 0 {
		t.Error("钳制畸形 run 后仍应有墨迹")
	}
}

// TestTextImageLeadingPinsDocumentedLineHeight pins the line-height formula
// lineHeight = Leading > 0 ? Leading : FontSize*1.2 and the image height
// ceil(max(lines*lineHeight, ceil(lineHeight)) + 2*TextPadding): FontSize 100
// gives lineHeight 120, so one line is 144 and three lines are 384; Leading
// 150 gives 174 and 474.
func TestTextImageLeadingPinsDocumentedLineHeight(t *testing.T) {
	cases := []struct {
		name    string
		content string
		leading float64
		wantH   int
	}{
		{"单行自动行高 120", "A", 0, 144},
		{"单行 Leading 150", "A", 150, 174},
		{"三行自动行高 360", "A\nB\nC", 0, 384},
		{"三行 Leading 150", "A\nB\nC", 150, 474},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style := domain.TextStyle{Content: tc.content, FontName: "Segoe UI",
				FontSize: 100, Leading: tc.leading}
			bmp, ok := TextImage(style)
			if !ok || bmp == nil {
				t.Fatal("应能光栅化")
			}
			if bmp.H != tc.wantH {
				t.Errorf("高度应为 %d，得到 %d", tc.wantH, bmp.H)
			}
		})
	}
}

// TestTextImageTrackingAddsPerCharacter pins that tracking is added once per
// character including the last one: two characters with Tracking 20 measure
// exactly 40 wider, and the height is untouched.
func TestTextImageTrackingAddsPerCharacter(t *testing.T) {
	base := domain.TextStyle{Content: "AB", FontName: "Segoe UI", FontSize: 100}
	tracked := base
	tracked.Tracking = 20
	baseBmp, ok := TextImage(base)
	if !ok || baseBmp == nil {
		t.Fatal("应能光栅化")
	}
	trackedBmp, ok := TextImage(tracked)
	if !ok || trackedBmp == nil {
		t.Fatal("带字距的文本应能光栅化")
	}
	if got := trackedBmp.W - baseBmp.W; got != 40 {
		t.Errorf("Tracking 20 应让两字符宽度 +40，得到 +%d", got)
	}
	if trackedBmp.H != baseBmp.H {
		t.Errorf("字距不应改变高度: %d vs %d", trackedBmp.H, baseBmp.H)
	}
}

// pickDistinctFacePair resolves a candidate pair of installed faces, preferring
// exactly matched, really different ones. Tests skip rather than fail when the
// machine has none (the font directory can be empty in CI).
func pickDistinctFacePair() (baseName, runName string, baseFont, runFont *Font, ok bool) {
	pairs := [][2]string{
		{"Segoe UI", "Consolas"},
		{"Consolas", "Segoe UI"},
		{"Segoe UI", "Courier New"},
		{"Arial", "Consolas"},
		{"Courier New", "Arial"},
	}
	for _, pair := range pairs {
		b, bExact := Resolve(pair[0])
		r, rExact := Resolve(pair[1])
		if b == nil || r == nil || !bExact || !rExact || b.Sfnt == r.Sfnt {
			continue
		}
		return pair[0], pair[1], b, r, true
	}
	return "", "", nil, nil, false
}

// runTestInkRows counts rows carrying at least one inked pixel.
func runTestInkRows(b *render.Bitmap) int {
	rows := 0
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if b.Pix[(y*b.W+x)*4+3] > 0 {
				rows++
				break
			}
		}
	}
	return rows
}
