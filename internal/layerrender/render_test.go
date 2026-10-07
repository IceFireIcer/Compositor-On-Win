package layerrender

import (
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestResolveSystemFont(t *testing.T) {
	f, exact := Resolve("Segoe UI")
	if f == nil {
		t.Skip("本机字体索引为空")
	}
	if !exact {
		t.Errorf("Segoe UI 应精确命中")
	}
	if f.PSName == "" || f.Family == "" {
		t.Errorf("字体名称为空: %+v", f)
	}
	// A missing name falls back to the system font without claiming exact.
	if _, exact := Resolve("NoSuchFont-XYZ"); exact {
		t.Error("缺失字体不应自称精确命中")
	}
}

func TestTextImageRendersInk(t *testing.T) {
	style := domain.TextStyle{
		Content:  "Hi",
		FontName: "Segoe UI",
		FontSize: 48,
		Red:      1, Green: 0, Blue: 0,
		Alignment: domain.TextAlignmentLeft,
	}
	bmp, ok := TextImage(style)
	if !ok {
		t.Fatal("文本应能光栅化")
	}
	inked := 0
	for i := 0; i < bmp.W*bmp.H; i++ {
		if bmp.Pix[i*4+3] > 0 {
			inked++
			// Premultiplied red ink: green/blue stay low.
			if bmp.Pix[i*4+1] > 32 || bmp.Pix[i*4+2] > 32 {
				t.Fatalf("红字应只带红通道: %v", bmp.Pix[i*4:i*4+4])
			}
		}
	}
	if inked < 50 {
		t.Fatalf("“Hi” 应有墨迹像素: %d", inked)
	}
}

func TestTextImageBoxWrapsAndAligns(t *testing.T) {
	text := "one two three four five six seven eight"
	narrow := domain.TextStyle{Content: text, FontName: "Segoe UI", FontSize: 24,
		BoxSize: &[2]float64{180, 400}}
	wide := domain.TextStyle{Content: text, FontName: "Segoe UI", FontSize: 24,
		BoxSize: &[2]float64{900, 400}}
	narrowBmp, ok := TextImage(narrow)
	if !ok {
		t.Fatal("窄框应能光栅化")
	}
	wideBmp, ok := TextImage(wide)
	if !ok {
		t.Fatal("宽框应能光栅化")
	}
	if narrowBmp.W != 180 || narrowBmp.H != 400 || wideBmp.W != 900 {
		t.Fatalf("框尺寸 %dx%d / %dx%d", narrowBmp.W, narrowBmp.H, wideBmp.W, wideBmp.H)
	}
	if inkRows(narrowBmp) <= inkRows(wideBmp) {
		t.Errorf("窄框折行应产生更多墨迹行: %d vs %d", inkRows(narrowBmp), inkRows(wideBmp))
	}
	// Alignment shifts ink horizontally.
	left := domain.TextStyle{Content: "Hi", FontName: "Segoe UI", FontSize: 32,
		Alignment: domain.TextAlignmentLeft, BoxSize: &[2]float64{300, 120}}
	right := left
	right.Alignment = domain.TextAlignmentRight
	lb, _ := TextImage(left)
	rb, _ := TextImage(right)
	if lb == nil || rb == nil {
		t.Fatal("对齐样本应能光栅化")
	}
	if firstInkColumn(lb) >= firstInkColumn(rb) {
		t.Errorf("右对齐应把墨迹推右: left=%d right=%d", firstInkColumn(lb), firstInkColumn(rb))
	}
}

// inkRows counts rows carrying at least one inked pixel.
func inkRows(b *render.Bitmap) int {
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

// firstInkColumn finds the leftmost inked column.
func firstInkColumn(b *render.Bitmap) int {
	for x := 0; x < b.W; x++ {
		for y := 0; y < b.H; y++ {
			if b.Pix[(y*b.W+x)*4+3] > 0 {
				return x
			}
		}
	}
	return -1
}

func TestShapeImageRectEllipseLine(t *testing.T) {
	red := 1.0
	rect, ok := ShapeImage(domain.ShapeStyle{Kind: domain.ShapeRectangle, Red: red}, 40, 30)
	if !ok {
		t.Fatal("矩形应能光栅化")
	}
	// 填充矩形：中心与角落都上色（0 圆角铺满）。
	for _, at := range [][2]int{{0, 0}, {39, 29}, {20, 15}} {
		i := (at[1]*40 + at[0]) * 4
		if rect.Pix[i+3] == 0 {
			t.Fatalf("矩形应铺满 (%d,%d)", at[0], at[1])
		}
	}
	// 椭圆：角落空、中心实。
	ell, ok := ShapeImage(domain.ShapeStyle{Kind: domain.ShapeEllipse, Green: 1}, 40, 40)
	if !ok {
		t.Fatal("椭圆应能光栅化")
	}
	corner := ell.Pix[(0*40+0)*4+3]
	center := ell.Pix[(20*40+20)*4+3]
	if corner != 0 || center == 0 {
		t.Fatalf("椭圆角落=%d 中心=%d", corner, center)
	}
	// 线：对角线中段上色、两角外的另一侧空。
	line, ok := ShapeImage(domain.ShapeStyle{Kind: domain.ShapeLine, Blue: 1, LineWidth: ptrF(6)}, 40, 40)
	if !ok {
		t.Fatal("线应能光栅化")
	}
	if line.Pix[(20*40+20)*4+3] == 0 {
		t.Fatal("对角线中点应有墨迹")
	}
	if line.Pix[(30*40+5)*4+3] != 0 {
		t.Fatal("偏离对角线处应为空")
	}
}

func ptrF(v float64) *float64 { return &v }

func TestVectorImageFillAndStroke(t *testing.T) {
	// 一个方形路径（文档坐标 10..30），填充绿 + 蓝描边。
	segments := []PathSeg{
		{Kind: 0, X: 10, Y: 10},
		{Kind: 1, X: 30, Y: 10},
		{Kind: 1, X: 30, Y: 30},
		{Kind: 1, X: 10, Y: 30},
		{Kind: 2},
	}
	fill := [3]float64{0, 1, 0}
	stroke := &Stroke{Red: 0, Green: 0, Blue: 1, Width: 4}
	bmp, ok := VectorImage(segments, 10, 10, 20, 20, &fill, stroke)
	if !ok {
		t.Fatal("矢量应能光栅化")
	}
	center := bmp.Pix[(10*20+10)*4 : (10*20+10)*4+4]
	if center[3] == 0 || center[1] <= center[2] {
		t.Fatalf("中心应为填充绿: %v", center)
	}
	topEdge := bmp.Pix[(0*20+10)*4 : (0*20+10)*4+4]
	if topEdge[3] == 0 || topEdge[2] <= topEdge[1] {
		t.Fatalf("顶边应为描边蓝: %v", topEdge)
	}
}
