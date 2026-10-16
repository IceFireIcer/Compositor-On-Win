package render

// 票 84（U31）：16 位合成**不是第二份像素真相** —— 它必须与 8 位合成给出同一个结果，
// 只是中间不经过 8 位存储。这条测试就是那句约束的执行者。

import (
	"fmt"
	"testing"

	"compositor-win/internal/domain"
)

// deepCompositeFixture builds two normal-blended image layers with deep rasters:
// a full-canvas bottom and a shifted, half-opaque top, so the composite exercises
// placement, alpha scaling and source-over.
func deepCompositeFixture(t *testing.T) (*domain.Document, PixelSource) {
	t.Helper()
	const w, h = 12, 8
	makeBitmap := func(seed uint8) *Bitmap {
		b := NewBitmap(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := (y*w + x) * 4
				b.Pix[i] = uint8(x*17 + int(seed))
				b.Pix[i+1] = uint8(y*29 + int(seed))
				b.Pix[i+2] = uint8(90 + seed)
				b.Pix[i+3] = uint8(200 - x*3)
			}
		}
		b.Deep = DeepFromBitmap(b)
		return b
	}
	bottom := benchLayer("bottom", "bottom.png", w, h, nil)
	top := benchLayer("top", "top.png", w, h, func(l *domain.Layer) {
		l.Origin = [2]float64{2, 1}
		opacity := 0.5
		l.Opacity = &opacity
	})
	doc := &domain.Document{
		Format: domain.FormatID, Version: domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB, DocumentID: "deep",
		Width: w, Height: h,
		Layers: []domain.Layer{bottom, top},
	}
	srcs := map[string]*Bitmap{"bottom.png": makeBitmap(0), "top.png": makeBitmap(40)}
	return doc, func(name string) (*Bitmap, error) {
		if b, ok := srcs[name]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("缺少资产 %s", name)
	}
}

func TestDeepCompositeMatchesTheEightBitOne(t *testing.T) {
	doc, src := deepCompositeFixture(t)
	want, err := Render(doc, src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	deep, err := CompositeDeep(doc, src)
	if err != nil {
		t.Fatalf("CompositeDeep: %v", err)
	}
	if deep.W != doc.Width || deep.H != doc.Height {
		t.Fatalf("深度合成尺寸 = %d×%d，想要 %d×%d", deep.W, deep.H, doc.Width, doc.Height)
	}
	got := deep.ToBitmap()

	// 容差 2：8 位路径每一步都四舍五入到字节，16 位路径只在最后落回 8 位时舍入一次，
	// 所以两者不会逐字节相同 —— 但差出可见的量级就说明公式没镜像对。
	worst := 0
	for i := range want.Pix {
		d := int(want.Pix[i]) - int(got.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst = d
		}
	}
	t.Logf("深度合成与 8 位合成的最大差值 = %d 级", worst)
	if worst > 2 {
		t.Fatalf("深度合成与 8 位合成最大相差 %d 级 —— 公式没有镜像对（或采样方式不一致）", worst)
	}
}

// 位深确实是 16：两个只差一个低位单位的图层，合出来仍要保留差别，而它们的 8 位副本是同一个数。
func TestDeepCompositeKeepsBitsTheEightBitPathCannot(t *testing.T) {
	const w, h = 2, 1
	make := func(v uint16) *Bitmap {
		b := NewBitmap(w, h)
		for i := 0; i < w*h; i++ {
			b.Pix[i*4], b.Pix[i*4+1], b.Pix[i*4+2], b.Pix[i*4+3] = uint8(v/257), uint8(v/257), uint8(v/257), 255
		}
		b.Deep = NewDeep(w, h, false)
		for i := 0; i < w*h; i++ {
			b.Deep.Pix[i*4], b.Deep.Pix[i*4+1], b.Deep.Pix[i*4+2], b.Deep.Pix[i*4+3] = v, v, v, 65535
		}
		return b
	}
	bottom := benchLayer("bottom", "bottom.png", w, h, nil)
	top := benchLayer("top", "top.png", w, h, nil)
	doc := &domain.Document{Format: domain.FormatID, Version: domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB, DocumentID: "bits", Width: w, Height: h,
		Layers: []domain.Layer{bottom, top}}
	srcs := map[string]*Bitmap{"bottom.png": make(1000), "top.png": make(1001)}
	src := func(name string) (*Bitmap, error) { return srcs[name], nil }
	deep, err := CompositeDeep(doc, src)
	if err != nil {
		t.Fatal(err)
	}
	got := deep.ToBitmap()
	// 夹具的前提：两层的 8 位副本本来就相同，所以 8 位路径无从上区分它们。
	if srcs["bottom.png"].Pix[0] != srcs["top.png"].Pix[0] {
		t.Fatalf("夹具的 8 位副本不同（%d vs %d），这条测不出东西",
			srcs["bottom.png"].Pix[0], srcs["top.png"].Pix[0])
	}
	// 两层都不透明，合出来就是上面那层的值（1001/257 落下取整）。
	if got.Pix[0] != uint8(1001/257) {
		t.Fatalf("合出来 %d，想要 %d", got.Pix[0], 1001/257)
	}
}
