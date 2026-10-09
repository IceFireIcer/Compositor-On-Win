package layerrender

// Ticket 45 acceptance 2 (typography): the wrap must not be ASCII-space only.
// AppKit's byWordWrapping also breaks between CJK characters (UAX #14's ID
// class) and falls back to a character boundary when one unbreakable chunk is
// wider than the box. These tests call the layout directly — the same package,
// so no rasterization is involved and the assertions stay font-independent
// (every width is measured with the face that will draw it).

import (
	"math"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// cjkFace resolves an installed face that actually has CJK glyphs; machines
// without one skip rather than fail.
func cjkFace(t *testing.T) *sfnt.Font {
	t.Helper()
	for _, name := range []string{"Microsoft YaHei", "SimSun", "MS Gothic", "Malgun Gothic",
		"Noto Sans CJK SC", "Segoe UI"} {
		fontObj, _ := Resolve(name)
		if fontObj == nil {
			continue
		}
		var buf sfnt.Buffer
		if gi, err := fontObj.Sfnt.GlyphIndex(&buf, '中'); err == nil && gi != 0 {
			return fontObj.Sfnt
		}
	}
	t.Skip("本机没有带 CJK 字形的字体")
	return nil
}

func runsOf(text string, face *sfnt.Font) []styledRune {
	out := make([]styledRune, 0, len([]rune(text)))
	for _, r := range text {
		out = append(out, styledRune{r: r, face: face, color: [3]float64{}})
	}
	return out
}

func joinRunes(lines [][]styledRune) string {
	out := []rune{}
	for _, line := range lines {
		for _, c := range line {
			out = append(out, c.r)
		}
	}
	return string(out)
}

// assertFits checks the wrap kept every non-empty line inside the box (a line
// holding one unbreakable glyph may exceed it, exactly as the rasterizer's own
// first-glyph rule allows).
func assertFits(t *testing.T, lines [][]styledRune, ppem, tracking, maxWidth float64, face *sfnt.Font) {
	t.Helper()
	var buf sfnt.Buffer
	for i, line := range lines {
		if len(line) <= 1 {
			continue
		}
		if w := measureLine(&buf, line, ppem, tracking); w > maxWidth+0.01 {
			t.Fatalf("第 %d 行宽 %.2f 超过 %.2f：%q", i, w, maxWidth, joinRunes([][]styledRune{line}))
		}
	}
}

func TestWrapHardLineBreaksCJKBetweenCharacters(t *testing.T) {
	face := cjkFace(t)
	const ppem = 50.0
	text := "中文字符测试"
	// Two glyphs fit, three do not.
	var buf sfnt.Buffer
	two := measureLine(&buf, runsOf("中文", face), ppem, 0)
	maxWidth := two + 1

	lines := layoutLines([][]styledRune{runsOf(text, face)}, ppem, 0, maxWidth)
	if len(lines) < 2 {
		t.Fatalf("CJK 应在字符间断行，得到 %d 行：%q", len(lines), joinRunes(lines))
	}
	if got := joinRunes(lines); got != text {
		t.Fatalf("断行不应丢字：%q → %q", text, got)
	}
	for i, line := range lines {
		if len(line) > 2 {
			t.Fatalf("第 %d 行放了 %d 个字，超过两字宽度上限", i, len(line))
		}
	}
	assertFits(t, lines, ppem, 0, maxWidth, face)
}

func TestWrapHardLineFallsBackToCharacterBoundaries(t *testing.T) {
	fontObj, _ := Resolve("Segoe UI")
	if fontObj == nil {
		t.Skip("本机字体索引为空")
	}
	face := fontObj.Sfnt
	const ppem = 50.0
	text := "AAAAAAAAAA" // one unbreakable word
	var buf sfnt.Buffer
	one := measureLine(&buf, runsOf("A", face), ppem, 0)
	maxWidth := one*2 + 1 // room for two letters, not three

	lines := layoutLines([][]styledRune{runsOf(text, face)}, ppem, 0, maxWidth)
	if len(lines) < 3 {
		t.Fatalf("超长单词应按字符边界回退断行，得到 %d 行", len(lines))
	}
	if got := joinRunes(lines); got != text {
		t.Fatalf("字符回退断行不应丢字：%q → %q", text, got)
	}
	for i, line := range lines {
		if len(line) == 0 {
			t.Fatalf("第 %d 行为空", i)
		}
		if len(line) > 3 {
			t.Fatalf("第 %d 行放了 %d 个字（上限三字宽）", i, len(line))
		}
	}
	assertFits(t, lines, ppem, 0, maxWidth, face)
}

func TestWrapHardLineBreaksAtSpacesAndDropsTheBreakSpace(t *testing.T) {
	fontObj, _ := Resolve("Segoe UI")
	if fontObj == nil {
		t.Skip("本机字体索引为空")
	}
	face := fontObj.Sfnt
	const ppem = 24.0
	text := "alpha beta gamma delta"
	var buf sfnt.Buffer
	two := measureLine(&buf, runsOf("alpha beta", face), ppem, 0)
	maxWidth := two + 1

	lines := layoutLines([][]styledRune{runsOf(text, face)}, ppem, 0, maxWidth)
	if len(lines) < 2 {
		t.Fatalf("空格处应断行，得到 %d 行", len(lines))
	}
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if line[0].r == ' ' || line[len(line)-1].r == ' ' {
			t.Fatalf("第 %d 行带了断行空格：%q", i, joinRunes([][]styledRune{line}))
		}
	}
	// Words and the spaces inside a line survive; only the break spaces go.
	joined := joinRunes(lines)
	if len(joined) != len(text)-countSpaces(joined, text) {
		t.Fatalf("断行只应吞掉断行处的空格：%q → %q", text, joined)
	}
	assertFits(t, lines, ppem, 0, maxWidth, face)
}

// countSpaces returns how many spaces the wrap dropped (the original text's
// spaces minus the ones that survived inside lines).
func countSpaces(wrapped, original string) int {
	before, after := 0, 0
	for _, r := range original {
		if r == ' ' {
			before++
		}
	}
	for _, r := range wrapped {
		if r == ' ' {
			after++
		}
	}
	return before - after
}

func TestWrapHardLineKeepsWideTextInsideTheBox(t *testing.T) {
	face := cjkFace(t)
	const ppem = 40.0
	text := "ab中文cd汉字"
	var buf sfnt.Buffer
	unit := measureLine(&buf, runsOf("中", face), ppem, 0)
	maxWidth := unit*2 + 1

	lines := layoutLines([][]styledRune{runsOf(text, face)}, ppem, 0, maxWidth)
	if got := joinRunes(lines); got != text {
		t.Fatalf("混排断行不应丢字：%q → %q", text, got)
	}
	if len(lines) < 2 {
		t.Fatalf("混排文本应在盒子内断行，得到 %d 行", len(lines))
	}
	assertFits(t, lines, ppem, 0, maxWidth, face)
}

// The measurement helper must not be affected by the wrap: this pins that
// layoutLines leaves an infinite box (point text) alone.
func TestLayoutLinesKeepsPointTextUnwrapped(t *testing.T) {
	fontObj, _ := Resolve("Segoe UI")
	if fontObj == nil {
		t.Skip("本机字体索引为空")
	}
	hard := [][]styledRune{runsOf("a very long line that would wrap in a box", fontObj.Sfnt)}
	lines := layoutLines(hard, 24, 0, math.Inf(1))
	if len(lines) != 1 {
		t.Fatalf("点文本不应断行，得到 %d 行", len(lines))
	}
}
