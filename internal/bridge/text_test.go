package bridge

// Ticket 44 tests: the text tool's commit path — new point text, paragraph
// boxes, editing a live layer, empty commits, and the .comp-style manifest
// round-trip (TextStyle travels in the document model).

import (
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestTextCommitCreatesPointTextLayer(t *testing.T) {
	svc, _ := newTestService(t, 200, 200)
	payload := `{"content":"Hello","fontName":"Segoe UI","fontSize":48,
		"red":1,"green":0,"blue":0,"alignment":"Left","tracking":0,"leading":0,
		"anchor":[50,100]}`
	env := parseJSON[envelope](t, mustTextCommit(t, svc, payload))
	if len(env.Doc.Layers) != 2 {
		t.Fatalf("应新增一层：%d", len(env.Doc.Layers))
	}
	l := env.Doc.Layers[1]
	if l.Text == nil || l.Text.Content != "Hello" || l.Text.FontSize != 48 {
		t.Fatalf("文字元数据 %+v", l.Text)
	}
	if l.Name != "Hello" {
		t.Fatalf("图层名 %q", l.Name)
	}
	if l.Transform.Origin[0] != 50-12 || l.Transform.Origin[1] >= 100 {
		t.Fatalf("点文本锚点应让首基线落在指针上：%v", l.Transform.Origin)
	}
	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[*l.ImageFile]
	inked := 0
	for i := 0; i < bmp.W*bmp.H; i++ {
		if bmp.Pix[i*4+3] > 0 {
			inked++
		}
	}
	if inked < 40 {
		t.Fatalf("文字应渲染出墨迹：%d", inked)
	}
	// Undo removes the layer.
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	back := mustDocumentSnapshot(t, svc)
	if len(back.Doc.Layers) != 1 {
		t.Fatal("撤销应移除文字层")
	}
}

func TestTextCommitParagraphBox(t *testing.T) {
	svc, _ := newTestService(t, 300, 300)
	payload := `{"content":"one two three four five six","fontName":"Segoe UI","fontSize":24,
		"red":0,"green":0,"blue":0,"alignment":"Center","tracking":0,"leading":0,
		"boxSize":[160,200],"anchor":[40,50]}`
	env := parseJSON[envelope](t, mustTextCommit(t, svc, payload))
	l := env.Doc.Layers[1]
	if l.Text == nil || l.Text.BoxSize == nil {
		t.Fatal("段落框应写入 BoxSize")
	}
	if l.Transform.Origin != [2]float64{40, 50} {
		t.Fatalf("框锚点即左上角：%v", l.Transform.Origin)
	}
	if l.Transform.Size != [2]float64{160, 200} {
		t.Fatalf("框尺寸 %v", l.Transform.Size)
	}
}

func TestTextCommitEditsExistingLayer(t *testing.T) {
	svc, _ := newTestService(t, 200, 200)
	payload := `{"content":"A","fontName":"Segoe UI","fontSize":32,"red":0,"green":0,"blue":0,
		"alignment":"Left","tracking":0,"leading":0,"anchor":[20,80]}`
	env := parseJSON[envelope](t, mustTextCommit(t, svc, payload))
	layerID := env.Doc.Layers[1].ID
	asset := *env.Doc.Layers[1].ImageFile
	edit := `{"layerId":"` + layerID + `","content":"Longer text","fontName":"Segoe UI","fontSize":32,
		"red":0,"green":0,"blue":0,"alignment":"Left","tracking":0,"leading":0,"anchor":[20,80]}`
	env2 := parseJSON[envelope](t, mustTextCommit(t, svc, edit))
	if len(env2.Doc.Layers) != 2 {
		t.Fatalf("编辑不应新增图层：%d", len(env2.Doc.Layers))
	}
	l := env2.Doc.Layers[1]
	if l.Text.Content != "Longer text" {
		t.Fatalf("内容 %q", l.Text.Content)
	}
	if *l.ImageFile != asset {
		t.Fatalf("编辑应复用资产名：%q vs %q", *l.ImageFile, asset)
	}
	sess := activeSessionOf(t, svc)
	bmp := sess.bitmaps[asset]
	// "Longer text" renders wider than the single "A".
	if bmp == nil || bmp.W <= 40 {
		t.Fatalf("编辑应替换像素，宽 %v", bmp)
	}
	if float64(bmp.W) != env2.Doc.Layers[1].Transform.Size[0] {
		t.Fatalf("变换尺寸应跟随新位图：%v vs %d", env2.Doc.Layers[1].Transform.Size[0], bmp.W)
	}
	// Non-text layer edit refuses.
	if _, err := svc.TextCommit(`{"layerId":"` + env.Doc.Layers[0].ID + `","content":"x",
		"fontName":"Segoe UI","fontSize":20,"red":0,"green":0,"blue":0,
		"alignment":"Left","tracking":0,"leading":0,"anchor":[0,0]}`); err == nil {
		t.Fatal("非文字层应拒绝编辑")
	}
}

func TestTextCommitEmptyNewLayerNoop(t *testing.T) {
	svc, _ := newTestService(t, 100, 100)
	env := parseJSON[envelope](t, mustTextCommit(t, svc,
		`{"content":"   ","fontName":"Segoe UI","fontSize":20,"red":0,"green":0,"blue":0,
		"alignment":"Left","tracking":0,"leading":0,"anchor":[10,10]}`))
	if len(env.Doc.Layers) != 1 {
		t.Fatalf("空提交不应建层：%d", len(env.Doc.Layers))
	}
}

func TestTextCommitValidation(t *testing.T) {
	svc, _ := newTestService(t, 100, 100)
	if _, err := svc.TextCommit(`{"content":"x","fontName":"Segoe UI","fontSize":0}`); err == nil {
		t.Fatal("零字号应拒绝")
	}
	if _, err := svc.TextCommit(`{"content":"x","fontName":"Segoe UI","fontSize":2001}`); err == nil {
		t.Fatal("字号超限应拒绝")
	}
	if _, err := svc.TextCommit(`{"content":"x","fontName":"Segoe UI","fontSize":20,"red":2}`); err == nil {
		t.Fatal("非法颜色应拒绝")
	}
}

// mustTextCommit runs TextCommit expecting success.
func mustTextCommit(t *testing.T, svc *Service, payload string) string {
	t.Helper()
	out, err := svc.TextCommit(payload)
	if err != nil {
		t.Fatalf("TextCommit failed: %v", err)
	}
	return out
}

var _ = strings.TrimSpace

func TestTextLayerCompositesAndClips(t *testing.T) {
	svc, _ := newTestService(t, 200, 120)
	// A text layer's pixels are ordinary; its clipping behaviour must be too.
	env := parseJSON[envelope](t, mustTextCommit(t, svc,
		`{"content":"Clip","fontName":"Segoe UI","fontSize":40,"red":1,"green":0,"blue":0,
		"alignment":"Left","tracking":0,"leading":0,"anchor":[60,60]}`))
	textLayer := env.Doc.Layers[1]
	sess := activeSessionOf(t, svc)
	tb := sess.bitmaps[*textLayer.ImageFile]
	for i := 0; i < len(tb.Pix); i += 4 {
		tb.Pix[i], tb.Pix[i+1], tb.Pix[i+2], tb.Pix[i+3] = 255, 0, 0, 255
	}
	// A full-canvas blue layer clipped onto the text layer: blue may only
	// appear within the text bitmap's box.
	blueAsset := newUUID() + ".png"
	bmp := render.NewBitmap(200, 120)
	for i := 0; i < len(bmp.Pix); i += 4 {
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 0, 255, 255
	}
	sess.bitmaps[blueAsset] = bmp
	normal := domain.BlendNormal
	opacity := 1.0
	blue := domain.Layer{
		ID:           newUUID(),
		Name:         "Blue",
		IsVisible:    true,
		Transform:    domain.Transform{Size: [2]float64{200, 120}, Sampling: domain.SamplingNearest},
		ImageFile:    &blueAsset,
		BlendMode:    &normal,
		Opacity:      &opacity,
		MaskSourceID: func() *string { id := textLayer.ID; return &id }(),
	}
	sess.doc.Layers = append(sess.doc.Layers, blue)
	// Composite through the CPU truth.
	out, err := render.Render(sess.doc, sess.pixelSource())
	if err != nil {
		t.Fatal(err)
	}
	// Inside the text box: blue. Outside: the white background (clipped away).
	inX := int(textLayer.Transform.Origin[0]) + 5
	inY := int(textLayer.Transform.Origin[1]) + 5
	inside := out.Pix[(inY*200+inX)*4 : (inY*200+inX)*4+4]
	if inside[2] < 200 || inside[0] > 60 {
		t.Fatalf("文字框内应为剪贴后的蓝色: %v（采样 %d,%d）", inside, inX, inY)
	}
	outside := out.Pix[(10*200+10)*4 : (10*200+10)*4+4]
	if outside[0] < 200 || outside[2] < 200 {
		t.Fatalf("文字框外应为白色背景: %v", outside)
	}
}
