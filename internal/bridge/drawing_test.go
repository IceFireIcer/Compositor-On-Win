package bridge

import (
	"bytes"
	"image/png"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestDrawShapeReverseRectanglePixelsAndSingleUndo(t *testing.T) {
	svc, tab := newTestService(t, 40, 30)
	out, err := svc.DrawShape(tab, "Rectangle", 30, 20, 10, 5, `{"foreground":{"r":1,"g":0,"b":0},"cornerRadius":0}`)
	if err != nil {
		t.Fatal(err)
	}
	e := parseEnvelope(t, out)
	if e.Rev != 1 || len(e.Doc.Layers) != 2 {
		t.Fatalf("shape commit = %+v", e)
	}
	l := e.Doc.Layers[1]
	if l.Shape == nil || l.Shape.Kind != domain.ShapeRectangle || l.Transform.Origin != [2]float64{10, 5} || l.Transform.Size != [2]float64{20, 15} {
		t.Fatalf("reverse rectangle = %+v", l)
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != l.ID {
		t.Fatal("new shape must become active")
	}
	assertDrawingPixel(t, svc, tab, 15, 10, [4]uint8{255, 0, 0, 255})
	assertDrawingPixel(t, svc, tab, 9, 10, [4]uint8{255, 255, 255, 255})
	undo, err := svc.Undo()
	if err != nil || len(parseEnvelope(t, undo).Doc.Layers) != 1 {
		t.Fatalf("undo shape: %s %v", undo, err)
	}
	if _, err := svc.Undo(); err == nil {
		t.Fatal("one drag must record exactly one undo step")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	assertDrawingPixel(t, svc, tab, 15, 10, [4]uint8{255, 0, 0, 255})
}

func assertDrawingPixel(t *testing.T, svc *Service, tab string, x, y int, want [4]uint8) {
	t.Helper()
	data, ok, err := svc.ws.RenderPNG(tab)
	if err != nil || !ok {
		t.Fatalf("render: %v %v", ok, err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bmp := render.BitmapFromImage(img)
	if got := pixelAt(t, bmp, x, y); got != want {
		t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
	}
}

func TestDrawShapeLineRetainsReverseEndpointsAndPersists(t *testing.T) {
	svc, tab := newTestService(t, 40, 30)
	out, err := svc.DrawShape(tab, "line", 30, 5, 10, 20, `{"foreground":{"r":0,"g":0,"b":1},"lineWidth":4}`)
	if err != nil {
		t.Fatal(err)
	}
	l := parseEnvelope(t, out).Doc.Layers[1]
	if l.Shape == nil || l.Shape.LineWidth == nil || *l.Shape.LineWidth != 4 || l.Shape.Start == nil || l.Shape.End == nil {
		t.Fatalf("line style = %+v", l.Shape)
	}
	if l.Transform.Origin != [2]float64{8, 3} || l.Transform.Size != [2]float64{24, 19} {
		t.Fatalf("stroke box = %+v", l.Transform)
	}
	// The stored normalized endpoints resolve back to the pointer locations,
	// including the negative Y slope of a reverse drag.
	for i, want := range [][2]float64{{30, 5}, {10, 20}} {
		p := l.Shape.Start
		if i == 1 {
			p = l.Shape.End
		}
		got := [2]float64{l.Origin[0] + p[0]*l.Size[0], l.Origin[1] + p[1]*l.Size[1]}
		if got != want {
			t.Fatalf("line endpoint %d = %v, want %v", i, got, want)
		}
	}
	assertDrawingPixel(t, svc, tab, 20, 12, [4]uint8{0, 0, 255, 255})
	assertDrawingPixel(t, svc, tab, 10, 5, [4]uint8{255, 255, 255, 255})
	path := filepath.Join(t.TempDir(), "drawing.comp")
	if _, err := svc.ws.SaveActive(path); err != nil {
		t.Fatal(err)
	}
	reopened := NewService(NewWorkspace())
	if _, err := reopened.OpenPath(path); err != nil {
		t.Fatal(err)
	}
	doc := mustDocumentSnapshot(t, reopened).Doc
	if doc.Version != 11 || !reflect.DeepEqual(doc.Layers[1].Shape, l.Shape) || doc.Layers[1].Transform != l.Transform {
		t.Fatalf("shape round-trip = %+v", doc.Layers[1])
	}
	assertDrawingPixel(t, reopened, reopened.ws.Snapshot().ActiveID, 20, 12, [4]uint8{0, 0, 255, 255})
}

func TestDrawShapeEllipseAndPillLeaveCornersClear(t *testing.T) {
	for _, kind := range []string{"Ellipse", "Rectangle"} {
		t.Run(kind, func(t *testing.T) {
			svc, tab := newTestService(t, 50, 30)
			if _, err := svc.DrawShape(tab, kind, 10, 5, 40, 25, `{"foreground":{"r":1},"cornerRadius":5000}`); err != nil {
				t.Fatal(err)
			}
			assertDrawingPixel(t, svc, tab, 10, 5, [4]uint8{255, 255, 255, 255})
			assertDrawingPixel(t, svc, tab, 25, 15, [4]uint8{255, 0, 0, 255})
		})
	}
}

func TestDrawDrawingRejectsNonFiniteGeometry(t *testing.T) {
	svc, tab := newTestService(t, 20, 20)
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.DrawShape(tab, "Rectangle", 0, 0, math.NaN(), 10, ""); err == nil {
		t.Fatal("non-finite shape geometry was accepted")
	}
	if _, err := svc.DrawGradient(tab, 0, 0, math.Inf(1), 10, "", ""); err == nil {
		t.Fatal("non-finite gradient geometry was accepted")
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatalf("invalid geometry changed document: before=%+v after=%+v", before, after)
	}
}

func TestDrawGradientSelectionPixelsUndoRedoAndNoop(t *testing.T) {
	svc, tab := newTestService(t, 40, 20)
	sel := solidSelPayload(10, 0, 20, 20, 255)
	settings := `{"shape":"Linear","style":"foregroundToBackground","foreground":{"r":0,"g":0,"b":0,"a":1},"background":{"r":1,"g":1,"b":1,"a":1},"opacity":1}`
	out, err := svc.DrawGradient(tab, 10.5, 10, 30.5, 10, settings, sel)
	if err != nil {
		t.Fatal(err)
	}
	if e := parseEnvelope(t, out); e.Rev != 1 || len(e.Doc.Layers) != 1 {
		t.Fatalf("gradient envelope = %s", out)
	}
	assertDrawingPixel(t, svc, tab, 5, 10, [4]uint8{255, 255, 255, 255})
	assertDrawingPixel(t, svc, tab, 10, 10, [4]uint8{0, 0, 0, 255})
	assertDrawingPixel(t, svc, tab, 20, 10, [4]uint8{128, 128, 128, 255})
	assertDrawingPixel(t, svc, tab, 29, 10, [4]uint8{242, 242, 242, 255})
	assertDrawingPixel(t, svc, tab, 30, 10, [4]uint8{255, 255, 255, 255})

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	assertDrawingPixel(t, svc, tab, 20, 10, [4]uint8{255, 255, 255, 255})
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	assertDrawingPixel(t, svc, tab, 20, 10, [4]uint8{128, 128, 128, 255})

	before := activeSessionOf(t, svc).hist.UndoCount()
	empty := solidSelPayload(0, 0, 40, 20, 0)
	noop, err := svc.DrawGradient(tab, 0, 0, 39, 0, settings, empty)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseEnvelope(t, noop).Rev; got != 3 {
		t.Fatalf("empty selection changed rev to %d", got)
	}
	if activeSessionOf(t, svc).hist.UndoCount() != before {
		t.Fatal("empty selection added an undo step")
	}
	click, err := svc.DrawGradient(tab, 10, 10, 10.2, 10.1, settings, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := parseEnvelope(t, click).Rev; got != 3 {
		t.Fatalf("gradient click changed rev to %d", got)
	}
	if activeSessionOf(t, svc).hist.UndoCount() != before {
		t.Fatal("gradient click added an undo step")
	}
}

func TestDrawGradientRadialAndTransparentDefaultsAreOrdinaryPixels(t *testing.T) {
	svc, tab := newTestService(t, 41, 41)
	settings := `{"shape":"Radial","foreground":{"r":0,"g":0,"b":0,"a":1},"opacity":1}`
	if _, err := svc.DrawGradient(tab, 20.5, 20.5, 30.5, 20.5, settings, ""); err != nil {
		t.Fatal(err)
	}
	assertDrawingPixel(t, svc, tab, 20, 20, [4]uint8{0, 0, 0, 255})
	assertDrawingPixel(t, svc, tab, 25, 20, [4]uint8{128, 128, 128, 255})
	assertDrawingPixel(t, svc, tab, 40, 40, [4]uint8{255, 255, 255, 255})
	l := mustDocumentSnapshot(t, svc).Doc.Layers[0]
	if l.Shape != nil || l.ImageFile == nil {
		t.Fatalf("gradient invented manifest metadata: %+v", l)
	}
}

func TestPixelDrawingRasterizesShapeOnlyOnCommitAndUndoRestoresStyle(t *testing.T) {
	for _, tool := range []string{"gradient", "brush"} {
		t.Run(tool, func(t *testing.T) {
			svc, tab := newTestService(t, 40, 30)
			out, err := svc.DrawShape(tab, "Rectangle", 10, 5, 30, 25, `{"foreground":{"r":1},"cornerRadius":0}`)
			if err != nil {
				t.Fatal(err)
			}
			shape := parseEnvelope(t, out).Doc.Layers[1]
			if tool == "gradient" {
				out, err = svc.DrawGradient(tab, 10.5, 15, 30.5, 15, `{"style":"foregroundToBackground","foreground":{"b":1},"background":{"b":1}}`, "")
			} else {
				var token string
				token, err = svc.BeginPaintStroke(tab, `{"diameter":8,"hardness":1,"b":1}`, "", 20, 15)
				if err == nil {
					if mustDocumentSnapshot(t, svc).Doc.Layers[1].Shape == nil {
						t.Fatal("pointer-down rasterized the shape before commit")
					}
					out, err = svc.EndPaintStroke(token, false)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if parseEnvelope(t, out).Doc.Layers[1].Shape != nil {
				t.Fatal("pixel edit retained shape metadata and hides edited pixels")
			}
			assertDrawingPixel(t, svc, tab, 20, 15, [4]uint8{0, 0, 255, 255})
			if _, err := svc.Undo(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(mustDocumentSnapshot(t, svc).Doc.Layers[1].Shape, shape.Shape) {
				t.Fatal("undo did not restore editable shape")
			}
			assertDrawingPixel(t, svc, tab, 20, 15, [4]uint8{255, 0, 0, 255})
			if _, err := svc.Redo(); err != nil {
				t.Fatal(err)
			}
			assertDrawingPixel(t, svc, tab, 20, 15, [4]uint8{0, 0, 255, 255})
		})
	}
}
