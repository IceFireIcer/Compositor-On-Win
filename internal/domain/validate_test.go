package domain

import (
	"strings"
	"testing"
)

func baseLayer(id string) Layer {
	return Layer{ID: id, Name: id, IsVisible: true, Transform: Transform{Sampling: SamplingHighQuality}}
}

func chainDoc(depth int) *Document {
	d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 100, Height: 100}
	for i := 0; i < depth; i++ {
		id := groupID(i)
		g := baseLayer(id)
		g.IsGroup = ptr(true)
		if i > 0 {
			g.ParentID = ptr(groupID(i - 1))
		}
		d.Layers = append(d.Layers, g)
	}
	leaf := baseLayer("leaf")
	if depth > 0 {
		leaf.ParentID = ptr(groupID(depth - 1))
	}
	d.Layers = append(d.Layers, leaf)
	return d
}

func groupID(i int) string {
	return "G" + string(rune('A'+i%26)) + string(rune('0'+i/26)) + "-0000"
}

func ptr[T any](v T) *T { return &v }

func TestValidateDepth(t *testing.T) {
	if err := chainDoc(64).Validate(); err != nil {
		t.Fatalf("64 nested groups + leaf must pass: %v", err)
	}
	// 65 nested groups: the deepest group sits at level 65 — rejected.
	d := chainDoc(65)
	leaf := d.Layers[len(d.Layers)-1]
	d.Layers = d.Layers[:len(d.Layers)-1]
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "层级") {
		t.Fatalf("65 nested groups must fail with 层级 error, got %v", err)
	}
	_ = leaf
}

func TestValidateClipChain(t *testing.T) {
	build := func(n int) *Document {
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 100, Height: 100}
		base := baseLayer("L-base")
		d.Layers = append(d.Layers, base)
		for i := 0; i < n-1; i++ {
			l := baseLayer("L" + itoa(i))
			if i == 0 {
				l.MaskSourceID = ptr("L-base")
			} else {
				l.MaskSourceID = ptr("L" + itoa(i-1))
			}
			d.Layers = append(d.Layers, l)
		}
		return d
	}
	if err := build(256).Validate(); err != nil {
		t.Fatalf("256-node clip chain must pass: %v", err)
	}
	if err := build(257).Validate(); err == nil || !strings.Contains(err.Error(), "剪贴") {
		t.Fatalf("257-node clip chain must fail with 剪贴 error, got %v", err)
	}
}

func TestValidateEffectSizes(t *testing.T) {
	build := func(size float64) *Document {
		l := baseLayer("a")
		l.Effects = &Effects{Stroke: &StrokeEffect{Size: size}}
		return &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{l}}
	}
	if err := build(500).Validate(); err != nil {
		t.Fatalf("stroke size 500 must pass: %v", err)
	}
	if err := build(500.5).Validate(); err == nil || !strings.Contains(err.Error(), "描边") {
		t.Fatalf("stroke size 500.5 must fail with 描边 error, got %v", err)
	}
}

func TestValidateHierarchy(t *testing.T) {
	t.Run("cycle rejected", func(t *testing.T) {
		a, b := baseLayer("a"), baseLayer("b")
		a.IsGroup, b.IsGroup = ptr(true), ptr(true)
		a.ParentID, b.ParentID = ptr("b"), ptr("a")
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{a, b}}
		if err := d.Validate(); err == nil {
			t.Fatal("cycle must fail")
		}
	})
	t.Run("missing parent rejected", func(t *testing.T) {
		l := baseLayer("a")
		l.ParentID = ptr("ghost")
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{l}}
		if err := d.Validate(); err == nil {
			t.Fatal("missing parent must fail")
		}
	})
	t.Run("parent must be a group", func(t *testing.T) {
		p, c := baseLayer("p"), baseLayer("c")
		c.ParentID = ptr("p")
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{p, c}}
		if err := d.Validate(); err == nil {
			t.Fatal("non-group parent must fail")
		}
	})
	t.Run("group cannot carry an image", func(t *testing.T) {
		g := baseLayer("g")
		img := "g.png"
		g.IsGroup, g.ImageFile = ptr(true), &img
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{g}}
		if err := d.Validate(); err == nil {
			t.Fatal("group with imageFile must fail")
		}
	})
	t.Run("duplicate ids rejected", func(t *testing.T) {
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{baseLayer("a"), baseLayer("a")}}
		if err := d.Validate(); err == nil {
			t.Fatal("duplicate ids must fail")
		}
	})
	t.Run("layer count limit", func(t *testing.T) {
		d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1}
		for i := 0; i <= 10000; i++ {
			d.Layers = append(d.Layers, baseLayer(cheapID(i)))
		}
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "图层") {
			t.Fatalf("10001 layers must fail with 图层 error, got %v", err)
		}
	})
}

func cheapID(i int) string {
	return "L" + string(rune('A'+i%26)) + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func TestValidateGuides(t *testing.T) {
	ok := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1}
	for i := 0; i < 1000; i++ {
		ok.GuidesList = &[]Guide{{ID: cheapID(i), Axis: GuideAxisHorizontal, Position: 1_000_000}}
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("1000 guides at the position bound must pass: %v", err)
	}
	bad := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1}
	bad.GuidesList = &[]Guide{{ID: "g", Axis: GuideAxis("diagonal"), Position: 0}}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown axis must fail")
	}
	over := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1}
	over.GuidesList = &[]Guide{{ID: "g", Axis: GuideAxisHorizontal, Position: 1_000_000.5}}
	if err := over.Validate(); err == nil {
		t.Fatal("|position| beyond 1e6 must fail")
	}
}

func TestValidateAdjustmentRanges(t *testing.T) {
	build := func(mutate func(*Adjustment)) *Document {
		adj := Adjustment{Kind: AdjustmentCurves}
		mutate(&adj)
		l := baseLayer("adj")
		l.Adjustment = &adj
		return &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{l}}
	}
	mustFail := func(name string, d *Document, want string) {
		t.Helper()
		err := d.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want containing %q", name, err, want)
		}
	}
	if err := build(func(a *Adjustment) { a.Hue = 360.0 }).Validate(); err != nil {
		t.Fatalf("hue 360 must pass: %v", err)
	}
	mustFail("hue 360.5", build(func(a *Adjustment) { a.Hue = 360.5 }), "色相")
	mustFail("saturation 101", build(func(a *Adjustment) { a.Saturation = 101 }), "饱和度")
	mustFail("blurRadius 0.05", build(func(a *Adjustment) { a.BlurRadius = ptr(0.05) }), "blurRadius")
	if err := build(func(a *Adjustment) { a.BlurRadius = ptr(250.0) }).Validate(); err != nil {
		t.Fatalf("blurRadius 250 must pass: %v", err)
	}
	mustFail("motionAngle 90.5", build(func(a *Adjustment) { a.MotionAngle = ptr(90.5) }), "motionAngle")
	mustFail("motionDistance 0", build(func(a *Adjustment) { a.MotionDistance = ptr(0.0) }), "motionDistance")
	mustFail("noiseAmount 400.5", build(func(a *Adjustment) { a.NoiseAmount = ptr(400.5) }), "noiseAmount")
	mustFail("adjustment on group", func() *Document {
		d := build(func(a *Adjustment) {})
		d.Layers[0].IsGroup = ptr(true)
		return d
	}(), "编组")
}

func TestValidateTextRuns(t *testing.T) {
	build := func(runs []TextColorRun) *Document {
		l := baseLayer("t")
		l.Text = &TextStyle{Content: "ab😀cd", ColorRuns: runs}
		return &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{l}}
	}
	// "ab😀cd" is 6 UTF-16 units (😀 is a surrogate pair).
	if err := build([]TextColorRun{{Location: 2, Length: 2, Red: 1}}).Validate(); err != nil {
		t.Fatalf("surrogate-pair run must pass: %v", err)
	}
	mustFail := func(name string, runs []TextColorRun) {
		t.Helper()
		if err := build(runs).Validate(); err == nil {
			t.Fatalf("%s must fail", name)
		}
	}
	mustFail("zero length", []TextColorRun{{Location: 0, Length: 0}})
	mustFail("overlap", []TextColorRun{{Location: 0, Length: 3}, {Location: 2, Length: 2}})
	mustFail("unsorted", []TextColorRun{{Location: 3, Length: 1}, {Location: 0, Length: 1}})
	mustFail("beyond content", []TextColorRun{{Location: 5, Length: 3}})
}

func TestValidateOpacityAndBlend(t *testing.T) {
	l := baseLayer("a")
	l.Opacity = ptr(1.0000001)
	d := &Document{Format: FormatID, Version: FormatVersion, ColorSpace: ColorSpaceSRGB, DocumentID: "D", Width: 1, Height: 1, Layers: []Layer{l}}
	if err := d.Validate(); err == nil {
		t.Fatal("opacity beyond 1 must fail")
	}
	l2 := baseLayer("b")
	bad := BlendMode("Darker Color")
	l2.BlendMode = &bad
	d.Layers = []Layer{l2}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "混合模式") {
		t.Fatalf("unknown blend mode must fail, got %v", err)
	}
}
