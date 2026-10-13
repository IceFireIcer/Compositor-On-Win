package bridge

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func TestFlipCanvasHorizontallyMirrorsPlacementAndGuides(t *testing.T) {
	svc, tabID := newTestService(t, 40, 30)
	initial := mustDocumentSnapshot(t, svc)
	layerID := initial.Doc.Layers[0].ID
	wantBefore := domain.Transform{
		Origin:   [2]float64{2, 3},
		Size:     [2]float64{10, 5},
		Rotation: 30,
		FlipY:    true,
		Sampling: domain.SamplingSmooth,
	}
	if _, err := svc.ApplyLayerTransform(layerID, payloadJSON(t, wantBefore)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetGuides(`[{"id":"B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E","axis":"vertical","position":7},{"id":"C3D4E5F6-A7B8-4C9D-8E1F-2A3B4C5D6E7F","axis":"horizontal","position":11}]`); err != nil {
		t.Fatal(err)
	}
	beforeFlip := mustDocumentSnapshot(t, svc)

	out, err := svc.FlipCanvas(tabID, "horizontal")
	if err != nil {
		t.Fatal(err)
	}
	after := parseEnvelope(t, out)
	got := after.Doc.Layers[0].Transform
	want := wantBefore
	want.Origin[0] = 40 - wantBefore.Origin[0] - wantBefore.Size[0]
	want.Rotation = -wantBefore.Rotation
	want.FlipX = !wantBefore.FlipX
	if got != want {
		t.Fatalf("水平翻转后的变换 = %+v，想要 %+v", got, want)
	}
	guides := after.Doc.Guides()
	if guides[0].Position != 33 || guides[1].Position != 11 {
		t.Fatalf("水平翻转后的参考线 = %+v", guides)
	}

	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 3 {
		t.Fatalf("翻转应作为一步历史追加，得到 %d 条历史", got)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if restored := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(restored.Doc, beforeFlip.Doc) {
		t.Fatalf("撤销翻转未恢复原文档:\n got=%+v\nwant=%+v", restored.Doc, beforeFlip.Doc)
	}
}

type canvasFlipFixture struct {
	tabID       string
	before      *domain.Document
	assets      map[string]*render.Bitmap
	assetPixels map[string][]byte
}

func newCanvasFlipFixture(t *testing.T) (*Service, canvasFlipFixture) {
	t.Helper()
	svc, tabID := newTestService(t, 100, 80)
	imageName := "CHILD.PNG"
	maskName := "CHILD.MASK.PNG"
	childImage := render.NewBitmap(3, 2)
	for i := range childImage.Pix {
		childImage.Pix[i] = uint8(i + 1)
	}
	maskImage := render.NewBitmap(2, 2)
	for i := range maskImage.Pix {
		maskImage.Pix[i] = uint8(200 + i)
	}
	groupID, childID, plainID := "GROUP", "CHILD", "PLAIN"
	group := true
	plainGroup := false
	maskLinked := false
	childPlacement := domain.Transform{
		Origin:   [2]float64{4, 6},
		Size:     [2]float64{5, 3},
		Rotation: 33,
		FlipX:    true,
		FlipY:    true,
		Sampling: domain.SamplingHighQuality,
	}
	child := domain.Layer{
		ID:        childID,
		Name:      "Child",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{10, 20},
			Size:     [2]float64{12, 8},
			Rotation: -25,
			FlipX:    true,
			Sampling: domain.SamplingSmooth,
		},
		ImageFile:     &imageName,
		ParentID:      &groupID,
		IsGroup:       &plainGroup,
		MaskFile:      &maskName,
		MaskLinked:    &maskLinked,
		MaskPlacement: &childPlacement,
	}
	groupLayer := domain.Layer{
		ID:        groupID,
		Name:      "Group",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{5, 7},
			Size:     [2]float64{40, 30},
			Rotation: 15,
			FlipY:    true,
			Sampling: domain.SamplingNearest,
		},
		IsGroup: &group,
	}
	plain := domain.Layer{
		ID:        plainID,
		Name:      "Plain",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{60, 1},
			Size:     [2]float64{20, 10},
			Rotation: 0,
			FlipX:    true,
			FlipY:    false,
			Sampling: domain.SamplingNearest,
		},
		IsGroup: &plainGroup,
	}
	active := childID
	doc := &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    tabID,
		Width:         100,
		Height:        80,
		ActiveLayerID: &active,
		Layers:        []domain.Layer{groupLayer, child, plain},
	}
	assets := map[string]*render.Bitmap{imageName: childImage, maskName: maskImage}
	assetPixels := map[string][]byte{
		imageName: append([]byte(nil), childImage.Pix...),
		maskName:  append([]byte(nil), maskImage.Pix...),
	}
	svc.ws.mu.Lock()
	sess := svc.ws.sessions[tabID]
	sess.doc = doc
	sess.bitmaps = assets
	before := cloneDocForEdit(doc)
	svc.ws.mu.Unlock()
	return svc, canvasFlipFixture{tabID: tabID, before: before, assets: assets, assetPixels: assetPixels}
}

func TestFlipCanvasVerticallyMirrorsGroupsMasksGuidesAndUndoAsOneStep(t *testing.T) {
	svc, fixture := newCanvasFlipFixture(t)
	if _, err := svc.SetGuides(`[{"id":"B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E","axis":"vertical","position":13},{"id":"C3D4E5F6-A7B8-4C9D-8E1F-2A3B4C5D6E7F","axis":"horizontal","position":6},{"id":"D4E5F6A7-B8C9-4D0E-9F1A-2B3C4D5E6F70","axis":"horizontal","position":60.5}]`); err != nil {
		t.Fatal(err)
	}
	fixture.before = cloneDocForEdit(mustDocumentSnapshot(t, svc).Doc)
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 1 {
		t.Fatalf("fixture guide setup should be one history step, got %d", got)
	}

	out, err := svc.FlipCanvas(fixture.tabID, "vertical")
	if err != nil {
		t.Fatal(err)
	}
	after := parseEnvelope(t, out)
	byID := make(map[string]domain.Layer, len(after.Doc.Layers))
	for _, layer := range after.Doc.Layers {
		byID[layer.ID] = layer
	}
	if got := byID["GROUP"].Transform; got != (domain.Transform{
		Origin:   [2]float64{5, 43},
		Size:     [2]float64{40, 30},
		Rotation: -15,
		FlipY:    false,
		Sampling: domain.SamplingNearest,
	}) {
		t.Fatalf("group transform after vertical flip = %+v", got)
	}
	if got := byID["CHILD"].Transform; got != (domain.Transform{
		Origin:   [2]float64{10, 52},
		Size:     [2]float64{12, 8},
		Rotation: 25,
		FlipX:    true,
		FlipY:    true,
		Sampling: domain.SamplingSmooth,
	}) {
		t.Fatalf("child transform after vertical flip = %+v", got)
	}
	if got := *byID["CHILD"].MaskPlacement; got != (domain.Transform{
		Origin:   [2]float64{4, 71},
		Size:     [2]float64{5, 3},
		Rotation: -33,
		FlipX:    true,
		FlipY:    false,
		Sampling: domain.SamplingHighQuality,
	}) {
		t.Fatalf("mask placement after vertical flip = %+v", got)
	}
	if got := byID["PLAIN"].Transform; got.Origin != [2]float64{60, 69} || !got.FlipY || got.Rotation != 0 {
		t.Fatalf("straight layer after vertical flip = %+v", got)
	}
	guides := after.Doc.Guides()
	if got := guides[0]; got.Position != 13 || got.Axis != domain.GuideAxisVertical {
		t.Fatalf("vertical guide should keep its position = %+v", got)
	}
	if guides[1].Position != 74 || guides[2].Position != 19.5 {
		t.Fatalf("horizontal guides after vertical flip = %+v", guides)
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "翻转画布（垂直）" {
		t.Fatalf("history name = %q", got)
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 2 {
		t.Fatalf("flip should add exactly one history entry, got %d", got)
	}

	for name, before := range fixture.assets {
		got := activeSessionOf(t, svc).bitmaps[name]
		if got != before || !bytes.Equal(got.Pix, fixture.assetPixels[name]) {
			t.Fatalf("flip replaced asset pointer %q", name)
		}
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored := mustDocumentSnapshot(t, svc)
	if !reflect.DeepEqual(restored.Doc, fixture.before) {
		t.Fatalf("one undo did not restore the whole pre-flip document:\n got=%+v\nwant=%+v", restored.Doc, fixture.before)
	}
	for name, before := range fixture.assets {
		got := activeSessionOf(t, svc).bitmaps[name]
		if got != before || !bytes.Equal(got.Pix, fixture.assetPixels[name]) {
			t.Fatalf("undo changed asset %q", name)
		}
	}
}

func TestFlipCanvasRejectsInvalidAxisWithoutHistory(t *testing.T) {
	svc, tabID := newTestService(t, 20, 10)
	before := mustDocumentSnapshot(t, svc)
	if _, err := svc.FlipCanvas(tabID, "diagonal"); err == nil {
		t.Fatal("invalid canvas flip axis should fail")
	} else if !strings.Contains(err.Error(), "horizontal") {
		t.Fatalf("invalid axis error = %q", err)
	}
	after := mustDocumentSnapshot(t, svc)
	if !reflect.DeepEqual(after.Doc, before.Doc) || after.Rev != before.Rev {
		t.Fatalf("invalid flip changed the document: before=%+v after=%+v", before, after)
	}
	if got := activeSessionOf(t, svc).hist.UndoCount(); got != 0 {
		t.Fatalf("invalid flip created %d history entries", got)
	}
}

func TestFlipCanvasRejectsSwitchedClosedAndBusyTabs(t *testing.T) {
	t.Run("switched", func(t *testing.T) {
		svc, tabID := newTestService(t, 20, 10)
		before := mustDocumentSnapshot(t, svc)
		other, err := svc.ws.newDocumentWithWhiteBackground(12, 8, 72)
		if err != nil {
			t.Fatal(err)
		}
		otherBefore := mustDocumentSnapshot(t, svc)
		if _, err := svc.FlipCanvas(tabID, "horizontal"); err == nil || !strings.Contains(err.Error(), "TRANSFORM_STALE") {
			t.Fatalf("switched-tab flip error = %v", err)
		}
		if got := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(got.Doc, otherBefore.Doc) {
			t.Fatalf("switched-tab rejection changed active tab: %+v", got.Doc)
		}
		if _, err := svc.ws.SelectTab(tabID); err != nil {
			t.Fatal(err)
		}
		if got := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(got.Doc, before.Doc) {
			t.Fatalf("switched-tab rejection changed original tab: %+v", got.Doc)
		}
		_ = other
	})

	t.Run("closed", func(t *testing.T) {
		svc, tabID := newTestService(t, 20, 10)
		if _, err := svc.ws.CloseTab(tabID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.FlipCanvas(tabID, "horizontal"); !errors.Is(err, errNoDocument) {
			t.Fatalf("closed-tab flip error = %v", err)
		}
	})

	t.Run("busy stroke", func(t *testing.T) {
		svc, tabID := newTestService(t, 20, 10)
		before := mustDocumentSnapshot(t, svc)
		if _, err := svc.BeginStroke(1, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.FlipCanvas(tabID, "horizontal"); !errors.Is(err, errDocumentBusy) {
			t.Fatalf("busy-stroke flip error = %v", err)
		}
		if got := mustDocumentSnapshot(t, svc); !reflect.DeepEqual(got.Doc, before.Doc) {
			t.Fatalf("busy-stroke rejection changed document: %+v", got.Doc)
		}
		if _, err := svc.EndStroke(); err != nil {
			t.Fatal(err)
		}
	})
}
